/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/sethvargo/go-password/password"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/expectations"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

// createPostgresClusterObjects ensure that we have the required global objects
func (r *ClusterReconciler) createPostgresClusterObjects(ctx context.Context, cluster *apiv1.Cluster) error {
	err := r.createPostgresPKI(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.createPostgresSecrets(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.createPostgresServices(ctx, cluster)
	if err != nil {
		return err
	}

	// The PDB should not be enforced if we are inside a maintenance
	// windows and we chose to don't allocate more space
	if cluster.IsNodeMaintenanceWindowReusePVC() {
		err = r.deletePodDisruptionBudget(ctx, cluster)
	} else {
		err = r.createPodDisruptionBudget(ctx, cluster)
	}
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

	return nil
}

func (r *ClusterReconciler) createPostgresSecrets(ctx context.Context, cluster *apiv1.Cluster) error {
	if cluster.Spec.SuperuserSecret == nil ||
		cluster.Spec.SuperuserSecret.Name == "" {
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
		utils.SetAsOwnedBy(&postgresSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
		utils.SetOperatorVersion(&postgresSecret.ObjectMeta, versions.Version)
		utils.InheritAnnotations(&postgresSecret.ObjectMeta, cluster.Annotations, configuration.Current)
		utils.InheritLabels(&postgresSecret.ObjectMeta, cluster.Labels, configuration.Current)
		if err := r.Create(ctx, postgresSecret); err != nil {
			if !apierrs.IsAlreadyExists(err) {
				return err
			}
		}
	}

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
		utils.SetAsOwnedBy(&appSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
		utils.SetOperatorVersion(&appSecret.ObjectMeta, versions.Version)
		utils.InheritAnnotations(&appSecret.ObjectMeta, cluster.Annotations, configuration.Current)
		utils.InheritLabels(&appSecret.ObjectMeta, cluster.Labels, configuration.Current)
		if err := r.Create(ctx, appSecret); err != nil {
			if !apierrs.IsAlreadyExists(err) {
				return err
			}
		}
	}

	return nil
}

func (r *ClusterReconciler) createPostgresServices(ctx context.Context, cluster *apiv1.Cluster) error {
	anyService := specs.CreateClusterAnyService(*cluster)
	utils.SetAsOwnedBy(&anyService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&anyService.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&anyService.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&anyService.ObjectMeta, cluster.Labels, configuration.Current)
	if err := r.Create(ctx, anyService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readService := specs.CreateClusterReadService(*cluster)
	utils.SetAsOwnedBy(&readService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&readService.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&readService.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&readService.ObjectMeta, cluster.Labels, configuration.Current)
	if err := r.Create(ctx, readService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readOnlyService := specs.CreateClusterReadOnlyService(*cluster)
	utils.SetAsOwnedBy(&readOnlyService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&readOnlyService.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&readOnlyService.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&readOnlyService.ObjectMeta, cluster.Labels, configuration.Current)
	if err := r.Create(ctx, readOnlyService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readWriteService := specs.CreateClusterReadWriteService(*cluster)
	utils.SetAsOwnedBy(&readWriteService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&readWriteService.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&readWriteService.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&readWriteService.ObjectMeta, cluster.Labels, configuration.Current)
	if err := r.Create(ctx, readWriteService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

// createOrUpdatePodDisruptionBudget ensure that we have a PDB requiring to remove one node at a time
func (r *ClusterReconciler) createPodDisruptionBudget(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	targetPdb := specs.CreatePodDisruptionBudget(*cluster)
	utils.SetAsOwnedBy(&targetPdb.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&targetPdb.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&targetPdb.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&targetPdb.ObjectMeta, cluster.Labels, configuration.Current)

	err := r.Create(ctx, &targetPdb)
	if err != nil {
		if !apierrs.IsAlreadyExists(err) {
			log.Error(err, "Unable to create PodDisruptionBudget", "object", targetPdb)
			return err
		}
	} else {
		r.Recorder.Event(cluster, "Normal", "CreatingPodDisruptionBudget", "Creating Pod Disruption Budget")
	}

	return nil
}

// deletePodDisruptionBudget ensure that we delete the PDB requiring to remove one node at a time
func (r *ClusterReconciler) deletePodDisruptionBudget(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// If we have a PDB, we need to delete it
	var targetPdb v1beta1.PodDisruptionBudget
	err := r.Get(ctx, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}, &targetPdb)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Error(err, "Unable to retrieve PodDisruptionBudget")
			return err
		}
		return nil
	}

	r.Recorder.Event(cluster, "Normal", "DeletingPodDisruptionBudget", "Deleting Pod Disruption Budget")

	err = r.Delete(ctx, &targetPdb)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Error(err, "Unable to delete PodDisruptionBudget", "object", targetPdb)
			return err
		}
		return nil
	}
	return nil
}

// createOrPatchServiceAccount create or synchronize the ServiceAccount used by the
// cluster with the latest cluster specification
func (r *ClusterReconciler) createOrPatchServiceAccount(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

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
		log.Error(err, "Cannot detect if a ServiceAccount need to be refreshed or not, refreshing it",
			"serviceAccount", sa)
		serviceAccountAligned = false
	}

	if serviceAccountAligned {
		return nil
	}

	generatedServiceAccount, err := r.generateServiceAccountForCluster(cluster, generatedPullSecretNames)
	if err != nil {
		return fmt.Errorf("while generating service accouynt: %w", err)
	}

	r.Recorder.Event(cluster, "Normal", "UpdatingServiceAccount", "Updating ServiceAccount")
	patchedServiceAccount := sa
	patchedServiceAccount.Annotations = generatedServiceAccount.Annotations
	patchedServiceAccount.ImagePullSecrets = generatedServiceAccount.ImagePullSecrets
	if err := r.Patch(ctx, &patchedServiceAccount, client.MergeFrom(&sa)); err != nil {
		return fmt.Errorf("while patching service account: %w", err)
	}

	return nil
}

// createServiceAccount create the service account for this PostgreSQL cluster
func (r *ClusterReconciler) createServiceAccount(ctx context.Context, cluster *apiv1.Cluster) error {
	generatedPullSecretNames, err := r.generateServiceAccountPullSecretsNames(ctx, cluster)
	if err != nil {
		return fmt.Errorf("while generating pull secret names: %w", err)
	}

	serviceAccount, err := r.generateServiceAccountForCluster(cluster, generatedPullSecretNames)
	if err != nil {
		return fmt.Errorf("while creating new ServiceAccount: %w", err)
	}

	err = r.Create(ctx, serviceAccount)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		return err
	}

	return nil
}

// generateServiceAccountForCluster create a serviceAccount entity for the cluster
func (r *ClusterReconciler) generateServiceAccountForCluster(
	cluster *apiv1.Cluster, pullSecretNames []string) (*corev1.ServiceAccount, error) {
	serviceAccount, err := specs.CreateServiceAccount(cluster.ObjectMeta, pullSecretNames)
	if err != nil {
		return nil, err
	}
	utils.SetAsOwnedBy(&serviceAccount.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&serviceAccount.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&serviceAccount.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&serviceAccount.ObjectMeta, cluster.Labels, configuration.Current)

	return serviceAccount, nil
}

// generateServiceAccountPullSecretsNames extract the list of pull secret names given
// the cluster configuration
func (r *ClusterReconciler) generateServiceAccountPullSecretsNames(
	ctx context.Context, cluster *apiv1.Cluster) ([]string, error) {
	pullSecretNames := make([]string, 0, len(cluster.Spec.ImagePullSecrets))

	// Try to copy the secret from the operator
	operatorPullSecret, err := r.copyPullSecretFromOperator(ctx, cluster)
	if err != nil {
		return nil, err
	}

	if operatorPullSecret {
		pullSecretNames = append(pullSecretNames, configuration.Current.OperatorPullSecretName)
	}

	// Append the secrets specified by the user
	for _, secretReference := range cluster.Spec.ImagePullSecrets {
		pullSecretNames = append(pullSecretNames, secretReference.Name)
	}

	return pullSecretNames, nil
}

// copyPullSecretFromOperator will create a secret to download the operator, if the
// operator was downloaded via a Secret.
// It will return "true" if a secret need to be used to use the operator, false
// if not
func (r *ClusterReconciler) copyPullSecretFromOperator(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	if configuration.Current.OperatorNamespace == "" {
		// We are not getting started via a k8s deployment. Perhaps we are running in our development environment
		return false, nil
	}

	// Let's find the operator secret
	var operatorSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Name:      configuration.Current.OperatorPullSecretName,
		Namespace: configuration.Current.OperatorNamespace,
	}, &operatorSecret); err != nil {
		if apierrs.IsNotFound(err) {
			// There is no secret like that, probably because we are running in our development environment
			return false, nil
		}
		return false, err
	}

	// Let's create the secret with the required info
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      configuration.Current.OperatorPullSecretName,
		},
		Data: operatorSecret.Data,
		Type: operatorSecret.Type,
	}
	utils.SetAsOwnedBy(&secret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&secret.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&secret.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&secret.ObjectMeta, cluster.Labels, configuration.Current)

	// Another sync loop may have already created the service. Let's check that
	if err := r.Create(ctx, &secret); err != nil && !apierrs.IsAlreadyExists(err) {
		return false, err
	}

	return true, nil
}

