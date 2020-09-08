/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/sethvargo/go-password/password"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/specs"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/versions"
)

// createPostgresClusterObjects ensure that we have the required global objects
func (r *ClusterReconciler) createPostgresClusterObjects(ctx context.Context, cluster *v1alpha1.Cluster) error {
	err := r.createPostgresConfigMap(ctx, cluster)
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

func (r *ClusterReconciler) createPostgresConfigMap(ctx context.Context, cluster *v1alpha1.Cluster) error {
	configMap := specs.CreatePostgresConfigMap(cluster)
	utils.SetAsOwnedBy(&configMap.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&configMap.ObjectMeta, versions.Version)
	if err := r.Create(ctx, configMap); err != nil {
		if apierrs.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	return nil
}

func (r *ClusterReconciler) createPostgresSecrets(ctx context.Context, cluster *v1alpha1.Cluster) error {
	postgresPassword, err := password.Generate(64, 10, 0, false, true)
	if err != nil {
		return err
	}
	appPassword, err := password.Generate(64, 10, 0, false, true)
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
		if apierrs.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	appSecret := specs.CreateSecret(
		cluster.GetApplicationSecretName(),
		cluster.Namespace,
		cluster.GetServiceReadWriteName(),
		cluster.Spec.ApplicationConfiguration.Database,
		cluster.Spec.ApplicationConfiguration.Owner,
		appPassword)
	utils.SetAsOwnedBy(&appSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&appSecret.ObjectMeta, versions.Version)
	if err := r.Create(ctx, appSecret); err != nil {
		if apierrs.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	return nil
}

func (r *ClusterReconciler) createPostgresServices(ctx context.Context, cluster *v1alpha1.Cluster) error {
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
func (r *ClusterReconciler) createPodDisruptionBudget(ctx context.Context, cluster *v1alpha1.Cluster) error {
	log := r.Log.WithName("cluster-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	targetPdb := specs.CreatePodDisruptionBudget(*cluster)
	utils.SetAsOwnedBy(&targetPdb.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&targetPdb.ObjectMeta, versions.Version)

	err := r.Create(ctx, &targetPdb)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.Error(err, "Unable to create PodDisruptionBugdet", "object", targetPdb)
		return err
	}

	return nil
}

// deletePodDisruptionBudget ensure that we delete the PDB requiring to remove one node at a time
func (r *ClusterReconciler) deletePodDisruptionBudget(ctx context.Context, cluster *v1alpha1.Cluster) error {
	// If we have a PDB, we need to delete it
	var targetPdb v1beta1.PodDisruptionBudget
	err := r.Get(ctx, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}, &targetPdb)
	if apierrs.IsNotFound(err) {
		// Nothing to do here
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "Unable to Get PDB")
	}

	err = r.Delete(ctx, &targetPdb)
	if err != nil {
		return errors.Wrap(err, "Can't delete PDB while cluster is in upgrade mode.")
	}
	return nil
}

// createServiceAccount create the service account for this PostgreSQL cluster
func (r *ClusterReconciler) createServiceAccount(ctx context.Context, cluster *v1alpha1.Cluster) error {
	log := r.Log.WithName("cluster-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	serviceAccount := specs.CreateServiceAccount(cluster.ObjectMeta)
	utils.SetAsOwnedBy(&serviceAccount.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&serviceAccount.ObjectMeta, versions.Version)

	err := r.Create(ctx, &serviceAccount)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.Error(err, "Unable to create ServiceAccount", "object", serviceAccount)
		return err
	}

	return nil
}

// createRole create the role
func (r *ClusterReconciler) createRole(ctx context.Context, cluster *v1alpha1.Cluster) error {
	log := r.Log.WithName("cluster-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	roleBinding := specs.CreateRole(*cluster)
	utils.SetAsOwnedBy(&roleBinding.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&roleBinding.ObjectMeta, versions.Version)

	err := r.Create(ctx, &roleBinding)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		log.Error(err, "Unable to create the Role", "object", roleBinding)
		return err
	}

	return nil
}

// createRoleBinding create the role binding
func (r *ClusterReconciler) createRoleBinding(ctx context.Context, cluster *v1alpha1.Cluster) error {
	log := r.Log.WithName("cluster-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

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
func (r *ClusterReconciler) generateNodeSerial(ctx context.Context, cluster *v1alpha1.Cluster) (int32, error) {
	cluster.Status.LatestGeneratedNode++
	if err := r.Status().Update(ctx, cluster); err != nil {
		return 0, err
	}

	return cluster.Status.LatestGeneratedNode, nil
}

func (r *ClusterReconciler) createPrimaryInstance(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) error {
	log := r.Log.WithName("cluster-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	// Generate a new node serial
	nodeSerial, err := r.generateNodeSerial(ctx, cluster)
	if err != nil {
		return err
	}

	// We are bootstrapping a cluster and in need to create the first node
	pod := specs.CreatePrimaryPod(*cluster, nodeSerial)
	if err := ctrl.SetControllerReference(cluster, pod, r.Scheme); err != nil {
		log.Error(err, "Unable to set the owner reference for instance")
		return err
	}

	if err = r.setPrimaryInstance(ctx, cluster, pod.Name); err != nil {
		log.Error(err, "Unable to set the primary instance name")
		return err
	}

	log.Info("Creating new Pod",
		"name", pod.Name,
		"primary", true)

	specs.SetOperatorVersion(&pod.ObjectMeta, versions.Version)

	if err = r.Create(ctx, pod); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Pod was already created, maybe the cache is stale.
			// Let's reconcile another time
			return nil
		}

		log.Error(err, "Unable to create pod", "pod", pod)
		return err
	}

	pvcSpec := specs.CreatePVC(cluster.Spec.StorageConfiguration, cluster.Name, cluster.Namespace, nodeSerial)
	utils.SetAsOwnedBy(&pvcSpec.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&pvcSpec.ObjectMeta, versions.Version)
	if err = r.Create(ctx, pvcSpec); err != nil && !apierrs.IsAlreadyExists(err) {
		log.Error(err, "Unable to create a PVC for this node", "nodeSerial", nodeSerial)
		return err
	}

	return nil
}

func (r *ClusterReconciler) joinReplicaInstance(
	ctx context.Context,
	nodeSerial int32,
	cluster *v1alpha1.Cluster,
) error {
	log := r.Log.WithName("cluster-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	var pod *corev1.Pod
	var err error

	pod = specs.JoinReplicaInstance(*cluster, nodeSerial)

	log.Info("Creating new Pod",
		"pod", pod.Name,
		"primary", false)

	if err := ctrl.SetControllerReference(cluster, pod, r.Scheme); err != nil {
		log.Error(err, "Unable to set the owner reference for joined PostgreSQL node")
		return err
	}

	specs.SetOperatorVersion(&pod.ObjectMeta, versions.Version)

	if err = r.Create(ctx, pod); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Pod was already created, maybe the cache is stale.
			// Let's reconcile another time
			log.Info("Pod already exist, maybe the cache is stale", "pod", pod.Name)
			return nil
		}

		log.Error(err, "Unable to create Pod", "pod", pod)
		return err
	}

	pvcSpec := specs.CreatePVC(cluster.Spec.StorageConfiguration, cluster.Name, cluster.Namespace, nodeSerial)
	utils.SetAsOwnedBy(&pvcSpec.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	specs.SetOperatorVersion(&pvcSpec.ObjectMeta, versions.Version)
	if err = r.Create(ctx, pvcSpec); err != nil && !apierrs.IsAlreadyExists(err) {
		log.Error(err, "Unable to create a PVC for this node", "nodeSerial", nodeSerial)
		return err
	}

	return nil
}

// handleDanglingPVC reattach a dangling PVC
func (r *ClusterReconciler) handleDanglingPVC(ctx context.Context, cluster *v1alpha1.Cluster) error {
	log := r.Log.WithName("cluster-native-postgresql").WithValues("namespace", cluster.Namespace, "name", cluster.Name)

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

	if err := r.Create(ctx, pod); err != nil {
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
// giving precedence to the target master if between the set
func electPvcToReattach(cluster *v1alpha1.Cluster) string {
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
func (r *ClusterReconciler) removeDanglingPVCs(ctx context.Context, cluster *v1alpha1.Cluster) error {
	for _, pvcName := range cluster.Status.DanglingPVC {
		var pvc corev1.PersistentVolumeClaim

		err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: pvcName}, &pvc)
		if err != nil {
			return err
		}

		err = r.Delete(ctx, &pvc)
		if err != nil {
			return err
		}
	}

	return nil
}
