/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sethvargo/go-password/password"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// createPostgresClusterObjects ensures that we have the required global objects
func (r *ClusterReconciler) createPostgresClusterObjects(ctx context.Context, cluster *apiv1.Cluster) error {
	err := r.setupPostgresPKI(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.reconcilePostgresSecrets(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.reconcilePostgresServices(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.reconcilePodDisruptionBudget(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.createOrPatchServiceAccount(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.createOrPatchRole(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.createRoleBinding(ctx, cluster)
	if err != nil {
		return err
	}

	if !cluster.Spec.Monitoring.AreDefaultQueriesDisabled() {
		err = r.createOrPatchDefaultMetrics(ctx, cluster)
		if err != nil {
			return nil
		}
	}

	err = createOrPatchPodMonitor(ctx, r.Client, r.DiscoveryClient, specs.NewClusterPodMonitorManager(cluster))
	if err != nil {
		return err
	}

	err = r.reconcileFailoverQuorumObject(ctx, cluster)
	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterReconciler) reconcilePodDisruptionBudget(ctx context.Context, cluster *apiv1.Cluster) error {
	if !cluster.GetEnablePDB() {
		return r.deletePodDisruptionBudgetsIfExist(ctx, cluster)
	}

	primaryPDB := specs.BuildPrimaryPodDisruptionBudget(cluster)
	replicaPDB := specs.BuildReplicasPodDisruptionBudget(cluster)

	if cluster.IsNodeMaintenanceWindowInProgress() && cluster.IsReusePVCEnabled() {
		// The replica PDB should not be enforced if we are inside a maintenance
		// window, and we chose to avoid allocating more storage space.
		replicaPDB = nil

		// If this a single-instance cluster, we need to delete
		// the PodDisruptionBudget for the primary node too
		// otherwise the user won't be able to drain the workloads
		// from the underlying node.
		if cluster.Spec.Instances == 1 {
			primaryPDB = nil
		}
	}

	if err := r.handlePDB(ctx, cluster, primaryPDB, r.deletePrimaryPodDisruptionBudgetIfExists); err != nil {
		return err
	}

	return r.handlePDB(ctx, cluster, replicaPDB, r.deleteReplicasPodDisruptionBudgetIfExists)
}

func (r *ClusterReconciler) handlePDB(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pdb *policyv1.PodDisruptionBudget,
	deleteFunc func(context.Context, *apiv1.Cluster) error,
) error {
	if pdb != nil {
		return r.createOrPatchOwnedPodDisruptionBudget(ctx, cluster, pdb)
	}
	return deleteFunc(ctx, cluster)
}

func (r *ClusterReconciler) reconcilePostgresSecrets(ctx context.Context, cluster *apiv1.Cluster) error {
	err := r.reconcileSuperuserSecret(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.reconcileAppUserSecret(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.reconcilePoolerSecrets(ctx, cluster)
	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterReconciler) reconcileSuperuserSecret(ctx context.Context, cluster *apiv1.Cluster) error {
	// We need to create a secret for the 'postgres' user when superuser
	// access is enabled and the user haven't specified his own
	if cluster.GetEnableSuperuserAccess() &&
		(cluster.Spec.SuperuserSecret == nil || cluster.Spec.SuperuserSecret.Name == "") {
		postgresPassword, err := password.Generate(64, 10, 0, false, true)
		if err != nil {
			return err
		}
		postgresSecret := specs.CreateSecret(
			cluster.GetSuperuserSecretName(),
			cluster.Namespace,
			cluster.GetServiceReadWriteName(),
			"*",
			"postgres",
			postgresPassword,
			utils.UserTypeSuperuser)
		cluster.SetInheritedDataAndOwnership(&postgresSecret.ObjectMeta)

		return createOrPatchClusterCredentialSecret(ctx, r.Client, postgresSecret)
	}

	// If we don't have Superuser enabled we make sure the automatically generated secret doesn't exist
	if !cluster.GetEnableSuperuserAccess() {
		var secret corev1.Secret
		err := r.Get(
			ctx,
			client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.GetSuperuserSecretName()},
			&secret)
		if err != nil {
			if apierrs.IsNotFound(err) || apierrs.IsForbidden(err) {
				return nil
			}
			return err
		}

		if _, owned := IsOwnedByCluster(&secret); owned {
			return r.Delete(ctx, &secret)
		}
	}

	return nil
}

func (r *ClusterReconciler) reconcileAppUserSecret(ctx context.Context, cluster *apiv1.Cluster) error {
	if cluster.ShouldCreateApplicationSecret() {
		appPassword, err := password.Generate(64, 10, 0, false, true)
		if err != nil {
			return err
		}
		appSecret := specs.CreateSecret(
			cluster.GetApplicationSecretName(),
			cluster.Namespace,
			cluster.GetServiceReadWriteName(),
			cluster.GetApplicationDatabaseName(),
			cluster.GetApplicationDatabaseOwner(),
			appPassword,
			utils.UserTypeApp)

		cluster.SetInheritedDataAndOwnership(&appSecret.ObjectMeta)
		return createOrPatchClusterCredentialSecret(ctx, r.Client, appSecret)
	}
	return nil
}

func createOrPatchClusterCredentialSecret(
	ctx context.Context,
	cli client.Client,
	proposed *corev1.Secret,
) error {
	var currentSecret corev1.Secret
	if err := cli.Get(
		ctx,
		client.ObjectKey{Namespace: proposed.Namespace, Name: proposed.Name},
		&currentSecret); apierrs.IsNotFound(err) {
		return cli.Create(ctx, proposed)
	} else if err != nil {
		return err
	}

	// we can patch only secrets that are owned by us
	if _, owned := IsOwnedByCluster(&currentSecret); !owned {
		return nil
	}

	patchedSecret := currentSecret.DeepCopy()
	utils.MergeObjectsMetadata(patchedSecret, proposed)

	// we cannot compare the data due to the password being randomly generated everytime
	if reflect.DeepEqual(patchedSecret.Labels, currentSecret.Labels) &&
		reflect.DeepEqual(patchedSecret.Annotations, currentSecret.Annotations) {
		return nil
	}

	return cli.Patch(ctx, patchedSecret, client.MergeFrom(&currentSecret))
}

func (r *ClusterReconciler) reconcilePoolerSecrets(ctx context.Context, cluster *apiv1.Cluster) error {
	if cluster.Status.PoolerIntegrations == nil {
		return nil
	}

	if len(cluster.Status.PoolerIntegrations.PgBouncerIntegration.Secrets) > 0 {
		var clientCaSecret corev1.Secret

		err := r.Get(ctx, client.ObjectKey{Namespace: cluster.GetNamespace(), Name: cluster.GetClientCASecretName()},
			&clientCaSecret)
		if err != nil {
			return err
		}

		for _, secretName := range cluster.Status.PoolerIntegrations.PgBouncerIntegration.Secrets {
			replicationSecretName := client.ObjectKey{
				Namespace: cluster.GetNamespace(),
				Name:      secretName,
			}
			err = r.ensureLeafCertificate(
				ctx,
				cluster,
				replicationSecretName,
				apiv1.PGBouncerPoolerUserName,
				&clientCaSecret,
				certs.CertTypeClient,
				nil,
				map[string]string{utils.WatchedLabelName: "true"})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ClusterReconciler) reconcilePostgresServices(ctx context.Context, cluster *apiv1.Cluster) error {
	anyService := specs.CreateClusterAnyService(*cluster)
	cluster.SetInheritedDataAndOwnership(&anyService.ObjectMeta)

	if err := r.serviceReconciler(ctx, cluster, anyService, configuration.Current.CreateAnyService); err != nil {
		return err
	}

	readService := specs.CreateClusterReadService(*cluster)
	cluster.SetInheritedDataAndOwnership(&readService.ObjectMeta)

	if err := r.serviceReconciler(ctx, cluster, readService, cluster.IsReadServiceEnabled()); err != nil {
		return err
	}

	readOnlyService := specs.CreateClusterReadOnlyService(*cluster)
	cluster.SetInheritedDataAndOwnership(&readOnlyService.ObjectMeta)

	if err := r.serviceReconciler(ctx, cluster, readOnlyService, cluster.IsReadOnlyServiceEnabled()); err != nil {
		return err
	}

	readWriteService := specs.CreateClusterReadWriteService(*cluster)
	cluster.SetInheritedDataAndOwnership(&readWriteService.ObjectMeta)

	if err := r.serviceReconciler(ctx, cluster, readWriteService, cluster.IsReadWriteServiceEnabled()); err != nil {
		return err
	}

	return r.reconcileManagedServices(ctx, cluster)
}

func (r *ClusterReconciler) reconcileManagedServices(ctx context.Context, cluster *apiv1.Cluster) error {
	managedServices, err := specs.BuildManagedServices(*cluster)
	if err != nil {
		return err
	}
	for idx := range managedServices {
		if err := r.serviceReconciler(ctx, cluster, &managedServices[idx], true); err != nil {
			return err
		}
	}

	// we delete the old managed services not appearing anymore in the spec
	var livingServices corev1.ServiceList
	if err := r.List(ctx, &livingServices, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		utils.IsManagedLabelName: "true",
		utils.ClusterLabelName:   cluster.Name,
	}); err != nil {
		return err
	}

	containService := func(expected corev1.Service) func(iterated corev1.Service) bool {
		return func(iterated corev1.Service) bool {
			return iterated.Name == expected.Name
		}
	}

	for idx := range livingServices.Items {
		livingService := livingServices.Items[idx]
		isEnabled := slices.ContainsFunc(managedServices, containService(livingService))
		if isEnabled {
			continue
		}

		// Ensure the service is not present
		if err := r.serviceReconciler(ctx, cluster, &livingService, false); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClusterReconciler) serviceReconciler(
	ctx context.Context,
	cluster *apiv1.Cluster,
	proposed *corev1.Service,
	enabled bool,
) error {
	strategy := apiv1.ServiceUpdateStrategyPatch
	annotationStrategy := apiv1.ServiceUpdateStrategy(proposed.Annotations[utils.UpdateStrategyAnnotation])
	if annotationStrategy == apiv1.ServiceUpdateStrategyReplace {
		strategy = apiv1.ServiceUpdateStrategyReplace
	}

	contextLogger := log.FromContext(ctx).WithValues(
		"serviceName", proposed.Name,
		"updateStrategy", strategy,
	)

	var livingService corev1.Service
	err := r.Get(ctx, types.NamespacedName{Name: proposed.Name, Namespace: proposed.Namespace}, &livingService)
	if apierrs.IsNotFound(err) {
		if !enabled {
			return nil
		}
		contextLogger.Info("creating service")
		return r.Create(ctx, proposed)
	}
	if err != nil {
		return err
	}

	if owner, _ := IsOwnedByCluster(&livingService); owner != cluster.Name {
		return fmt.Errorf("refusing to reconcile service: %s, not owned by the cluster", livingService.Name)
	}

	if !livingService.DeletionTimestamp.IsZero() {
		contextLogger.Info("waiting for service to be deleted")
		return ErrNextLoop
	}

	if !enabled {
		contextLogger.Info("deleting service, due to not being managed anymore")
		return r.Delete(ctx, &livingService)
	}
	var shouldUpdate bool

	// we ensure that the selector perfectly match
	if !reflect.DeepEqual(proposed.Spec.Selector, livingService.Spec.Selector) {
		livingService.Spec.Selector = proposed.Spec.Selector
		shouldUpdate = true
	}

	// we ensure we've some space to store the labels and the annotations
	if livingService.Labels == nil {
		livingService.Labels = make(map[string]string)
	}
	if livingService.Annotations == nil {
		livingService.Annotations = make(map[string]string)
	}

	// we preserve existing labels/annotation that could be added by third parties
	if !utils.IsMapSubset(livingService.Labels, proposed.Labels) {
		maps.Copy(livingService.Labels, proposed.Labels)
		shouldUpdate = true
	}

	if !utils.IsMapSubset(livingService.Annotations, proposed.Annotations) {
		maps.Copy(livingService.Annotations, proposed.Annotations)
		shouldUpdate = true
	}

	if !shouldUpdate {
		return nil
	}

	if strategy == apiv1.ServiceUpdateStrategyPatch {
		contextLogger.Info("reconciling service")
		// we update to ensure that we substitute the selectors
		return r.Update(ctx, &livingService)
	}

	contextLogger.Info("deleting the service")
	if err := r.Delete(ctx, &livingService); err != nil {
		return err
	}

	return ErrNextLoop
}

// createOrPatchOwnedPodDisruptionBudget ensures that we have a PDB requiring to remove one node at a time
func (r *ClusterReconciler) createOrPatchOwnedPodDisruptionBudget(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pdb *policyv1.PodDisruptionBudget,
) error {
	if pdb == nil {
		return nil
	}

	var oldPdb policyv1.PodDisruptionBudget

	if err := r.Get(ctx, client.ObjectKey{Name: pdb.Name, Namespace: pdb.Namespace}, &oldPdb); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting PodDisruptionBudget: %w", err)
		}

		r.Recorder.Event(cluster, "Normal", "CreatingPodDisruptionBudget",
			fmt.Sprintf("Creating PodDisruptionBudget %s", pdb.Name))
		if err = r.Create(ctx, pdb); err != nil {
			return fmt.Errorf("while creating PodDisruptionBudget: %w", err)
		}
		return nil
	}

	patchedPdb := oldPdb.DeepCopy()
	patchedPdb.Spec = pdb.Spec
	utils.MergeObjectsMetadata(patchedPdb, pdb)

	if reflect.DeepEqual(patchedPdb.Spec, oldPdb.Spec) && reflect.DeepEqual(patchedPdb.ObjectMeta, oldPdb.ObjectMeta) {
		// Everything fine, the two pdbs are the same for us
		return nil
	}

	r.Recorder.Event(cluster, "Normal", "UpdatingPodDisruptionBudget",
		fmt.Sprintf("Updating PodDisruptionBudget %s", pdb.Name))

	if err := r.Patch(ctx, patchedPdb, client.MergeFrom(&oldPdb)); err != nil {
		return fmt.Errorf("while patching PodDisruptionBudget: %w", err)
	}

	return nil
}

func (r *ClusterReconciler) deletePodDisruptionBudgetsIfExist(ctx context.Context, cluster *apiv1.Cluster) error {
	if err := r.deletePrimaryPodDisruptionBudgetIfExists(ctx, cluster); err != nil {
		return err
	}

	return r.deleteReplicasPodDisruptionBudgetIfExists(ctx, cluster)
}

func (r *ClusterReconciler) deletePrimaryPodDisruptionBudgetIfExists(
	ctx context.Context,
	cluster *apiv1.Cluster,
) error {
	return r.deletePodDisruptionBudgetIfExists(
		ctx,
		cluster,
		client.ObjectKey{Name: cluster.Name + apiv1.PrimaryPodDisruptionBudgetSuffix, Namespace: cluster.Namespace})
}

func (r *ClusterReconciler) deleteReplicasPodDisruptionBudgetIfExists(
	ctx context.Context,
	cluster *apiv1.Cluster,
) error {
	return r.deletePodDisruptionBudgetIfExists(
		ctx,
		cluster,
		client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace},
	)
}

func (r *ClusterReconciler) deletePodDisruptionBudgetIfExists(
	ctx context.Context,
	cluster *apiv1.Cluster,
	key types.NamespacedName,
) error {
	// If we have a PDB, we need to delete it
	var targetPdb policyv1.PodDisruptionBudget
	err := r.Get(ctx, key, &targetPdb)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("unable to retrieve PodDisruptionBudget: %w", err)
		}
		return nil
	}

	r.Recorder.Event(cluster,
		"Normal",
		"DeletingPodDisruptionBudget",
		"Deleting Pod Disruption Budget "+key.Name)

	err = r.Delete(ctx, &targetPdb)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("unable to delete PodDisruptionBudget: %w", err)
		}
		return nil
	}
	return nil
}

// createOrPatchServiceAccount creates or synchronizes the ServiceAccount used by the
// cluster with the latest cluster specification
func (r *ClusterReconciler) createOrPatchServiceAccount(ctx context.Context, cluster *apiv1.Cluster) error {
	var sa corev1.ServiceAccount
	if err := r.Get(ctx, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}, &sa); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting service account: %w", err)
		}

		r.Recorder.Event(cluster, "Normal", "CreatingServiceAccount", "Creating ServiceAccount")
		return r.createServiceAccount(ctx, cluster)
	}

	generatedPullSecretNames, err := r.generateServiceAccountPullSecretsNames(ctx, cluster)
	if err != nil {
		return fmt.Errorf("while generating pull secret names: %w", err)
	}

	origSa := sa.DeepCopy()
	err = specs.UpdateServiceAccount(generatedPullSecretNames, &sa)
	if err != nil {
		return fmt.Errorf("while generating service account: %w", err)
	}
	// we add the ownerMetadata only when creating the SA
	cluster.SetInheritedData(&sa.ObjectMeta)
	cluster.Spec.ServiceAccountTemplate.MergeMetadata(&sa)

	if specs.IsServiceAccountAligned(ctx, origSa, generatedPullSecretNames, sa.ObjectMeta) {
		return nil
	}

	r.Recorder.Event(cluster, "Normal", "UpdatingServiceAccount", "Updating ServiceAccount")
	if err := r.Patch(ctx, &sa, client.MergeFrom(origSa)); err != nil {
		return fmt.Errorf("while patching service account: %w", err)
	}

	return nil
}