// createOrPatchRole ensure that the required role for the instance manager exists and
// contains the right rules
func (r *ClusterReconciler) createOrPatchRole(ctx context.Context, cluster *apiv1.Cluster) error {
	var role rbacv1.Role
	if err := r.Get(ctx, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}, &role); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting role: %w", err)
		}

		r.Recorder.Event(cluster, "Normal", "CreatingRole", "Creating Cluster Role")
		return r.createRole(ctx, cluster)
	}

	generatedRole := specs.CreateRole(*cluster)
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

// createRole create the role
func (r *ClusterReconciler) createRole(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	role := specs.CreateRole(*cluster)
	utils.SetAsOwnedBy(&role.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&role.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&role.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&role.ObjectMeta, cluster.Labels, configuration.Current)

	err := r.Create(ctx, &role)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.Error(err, "Unable to create the Role", "object", role)
		return err
	}

	return nil
}

// createRoleBinding create the role binding
func (r *ClusterReconciler) createRoleBinding(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	roleBinding := specs.CreateRoleBinding(cluster.ObjectMeta)
	utils.SetAsOwnedBy(&roleBinding.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&roleBinding.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&roleBinding.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&roleBinding.ObjectMeta, cluster.Labels, configuration.Current)

	err := r.Create(ctx, &roleBinding)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.Error(err, "Unable to create the ServiceAccount", "object", roleBinding)
		return err
	}

	return nil
}

