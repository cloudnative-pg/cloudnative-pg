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
	"reflect"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// updateOwnedObjects ensure that we have the required objects
func (r *PoolerReconciler) updateOwnedObjects(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	if err := r.updateServiceAccount(ctx, pooler, resources); err != nil {
		return err
	}

	if err := r.updateRBAC(ctx, pooler, resources); err != nil {
		return err
	}

	if err := r.updateDeployment(ctx, pooler, resources); err != nil {
		return err
	}

	if err := r.reconcileService(ctx, pooler, resources); err != nil {
		return err
	}

	return createOrPatchPodMonitor(ctx, r.Client, r.DiscoveryClient, pgbouncer.NewPoolerPodMonitorManager(pooler))
}

// updateDeployment update the deployment or create it when needed
//
//nolint:dupl
func (r *PoolerReconciler) updateDeployment(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)

	generatedDeployment, err := pgbouncer.Deployment(pooler, resources.Cluster)
	if err != nil {
		return err
	}

	switch {
	case resources.Deployment == nil:
		// Create a new deployment
		if err := ctrl.SetControllerReference(pooler, generatedDeployment, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating deployment")
		err := r.Create(ctx, generatedDeployment)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.Deployment = generatedDeployment
		return nil

	case resources.Deployment != nil:
		currentVersion := resources.Deployment.Annotations[utils.PoolerSpecHashAnnotationName]
		updatedVersion := generatedDeployment.Annotations[utils.PoolerSpecHashAnnotationName]
		if currentVersion == updatedVersion {
			// Everything fine, the two deployments are using the
			// same specifications
			return nil
		}

		deployment := resources.Deployment.DeepCopy()
		deployment.Spec.Replicas = generatedDeployment.Spec.Replicas

		// If the Pooler is annotated with `cnpg.io/reconcilePodSpec: disabled`,
		// we avoid patching the deployment spec, except the number replicas
		if !utils.IsPodSpecReconciliationDisabled(&pooler.ObjectMeta) {
			deployment.Spec = generatedDeployment.Spec
		}

		utils.MergeObjectsMetadata(deployment, generatedDeployment)

		contextLog.Info("Updating deployment")
		err = r.Patch(ctx, deployment, client.MergeFrom(resources.Deployment))
		if err != nil {
			return err
		}

		resources.Deployment = deployment
	}

	return nil
}

// reconcileService update or create the pgbouncer service as needed
func (r *PoolerReconciler) reconcileService(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)
	expectedService, err := pgbouncer.Service(pooler, resources.Cluster)
	if err != nil {
		return err
	}
	if err := ctrl.SetControllerReference(pooler, expectedService, r.Scheme); err != nil {
		return err
	}

	if resources.Service == nil {
		contextLog.Info("Creating the service")
		err := r.Create(ctx, expectedService)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.Service = expectedService
		return nil
	}

	patchedService := resources.Service.DeepCopy()
	patchedService.Spec = expectedService.Spec
	utils.MergeObjectsMetadata(patchedService, expectedService)

	if reflect.DeepEqual(patchedService.ObjectMeta, resources.Service.ObjectMeta) &&
		reflect.DeepEqual(patchedService.Spec, resources.Service.Spec) {
		return nil
	}

	contextLog.Info("Updating the service metadata")

	return r.Patch(ctx, patchedService, client.MergeFrom(resources.Service))
}

// updateRBAC update or create the pgbouncer RBAC
func (r *PoolerReconciler) updateRBAC(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)

	role := pgbouncer.Role(pooler)
	if resources.Role == nil {
		if err := ctrl.SetControllerReference(pooler, role, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating role")
		err := r.Create(ctx, role)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.Role = role
	} else if !reflect.DeepEqual(role.Rules, resources.Role.Rules) {
		contextLog.Info("Updating role")
		resources.Role.Rules = role.Rules
		if err := r.Update(ctx, resources.Role); err != nil {
			return err
		}
	}

	roleBinding := pgbouncer.RoleBinding(pooler, pooler.GetServiceAccountName())
	if resources.RoleBinding == nil {
		if err := ctrl.SetControllerReference(pooler, &roleBinding, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating role binding")
		if err := r.Create(ctx, &roleBinding); err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.RoleBinding = &roleBinding
	} else if !reflect.DeepEqual(roleBinding.Subjects, resources.RoleBinding.Subjects) ||
		!reflect.DeepEqual(roleBinding.RoleRef, resources.RoleBinding.RoleRef) {
		resources.RoleBinding.RoleRef = roleBinding.RoleRef
		resources.RoleBinding.Subjects = roleBinding.Subjects
		if err := r.Update(ctx, resources.RoleBinding); err != nil {
			return err
		}
	}

	return nil
}

// updateServiceAccount update or create the pgbouncer ServiceAccount
// The goal of this method is to make sure that:
//
//   - the ServiceAccount exits
//   - it contains the ImagePullSecret if required
//
// # Any other property of the ServiceAccount is preserved
//
// If a custom ServiceAccount is specified via serviceAccountName,
// this method validates that it exists but does not create or modify it.
func (r *PoolerReconciler) updateServiceAccount(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)

	// If a custom ServiceAccount is specified, validate it exists and return
	if pooler.Spec.ServiceAccountName != nil {
		return r.validateExistingServiceAccount(ctx, pooler)
	}

	pullSecretName, err := r.ensureServiceAccountPullSecret(ctx, pooler, configuration.Current)
	if err != nil {
		return err
	}

	if resources.ServiceAccount == nil {
		serviceAccount := pgbouncer.ServiceAccount(pooler)
		ensureServiceAccountHaveImagePullSecret(serviceAccount, pullSecretName)
		contextLog.Info("Creating service account")
		if err := ctrl.SetControllerReference(pooler, serviceAccount, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, serviceAccount); err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.ServiceAccount = serviceAccount
		return nil
	}

	origServiceAccount := resources.ServiceAccount.DeepCopy()
	ensureServiceAccountHaveImagePullSecret(resources.ServiceAccount, pullSecretName)
	if !reflect.DeepEqual(origServiceAccount, resources.ServiceAccount) {
		contextLog.Info("Updating service account")
		if err := r.Patch(ctx, resources.ServiceAccount, client.MergeFrom(origServiceAccount)); err != nil {
			return err
		}
	}

	return nil
}

