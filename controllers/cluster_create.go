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
	"fmt"
	"reflect"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sethvargo/go-password/password"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
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

	err = r.createPostgresServices(ctx, cluster)
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

	err = r.createOrPatchPodMonitor(ctx, cluster)
	if err != nil {
		return err
	}

	// TODO: only required to cleanup custom monitoring queries configmaps from older versions (v1.10 and v1.11)
	// 		 that could have been copied with the source configmap name instead of the new default one.
	// 		 Should be removed in future releases.
	// should never return an error, not a requirement, just a nice to have
	r.deleteOldCustomQueriesConfigmap(ctx, cluster)

	return nil
}

func (r *ClusterReconciler) reconcilePodDisruptionBudget(ctx context.Context, cluster *apiv1.Cluster) error {
	// The PDB should not be enforced if we are inside a maintenance
	// window, and we chose to avoid allocating more storage space.
	if cluster.IsNodeMaintenanceWindowInProgress() && cluster.IsReusePVCEnabled() {
		if err := r.deleteReplicasPodDisruptionBudget(ctx, cluster); err != nil {
			return err
		}

		if cluster.Spec.Instances == 1 {
			// If this a single-instance cluster, we need to delete
			// the PodDisruptionBudget for the primary node too
			// otherwise the user won't be able to drain the workloads
			// from the underlying node.
			return r.deletePrimaryPodDisruptionBudget(ctx, cluster)
		}

		// Make sure that if the cluster was scaled down and scaled up
		// we create the primary PDB even if we're under a maintenance window
		return r.createOrPatchOwnedPodDisruptionBudget(ctx,
			cluster,
			specs.BuildPrimaryPodDisruptionBudget(cluster),
		)
	}

	// Reconcile the primary PDB
	err := r.createOrPatchOwnedPodDisruptionBudget(ctx,
		cluster,
		specs.BuildPrimaryPodDisruptionBudget(cluster),
	)
	if err != nil {
		return err
	}

	return r.createOrPatchOwnedPodDisruptionBudget(ctx,
		cluster,
		specs.BuildReplicasPodDisruptionBudget(cluster),
	)
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
			postgresPassword)
		SetClusterOwnerAnnotationsAndLabels(&postgresSecret.ObjectMeta, cluster)

		if err := r.Create(ctx, postgresSecret); err != nil {
			if !apierrs.IsAlreadyExists(err) {
				return err
			}
		}
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

		if _, owned := isOwnedByCluster(&secret); owned {
			return r.Delete(ctx, &secret)
		}
	}

	return nil
}

