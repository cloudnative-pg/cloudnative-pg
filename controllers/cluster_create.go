/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/sethvargo/go-password/password"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/expectations"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

const (
	// This is the name of the secret that we may be using to
	// download the operator image
	operatorSecretName = "postgresql-operator-pull-secret" //nolint:gosec
)

// createPostgresClusterObjects ensure that we have the required global objects
func (r *ClusterReconciler) createPostgresClusterObjects(ctx context.Context, cluster *apiv1.Cluster) error {
	err := r.createOrPatchPostgresConfigMap(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.createPostgresPKI(ctx, cluster)
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

	err = r.createServiceAccount(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.createRole(ctx, cluster)
	if err != nil {
		return err
	}

	err = r.createRoleBinding(ctx, cluster)
	if err != nil {
		return err
	}

	return nil
}

func (r *ClusterReconciler) createOrPatchPostgresConfigMap(ctx context.Context, cluster *apiv1.Cluster) error {
	var configMap corev1.ConfigMap
	if err := r.Get(ctx, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}, &configMap); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting config map: %w", err)
		}

		r.Recorder.Event(cluster, "Normal", "CreatingConfigMap", "Creating Cluster ConfigMap")
		return r.createPostgresConfigMap(ctx, cluster)
	}

	generatedConfigMap, err := specs.CreatePostgresConfigMap(cluster)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(generatedConfigMap.Data, configMap.Data) {
		// Everything fine, the two config maps are exactly the same
		return nil
	}

	r.Recorder.Event(cluster, "Normal", "UpdatingConfigMap", "Updating Cluster ConfigMap")

	// The configuration changed, and we need the patch the
	// configMap we have
	patchedConfigMap := configMap
	patchedConfigMap.Data = generatedConfigMap.Data
	if err := r.Patch(ctx, &patchedConfigMap, client.MergeFrom(&configMap)); err != nil {
		return fmt.Errorf("while patching config map: %w", err)
	}

	return nil
}

func (r *ClusterReconciler) createPostgresConfigMap(ctx context.Context, cluster *apiv1.Cluster) error {
	generatedConfigMap, err := specs.CreatePostgresConfigMap(cluster)
	if err != nil {
		return err
	}
	utils.SetAsOwnedBy(&generatedConfigMap.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&generatedConfigMap.ObjectMeta, versions.Version)

	if err := r.Create(ctx, generatedConfigMap); err != nil {
		if apierrs.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("while creating config map: %w", err)
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
		specs.SetOperatorVersion(&postgresSecret.ObjectMeta, versions.Version)
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
		specs.SetOperatorVersion(&appSecret.ObjectMeta, versions.Version)
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
	specs.SetOperatorVersion(&anyService.ObjectMeta, versions.Version)
	if err := r.Create(ctx, anyService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readService := specs.CreateClusterReadService(*cluster)
	utils.SetAsOwnedBy(&readService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&readService.ObjectMeta, versions.Version)
	if err := r.Create(ctx, readService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readOnlyService := specs.CreateClusterReadOnlyService(*cluster)
	utils.SetAsOwnedBy(&readOnlyService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&readOnlyService.ObjectMeta, versions.Version)
	if err := r.Create(ctx, readOnlyService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readWriteService := specs.CreateClusterReadWriteService(*cluster)
	utils.SetAsOwnedBy(&readWriteService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&readWriteService.ObjectMeta, versions.Version)
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
	specs.SetOperatorVersion(&targetPdb.ObjectMeta, versions.Version)

	err := r.Create(ctx, &targetPdb)
	if err != nil {
		if !apierrs.IsAlreadyExists(err) {
			log.Error(err, "Unable to create PodDisruptionBugdet", "object", targetPdb)
			return err
		}
	} else {
		r.Recorder.Event(cluster, "Normal", "CreatingPodDisruptionBugdet", "Creating Pod Disruption Budget")
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
			log.Error(err, "Unable to retrieve PodDisruptionBugdet")
			return err
		}
		return nil
	}

	r.Recorder.Event(cluster, "Normal", "DeletingPodDisruptionBugdet", "Deleting Pod Disruption Budget")

	err = r.Delete(ctx, &targetPdb)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Error(err, "Unable to delete PodDisruptionBugdet", "object", targetPdb)
			return err
		}
		return nil
	}
	return nil
}

// createServiceAccount create the service account for this PostgreSQL cluster
func (r *ClusterReconciler) createServiceAccount(ctx context.Context, cluster *apiv1.Cluster) error {
	var pullSecretNames []string

	// Try to copy the secret from the operator
	operatorPullSecret, err := r.copyPullSecretFromOperator(ctx, cluster)
	if err != nil {
		return err
	}

	if operatorPullSecret {
		pullSecretNames = append(pullSecretNames, operatorSecretName)
	}

	// Append the secrets specified by the user
	for _, secretReference := range cluster.Spec.ImagePullSecrets {
		pullSecretNames = append(pullSecretNames, secretReference.Name)
	}

	serviceAccount := specs.CreateServiceAccount(cluster.ObjectMeta, pullSecretNames)
	utils.SetAsOwnedBy(&serviceAccount.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&serviceAccount.ObjectMeta, versions.Version)

	err = r.Create(ctx, &serviceAccount)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		return err
	}

	return nil
}

