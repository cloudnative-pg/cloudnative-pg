/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"reflect"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/executablehash"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// StatusRequestRetry is the default backoff used to query the instance manager
// for the status of each PostgreSQL instance.
var StatusRequestRetry = wait.Backoff{
	Steps:    5,
	Duration: 10 * time.Millisecond,
	Factor:   5.0,
	Jitter:   0.1,
}

// managedResources contains the resources that are created a cluster
// and need to be managed by the controller
type managedResources struct {
	// nodes this is a map composed of [nodeName]corev1.Node
	nodes map[string]corev1.Node
	pods  corev1.PodList
	pvcs  corev1.PersistentVolumeClaimList
	jobs  batchv1.JobList
}

// Count the number of jobs that are still running
func (resources *managedResources) countRunningJobs() int {
	jobCount := len(resources.jobs.Items)
	completeJobs := utils.CountCompleteJobs(resources.jobs.Items)
	return jobCount - completeJobs
}

// Check if every managed Pod is active and will be schedules
func (resources *managedResources) allPodsAreActive() bool {
	for idx := range resources.pods.Items {
		if !utils.IsPodActive(resources.pods.Items[idx]) {
			return false
		}
	}
	return true
}

// Check if at least one Pod is alive (active and not crash-looping)
func (resources *managedResources) noPodsAreAlive() bool {
	for idx := range resources.pods.Items {
		if utils.IsPodAlive(resources.pods.Items[idx]) {
			return false
		}
	}
	return true
}

// Retrieve a PVC by name
func (resources *managedResources) getPVC(name string) *corev1.PersistentVolumeClaim {
	for _, pvc := range resources.pvcs.Items {
		if name == pvc.Name {
			return &pvc
		}
	}

	return nil
}

// An InstanceStatusError reports an unsuccessful attempt to retrieve an instance status
type InstanceStatusError struct {
	StatusCode int
	Body       string
}

func (i InstanceStatusError) Error() string {
	return fmt.Sprintf("error status code: %v, body: %v", i.StatusCode, i.Body)
}

// getManagedResources get the managed resources of various types
func (r *ClusterReconciler) getManagedResources(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (*managedResources, error) {
	// Update the status of this resource
	childPods, err := r.getManagedPods(ctx, cluster)
	if err != nil {
		return nil, err
	}

	childPVCs, err := r.getManagedPVCs(ctx, cluster)
	if err != nil {
		return nil, err
	}

	childJobs, err := r.getManagedJobs(ctx, cluster)
	if err != nil {
		return nil, err
	}

	nodes, err := r.getNodes(ctx)
	if err != nil {
		return nil, err
	}

	return &managedResources{
		pods:  childPods,
		pvcs:  childPVCs,
		jobs:  childJobs,
		nodes: nodes,
	}, nil
}

func (r *ClusterReconciler) getNodes(ctx context.Context) (map[string]corev1.Node, error) {
	var nodes corev1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		return nil, err
	}

	data := make(map[string]corev1.Node, len(nodes.Items))
	for _, item := range nodes.Items {
		data[item.Name] = item
	}

	return data, nil
}