// createServiceAccount creates the service account for this PostgreSQL cluster
func (r *ClusterReconciler) createServiceAccount(ctx context.Context, cluster *apiv1.Cluster) error {
	generatedPullSecretNames, err := r.generateServiceAccountPullSecretsNames(ctx, cluster)
	if err != nil {
		return fmt.Errorf("while generating pull secret names: %w", err)
	}

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
			Labels: map[string]string{
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
	}
	err = specs.UpdateServiceAccount(generatedPullSecretNames, serviceAccount)
	if err != nil {
		return fmt.Errorf("while creating new ServiceAccount: %w", err)
	}

	cluster.SetInheritedDataAndOwnership(&serviceAccount.ObjectMeta)
	cluster.Spec.ServiceAccountTemplate.MergeMetadata(serviceAccount)

	err = r.Create(ctx, serviceAccount)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		return err
	}

	return nil
}

// generateServiceAccountPullSecretsNames extracts the list of pull secret names given
// the cluster configuration
func (r *ClusterReconciler) generateServiceAccountPullSecretsNames(
	ctx context.Context, cluster *apiv1.Cluster,
) ([]string, error) {
	pullSecretNames := make([]string, 0, len(cluster.Spec.ImagePullSecrets))

	// Try to copy the secret from the operator
	operatorPullSecret, err := r.copyPullSecretFromOperator(ctx, cluster)
	if err != nil {
		return nil, err
	}

	if operatorPullSecret != "" {
		pullSecretNames = append(pullSecretNames, operatorPullSecret)
	}

	// Append the secrets specified by the user
	for _, secretReference := range cluster.Spec.ImagePullSecrets {
		pullSecretNames = append(pullSecretNames, secretReference.Name)
	}

	return pullSecretNames, nil
}