// copyPullSecretFromOperator will create a secret to download the operator, if the
// operator was downloaded via a Secret.
// It will return "true" if a secret need to be used to use the operator, false
// if not
func (r *ClusterReconciler) copyPullSecretFromOperator(ctx context.Context, cluster *apiv1.Cluster) (bool, error) {
	operatorDeployNamespace := os.Getenv("OPERATOR_NAMESPACE")
	if operatorDeployNamespace == "" {
		// We are not getting started via a k8s deployment. Perhaps we are running in our development environment
		return false, nil
	}

	// Let's find the operator secret
	var operatorSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Name:      operatorSecretName,
		Namespace: operatorDeployNamespace,
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
			Name:      operatorSecretName,
		},
		Data: operatorSecret.Data,
		Type: operatorSecret.Type,
	}
	utils.SetAsOwnedBy(&secret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&secret.ObjectMeta, versions.Version)

	// Another sync loop may have already created the service. Let's check that
	if err := r.Create(ctx, &secret); err != nil && !apierrs.IsAlreadyExists(err) {
		return false, err
	}

	return true, nil
}

// createRole create the role
func (r *ClusterReconciler) createRole(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	openshift, err := utils.IsOpenShift()
	if err != nil {
		return fmt.Errorf("while creating cluster role: %w", err)
	}

	roleBinding := specs.CreateRole(*cluster, openshift)
	utils.SetAsOwnedBy(&roleBinding.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&roleBinding.ObjectMeta, versions.Version)

	err = r.Create(ctx, &roleBinding)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.Error(err, "Unable to create the Role", "object", roleBinding)
		return err
	}

	return nil
}

// createRoleBinding create the role binding
func (r *ClusterReconciler) createRoleBinding(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	roleBinding := specs.CreateRoleBinding(cluster.ObjectMeta)
	utils.SetAsOwnedBy(&roleBinding.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&roleBinding.ObjectMeta, versions.Version)

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
				r.Recorder.Eventf(cluster, "Normal", "ErrorNoBackup",
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
	specs.SetOperatorVersion(&pvcSpec.ObjectMeta, versions.Version)
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

	specs.SetOperatorVersion(&job.ObjectMeta, versions.Version)

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

	specs.SetOperatorVersion(&job.ObjectMeta, versions.Version)

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
	specs.SetOperatorVersion(&pvcSpec.ObjectMeta, versions.Version)

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

// handleDanglingPVC reattach a dangling PVC
func (r *ClusterReconciler) handleDanglingPVC(ctx context.Context, cluster *apiv1.Cluster) error {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	pvcToReattach := electPvcToReattach(cluster)
	if pvcToReattach == "" {
		return nil
	}

	if cluster.IsNodeMaintenanceWindowNotReusePVC() || cluster.Spec.Instances <= cluster.Status.Instances {
		log.Info(
			"Detected unneeded PVCs, removing them",
			"statusInstances", cluster.Status.Instances,
			"specInstances", cluster.Spec.Instances,
			"maintenanceWindow", cluster.IsNodeMaintenanceWindowInProgress(),
			"maintenanceWindowReusePVC", cluster.IsNodeMaintenanceWindowReusePVC(),
			"danglingPVCs", cluster.Status.DanglingPVC)
		return r.removeDanglingPVCs(ctx, cluster)
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

	specs.SetOperatorVersion(&pod.ObjectMeta, versions.Version)

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

// electPvcToReattach will choose a PVC between the dangling ones that should be reattached to the cluster,
// giving precedence to the target primary if between the set
func electPvcToReattach(cluster *apiv1.Cluster) string {
	if len(cluster.Status.DanglingPVC) == 0 {
		return ""
	}

	for _, name := range cluster.Status.DanglingPVC {
		if name == cluster.Status.TargetPrimary {
			return name
		}
	}

	return cluster.Status.DanglingPVC[0]
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
