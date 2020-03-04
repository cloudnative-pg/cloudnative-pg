/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"

	"github.com/sethvargo/go-password/password"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/specs"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"
)

const (
	operatorDeployNamespace = "postgresql-operator-system"
	operatorSecretName      = "postgresql-operator-pull-secret" //nolint:gosec
)

// createPostgresClusterObjects ensure that we have the required global objects
func (r *ClusterReconciler) createPostgresClusterObjects(ctx context.Context, cluster *v1alpha1.Cluster) error {
	// Ensure we have the secret that allow us to download the image of
	// PostgreSQL
	if err := r.createImagePullSecret(ctx, cluster); err != nil {
		r.Log.Error(err,
			"Can't generate the image pull secret",
			"namespace", cluster.Namespace,
			"name", cluster.Name)
		return err
	}

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

	err = r.createPodDisruptionBudget(ctx, cluster)
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

// createImagePullSecret will create a secret to download the images for Postgres BDR, if such a secret
// already exist in the namespace of the operator.
func (r *ClusterReconciler) createImagePullSecret(ctx context.Context, cluster *v1alpha1.Cluster) error {
	// Do not create ImagePullSecret if it has been specified by the user
	if len(cluster.Spec.ImagePullSecret) > 0 {
		return nil
	}

	// Let's find the operator secret
	var operatorSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Name:      operatorSecretName,
		Namespace: operatorDeployNamespace,
	}, &operatorSecret); err != nil {
		if apierrs.IsNotFound(err) {
			// There is no secret like that, probably because we are running in our development environment
			return nil
		}
		return err
	}

	// Let's create the secret with the required info
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name + "-pull-secret",
		},
		Data: operatorSecret.Data,
		Type: operatorSecret.Type,
	}
	utils.SetAsOwnedBy(&secret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)

	// Another sync loop may have already created the service. Let's check that
	if err := r.Create(ctx, &secret); err != nil && !apierrs.IsAlreadyExists(err) {
		return err
	}

	// Set the secret name in the Spec to be used when creating a Pod
	cluster.Spec.ImagePullSecret = secret.Name

	return nil
}