func (r *ClusterReconciler) getManagedPods(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (corev1.PodList, error) {
	var childPods corev1.PodList
	if err := r.List(ctx, &childPods,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{podOwnerKey: cluster.Name},
	); err != nil {
		log.FromContext(ctx).Error(err, "Unable to list child pods resource")
		return corev1.PodList{}, err
	}

	sort.Slice(childPods.Items, func(i, j int) bool {
		return childPods.Items[i].Name < childPods.Items[j].Name
	})

	return childPods, nil
}

func (r *ClusterReconciler) getManagedPVCs(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (corev1.PersistentVolumeClaimList, error) {
	var childPVCs corev1.PersistentVolumeClaimList
	if err := r.List(ctx, &childPVCs,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{pvcOwnerKey: cluster.Name},
	); err != nil {
		log.FromContext(ctx).Error(err, "Unable to list child PVCs")
		return corev1.PersistentVolumeClaimList{}, err
	}

	sort.Slice(childPVCs.Items, func(i, j int) bool {
		return childPVCs.Items[i].Name < childPVCs.Items[j].Name
	})

	return childPVCs, nil
}

// getManagedJobs extract the list of jobs which are being created
// by this cluster
func (r *ClusterReconciler) getManagedJobs(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (batchv1.JobList, error) {
	var childJobs batchv1.JobList
	if err := r.List(ctx, &childJobs,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{jobOwnerKey: cluster.Name},
	); err != nil {
		return batchv1.JobList{}, err
	}

	sort.Slice(childJobs.Items, func(i, j int) bool {
		return childJobs.Items[i].Name < childJobs.Items[j].Name
	})

	return childJobs, nil
}

// Set the PvcStatusAnnotation to Ready for a PVC
func (r *ClusterReconciler) setPVCStatusReady(
	ctx context.Context,
	pvc *corev1.PersistentVolumeClaim,
) error {
	contextLogger := log.FromContext(ctx)

	if pvc.Annotations[specs.PVCStatusAnnotationName] == specs.PVCStatusReady {
		return nil
	}

	contextLogger.Debug("Marking PVC as ready", "pvcName", pvc.Name)

	oldPvc := pvc.DeepCopy()

	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string, 1)
	}
	pvc.Annotations[specs.PVCStatusAnnotationName] = specs.PVCStatusReady

	return r.Patch(ctx, pvc, client.MergeFrom(oldPvc))
}

func (r *ClusterReconciler) updateResourceStatus(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) error {
	// Retrieve the cluster key

	existingClusterStatus := cluster.Status

	newPVCCount := int32(len(resources.pvcs.Items))
	cluster.Status.PVCCount = newPVCCount
	pvcClassification := specs.DetectPVCs(resources.pods.Items, resources.jobs.Items, resources.pvcs.Items)
	cluster.Status.DanglingPVC = pvcClassification.Dangling
	cluster.Status.HealthyPVC = pvcClassification.Healthy
	cluster.Status.InitializingPVC = pvcClassification.Initializing
	cluster.Status.ResizingPVC = pvcClassification.Resizing

	// From now on, we'll consider only Active pods: those Pods
	// that will possibly work. Let's forget about the failed ones
	filteredPods := utils.FilterActivePods(resources.pods.Items)

	// Count pods
	newInstances := len(filteredPods)
	cluster.Status.Instances = newInstances
	cluster.Status.ReadyInstances = utils.CountReadyPods(filteredPods)

	// Count jobs
	newJobs := int32(len(resources.jobs.Items))
	cluster.Status.JobCount = newJobs

	// Instances status
	cluster.Status.InstancesStatus = utils.ListStatusPods(resources.pods.Items)

	cluster.Status.Topology = getPodsTopology(
		ctx,
		resources.pods.Items,
		resources.nodes,
		cluster.Spec.PostgresConfiguration.SyncReplicaElectionConstraint,
	)

	// Services
	cluster.Status.WriteService = cluster.GetServiceReadWriteName()
	cluster.Status.ReadService = cluster.GetServiceReadName()

	// If we are switching, check if the target primary is still active
	// Ignore this check if current primary is empty (it happens during the bootstrap)
	if cluster.Status.TargetPrimary != cluster.Status.CurrentPrimary &&
		cluster.Status.CurrentPrimary != "" {
		found := false
		if cluster.Status.ReadyInstances > 0 {
			for _, pod := range utils.FilterActivePods(resources.pods.Items) {
				// If the target primary is not active, it will never be promoted
				// since is will not be scheduled anymore
				if pod.Name == cluster.Status.TargetPrimary {
					found = true
					break
				}
			}
		}

		if !found {
			// Reset the target primary, since the available one is not active
			// or not present
			log.FromContext(ctx).Info("Wrong target primary, the chosen one is not active or not present",
				"targetPrimary", cluster.Status.TargetPrimary,
				"pods", resources.pods)
			cluster.Status.TargetPrimary = cluster.Status.CurrentPrimary
			cluster.Status.TargetPrimaryTimestamp = utils.GetCurrentTimestamp()
		}
	}

	// set server CA secret,TLS secret and alternative DNS names with default values
	cluster.Status.Certificates.ServerCASecret = cluster.GetServerCASecretName()
	cluster.Status.Certificates.ServerTLSSecret = cluster.GetServerTLSSecretName()
	cluster.Status.Certificates.ClientCASecret = cluster.GetClientCASecretName()
	cluster.Status.Certificates.ReplicationTLSSecret = cluster.GetReplicationSecretName()
	cluster.Status.Certificates.ServerAltDNSNames = cluster.GetClusterAltDNSNames()

	// Set the version of the operator inside the status. This will allow us
	// to discover the exact version of the operator which worked the last time
	// on this cluster
	cluster.Status.CommitHash = versions.Info.Commit

	if poolerIntegrations, err := r.getPoolerIntegrationsNeeded(ctx, cluster); err == nil {
		cluster.Status.PoolerIntegrations = poolerIntegrations
	} else {
		log.Error(err, "while checking pooler integrations were needed, ignored")
	}

	// Set the current hash code of the operator binary inside the status.
	// This is used by the instance manager to validate if a certain binary is
	// valid or not
	var err error
	cluster.Status.OperatorHash, err = executablehash.Get()
	if err != nil {
		return err
	}

	// refresh expiration dates of certifications
	if err := r.refreshCertsExpirations(ctx, cluster); err != nil {
		return err
	}

	if err := r.refreshSecretResourceVersions(ctx, cluster); err != nil {
		return err
	}

	if err := r.refreshConfigMapResourceVersions(ctx, cluster); err != nil {
		return err
	}

	if !reflect.DeepEqual(existingClusterStatus, cluster.Status) {
		return r.Status().Update(ctx, cluster)
	}
	return nil
}

// removeConditionsWithInvalidReason will remove every condition which has a not valid
// reason from the K8s API point-of-view
func (r *ClusterReconciler) removeConditionsWithInvalidReason(ctx context.Context, cluster *apiv1.Cluster) error {
	// Nothing to do if cluster has no conditions
	if len(cluster.Status.Conditions) == 0 {
		return nil
	}

	contextLogger := log.FromContext(ctx)
	conditions := make([]metav1.Condition, 0, len(cluster.Status.Conditions))
	for _, entry := range cluster.Status.Conditions {
		if utils.IsConditionReasonValid(entry.Reason) {
			conditions = append(conditions, entry)
		}
	}

	if !reflect.DeepEqual(cluster.Status.Conditions, conditions) {
		contextLogger.Info("Updating Cluster to remove conditions with invalid reason")
		cluster.Status.Conditions = conditions
		if err := r.Status().Update(ctx, cluster); err != nil {
			return err
		}

		// Restart the reconciliation loop as the status is changed
		return ErrNextLoop
	}

	return nil
}

// updateOnlineUpdateEnabled updates the `OnlineUpdateEnabled` value in the cluster status
func (r *ClusterReconciler) updateOnlineUpdateEnabled(
	ctx context.Context, cluster *apiv1.Cluster, onlineUpdateEnabled bool,
) error {
	// do nothing if onlineUpdateEnabled have not changed
	if cluster.Status.OnlineUpdateEnabled == onlineUpdateEnabled {
		return nil
	}

	cluster.Status.OnlineUpdateEnabled = onlineUpdateEnabled
	return r.Status().Update(ctx, cluster)
}

// SetClusterOwnerAnnotationsAndLabels sets the cluster as owner of the passed object and then
// sets all the needed annotations and labels
func SetClusterOwnerAnnotationsAndLabels(obj *metav1.ObjectMeta, cluster *apiv1.Cluster) {
	utils.InheritAnnotations(obj, cluster.Annotations, cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(obj, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)
	utils.LabelClusterName(obj, cluster.GetName())
	utils.SetAsOwnedBy(obj, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(obj, versions.Version)
}

// getPoolerIntegrationsNeeded returns a struct with all the pooler integrations needed
func (r *ClusterReconciler) getPoolerIntegrationsNeeded(ctx context.Context,
	cluster *apiv1.Cluster,
) (*apiv1.PoolerIntegrations, error) {
	var poolers apiv1.PoolerList

	err := r.List(ctx, &poolers,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{poolerClusterKey: cluster.Name})
	if err != nil {
		return nil, fmt.Errorf("while getting poolers for cluster %s: %w", cluster.Name, err)
	}

	pgbouncerPoolerIntegrations, err := r.getPgbouncerIntegrationStatus(ctx, cluster, poolers)

	for _, pooler := range poolers.Items {
		if !slices.Contains(pgbouncerPoolerIntegrations.Secrets, pooler.Name) {
			log.Info("pooler not automatically configured, manual configuration required",
				"cluster", pooler.Spec.Cluster.Name, "pooler", pooler.Name)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("while getting integration status for pgbouncer poolers in cluster %s: %w",
			cluster.Name, err)
	}

	return &apiv1.PoolerIntegrations{
		PgBouncerIntegration: pgbouncerPoolerIntegrations,
	}, nil
}

// getPgbouncerIntegrationStatus gets the status of the pgbouncer integration
func (r *ClusterReconciler) getPgbouncerIntegrationStatus(
	ctx context.Context, cluster *apiv1.Cluster, poolers apiv1.PoolerList,
) (apiv1.PgBouncerIntegrationStatus, error) {
	poolersIntegrations := apiv1.PgBouncerIntegrationStatus{}
	for _, pooler := range poolers.Items {
		// We are dealing with pgbouncer integration
		if pooler.Spec.PgBouncer == nil {
			continue
		}

		// The integrated poolers are the ones whose permissions are directly
		// managed by the instance manager.
		//
		// For this to be done the user needs to avoid setting an authQuery
		// and an authQuerySecret manually on the pooler: this would mean
		// that the user intend to manually manage them.
		//
		// If this happens, we declare the pooler automatically integrated
		// in the following two cases:
		//
		// 1. the secret still doesn't exist (we will create it in the
		//    operator reconciliation loop)
		// 2. the secret exists and has been created by the operator
		//    (owned by the Cluster)

		// We skip secrets which were directly setup by the user with
		// the authQuery and authQuerySecret parameters inside the
		// pooler
		if pooler.Spec.PgBouncer.AuthQuery != "" {
			continue
		}

		if pooler.Spec.PgBouncer.AuthQuerySecret != nil && pooler.Spec.PgBouncer.AuthQuerySecret.Name != "" {
			continue
		}

		secretName := pooler.GetAuthQuerySecretName()
		// there is no need to examine further, the potential secret we may add is already present.
		// This saves us:
		// - further API calls to the kube-api server,
		// - redundant iterations of the secrets passed
		if slices.Contains(poolersIntegrations.Secrets, secretName) {
			continue
		}

		// Check the secret existence and ownership
		authQuerySecret := corev1.Secret{}
		err := r.Get(
			ctx,
			client.ObjectKey{Namespace: cluster.Namespace, Name: pooler.GetAuthQuerySecretName()},
			&authQuerySecret,
		)
		if apierrs.IsNotFound(err) {
			poolersIntegrations.Secrets = append(poolersIntegrations.Secrets, secretName)
			continue
		}

		if err != nil {
			return apiv1.PgBouncerIntegrationStatus{}, fmt.Errorf("while getting secret for pooler integration")
		}
		if owner, ok := isOwnedByCluster(&authQuerySecret); ok && owner == cluster.Name {
			poolersIntegrations.Secrets = append(poolersIntegrations.Secrets, secretName)
			continue
		}
	}

	return poolersIntegrations, nil
}

// refreshCertExpiration check the expiration date of all the certificates used by the cluster
func (r *ClusterReconciler) refreshCertsExpirations(ctx context.Context, cluster *apiv1.Cluster) error {
	namespace := cluster.GetNamespace()

	cluster.Status.Certificates.Expirations = make(map[string]string, 4)
	certificates := cluster.Status.Certificates

	err := r.setCertExpiration(ctx, cluster, certificates.ServerCASecret, namespace, certs.CACertKey)
	if err != nil {
		return err
	}

	err = r.setCertExpiration(ctx, cluster, certificates.ServerTLSSecret, namespace, certs.TLSCertKey)
	if err != nil {
		return err
	}

	err = r.setCertExpiration(ctx, cluster, certificates.ClientCASecret, namespace, certs.CACertKey)
	if err != nil {
		return err
	}

	err = r.setCertExpiration(ctx, cluster, certificates.ReplicationTLSSecret, namespace, certs.TLSCertKey)
	if err != nil {
		return err
	}

	return nil
}

// setCertExpiration check the expiration date of a certificates used by the cluster
func (r *ClusterReconciler) setCertExpiration(ctx context.Context, cluster *apiv1.Cluster, secretName string,
	namespace string, certKey string,
) error {
	var secret corev1.Secret
	err := r.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      secretName,
	}, &secret)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return err
	}
	cert, ok := secret.Data[certKey]

	if !ok {
		return err
	}

	keyPair := certs.KeyPair{Certificate: cert}
	_, expDate, err := keyPair.IsExpiring()
	if err != nil {
		return err
	}

	cluster.Status.Certificates.Expirations[secretName] = expDate.String()

	return nil
}

// refreshConfigMapResourceVersions set the resource version of the secrets
func (r *ClusterReconciler) refreshConfigMapResourceVersions(ctx context.Context, cluster *apiv1.Cluster) error {
	versions := apiv1.ConfigMapResourceVersion{}
	if cluster.Spec.Monitoring != nil {
		versions.Metrics = make(map[string]string)
		for _, config := range cluster.Spec.Monitoring.CustomQueriesConfigMap {
			version, err := r.getConfigMapResourceVersion(ctx, cluster, config.Name)
			if err != nil {
				return err
			}
			versions.Metrics[config.Name] = version
		}
	}

	cluster.Status.ConfigMapResourceVersion = versions

	return nil
}

// refreshSecretResourceVersions set the resource version of the secrets
func (r *ClusterReconciler) refreshSecretResourceVersions(ctx context.Context, cluster *apiv1.Cluster) error {
	versions := apiv1.SecretsResourceVersion{}
	var version string
	var err error

	if cluster.GetEnableSuperuserAccess() {
		version, err = r.getSecretResourceVersion(ctx, cluster, cluster.GetSuperuserSecretName())
		if err != nil {
			return err
		}
		versions.SuperuserSecretVersion = version
	}

	version, err = r.getSecretResourceVersion(ctx, cluster, cluster.GetApplicationSecretName())
	if err != nil {
		return err
	}
	versions.ApplicationSecretVersion = version

	certificates := cluster.Status.Certificates

	// Reset the content of the unused CASecretVersion field
	cluster.Status.SecretsResourceVersion.CASecretVersion = ""

	version, err = r.getSecretResourceVersion(ctx, cluster, certificates.ClientCASecret)
	if err != nil {
		return err
	}
	versions.ClientCASecretVersion = version

	version, err = r.getSecretResourceVersion(ctx, cluster, certificates.ReplicationTLSSecret)
	if err != nil {
		return err
	}
	versions.ReplicationSecretVersion = version

	version, err = r.getSecretResourceVersion(ctx, cluster, certificates.ServerCASecret)
	if err != nil {
		return err
	}
	versions.ServerCASecretVersion = version

	version, err = r.getSecretResourceVersion(ctx, cluster, certificates.ServerTLSSecret)
	if err != nil {
		return err
	}
	versions.ServerSecretVersion = version

	if cluster.Spec.Backup.IsBarmanEndpointCASet() {
		version, err = r.getSecretResourceVersion(ctx, cluster,
			cluster.Spec.Backup.BarmanObjectStore.EndpointCA.Name)
		if err != nil {
			return err
		}
		versions.BarmanEndpointCA = version
	}

	if cluster.Spec.Monitoring != nil {
		versions.Metrics = make(map[string]string)
		for _, secret := range cluster.Spec.Monitoring.CustomQueriesSecret {
			version, err = r.getSecretResourceVersion(ctx, cluster, secret.Name)
			if err != nil {
				return err
			}
			versions.Metrics[secret.Name] = version
		}
	}

	cluster.Status.SecretsResourceVersion = versions

	return nil
}

// getSecretResourceVersion retrieves the resource version of a secret
func (r *ClusterReconciler) getSecretResourceVersion(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name string,
) (string, error) {
	return r.getObjectResourceVersion(ctx, cluster, name, &corev1.Secret{})
}

// getSecretResourceVersion retrieves the resource version of a configmap
func (r *ClusterReconciler) getConfigMapResourceVersion(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name string,
) (string, error) {
	return r.getObjectResourceVersion(ctx, cluster, name, &corev1.ConfigMap{})
}

// getObjectResourceVersion retrieves the resource version of an object
func (r *ClusterReconciler) getObjectResourceVersion(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name string,
	object client.Object,
) (string, error) {
	err := r.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: name},
		object)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return object.GetResourceVersion(), nil
}