// copyPullSecretFromOperator will create a secret to download the operator, if the
// operator was downloaded via a Secret.
// It will return the string of the secret name if a secret need to be used to use the operator
func (r *ClusterReconciler) copyPullSecretFromOperator(ctx context.Context, cluster *apiv1.Cluster) (string, error) {
	if configuration.Current.OperatorNamespace == "" {
		// We are not getting started via a k8s deployment. Perhaps we are running in our development environment
		return "", nil
	}

	// Let's find the operator secret
	var operatorSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Name:      configuration.Current.OperatorPullSecretName,
		Namespace: configuration.Current.OperatorNamespace,
	}, &operatorSecret); err != nil {
		if apierrs.IsNotFound(err) {
			// There is no secret like that, probably because we are running in our development environment
			return "", nil
		}
		return "", err
	}

	clusterSecretName := fmt.Sprintf("%s-pull", cluster.Name)

	// Let's create the secret with the required info
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      clusterSecretName,
		},
		Data: operatorSecret.Data,
		Type: operatorSecret.Type,
	}
	cluster.SetInheritedDataAndOwnership(&secret.ObjectMeta)

	// Another sync loop may have already created the service. Let's check that
	if err := r.Create(ctx, &secret); err != nil && !apierrs.IsAlreadyExists(err) {
		return "", err
	}

	return clusterSecretName, nil
}