func (r *ClusterReconciler) createPostgresConfigMap(ctx context.Context, cluster *v1alpha1.Cluster) error {
	configMap := specs.CreatePostgresConfigMap(cluster)
	utils.SetAsOwnedBy(&configMap.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
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

	postgresSecret := specs.CreateSecret(cluster.GetSuperuserSecretName(), cluster.Namespace, "postgres", postgresPassword)
	utils.SetAsOwnedBy(&postgresSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	if err := r.Create(ctx, postgresSecret); err != nil {
		if apierrs.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	appSecret := specs.CreateSecret(cluster.GetApplicationSecretName(), cluster.Namespace,
		cluster.Spec.ApplicationConfiguration.Owner, appPassword)
	utils.SetAsOwnedBy(&appSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
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
	if err := r.Create(ctx, anyService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readService := specs.CreateClusterReadService(*cluster)
	utils.SetAsOwnedBy(&readService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	if err := r.Create(ctx, readService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	readWriteService := specs.CreateClusterReadWriteService(*cluster)
	utils.SetAsOwnedBy(&readWriteService.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	if err := r.Create(ctx, readWriteService); err != nil {
		if !apierrs.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

// createOrUpdatePodDisruptionBudget ensure that we have a PDB requiring to remove one node at a time
func (r *ClusterReconciler) createPodDisruptionBudget(ctx context.Context, cluster *v1alpha1.Cluster) error {
	targetPdb := specs.CreatePodDisruptionBudget(*cluster)
	utils.SetAsOwnedBy(&targetPdb.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)

	err := r.Create(ctx, &targetPdb)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		r.Log.Error(err, "Unable to create PodDisruptionBugdet", "object", targetPdb)
		return err
	}

	return nil
}

// createServiceAccount create the service account for this PostgreSQL cluster
func (r *ClusterReconciler) createServiceAccount(ctx context.Context, cluster *v1alpha1.Cluster) error {
	serviceAccount := specs.CreateServiceAccount(*cluster)
	utils.SetAsOwnedBy(&serviceAccount.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)

	err := r.Create(ctx, &serviceAccount)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		r.Log.Error(err, "Unable to create ServiceAccount", "object", serviceAccount)
		return err
	}

	return nil
}

// createRole create the role
func (r *ClusterReconciler) createRole(ctx context.Context, cluster *v1alpha1.Cluster) error {
	roleBinding := specs.CreateRole(*cluster)
	utils.SetAsOwnedBy(&roleBinding.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)

	err := r.Create(ctx, &roleBinding)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		r.Log.Error(err, "Unable to create the Role", "object", roleBinding)
		return err
	}

	return nil
}

// createRoleBinding create the role binding
func (r *ClusterReconciler) createRoleBinding(ctx context.Context, cluster *v1alpha1.Cluster) error {
	roleBinding := specs.CreateRoleBinding(*cluster)
	utils.SetAsOwnedBy(&roleBinding.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)

	err := r.Create(ctx, &roleBinding)
	if err != nil && !apierrs.IsAlreadyExists(err) {
		r.Log.Error(err, "Unable to create the ServiceAccount", "object", roleBinding)
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
	nodeSerial int32,
	cluster *v1alpha1.Cluster,
) error {
	// We are bootstrapping a cluster and in need to create the first node
	var pod *corev1.Pod
	var err error

	r.Log.Info("Creating new PostgreSQL primary instance", "namespace", cluster.Namespace, "name", cluster.Name)

	pod = specs.CreatePrimaryPod(*cluster, nodeSerial)
	if err := ctrl.SetControllerReference(cluster, pod, r.Scheme); err != nil {
		r.Log.Error(err, "Unable to set the owner reference for instance")
		return err
	}

	if err = r.setPrimaryInstance(ctx, cluster, pod.Name); err != nil {
		r.Log.Error(err, "Unable to set the primary instance name")
		return err
	}

	if err = r.Create(ctx, pod); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Pod was already created, maybe the cache is stale.
			// Let's reconcile another time
			return nil
		}

		r.Log.Error(err, "Unable to create pod", "pod", pod)
		return err
	}

	if cluster.IsUsingPersistentStorage() {
		pvcSpec := specs.CreatePVC(*cluster.Spec.StorageConfiguration, cluster.Name, cluster.Namespace, nodeSerial)
		utils.SetAsOwnedBy(&pvcSpec.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
		if err = r.Create(ctx, pvcSpec); err != nil && !apierrs.IsAlreadyExists(err) {
			r.Log.Error(err, "Unable to create a PVC for this node", "nodeSerial", nodeSerial)
			return err
		}
	}

	return nil
}

func (r *ClusterReconciler) joinReplicaInstance(
	ctx context.Context,
	nodeSerial int32,
	cluster *v1alpha1.Cluster,
) (ctrl.Result, error) {
	var pod *corev1.Pod
	var err error

	pod = specs.JoinReplicaInstance(*cluster, nodeSerial)

	r.Log.Info("Creating new Pod",
		"name", cluster.Name,
		"namespace", cluster.Namespace,
		"podName", pod.Name)

	if err := ctrl.SetControllerReference(cluster, pod, r.Scheme); err != nil {
		r.Log.Error(err, "Unable to set the owner reference for joined PostgreSQL node")
		return ctrl.Result{}, err
	}

	if err = r.Create(ctx, pod); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// This Pod was already created, maybe the cache is stale.
			// Let's reconcile another time
			r.Log.Info("Pod already exist, maybe the cache is stale", "pod", pod.Name)
			return ctrl.Result{}, nil
		}

		r.Log.Error(err, "Unable to create Pod", "pod", pod)
		return ctrl.Result{}, err
	}

	if cluster.IsUsingPersistentStorage() {
		pvcSpec := specs.CreatePVC(*cluster.Spec.StorageConfiguration, cluster.Name, cluster.Namespace, nodeSerial)
		pvcSpec.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion: "v1",
				Kind:       "Pod",
				Name:       cluster.Name,
				UID:        cluster.UID,
			},
		})

		if err = r.Create(ctx, pvcSpec); err != nil && !apierrs.IsAlreadyExists(err) {
			r.Log.Error(err, "Unable to create a PVC for this node", "nodeSerial", nodeSerial)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
