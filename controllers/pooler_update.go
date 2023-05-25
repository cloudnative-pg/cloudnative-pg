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

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/hash"
)

// updateOwnedObjects ensure that we have the required objects
func (r *PoolerReconciler) updateOwnedObjects(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	if err := r.updateDeployment(ctx, pooler, resources); err != nil {
		return err
	}

	if err := r.updateServiceAccount(ctx, pooler, resources); err != nil {
		return err
	}

	if err := r.updateRBAC(ctx, pooler, resources); err != nil {
		return err
	}

	if err := r.updateService(ctx, pooler, resources); err != nil {
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

	deployment, err := pgbouncer.Deployment(pooler, resources.Cluster)
	if err != nil {
		return err
	}

	switch {
	case resources.Deployment == nil:
		// Create a new deployment
		if err := ctrl.SetControllerReference(pooler, deployment, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating deployment")
		err := r.Create(ctx, deployment)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.Deployment = deployment
		return nil

	case resources.Deployment != nil:
		currentVersion := resources.Deployment.Annotations[pgbouncer.PgbouncerPoolerSpecHash]
		updatedVersion, err := hash.ComputeHash(pooler.Spec)
		if err != nil {
			return err
		}

		if currentVersion == updatedVersion {
			// Everything fine, the two deployments are using the
			// same specifications
			return nil
		}

		updatedDeployment := resources.Deployment.DeepCopy()
		updatedDeployment.Spec = deployment.Spec
		if updatedDeployment.Annotations == nil {
			updatedDeployment.Annotations = make(map[string]string)
		}
		updatedDeployment.Annotations[pgbouncer.PgbouncerPoolerSpecHash] = updatedVersion

		contextLog.Info("Updating deployment")
		err = r.Patch(ctx, updatedDeployment, client.MergeFrom(resources.Deployment))
		if err != nil {
			return err
		}

		resources.Deployment = updatedDeployment
	}

	return nil
}

// updateService update or create the pgbouncer service as needed
//
//nolint:dupl
func (r *PoolerReconciler) updateService(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)

	if resources.Service == nil {
		service := pgbouncer.Service(pooler)
		if err := ctrl.SetControllerReference(pooler, service, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating service")
		err := r.Create(ctx, service)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.Service = service
		return nil
	}

	return nil
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

	if resources.RoleBinding == nil {
		roleBinding := pgbouncer.RoleBinding(pooler)
		if err := ctrl.SetControllerReference(pooler, &roleBinding, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating role binding")
		err := r.Create(ctx, &roleBinding)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.RoleBinding = &roleBinding
	}

	return nil
}

// updateServiceAccount update or create the pgbouncer ServiceAccount
// The goal of this method is to make sure that:
//
//   - the ServiceAccount exits
//   - it contains the ImagePullSecret if required
//
// Any other property of the ServiceAccount is preserved
func (r *PoolerReconciler) updateServiceAccount(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)

	pullSecretName, err := r.ensureServiceAccountPullSecret(ctx, pooler)
	if err != nil {
		return err
	}

	if resources.ServiceAccount == nil {
		serviceAccount := pgbouncer.ServiceAccount(pooler)
		ensureServiceAccountHaveImagePullSecret(resources.ServiceAccount, pullSecretName)
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

// ensureServiceAccountPullSecret will create the image pull secret in the pooler namespace
// The returned poolerSecretName can be an empty string if no pull secret is required
func (r *PoolerReconciler) ensureServiceAccountPullSecret(
	ctx context.Context,
	pooler *apiv1.Pooler,
) (pullSecretName string, err error) {
	contextLog := log.FromContext(ctx)
	if configuration.Current.OperatorNamespace == "" {
		// We are not getting started via a k8s deployment. Perhaps we are running in our development environment
		return "", nil
	}

	// no pull secret name, there is nothing to do
	if configuration.Current.OperatorPullSecretName == "" {
		return "", nil
	}

	// Let's find the operator secret
	var operatorSecret corev1.Secret
	if err = r.Get(ctx, client.ObjectKey{
		Name:      configuration.Current.OperatorPullSecretName,
		Namespace: configuration.Current.OperatorNamespace,
	}, &operatorSecret); err != nil {
		if apierrs.IsNotFound(err) {
			// There is no secret like that, probably because we are running in our development environment
			return "", nil
		}
		return "", err
	}

	pullSecretName = fmt.Sprintf("%s-pull", pooler.Name)

	// Let's create the secret with the required info
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pooler.Namespace,
			// we change name to avoid ownership conflicts with the managed secret from cnpg cluster
			Name: pullSecretName,
		},
		Data: operatorSecret.Data,
		Type: operatorSecret.Type,
	}

	if err = ctrl.SetControllerReference(pooler, &secret, r.Scheme); err != nil {
		return "", err
	}

	contextLog.Debug("creating image pull secret for service account")
	// Another sync loop may have already created the service. Let's check that
	if err = r.Create(ctx, &secret); err != nil && !apierrs.IsAlreadyExists(err) {
		return "", err
	}

	return pullSecretName, nil
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
