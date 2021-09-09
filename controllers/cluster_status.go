/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"sort"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

// managedResources contains the resources that are created a cluster
// and need to be managed by the controller
type managedResources struct {
	pods corev1.PodList
	pvcs corev1.PersistentVolumeClaimList
	jobs batchv1.JobList
}

// Count the number of jobs that are still running
func (resources managedResources) countRunningJobs() int {
	jobCount := len(resources.jobs.Items)
	completeJobs := utils.CountCompleteJobs(resources.jobs.Items)
	return jobCount - completeJobs
}

// Check if every managed Pod is active and will be schedules
func (resources managedResources) allPodsAreActive() bool {
	for idx := range resources.pods.Items {
		if !utils.IsPodActive(resources.pods.Items[idx]) {
			return false
		}
	}
	return true
}

// Check if at least one Pod is alive (active and not crash-looping)
func (resources managedResources) noPodsAreAlive() bool {
	for idx := range resources.pods.Items {
		if utils.IsPodAlive(resources.pods.Items[idx]) {
			return false
		}
	}
	return true
}

// getManagedResources get the managed resources of various types
func (r *ClusterReconciler) getManagedResources(ctx context.Context,
	cluster apiv1.Cluster) (*managedResources, error) {
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

	return &managedResources{
		pods: childPods,
		pvcs: childPVCs,
		jobs: childJobs,
	}, nil
}

func (r *ClusterReconciler) getManagedPods(
	ctx context.Context,
	cluster apiv1.Cluster,
) (corev1.PodList, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	var childPods corev1.PodList
	if err := r.List(ctx, &childPods,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{podOwnerKey: cluster.Name},
	); err != nil {
		log.Error(err, "Unable to list child pods resource")
		return corev1.PodList{}, err
	}

	sort.Slice(childPods.Items, func(i, j int) bool {
		return childPods.Items[i].Name < childPods.Items[j].Name
	})

	return childPods, nil
}

func (r *ClusterReconciler) getManagedPVCs(
	ctx context.Context,
	cluster apiv1.Cluster,
) (corev1.PersistentVolumeClaimList, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	var childPVCs corev1.PersistentVolumeClaimList
	if err := r.List(ctx, &childPVCs,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{pvcOwnerKey: cluster.Name},
	); err != nil {
		log.Error(err, "Unable to list child PVCs")
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
	cluster apiv1.Cluster,
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
	cluster.Status.InitializingPVC = pvcClassification.Initializing
	cluster.Status.HealthyPVC = pvcClassification.Healthy

	// From now on, we'll consider only Active pods: those Pods
	// that will possibly work. Let's forget about the failed ones
	filteredPods := utils.FilterActivePods(resources.pods.Items)

	// Count pods
	newInstances := int32(len(filteredPods))
	cluster.Status.Instances = newInstances
	cluster.Status.ReadyInstances = int32(utils.CountReadyPods(filteredPods))

	// Count jobs
	newJobs := int32(len(resources.jobs.Items))
	cluster.Status.JobCount = newJobs

	// Instances status
	cluster.Status.InstancesStatus = utils.ListStatusPods(resources.pods.Items)

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
			r.Log.Info("Wrong target primary, the chosen one is not active or not present",
				"targetPrimary", cluster.Status.TargetPrimary,
				"pods", resources.pods)
			cluster.Status.TargetPrimary = cluster.Status.CurrentPrimary
		}
	}

	// set server CA secret,TLS secret and alternative DNS names with default values
	cluster.Status.Certificates.ServerCASecret = cluster.GetServerCASecretName()
	cluster.Status.Certificates.ServerTLSSecret = cluster.GetServerTLSSecretName()
	cluster.Status.Certificates.ClientCASecret = cluster.GetClientCASecretName()
	cluster.Status.Certificates.ReplicationTLSSecret = cluster.GetReplicationSecretName()
	cluster.Status.Certificates.ServerAltDNSNames = cluster.GetClusterAltDNSNames()

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

// SetClusterOwnerAnnotationsAndLabels sets the cluster as owner of the passed object and then
// sets all the needed annotations and labels
func SetClusterOwnerAnnotationsAndLabels(obj *v1.ObjectMeta, cluster *apiv1.Cluster) {
	utils.SetAsOwnedBy(obj, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(obj, versions.Version)
	utils.InheritAnnotations(obj, cluster.Annotations, configuration.Current)
	utils.InheritLabels(obj, cluster.Labels, configuration.Current)
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
	namespace string, certKey string) error {
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

// ExtractInstancesStatus extracts the status of the underlying PostgreSQL instance from
// the requested Pod, via the instance manager. In case of failure, errors are passed
// in the result list
func ExtractInstancesStatus(
	ctx context.Context,
	filteredPods []corev1.Pod,
) postgres.PostgresqlStatusList {
	var result postgres.PostgresqlStatusList

	for idx := range filteredPods {
		instanceStatus := getReplicaStatusFromPodViaHTTP(ctx, filteredPods[idx])
		instanceStatus.IsReady = utils.IsPodReady(filteredPods[idx])
		instanceStatus.Node = filteredPods[idx].Spec.NodeName
		instanceStatus.Pod = filteredPods[idx]
		result.Items = append(result.Items, instanceStatus)
	}
	return result
}

// getReplicaStatusFromPodViaHTTP retrieves the status of PostgreSQL pods via an HTTP request with GET method.
func getReplicaStatusFromPodViaHTTP(ctx context.Context, pod corev1.Pod) postgres.PostgresqlStatus {
	var result postgres.PostgresqlStatus

	statusURL := url.Build(pod.Status.PodIP, url.PathPgStatus)
	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		result.ExecError = err
		return result
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		result.ExecError = err
		return result
	}

	if resp.StatusCode != 200 {
		bytes, _ := ioutil.ReadAll(resp.Body)
		result.ExecError = fmt.Errorf("%v - %v", resp.StatusCode, string(bytes))
		_ = resp.Body.Close()
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		result.ExecError = err
		return result
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		result.ExecError = err
		return result
	}

	err = resp.Body.Close()
	if err != nil {
		result.ExecError = err
		return result
	}

	return result
}