func (r *ClusterReconciler) reconcileAppUserSecret(ctx context.Context, cluster *apiv1.Cluster) error {
	if cluster.ShouldCreateApplicationDatabase() &&
		(cluster.Spec.Bootstrap.InitDB.Secret == nil ||
			cluster.Spec.Bootstrap.InitDB.Secret.Name == "") {
		appPassword, err := password.Generate(64, 10, 0, false, true)
		if err != nil {
			return err
		}

		appSecret := specs.CreateSecret(
			cluster.GetApplicationSecretName(),
			cluster.Namespace,
			cluster.GetServiceReadWriteName(),
			cluster.Spec.Bootstrap.InitDB.Database,
			cluster.Spec.Bootstrap.InitDB.Owner,
			appPassword)
		SetClusterOwnerAnnotationsAndLabels(&appSecret.ObjectMeta, cluster)

		if err := r.Create(ctx, appSecret); err != nil {
			if !apierrs.IsAlreadyExists(err) {
				return err
			}
		}
	}

	return nil
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
				postgres.PGBouncerPoolerUserName,
				&clientCaSecret,
				certs.CertTypeClient,
				nil,
				map[string]string{specs.WatchedLabelName: "true"})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ClusterReconciler) createPostgresServices(ctx context.Context, cluster *apiv1.Cluster) error {
	anyService := specs.CreateClusterAnyService(*cluster)
	SetClusterOwnerAnnotationsAndLabels(&anyService.ObjectMeta, cluster)

	if err := r.Create(ctx, anyService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readService := specs.CreateClusterReadService(*cluster)
	SetClusterOwnerAnnotationsAndLabels(&readService.ObjectMeta, cluster)

	if err := r.Create(ctx, readService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readOnlyService := specs.CreateClusterReadOnlyService(*cluster)
	SetClusterOwnerAnnotationsAndLabels(&readOnlyService.ObjectMeta, cluster)

	if err := r.Create(ctx, readOnlyService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readWriteService := specs.CreateClusterReadWriteService(*cluster)
	SetClusterOwnerAnnotationsAndLabels(&readWriteService.ObjectMeta, cluster)

	if err := r.Create(ctx, readWriteService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

// createOrPatchOwnedPodDisruptionBudget ensures that we have a PDB requiring to remove one node at a time
func (r *ClusterReconciler) createOrPatchOwnedPodDisruptionBudget(
	ctx context.Context,
	cluster *apiv1.Cluster,
	pdb *v1beta1.PodDisruptionBudget,
) error {
	if pdb == nil {
		return nil
	}

	var oldPdb v1beta1.PodDisruptionBudget

	if err := r.Get(ctx, client.ObjectKey{Name: pdb.Name, Namespace: pdb.Namespace}, &oldPdb); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting PodDisruptionBudget: %w", err)
		}
		SetClusterOwnerAnnotationsAndLabels(&pdb.ObjectMeta, cluster)

		r.Recorder.Event(cluster, "Normal", "CreatingPodDisruptionBudget",
			fmt.Sprintf("Creating PodDisruptionBudget %s", pdb.Name))
		if err = r.Create(ctx, pdb); err != nil {
			return fmt.Errorf("while creating PodDisruptionBudget: %w", err)
		}
		return nil
	}

	if reflect.DeepEqual(pdb.Spec, oldPdb.Spec) {
		// Everything fine, the two pdbs are the same for us
		return nil
	}

	r.Recorder.Event(cluster, "Normal", "UpdatingPodDisruptionBudget",
		fmt.Sprintf("Updating PodDisruptionBudget %s", pdb.Name))

	patchedPdb := oldPdb
	patchedPdb.Spec = pdb.Spec

	if err := r.Patch(ctx, &patchedPdb, client.MergeFrom(&oldPdb)); err != nil {
		return fmt.Errorf("while patching PodDisruptionBudget: %w", err)
	}

	return nil
}

// deleteReplicasPodDisruptionBudget ensures that we delete the PDB requiring to remove one node at a time
func (r *ClusterReconciler) deletePrimaryPodDisruptionBudget(ctx context.Context, cluster *apiv1.Cluster) error {
	return r.deletePodDisruptionBudget(
		ctx,
		cluster,
		client.ObjectKey{Name: cluster.Name + apiv1.PrimaryPodDisruptionBudgetSuffix, Namespace: cluster.Namespace})
}

// deleteReplicasPodDisruptionBudget ensures that we delete the PDB requiring to remove one node at a time
func (r *ClusterReconciler) deleteReplicasPodDisruptionBudget(ctx context.Context, cluster *apiv1.Cluster) error {
	return r.deletePodDisruptionBudget(ctx, cluster, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace})
}

// deleteReplicasPodDisruptionBudget ensures that we delete the PDB requiring to remove one node at a time
func (r *ClusterReconciler) deletePodDisruptionBudget(
	ctx context.Context,
	cluster *apiv1.Cluster,
	key types.NamespacedName,
) error {
	// If we have a PDB, we need to delete it
	var targetPdb v1beta1.PodDisruptionBudget
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
	contextLogger := log.FromContext(ctx)

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

	serviceAccountAligned, err := specs.IsServiceAccountAligned(&sa, generatedPullSecretNames)
	if err != nil {
		contextLogger.Error(err, "Cannot detect if a ServiceAccount need to be refreshed or not, refreshing it",
			"serviceAccount", sa)
		serviceAccountAligned = false
	}

	if serviceAccountAligned {
		return nil
	}

	origSa := sa.DeepCopy()
	err = specs.UpdateServiceAccount(generatedPullSecretNames, &sa)
	if err != nil {
		return fmt.Errorf("while generating service account: %w", err)
	}

	r.Recorder.Event(cluster, "Normal", "UpdatingServiceAccount", "Updating ServiceAccount")
	SetClusterOwnerAnnotationsAndLabels(&sa.ObjectMeta, cluster)
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
		},
	}
	err = specs.UpdateServiceAccount(generatedPullSecretNames, serviceAccount)
	if err != nil {
		return fmt.Errorf("while creating new ServiceAccount: %w", err)
	}

	SetClusterOwnerAnnotationsAndLabels(&serviceAccount.ObjectMeta, cluster)
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
	SetClusterOwnerAnnotationsAndLabels(&secret.ObjectMeta, cluster)

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
	if reflect.DeepEqual(generatedRole.Rules, role.Rules) {
		// Everything fine, the two config maps are exactly the same
		return nil
	}

	r.Recorder.Event(cluster, "Normal", "UpdatingRole", "Updating Cluster Role")

	// The configuration changed, and we need the patch the
	// configMap we have
	patchedRole := role
	patchedRole.Rules = generatedRole.Rules
	if err := r.Patch(ctx, &patchedRole, client.MergeFrom(&role)); err != nil {
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
					specs.WatchedLabelName: "true",
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

	// The configuration changed, and we need the patch the secret we have
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
		newConfigMap := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apiv1.DefaultMonitoringSecretName,
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					specs.WatchedLabelName: "true",
				},
			},
			Data: map[string][]byte{
				apiv1.DefaultMonitoringKey: sourceSecret.Data[apiv1.DefaultMonitoringKey],
			},
		}
		utils.SetOperatorVersion(&newConfigMap.ObjectMeta, versions.Version)
		return r.Create(ctx, &newConfigMap)
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