// generateNodeSerial extract the first free node serial in this pods
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
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

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
		log.Info("refusing to create the primary instance while the latest generated serial is not zero",
			"latestGeneratedNode", cluster.Status.LatestGeneratedNode)
		return ctrl.Result{}, nil
	}

	var backup apiv1.Backup
	if cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.Recovery != nil {
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

				// Missing backup
				log.Info("Missing backup object, can't continue full recovery",
					"backup", cluster.Spec.Bootstrap.Recovery.Backup)
				return ctrl.Result{
					Requeue:      true,
					RequeueAfter: time.Minute,
				}, nil
			}

			return ctrl.Result{}, fmt.Errorf("cannot get the backup object: %w", err)
		}
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
			log.Info("The size specified for the cluster is not valid",
				"size",
				cluster.Spec.StorageConfiguration.Size)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, err
	}

	// Retrieve the cluster key
	key := expectations.KeyFunc(cluster)

	// We expect the creation of a PVC
	if err := r.pvcExpectations.ExpectCreations(key, 1); err != nil {
		log.Error(err, "Unable to set pvcExpectations", "key", key, "adds", 1)
	}

	utils.SetAsOwnedBy(&pvcSpec.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&pvcSpec.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&pvcSpec.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&pvcSpec.ObjectMeta, cluster.Labels, configuration.Current)
	if err = r.Create(ctx, pvcSpec); err != nil && !apierrs.IsAlreadyExists(err) {
		// We cannot observe a creation if it was not accepted by the server
		r.pvcExpectations.CreationObserved(key)

		log.Error(err, "Unable to create a PVC for this node", "nodeSerial", nodeSerial)
		return ctrl.Result{}, err
	}

	// We are bootstrapping a cluster and in need to create the first node
	var job *batchv1.Job

	if cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.Recovery != nil {
		r.Recorder.Event(cluster, "Normal", "CreatingInstance", "Primary instance (from backup)")
		job = specs.CreatePrimaryJobViaRecovery(*cluster, nodeSerial, &backup)
	} else {
		r.Recorder.Event(cluster, "Normal", "CreatingInstance", "Primary instance (initdb)")
		job = specs.CreatePrimaryJobViaInitdb(*cluster, nodeSerial)
	}

	if err := ctrl.SetControllerReference(cluster, job, r.Scheme); err != nil {
		log.Error(err, "Unable to set the owner reference for instance")
		return ctrl.Result{}, err
	}

	if err = r.setPrimaryInstance(ctx, cluster, fmt.Sprintf("%v-%v", cluster.Name, nodeSerial)); err != nil {
		log.Error(err, "Unable to set the primary instance name")
		return ctrl.Result{}, err
	}

	err = r.RegisterPhase(ctx, cluster, apiv1.PhaseFirstPrimary,
		fmt.Sprintf("Creating primary instance %v", job.Name))
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Creating new Job",
		"name", job.Name,
		"primary", true)

	utils.SetOperatorVersion(&job.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&job.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritAnnotations(&job.Spec.Template.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&job.ObjectMeta, cluster.Labels, configuration.Current)
	utils.InheritLabels(&job.Spec.Template.ObjectMeta, cluster.Labels, configuration.Current)

	// We expect the creation of a Job
	if err := r.jobExpectations.ExpectCreations(key, 1); err != nil {
		log.Error(err, "Unable to set jobExpectations", "key", key, "adds", 1)
	}

	if err = r.Create(ctx, job); err != nil {
		// We cannot observe a creation if it was not accepted by the server
		r.jobExpectations.CreationObserved(key)

		if apierrs.IsAlreadyExists(err) {
			// This Job was already created, maybe the cache is stale.
			return ctrl.Result{}, nil
		}

		log.Error(err, "Unable to create job", "job", job)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ClusterReconciler) joinReplicaInstance(
	ctx context.Context,
	nodeSerial int32,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	var job *batchv1.Job
	var err error

	job = specs.JoinReplicaInstance(*cluster, nodeSerial)

	log.Info("Creating new Job",
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
		log.Error(err, "Unable to set the owner reference for joined PostgreSQL node")
		return ctrl.Result{}, err
	}

	utils.SetOperatorVersion(&job.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&job.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritAnnotations(&job.Spec.Template.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&job.ObjectMeta, cluster.Labels, configuration.Current)
	utils.InheritLabels(&job.Spec.Template.ObjectMeta, cluster.Labels, configuration.Current)

	// Retrieve the cluster key
	key := expectations.KeyFunc(cluster)

	// We expect the creation of a Job
	if err := r.jobExpectations.ExpectCreations(key, 1); err != nil {
		log.Error(err, "Unable to set jobExpectations", "key", key, "adds", 1)
	}

	if err = r.Create(ctx, job); err != nil {
		// We cannot observe a creation if it was not accepted by the server
		r.jobExpectations.CreationObserved(key)

		if apierrs.IsAlreadyExists(err) {
			// This Job was already created, maybe the cache is stale.
			log.Info("Job already exist, maybe the cache is stale", "pod", job.Name)
			return ctrl.Result{}, nil
		}

		log.Error(err, "Unable to create Job", "job", job)
		return ctrl.Result{}, err
	}

	pvcSpec, err := specs.CreatePVC(cluster.Spec.StorageConfiguration, cluster.Name, cluster.Namespace, nodeSerial)
	if err != nil {
		if err == specs.ErrorInvalidSize {
			// This error should have been caught by the validating
			// webhook, but since we are here the user must have disabled server-side
			// validation and we must react.
			log.Info("The size specified for the cluster is not valid",
				"size",
				cluster.Spec.StorageConfiguration.Size)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		return ctrl.Result{}, fmt.Errorf("unable to create a PVC spec for node with serial %v: %w", nodeSerial, err)
	}

	utils.SetAsOwnedBy(&pvcSpec.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	utils.SetOperatorVersion(&pvcSpec.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&pvcSpec.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&pvcSpec.ObjectMeta, cluster.Labels, configuration.Current)

	// We expect the creation of a PVC
	if err := r.pvcExpectations.ExpectCreations(key, 1); err != nil {
		log.Error(err, "Unable to set pvcExpectations", "key", key, "adds", 1)
	}

	if err = r.Create(ctx, pvcSpec); err != nil && !apierrs.IsAlreadyExists(err) {
		// We cannot observe a creation if it was not accepted by the server
		r.pvcExpectations.CreationObserved(key)

		log.Error(err, "Unable to create a PVC for this node", "nodeSerial", nodeSerial)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcilePVCs reattach a dangling PVC
func (r *ClusterReconciler) reconcilePVCs(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	pvcToReattach := electPvcToReattach(cluster)
	if pvcToReattach == "" {
		return nil
	}

	if len(cluster.Status.DanglingPVC) > 0 {
		if cluster.IsNodeMaintenanceWindowNotReusePVC() || cluster.Spec.Instances <= cluster.Status.Instances {
			log.Info(
				"Detected unneeded PVCs, removing them",
				"statusInstances", cluster.Status.Instances,
				"specInstances", cluster.Spec.Instances,
				"maintenanceWindow", cluster.Spec.NodeMaintenanceWindow,
				"danglingPVCs", cluster.Status.DanglingPVC)
			return r.removeDanglingPVCs(ctx, cluster)
		}
	}

	pvc := corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKey{Name: pvcToReattach, Namespace: cluster.Namespace}, &pvc)
	if err != nil {
		return fmt.Errorf("error while reattaching PVC: %v", err)
	}

	nodeSerial, err := specs.GetNodeSerial(pvc.ObjectMeta)
	if err != nil {
		return fmt.Errorf("cannot detect serial from PVC %v: %v", pvc.Name, err)
	}

	pod := specs.PodWithExistingStorage(*cluster, int32(nodeSerial))

	log.Info("Creating new Pod to reattach a PVC",
		"pod", pod.Name,
		"pvc", pvc.Name)

	if err := ctrl.SetControllerReference(cluster, pod, r.Scheme); err != nil {
		log.Error(err, "Unable to set the owner reference for the Pod")
		return err
	}

	utils.SetOperatorVersion(&pod.ObjectMeta, versions.Version)
	utils.InheritAnnotations(&pod.ObjectMeta, cluster.Annotations, configuration.Current)
	utils.InheritLabels(&pod.ObjectMeta, cluster.Labels, configuration.Current)

	// We expect the creation of a Pod
	if err := r.podExpectations.ExpectCreations(expectations.KeyFunc(cluster), 1); err != nil {
		log.Error(err, "Unable to set podExpectations", "key", expectations.KeyFunc(cluster), "adds", 1)
	}

	if err := r.Create(ctx, pod); err != nil {
		// We cannot observe a creation if it was not accepted by the server
		r.podExpectations.CreationObserved(expectations.KeyFunc(cluster))

		if apierrs.IsAlreadyExists(err) {
			// This Pod was already created, maybe the cache is stale.
			// Let's reconcile another time
			log.Info("Pod already exist, maybe the cache is stale", "pod", pod.Name)
			return nil
		}

		log.Error(err, "Unable to create Pod", "pod", pod)
		return err
	}

	return nil
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

		// We expect the deletion of a PVC
		if err := r.pvcExpectations.ExpectDeletions(expectations.KeyFunc(cluster), 1); err != nil {
			log.Error(err, "Unable to set podExpectations", "key", expectations.KeyFunc(cluster), "dels", 1)
		}

		err = r.Delete(ctx, &pvc)
		if err != nil {
			// We cannot observe a deletion if it was not accepted by the server
			r.podExpectations.DeletionObserved(expectations.KeyFunc(cluster))

			// Ignore if NotFound, otherwise report the error
			if apierrs.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("removing unneeded PVC %v: %v", pvc.Name, err)
		}
	}

	return nil
}