func (r *ClusterReconciler) setPrimaryInstance(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podName string,
) error {
	cluster.Status.TargetPrimary = podName
	cluster.Status.TargetPrimaryTimestamp = utils.GetCurrentTimestamp()
	return r.Status().Update(ctx, cluster)
}

// RegisterPhase update phase in the status cluster with the
// proper reason
func (r *ClusterReconciler) RegisterPhase(ctx context.Context,
	cluster *apiv1.Cluster,
	phase string,
	reason string,
) error {
	existingClusterStatus := cluster.Status

	cluster.Status.Phase = phase
	cluster.Status.PhaseReason = reason

	if !reflect.DeepEqual(existingClusterStatus, cluster.Status) {
		if err := r.Status().Update(ctx, cluster); err != nil {
			return err
		}
	}

	return nil
}

// updateClusterStatusThatRequiresInstancesState updates all the cluster status fields that require the instances status
func (r *ClusterReconciler) updateClusterStatusThatRequiresInstancesState(
	ctx context.Context,
	cluster *apiv1.Cluster,
	statuses postgres.PostgresqlStatusList,
) error {
	existingClusterStatus := cluster.Status
	cluster.Status.InstancesReportedState = make(map[apiv1.PodName]apiv1.InstanceReportedState, len(statuses.Items))

	// we extract the instances reported state
	for _, item := range statuses.Items {
		cluster.Status.InstancesReportedState[apiv1.PodName(item.Pod.Name)] = apiv1.InstanceReportedState{
			IsPrimary:  item.IsPrimary,
			TimeLineID: item.TimeLineID,
		}
	}

	// we update any relevant cluster status that depends on the primary instance
	for _, item := range statuses.Items {
		// we refresh the last known timeline on the status root.
		// This avoids to have a zero timeline id in case that no primary instance is up during reconciliation.
		if item.IsPrimary && item.TimeLineID != 0 {
			cluster.Status.TimelineID = item.TimeLineID
		}
	}

	if !reflect.DeepEqual(existingClusterStatus, cluster.Status) {
		return r.Status().Update(ctx, cluster)
	}
	return nil
}