// createOrPatchRole ensures that the required role for the instance manager exists and
// contains the right rules
func (r *ClusterReconciler) createOrPatchRole(ctx context.Context, cluster *apiv1.Cluster) error {
	originBackup, err := r.getOriginBackup(ctx, cluster)
	if err != nil {
		return err
	}

	var role rbacv1.Role
	if err := r.Get(ctx, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}, &role); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting role: %w", err)
		}

		r.Recorder.Event(cluster, "Normal", "CreatingRole", "Creating Cluster Role")
		return r.createRole(ctx, cluster, originBackup)
	}

	generatedRole := specs.CreateRole(*cluster, originBackup)
	if equality.Semantic.DeepEqual(generatedRole.Rules, role.Rules) {
		// Everything fine, the two rules have the same content
		return nil
	}

	r.Recorder.Event(cluster, "Normal", "UpdatingRole", "Updating Cluster Role")

	// The configuration changed, and we need the patch the
	// configMap we have
	patchedRole := role.DeepCopy()
	patchedRole.Rules = generatedRole.Rules
	if err := r.Patch(ctx, patchedRole, client.MergeFrom(&role)); err != nil {
		return fmt.Errorf("while patching role: %w", err)
	}

	return nil
}

// createOrPatchDefaultMetricsConfigmap ensures that the required configmap containing
// default monitoring queries exists and contains the latest queries
func (r *ClusterReconciler) createOrPatchDefaultMetricsConfigmap(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	// we extract the operator configMap that needs to be cloned in the namespace where the cluster lives
	var sourceConfigmap corev1.ConfigMap
	if err := r.Get(ctx,
		client.ObjectKey{
			Name:      configuration.Current.MonitoringQueriesConfigmap,
			Namespace: configuration.Current.OperatorNamespace,
		}, &sourceConfigmap); err != nil {
		if apierrs.IsNotFound(err) {
			contextLogger.Error(err, "while trying to get default metrics configMap")
			return nil
		}
		return err
	}

	if _, ok := sourceConfigmap.Data[apiv1.DefaultMonitoringKey]; !ok {
		contextLogger.Warning("key not found while checking default metrics configMap", "key",
			apiv1.DefaultMonitoringKey, "configmap_name", sourceConfigmap.Name)
		return nil
	}

	if cluster.Namespace == configuration.Current.OperatorNamespace &&
		configuration.Current.MonitoringQueriesConfigmap == apiv1.DefaultMonitoringConfigMapName {
		contextLogger.Debug(
			"skipping default metrics synchronization. The cluster resides in the same namespace of the operator",
			"clusterNamespace", cluster.Namespace,
			"clusterName", cluster.Name,
		)
		return nil
	}

	// we clone the configmap in the cluster namespace
	var targetConfigMap corev1.ConfigMap
	if err := r.Get(ctx,
		client.ObjectKey{
			Name:      apiv1.DefaultMonitoringConfigMapName,
			Namespace: cluster.Namespace,
		}, &targetConfigMap); err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}
		// if the configMap does not exist we create it
		newConfigMap := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apiv1.DefaultMonitoringConfigMapName,
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					utils.WatchedLabelName:                "true",
					utils.KubernetesAppManagedByLabelName: utils.ManagerName,
				},
			},
			Data: map[string]string{
				apiv1.DefaultMonitoringKey: sourceConfigmap.Data[apiv1.DefaultMonitoringKey],
			},
		}
		utils.SetOperatorVersion(&newConfigMap.ObjectMeta, versions.Version)
		return r.Create(ctx, &newConfigMap)
	}

	// we check that we own the existing configmap
	if _, ok := targetConfigMap.Annotations[utils.OperatorVersionAnnotationName]; !ok {
		contextLogger.Warning("A configmap with the same name as the one the operator would have created for "+
			"default metrics already exists, without the required annotation",
			"configmap", targetConfigMap.Name, "annotation", utils.OperatorVersionAnnotationName)
		return nil
	}

	if reflect.DeepEqual(sourceConfigmap.Data, targetConfigMap.Data) {
		// Everything fine, the two secrets are exactly the same
		return nil
	}

	// The configuration changed, and we need the patch the configMap we have
	patchedConfigMap := targetConfigMap.DeepCopy()
	utils.SetOperatorVersion(&patchedConfigMap.ObjectMeta, versions.Version)
	patchedConfigMap.Data = sourceConfigmap.Data

	if err := r.Patch(ctx, patchedConfigMap, client.MergeFrom(&targetConfigMap)); err != nil {
		return fmt.Errorf("while patching default monitoring queries configmap: %w", err)
	}

	return nil
}