// createOrPatchPodMonitor
func (r *ClusterReconciler) createOrPatchPodMonitor(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	// Checking for the PodMonitor resource in the cluster
	havePodMonitor, err := utils.PodMonitorExist(r.DiscoveryClient)
	if err != nil || !havePodMonitor {
		contextLogger.Debug("Kind PodMonitor not detected", "err", err)
		return err
	}

	// We get the current pod monitor
	podMonitor := &monitoringv1.PodMonitor{}
	if err := r.Get(
		ctx,
		client.ObjectKey{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		podMonitor,
	); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting the podmonitor: %w", err)
		}
		podMonitor = nil
	}

	switch {
	// Pod monitor disabled and no pod monitor - nothing to do
	case !cluster.IsPodMonitorEnabled() && podMonitor == nil:
		return nil
	// Pod monitor disabled and pod monitor present - delete it
	case !cluster.IsPodMonitorEnabled() && podMonitor != nil:
		contextLogger.Info("Deleting PodMonitor")
		if err := r.Delete(ctx, podMonitor); err != nil {
			if !apierrs.IsNotFound(err) {
				return err
			}
		}
		return nil
	// Pod monitor enabled and no pod monitor - create it
	case cluster.IsPodMonitorEnabled() && podMonitor == nil:
		contextLogger.Debug("Creating PodMonitor")
		newPodMonitor := specs.CreatePodMonitor(cluster)
		SetClusterOwnerAnnotationsAndLabels(&newPodMonitor.ObjectMeta, cluster)
		return r.Create(ctx, newPodMonitor)
	// Pod monitor enabled and pod monitor present - update it
	default:
		origPodMonitor := podMonitor.DeepCopy()
		podMonitor.Spec = specs.CreatePodMonitor(cluster).Spec

		// If there's no changes we are done
		if reflect.DeepEqual(origPodMonitor, podMonitor) {
			return nil
		}

		// Patch the PodMonitor, so we always reconcile it with the cluster changes
		contextLogger.Debug("Patching PodMonitor")
		return r.Patch(ctx, podMonitor, client.MergeFrom(origPodMonitor))
	}
}

// createRole creates the role
func (r *ClusterReconciler) createRole(ctx context.Context, cluster *apiv1.Cluster, backupOrigin *apiv1.Backup) error {
	role := specs.CreateRole(*cluster, backupOrigin)
	SetClusterOwnerAnnotationsAndLabels(&role.ObjectMeta, cluster)

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
	SetClusterOwnerAnnotationsAndLabels(&roleBinding.ObjectMeta, cluster)

	err := r.Create(ctx, &roleBinding)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.FromContext(ctx).Error(err, "Unable to create the ServiceAccount", "object", roleBinding)
		return err
	}

	return nil
}

// generateNodeSerial extracts the first free node serial in this pods
func (r *ClusterReconciler) generateNodeSerial(ctx context.Context, cluster *apiv1.Cluster) (int32, error) {
	cluster.Status.LatestGeneratedNode++
	if err := r.Status().Update(ctx, cluster); err != nil {
		return 0, err
	}

	return cluster.Status.LatestGeneratedNode, nil
}