// extractInstancesStatus extracts the status of the underlying PostgreSQL instance from
// the requested Pod, via the instance manager. In case of failure, errors are passed
// in the result list
func (r *ClusterReconciler) extractInstancesStatus(
	ctx context.Context,
	activePods []corev1.Pod,
) postgres.PostgresqlStatusList {
	var result postgres.PostgresqlStatusList

	for idx := range activePods {
		instanceStatus := r.getReplicaStatusFromPodViaHTTP(ctx, activePods[idx])

		// IsReady is not populated by the instance manager, so we detect it from the
		// Pod status
		instanceStatus.IsReady = utils.IsPodReady(activePods[idx])
		instanceStatus.Node = activePods[idx].Spec.NodeName
		instanceStatus.Pod = activePods[idx]

		result.Items = append(result.Items, instanceStatus)
	}
	return result
}

// getReplicaStatusFromPodViaHTTP retrieves the status of PostgreSQL pod via HTTP, retrying
// the request if some communication error is encountered
func (r *ClusterReconciler) getReplicaStatusFromPodViaHTTP(
	ctx context.Context,
	pod corev1.Pod,
) (result postgres.PostgresqlStatus) {
	isErrorRetryable := func(err error) bool {
		contextLog := log.FromContext(ctx)

		// If it's a timeout, we do not want to retry
		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			return false
		}

		// If the pod answered with a not ok status, it is pointless to retry
		var instanceStatusError InstanceStatusError
		if errors.As(err, &instanceStatusError) {
			return false
		}

		contextLog.Debug("Error while requesting the status of an instance, retrying",
			"pod", pod.Name,
			"error", err)
		return true
	}

	// The retry here is to support restarting the instance manager during
	// online upgrades. It is not intended to wait for recovering from any
	// other remote failure.
	_ = retry.OnError(StatusRequestRetry, isErrorRetryable, func() error {
		result = rawInstanceStatusRequest(ctx, r.timeoutHTTPClient, pod)
		return result.Error
	})

	return result
}