// createOrPatchDefaultMetricsConfigmap ensures that the required secret containing default
// monitoring queries exists and contains the latest queries
func (r *ClusterReconciler) createOrPatchDefaultMetricsSecret(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	// We extract the operator configMap that needs to be cloned in the namespace where the cluster lives
	var sourceSecret corev1.Secret
	if err := r.Get(ctx,
		client.ObjectKey{
			Name:      configuration.Current.MonitoringQueriesSecret,
			Namespace: configuration.Current.OperatorNamespace,
		}, &sourceSecret); err != nil {
		if apierrs.IsNotFound(err) {
			contextLogger.Error(err, "while trying to get default metrics secret")
			return nil
		}
		return err
	}

	if _, ok := sourceSecret.Data[apiv1.DefaultMonitoringKey]; !ok {
		contextLogger.Warning("key not found while checking default metrics secret", "key",
			apiv1.DefaultMonitoringKey, "secret_name", sourceSecret.Name)
		return nil
	}

	if cluster.Namespace == configuration.Current.OperatorNamespace &&
		configuration.Current.MonitoringQueriesSecret == apiv1.DefaultMonitoringSecretName {
		contextLogger.Debug(
			"skipping default metrics synchronization. The cluster resides in the same namespace of the operator",
			"clusterNamespace", cluster.Namespace,
			"clusterName", cluster.Name,
		)
		return nil
	}

	// We clone the secret in the cluster namespace
	var targetSecret corev1.Secret
	if err := r.Get(ctx,
		client.ObjectKey{
			Name:      apiv1.DefaultMonitoringSecretName,
			Namespace: cluster.Namespace,
		}, &targetSecret); err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}
		// If the secret does not exist we create it
		newSecret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apiv1.DefaultMonitoringSecretName,
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					utils.WatchedLabelName:                "true",
					utils.KubernetesAppManagedByLabelName: utils.ManagerName,
				},
			},
			Data: map[string][]byte{
				apiv1.DefaultMonitoringKey: sourceSecret.Data[apiv1.DefaultMonitoringKey],
			},
		}
		utils.SetOperatorVersion(&newSecret.ObjectMeta, versions.Version)
		return r.Create(ctx, &newSecret)
	}

	// We check that we own the existing configmap
	if _, ok := targetSecret.Annotations[utils.OperatorVersionAnnotationName]; !ok {
		contextLogger.Warning("A secret with the same name as the one the operator would have created for "+
			"default metrics already exists, without the required annotation",
			"secret", targetSecret.Name, "annotation", utils.OperatorVersionAnnotationName)
		return nil
	}

	if reflect.DeepEqual(sourceSecret.Data, targetSecret.Data) {
		// Everything fine, the two secrets are exactly the same
		return nil
	}

	// The configuration changed, and we need the patch the secret we have
	patchedSecret := targetSecret.DeepCopy()
	utils.SetOperatorVersion(&patchedSecret.ObjectMeta, versions.Version)
	patchedSecret.Data = sourceSecret.Data

	if err := r.Patch(ctx, patchedSecret, client.MergeFrom(&targetSecret)); err != nil {
		return fmt.Errorf("while patching default monitoring queries secret: %w", err)
	}

	return nil
}

func (r *ClusterReconciler) createOrPatchDefaultMetrics(ctx context.Context, cluster *apiv1.Cluster) (err error) {
	if configuration.Current.MonitoringQueriesConfigmap != "" {
		err = r.createOrPatchDefaultMetricsConfigmap(ctx, cluster)
		if err != nil {
			return err
		}
	}
	if configuration.Current.MonitoringQueriesSecret != "" {
		err = r.createOrPatchDefaultMetricsSecret(ctx, cluster)
		if err != nil {
			return err
		}
	}
	return nil
}

type podMonitorManager interface {
	// IsPodMonitorEnabled returns a boolean indicating if the PodMonitor should exists or not
	IsPodMonitorEnabled() bool
	// BuildPodMonitor builds a new PodMonitor object
	BuildPodMonitor() *monitoringv1.PodMonitor
}