// validateExistingServiceAccount checks if the specified ServiceAccount exists for a Pooler
func (r *PoolerReconciler) validateExistingServiceAccount(ctx context.Context, pooler *apiv1.Pooler) error {
	var sa corev1.ServiceAccount
	err := r.Get(ctx, client.ObjectKey{
		Name:      pooler.GetServiceAccountName(),
		Namespace: pooler.Namespace,
	}, &sa)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Recorder.Eventf(pooler, "Warning", "ServiceAccountNotFound",
				"Specified ServiceAccount %q not found in namespace %q",
				pooler.GetServiceAccountName(), pooler.Namespace)
			return fmt.Errorf("serviceAccount %q not found: %w", pooler.GetServiceAccountName(), err)
		}
		return fmt.Errorf("while validating existing service account: %w", err)
	}

	return nil
}

// ensureServiceAccountPullSecret will create the image pull secret in the pooler namespace
// The returned poolerSecretName can be an empty string if no pull secret is required
func (r *PoolerReconciler) ensureServiceAccountPullSecret(
	ctx context.Context,
	pooler *apiv1.Pooler,
	conf *configuration.Data,
) (string, error) {
	contextLog := log.FromContext(ctx).WithName("pooler_pull_secret")
	if conf.OperatorNamespace == "" {
		// We are not getting started via a k8s deployment. Perhaps we are running in our development environment
		return "", nil
	}

	// no pull secret name, there is nothing to do
	if conf.OperatorPullSecretName == "" {
		return "", nil
	}

	// Let's find the operator secret
	var operatorSecret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Name:      conf.OperatorPullSecretName,
		Namespace: conf.OperatorNamespace,
	}, &operatorSecret); err != nil {
		if apierrs.IsNotFound(err) {
			// There is no secret like that, probably because we are running in our development environment
			return "", nil
		}
		return "", err
	}

	pullSecretName := fmt.Sprintf("%s-pull", pooler.Name)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pooler.Namespace,
			// we change name to avoid ownership conflicts with the managed secret from cnpg cluster
			Name: pullSecretName,
		},
		Data: operatorSecret.Data,
		Type: operatorSecret.Type,
	}

	if err := ctrl.SetControllerReference(pooler, secret, r.Scheme); err != nil {
		return "", err
	}

	var remoteSecret corev1.Secret

	err := r.Get(ctx, client.ObjectKeyFromObject(secret), &remoteSecret)
	if apierrs.IsNotFound(err) {
		contextLog.Debug("creating image pull secret for service account")
		return pullSecretName, r.Create(ctx, secret)
	}
	if err != nil {
		return "", fmt.Errorf("while fetching remote pull secret: %w", err)
	}

	// we reconcile only if the secret is owned by us
	if _, isOwned := isOwnedByPoolerKind(&remoteSecret); !isOwned {
		return pullSecretName, nil
	}
	if reflect.DeepEqual(remoteSecret.Data, secret.Data) && reflect.DeepEqual(remoteSecret.Type, secret.Type) {
		return pullSecretName, nil
	}
	patchedSecret := remoteSecret.DeepCopy()
	patchedSecret.Data = secret.Data
	patchedSecret.Type = secret.Type

	contextLog.Info("patching the pull secret")
	return pullSecretName, r.Patch(ctx, patchedSecret, client.MergeFrom(&remoteSecret))
}

func ensureServiceAccountHaveImagePullSecret(serviceAccount *corev1.ServiceAccount, pullSecretName string) {
	if serviceAccount == nil || pullSecretName == "" {
		return
	}

	// If the secret is already in the serviceAccount we are done
	for _, item := range serviceAccount.ImagePullSecrets {
		if item.Name == pullSecretName {
			return
		}
	}

	// Add the secret in the service account
	serviceAccount.ImagePullSecrets = append(serviceAccount.ImagePullSecrets, corev1.LocalObjectReference{
		Name: pullSecretName,
	})
}