func (r *ClusterReconciler) createPrimaryInstance(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if cluster.Status.LatestGeneratedNode != 0 {
		// We are we creating a new blank primary when we had previously generated
		// other nodes and we don't have any PVC to reuse?
		// This can happen when:
		//
		// 1 - the user deletes all the PVCs and all the Pods in a cluster
		//    (and why would an user do that?)
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
		return ctrl.Result{}, nil
	}

	// Generate a new node serial
	nodeSerial, err := r.generateNodeSerial(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot generate node serial: %w", err)
	}

	pvcSpec, err := specs.CreatePVC(cluster.Spec.StorageConfiguration, cluster.Name, cluster.Namespace, nodeSerial)
	if err != nil {
		if err == specs.ErrorInvalidSize {
			// This error should have been caught by the validating
			// webhook, but since we are here the user must have disabled server-side
			// validation and we must react.
			contextLogger.Info("The size specified for the cluster is not valid",
				"size",
				cluster.Spec.StorageConfiguration.Size)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, err
	}

	SetClusterOwnerAnnotationsAndLabels(&pvcSpec.ObjectMeta, cluster)
	if err = r.Create(ctx, pvcSpec); err != nil && !apierrs.IsAlreadyExists(err) {
		contextLogger.Error(err, "Unable to create a PVC for this node", "nodeSerial", nodeSerial)
		return ctrl.Result{}, err
	}

	// We are bootstrapping a cluster and in need to create the first node
	var job *batchv1.Job

	switch {
	case cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.Recovery != nil:
		var backup *apiv1.Backup
		if cluster.Spec.Bootstrap.Recovery.Backup != nil {
			backup, err = r.getOriginBackup(ctx, cluster)
			if err != nil {
				return ctrl.Result{}, err
			}
			if backup == nil {
				contextLogger.Info("Missing backup object, can't continue full recovery",
					"backup", cluster.Spec.Bootstrap.Recovery.Backup)
				return ctrl.Result{
					Requeue:      true,
					RequeueAfter: time.Minute,
				}, nil
			}
		}

		r.Recorder.Event(cluster, "Normal", "CreatingInstance", "Primary instance (from backup)")
		job = specs.CreatePrimaryJobViaRecovery(*cluster, nodeSerial, backup)
	case cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.PgBaseBackup != nil:
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
		"name", job.Name,
		"primary", true)

	utils.SetOperatorVersion(&job.ObjectMeta, versions.Version)
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
	nodeSerial int32,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	var job *batchv1.Job
	var err error

	job = specs.JoinReplicaInstance(*cluster, nodeSerial)

	contextLogger.Info("Creating new Job",
		"job", job.Name,
		"primary", false)

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

	utils.SetOperatorVersion(&job.ObjectMeta, versions.Version)
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
			contextLogger.Info("Job already exist, maybe the cache is stale", "pod", job.Name)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, ErrNextLoop
		}

		contextLogger.Error(err, "Unable to create Job", "job", job)
		return ctrl.Result{}, err
	}

	pvcSpec, err := specs.CreatePVC(cluster.Spec.StorageConfiguration, cluster.Name, cluster.Namespace, nodeSerial)
	if err != nil {
		if err == specs.ErrorInvalidSize {
			// This error should have been caught by the validating
			// webhook, but since we are here the user must have disabled server-side
			// validation and we must react.
			contextLogger.Info("The size specified for the cluster is not valid",
				"size",
				cluster.Spec.StorageConfiguration.Size)
			return ctrl.Result{RequeueAfter: time.Minute}, ErrNextLoop
		}
		return ctrl.Result{}, fmt.Errorf("unable to create a PVC spec for node with serial %v: %w", nodeSerial, err)
	}

	SetClusterOwnerAnnotationsAndLabels(&pvcSpec.ObjectMeta, cluster)

	if err = r.Create(ctx, pvcSpec); err != nil && !apierrs.IsAlreadyExists(err) {
		return ctrl.Result{}, fmt.Errorf("unable to create a PVC for this node (nodeSerial: %d): %w",
			nodeSerial,
			err)
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, ErrNextLoop
}