// createOrPatchPodMonitor
func createOrPatchPodMonitor(
	ctx context.Context,
	cli client.Client,
	discoveryClient discovery.DiscoveryInterface,
	manager podMonitorManager,
) error {
	contextLogger := log.FromContext(ctx)

	// Checking for the PodMonitor Custom Resource Definition in the Kubernetes cluster
	havePodMonitorCRD, err := utils.PodMonitorExist(discoveryClient)
	if err != nil {
		return err
	}

	if !havePodMonitorCRD {
		if manager.IsPodMonitorEnabled() {
			// If the PodMonitor CRD does not exist, but the cluster has monitoring enabled,
			// the controller cannot do anything until the CRD is installed
			contextLogger.Warning("PodMonitor CRD not present. Cannot create the PodMonitor object")
		}
		return nil
	}

	expectedPodMonitor := manager.BuildPodMonitor()
	// We get the current pod monitor
	podMonitor := &monitoringv1.PodMonitor{}
	if err := cli.Get(
		ctx,
		client.ObjectKeyFromObject(expectedPodMonitor),
		podMonitor,
	); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting the podmonitor: %w", err)
		}
		podMonitor = nil
	}

	switch {
	// Pod monitor disabled and no pod monitor - nothing to do
	case !manager.IsPodMonitorEnabled() && podMonitor == nil:
		return nil
	// Pod monitor disabled and pod monitor present - delete it
	case !manager.IsPodMonitorEnabled() && podMonitor != nil:
		if _, owned := IsOwnedByCluster(podMonitor); owned {
			contextLogger.Info("Deleting PodMonitor")
			if err := cli.Delete(ctx, podMonitor); err != nil {
				if !apierrs.IsNotFound(err) {
					return err
				}
			}
		}
		return nil
	// Pod monitor enabled and no pod monitor - create it
	case manager.IsPodMonitorEnabled() && podMonitor == nil:
		contextLogger.Debug("Creating PodMonitor")
		return cli.Create(ctx, expectedPodMonitor)
	// Pod monitor enabled and pod monitor present - update it
	default:
		origPodMonitor := podMonitor.DeepCopy()
		podMonitor.Spec = expectedPodMonitor.Spec
		// We don't override the current labels/annotations given that there could be data that isn't managed by us
		utils.MergeObjectsMetadata(podMonitor, expectedPodMonitor)

		// If there's no changes we are done
		if reflect.DeepEqual(origPodMonitor, podMonitor) {
			return nil
		}

		// Patch the PodMonitor, so we always reconcile it with the cluster changes
		contextLogger.Debug("Patching PodMonitor")
		return cli.Patch(ctx, podMonitor, client.MergeFrom(origPodMonitor))
	}
}

// createRole creates the role
func (r *ClusterReconciler) createRole(ctx context.Context, cluster *apiv1.Cluster, backupOrigin *apiv1.Backup) error {
	role := specs.CreateRole(*cluster, backupOrigin)
	cluster.SetInheritedDataAndOwnership(&role.ObjectMeta)

	err := r.Create(ctx, &role)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.FromContext(ctx).Error(err, "Unable to create the Role", "object", role)
		return err
	}

	return nil
}

// createRoleBinding creates the role binding
func (r *ClusterReconciler) createRoleBinding(ctx context.Context, cluster *apiv1.Cluster) error {
	roleBinding := specs.CreateRoleBinding(cluster.ObjectMeta)
	cluster.SetInheritedDataAndOwnership(&roleBinding.ObjectMeta)

	err := r.Create(ctx, &roleBinding)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.FromContext(ctx).Error(err, "Unable to create the ServiceAccount", "object", roleBinding)
		return err
	}

	return nil
}

// generateNodeSerial extracts the first free node serial in this pods
func (r *ClusterReconciler) generateNodeSerial(ctx context.Context, cluster *apiv1.Cluster) (int, error) {
	cluster.Status.LatestGeneratedNode++
	if err := r.Status().Update(ctx, cluster); err != nil {
		return 0, err
	}

	return cluster.Status.LatestGeneratedNode, nil
}