// rawInstanceStatusRequest retrieves the status of PostgreSQL pods via an HTTP request with GET method.
func rawInstanceStatusRequest(
	ctx context.Context,
	client *http.Client,
	pod corev1.Pod,
) (result postgres.PostgresqlStatus) {
	statusURL := url.Build(pod.Status.PodIP, url.PathPgStatus, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		result.Error = err
		return result
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Error = err
		return result
	}

	defer func() {
		err = resp.Body.Close()
		if err != nil && result.Error == nil {
			result.Error = err
		}
	}()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		result.Error = err
		return result
	}

	if resp.StatusCode != 200 {
		result.Error = &InstanceStatusError{StatusCode: resp.StatusCode, Body: string(body)}
		return result
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		result.Error = err
		return result
	}

	return result
}

// getPodsTopology returns a map with all the information about the pods topology
func getPodsTopology(
	ctx context.Context,
	pods []corev1.Pod,
	nodes map[string]corev1.Node,
	topology apiv1.SyncReplicaElectionConstraints,
) apiv1.Topology {
	contextLogger := log.FromContext(ctx)
	data := make(map[apiv1.PodName]apiv1.PodTopologyLabels)
	for _, pod := range pods {
		podName := apiv1.PodName(pod.Name)
		data[podName] = make(map[string]string, 0)
		node, ok := nodes[pod.Spec.NodeName]
		if !ok {
			// node not found, it means that:
			// - the node could have been drained
			// - others
			contextLogger.Debug("node not found, skipping pod topology matching")
			return apiv1.Topology{}
		}
		for _, labelName := range topology.NodeLabelsAntiAffinity {
			data[podName][labelName] = node.Labels[labelName]
		}
	}

	return apiv1.Topology{SuccessfullyExtracted: true, Instances: data}
}