// reconcilePVCs reattaches a dangling PVC
func (r *ClusterReconciler) reconcilePVCs(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
	instancesStatus postgres.PostgresqlStatusList,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if !cluster.IsNodeMaintenanceWindowInProgress() &&
		cluster.Status.Instances-cluster.Status.ReadyInstances !=
			instancesStatus.InstancesReportingMightBeUnavailable() {
		// A pod is not ready, let's retry
		contextLogger.Debug("Waiting for node to be ready before attaching PVCs")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	pvcToReattach := electPvcToReattach(cluster)
	if pvcToReattach == "" {
		// This should never happen. This function should be invoked
		// only when there is something to reattach.
		contextLogger.Debug("Impossible to elect a PVC to reattach",
			"danglingPVCs", cluster.Status.DanglingPVC,
			"initializingPVCs", cluster.Status.InitializingPVC)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	if len(cluster.Status.DanglingPVC) > 0 {
		if (cluster.IsNodeMaintenanceWindowInProgress() && !cluster.IsReusePVCEnabled()) ||
			cluster.Spec.Instances <= cluster.Status.Instances {
			contextLogger.Info(
				"Detected unneeded PVCs, removing them",
				"statusInstances", cluster.Status.Instances,
				"specInstances", cluster.Spec.Instances,
				"maintenanceWindow", cluster.Spec.NodeMaintenanceWindow,
				"danglingPVCs", cluster.Status.DanglingPVC)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, r.removeDanglingPVCs(ctx, cluster)
		}
	}

	pvc := resources.getPVC(pvcToReattach)

	// This should not happen. However, we put this guard here
	// as an assertion to catch unexpected events.
	pvcStatus := pvc.Annotations[specs.PVCStatusAnnotationName]
	if pvcStatus != specs.PVCStatusReady {
		contextLogger.Info("Selected PVC is not ready yet, waiting for 1 second",
			"pvc", pvc.Name,
			"status", pvcStatus)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
	}

	nodeSerial, err := specs.GetNodeSerial(pvc.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot detect serial from PVC %v: %v", pvc.Name, err)
	}

	pod := specs.PodWithExistingStorage(*cluster, int32(nodeSerial))

	if configuration.Current.EnableAzurePVCUpdates {
		for _, resizingPVC := range cluster.Status.ResizingPVC {
			// This code works on the assumption that the PVC have the same name as the pod using it.
			if resizingPVC == pvc.Name {
				contextLogger.Info("PVC is in resizing status, retrying in 5 seconds", "pod", pod.Name)
				return ctrl.Result{RequeueAfter: 5 * time.Second}, ErrNextLoop
			}
		}
	}

	// If this cluster has been restarted, mark the Pod with the latest restart time
	if clusterRestart, ok := cluster.Annotations[specs.ClusterRestartAnnotationName]; ok {
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Annotations[specs.ClusterRestartAnnotationName] = clusterRestart
	}

	contextLogger.Info("Creating new Pod to reattach a PVC",
		"pod", pod.Name,
		"pvc", pvc.Name)

	if err := ctrl.SetControllerReference(cluster, pod, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to set the owner reference for the Pod: %w", err)
	}

	utils.SetOperatorVersion(&pod.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&pod.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(&pod.ObjectMeta, cluster.Labels,
		cluster.GetFixedInheritedLabels(), configuration.Current)

	if err := r.Create(ctx, pod); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Pod was already created, maybe the cache is stale.
			// Let's reconcile another time
			contextLogger.Info("Pod already exist, maybe the cache is stale", "pod", pod.Name)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, ErrNextLoop
		}

		return ctrl.Result{}, fmt.Errorf("unable to create Pod: %w", err)
	}

	// Do another reconcile cycle after handling a dangling PVC
	return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
}

// electPvcToReattach chooses a PVC between the initializing and the dangling ones that should be reattached
// to the cluster, giving precedence to the target primary if existing in the set. If the target primary is fine,
// let's start using the PVC we have initialized. After that we use the PVC that are initializing or dangling
func electPvcToReattach(cluster *apiv1.Cluster) string {
	pvcs := make([]string, 0, len(cluster.Status.InitializingPVC)+len(cluster.Status.DanglingPVC))
	pvcs = append(pvcs, cluster.Status.InitializingPVC...)
	pvcs = append(pvcs, cluster.Status.DanglingPVC...)
	if len(pvcs) == 0 {
		return ""
	}

	for _, name := range pvcs {
		if name == cluster.Status.TargetPrimary {
			return name
		}
	}

	return pvcs[0]
}

// removeDanglingPVCs will remove dangling PVCs
func (r *ClusterReconciler) removeDanglingPVCs(ctx context.Context, cluster *apiv1.Cluster) error {
	for _, pvcName := range cluster.Status.DanglingPVC {
		var pvc corev1.PersistentVolumeClaim

		err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: pvcName}, &pvc)
		if err != nil {
			// Ignore if NotFound, otherwise report the error
			if apierrs.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("removing unneeded PVC %v: %v", pvc.Name, err)
		}

		err = r.Delete(ctx, &pvc)
		if err != nil {
			// Ignore if NotFound, otherwise report the error
			if apierrs.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("removing unneeded PVC %v: %v", pvc.Name, err)
		}
	}

	return nil
}