// nolint: gocognit
func (r *ClusterReconciler) createPrimaryInstance(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if cluster.Status.LatestGeneratedNode != 0 {
		// We are we creating a new blank primary when we had previously generated
		// other nodes, and we don't have any PVC to reuse?
		// This can happen when:
		//
		// 1 - the user deletes all the PVCs and all the Pods in a cluster
		//    (and why would a user do that?)
		// 2 - the cache isn't ready for Pods and ready for the Cluster,
		//     so we actually haven't the first pod in our managed list
		//     but it's still in the API Server
		//
		// As far as the first option is concerned, we can just stop
		// healing this cluster as we have nothing to do.
		// For the second option we can just retry when the next
		// reconciliation loop is started by the informers.
		contextLogger.Info("refusing to create the primary instance while the latest generated serial is not zero",
			"latestGeneratedNode", cluster.Status.LatestGeneratedNode)

		if err := r.RegisterPhase(ctx, cluster,
			apiv1.PhaseUnrecoverable,
			"One or more instances were previously created, but no PersistentVolumeClaims (PVCs) exist. "+
				"The cluster is in an unrecoverable state. To resolve this, restore the cluster from a recent backup.",
		); err != nil {
			return ctrl.Result{}, fmt.Errorf("while registering the unrecoverable phase: %w", err)
		}
		return ctrl.Result{}, nil
	}

	var (
		backup           *apiv1.Backup
		recoverySnapshot *persistentvolumeclaim.StorageSource
	)
	// If the cluster is bootstrapping from recovery, it may do so from:
	//  1 - a backup object, which may be done with volume snapshots or object storage
	//  2 - volume snapshots
	// We need to check that whichever alternative is used, the backup/snapshot is completed.
	if cluster.Spec.Bootstrap != nil &&
		cluster.Spec.Bootstrap.Recovery != nil {
		var err error
		backup, err = r.getOriginBackup(ctx, cluster)
		if err != nil {
			return ctrl.Result{}, err
		}

		if res, err := r.checkReadyForRecovery(ctx, backup, cluster); !res.IsZero() || err != nil {
			return res, err
		}

		recoverySnapshot = persistentvolumeclaim.GetCandidateStorageSourceForPrimary(cluster, backup)
	}

	// Generate a new node serial
	nodeSerial, err := r.generateNodeSerial(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot generate node serial: %w", err)
	}

	// Create the PVCs from the cluster definition, and if bootstrapping from
	// recoverySnapshot, use that as the source
	if err := persistentvolumeclaim.CreateInstancePVCs(
		ctx,
		r.Client,
		cluster,
		recoverySnapshot,
		nodeSerial,
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot create primary instance PVCs: %w", err)
	}

	// We are bootstrapping a cluster and in need to create the first node
	var job *batchv1.Job

	isBootstrappingFromRecovery := cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.Recovery != nil
	isBootstrappingFromBaseBackup := cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.PgBaseBackup != nil
	switch {
	case isBootstrappingFromRecovery && recoverySnapshot != nil:
		metadata, err := persistentvolumeclaim.GetSourceMetadataOrNil(
			ctx,
			r.Client,
			cluster.Namespace,
			recoverySnapshot.DataSource,
		)
		if err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(cluster, "Normal", "CreatingInstance", "Primary instance (from volumeSnapshots)")
		job = specs.CreatePrimaryJobViaRestoreSnapshot(*cluster, nodeSerial, metadata, backup)

	case isBootstrappingFromRecovery:
		r.Recorder.Event(cluster, "Normal", "CreatingInstance", "Primary instance (from backup)")
		job = specs.CreatePrimaryJobViaRecovery(*cluster, nodeSerial, backup)

	case isBootstrappingFromBaseBackup:
		r.Recorder.Event(cluster, "Normal", "CreatingInstance", "Primary instance (from physical backup)")
		job = specs.CreatePrimaryJobViaPgBaseBackup(*cluster, nodeSerial)

	default:
		r.Recorder.Event(cluster, "Normal", "CreatingInstance", "Primary instance (initdb)")
		job = specs.CreatePrimaryJobViaInitdb(*cluster, nodeSerial)
	}

	if err := ctrl.SetControllerReference(cluster, job, r.Scheme); err != nil {
		contextLogger.Error(err, "Unable to set the owner reference for instance")
		return ctrl.Result{}, err
	}

	podName := fmt.Sprintf("%v-%v", cluster.Name, nodeSerial)
	if err = r.setPrimaryInstance(ctx, cluster, podName); err != nil {
		contextLogger.Error(err, "Unable to set the primary instance name")
		return ctrl.Result{}, err
	}

	err = r.RegisterPhase(ctx, cluster, apiv1.PhaseFirstPrimary,
		fmt.Sprintf("Creating primary instance %v", podName))
	if err != nil {
		return ctrl.Result{}, err
	}

	contextLogger.Info("Creating new Job",
		"jobName", job.Name,
		"primary", true)

	utils.InheritAnnotations(&job.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritAnnotations(&job.Spec.Template.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(&job.ObjectMeta, cluster.Labels,
		cluster.GetFixedInheritedLabels(), configuration.Current)
	utils.InheritLabels(&job.Spec.Template.ObjectMeta, cluster.Labels,
		cluster.GetFixedInheritedLabels(), configuration.Current)

	if err = r.Create(ctx, job); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Job was already created, maybe the cache is stale.
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		contextLogger.Error(err, "Unable to create job", "job", job)
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, ErrNextLoop
}

// getOriginBackup gets the backup that is used to bootstrap a new PostgreSQL cluster
func (r *ClusterReconciler) getOriginBackup(ctx context.Context, cluster *apiv1.Cluster) (*apiv1.Backup, error) {
	if cluster.Spec.Bootstrap == nil ||
		cluster.Spec.Bootstrap.Recovery == nil ||
		cluster.Spec.Bootstrap.Recovery.Backup == nil {
		return nil, nil
	}

	var backup apiv1.Backup
	backupObjectKey := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Spec.Bootstrap.Recovery.Backup.Name,
	}
	err := r.Get(ctx, backupObjectKey, &backup)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(cluster, "Warning", "ErrorNoBackup",
				"Backup object \"%v/%v\" is missing",
				backupObjectKey.Namespace, backupObjectKey.Name)

			return nil, nil
		}

		return nil, fmt.Errorf("cannot get the backup object: %w", err)
	}

	return &backup, nil
}

