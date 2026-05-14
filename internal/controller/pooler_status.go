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
	"reflect"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// updatePoolerStatus sets the status of the pooler and writes it inside kubernetes
func (r *PoolerReconciler) updatePoolerStatus(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	updatedStatus := pooler.Status.DeepCopy()

	if updatedStatus.Secrets == nil {
		updatedStatus.Secrets = &apiv1.PoolerSecrets{}
	}

	if updatedStatus.Secrets.PgBouncerSecrets == nil {
		updatedStatus.Secrets.PgBouncerSecrets = &apiv1.PgBouncerSecrets{}
	}

	if resources.AuthUserSecret != nil {
		updatedStatus.Secrets.PgBouncerSecrets.AuthQuery = apiv1.SecretVersion{
			Name:    resources.AuthUserSecret.Name,
			Version: resources.AuthUserSecret.ResourceVersion,
		}
	}

	if resources.ServerCASecret != nil {
		updatedStatus.Secrets.ServerCA = apiv1.SecretVersion{
			Name:    resources.ServerCASecret.Name,
			Version: resources.ServerCASecret.ResourceVersion,
		}
	} else {
		updatedStatus.Secrets.ServerCA = apiv1.SecretVersion{}
	}

	if resources.ClientCASecret != nil {
		updatedStatus.Secrets.ClientCA = apiv1.SecretVersion{
			Name:    resources.ClientCASecret.Name,
			Version: resources.ClientCASecret.ResourceVersion,
		}
	} else {
		updatedStatus.Secrets.ClientCA = apiv1.SecretVersion{}
	}

	if resources.ClientTLSSecret != nil {
		updatedStatus.Secrets.ClientTLS = apiv1.SecretVersion{
			Name:    resources.ClientTLSSecret.Name,
			Version: resources.ClientTLSSecret.ResourceVersion,
		}
	} else {
		updatedStatus.Secrets.ClientTLS = apiv1.SecretVersion{}
	}

	if resources.ServerTLSSecret != nil {
		updatedStatus.Secrets.ServerTLS = apiv1.SecretVersion{
			Name:    resources.ServerTLSSecret.Name,
			Version: resources.ServerTLSSecret.ResourceVersion,
		}
	} else {
		// Clear ServerTLS when not using manual TLS authentication.
		// This is particularly important for migration from v1.27, where
		// ServerTLS was always set to the cluster's server certificate.
		updatedStatus.Secrets.ServerTLS = apiv1.SecretVersion{}
	}

	if resources.Deployment != nil {
		updatedStatus.Instances = resources.Deployment.Status.Replicas
	}

	if service := resources.Service; service != nil && service.Spec.Type == corev1.ServiceTypeLoadBalancer {
		updatedStatus.LoadBalancer = service.Status.LoadBalancer.DeepCopy()
	} else {
		updatedStatus.LoadBalancer = nil
	}

	// then update the status if anything changed
	if !reflect.DeepEqual(pooler.Status, updatedStatus) {
		pooler.Status = *updatedStatus
		return r.Status().Update(ctx, pooler)
	}

	return nil
}