func (r *ClusterReconciler) joinReplicaInstance(
	ctx context.Context,
	nodeSerial int,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	var backupList apiv1.BackupList
	if err := r.List(ctx, &backupList,
		client.MatchingFields{clusterNameField: cluster.Name},
		client.InNamespace(cluster.Namespace),
	); err != nil {
		contextLogger.Error(err, "Error while getting backup list, when bootstrapping a new replica")
		return ctrl.Result{}, err
	}

	job := specs.JoinReplicaInstance(*cluster, nodeSerial)

	// If we can bootstrap this replica from a pre-existing source, we do it
	storageSource := persistentvolumeclaim.GetCandidateStorageSourceForReplica(ctx, cluster, backupList)
	if storageSource != nil {
		job = specs.RestoreReplicaInstance(*cluster, nodeSerial)
	}

	contextLogger.Info("Creating new Job",
		"job", job.Name,
		"primary", false,
		"storageSource", storageSource,
		"role", job.Spec.Template.Labels[utils.JobRoleLabelName],
	)

	r.Recorder.Eventf(cluster, "Normal", "CreatingInstance",
		"Creating instance %v-%v", cluster.Name, nodeSerial)

	if err := r.RegisterPhase(ctx, cluster,
		apiv1.PhaseCreatingReplica,
		fmt.Sprintf("Creating replica %v", job.Name)); err != nil {
		return ctrl.Result{}, err
	}

	if err := ctrl.SetControllerReference(cluster, job, r.Scheme); err != nil {
		contextLogger.Error(err, "Unable to set the owner reference for joined PostgreSQL node")
		return ctrl.Result{}, err
	}

	utils.InheritAnnotations(&job.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritAnnotations(&job.Spec.Template.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(&job.ObjectMeta, cluster.Labels,
		cluster.GetFixedInheritedLabels(), configuration.Current)
	utils.InheritLabels(&job.Spec.Template.ObjectMeta, cluster.Labels,
		cluster.GetFixedInheritedLabels(), configuration.Current)

	if err := r.Create(ctx, job); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Job was already created, maybe the cache is stale.
			contextLogger.Info("Job already exist, maybe the cache is stale", "pod", job.Name)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, ErrNextLoop
		}

		contextLogger.Error(err, "Unable to create Job", "job", job)
		return ctrl.Result{}, err
	}

	if err := persistentvolumeclaim.CreateInstancePVCs(
		ctx,
		r.Client,
		cluster,
		storageSource,
		nodeSerial,
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot create replica instance PVCs: %w", err)
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, ErrNextLoop
}

// ensureInstancesAreCreated recreates any missing instance
func (r *ClusterReconciler) ensureInstancesAreCreated(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
	instancesStatus postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	instanceToCreate, err := findInstancePodToCreate(ctx, cluster, instancesStatus, resources.pvcs.Items)
	if err != nil {
		return ctrl.Result{}, err
	}
	if instanceToCreate == nil {
		contextLogger.Trace(
			"haven't found any instance to create",
			"instances", instancesStatus.GetNames(),
			"dangling", cluster.Status.DanglingPVC,
			"unusable", cluster.Status.UnusablePVC,
		)
		return ctrl.Result{}, nil
	}

	if !cluster.IsNodeMaintenanceWindowInProgress() &&
		instancesStatus.InstancesReportingStatus() != cluster.Status.ReadyInstances {
		// A pod is not ready, let's retry
		contextLogger.Debug("Waiting for node to be ready before attaching PVCs")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	// TODO: this logic eventually should be moved elsewhere
	instancePVCs := persistentvolumeclaim.FilterByPodSpec(resources.pvcs.Items, instanceToCreate.Spec)
	for _, instancePVC := range instancePVCs {
		// This should not happen. However, we put this guard here
		// as an assertion to catch unexpected events.
		pvcStatus := instancePVC.Annotations[utils.PVCStatusAnnotationName]
		if pvcStatus != persistentvolumeclaim.StatusReady {
			contextLogger.Info("Selected PVC is not ready yet, waiting for 1 second",
				"pvc", instancePVC.Name,
				"status", pvcStatus,
				"instance", instanceToCreate.Name,
			)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
		}
	}

	// If this cluster has been restarted, mark the Pod with the latest restart time
	if clusterRestart, ok := cluster.Annotations[utils.ClusterRestartAnnotationName]; ok {
		if instanceToCreate.Annotations == nil {
			instanceToCreate.Annotations = make(map[string]string)
		}
		instanceToCreate.Annotations[utils.ClusterRestartAnnotationName] = clusterRestart
	}

	contextLogger.Info("Creating new Pod to reattach a PVC",
		"pod", instanceToCreate.Name,
		"pvc", instanceToCreate.Name)

	if err := ctrl.SetControllerReference(cluster, instanceToCreate, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to set the owner reference for the Pod: %w", err)
	}

	utils.InheritAnnotations(&instanceToCreate.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(&instanceToCreate.ObjectMeta, cluster.Labels,
		cluster.GetFixedInheritedLabels(), configuration.Current)

	if err := r.Create(ctx, instanceToCreate); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Pod was already created, maybe the cache is stale.
			// Let's reconcile another time
			contextLogger.Info("Instance already exist, maybe the cache is stale", "instance", instanceToCreate.Name)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
		}

		return ctrl.Result{}, fmt.Errorf("unable to create Pod: %w", err)
	}

	return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
}

// we elect a current instance that doesn't exist for creation
func findInstancePodToCreate(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instancesStatus postgres.PostgresqlStatusList,
	pvcs []corev1.PersistentVolumeClaim,
) (*corev1.Pod, error) {
	instanceThatHavePods := instancesStatus.GetNames()

	var missingInstancePVC *corev1.PersistentVolumeClaim

	iterablePVCs := cluster.Status.DanglingPVC
	// appending unusablePVC ensures that some corner cases are covered. (EX: an instance is deleted manually while
	// new type of PVCs were enabled)
	iterablePVCs = append(iterablePVCs, cluster.Status.UnusablePVC...)
	for _, name := range iterablePVCs {
		idx := slices.IndexFunc(pvcs, func(claim corev1.PersistentVolumeClaim) bool {
			return claim.Name == name
		})
		if idx == -1 {
			return nil, fmt.Errorf("programmatic error, pvc not found")
		}

		serial, err := specs.GetNodeSerial(pvcs[idx].ObjectMeta)
		if err != nil {
			return nil, err
		}

		instanceName := specs.GetInstanceName(cluster.Name, serial)
		if slices.Contains(instanceThatHavePods, instanceName) {
			continue
		}

		// We give the priority to reattaching the primary instance
		if isPrimary := specs.IsPrimary(pvcs[idx].ObjectMeta); isPrimary {
			missingInstancePVC = &pvcs[idx]
			break
		}

		if missingInstancePVC == nil {
			missingInstancePVC = &pvcs[idx]
		}
	}

	if missingInstancePVC != nil {
		serial, err := specs.GetNodeSerial(missingInstancePVC.ObjectMeta)
		if err != nil {
			return nil, err
		}
		return specs.NewInstance(ctx, *cluster, serial, true)
	}

	return nil, nil
}

// checkReadyForRecovery checks if the backup or volumeSnapshots are ready, and
// returns for requeue if not
func (r *ClusterReconciler) checkReadyForRecovery(
	ctx context.Context,
	backup *apiv1.Backup,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if cluster.Spec.Bootstrap.Recovery.Backup != nil {
		if backup == nil {
			contextLogger.Info("Missing backup object, can't continue full recovery",
				"backup", cluster.Spec.Bootstrap.Recovery.Backup)
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Minute,
			}, nil
		}
		if backup.Status.Phase != apiv1.BackupPhaseCompleted {
			contextLogger.Info("The source backup object is not completed, can't continue full recovery",
				"backup", cluster.Spec.Bootstrap.Recovery.Backup,
				"backupPhase", backup.Status.Phase)
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Minute,
			}, nil
		}
	}

	volumeSnapshotsRecovery := cluster.Spec.Bootstrap.Recovery.VolumeSnapshots
	if volumeSnapshotsRecovery != nil {
		status, err := persistentvolumeclaim.VerifyDataSourceCoherence(
			ctx, r.Client, cluster.Namespace, volumeSnapshotsRecovery)
		if err != nil {
			return ctrl.Result{}, err
		}
		if status.ContainsErrors() {
			contextLogger.Warning(
				"Volume snapshots verification failed, retrying",
				"status", status)
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: 5 * time.Second,
			}, nil
		}
		if status.ContainsWarnings() {
			contextLogger.Warning("Volume snapshots verification warnings",
				"status", status)
		}
	}
	return ctrl.Result{}, nil
}
