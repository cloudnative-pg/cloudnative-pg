/*
Copyright 2019-2022 The CloudNativePG Contributors

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
	"reflect"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
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

	if cluster := resources.Cluster; cluster != nil {
		updatedStatus.Secrets.ServerTLS = apiv1.SecretVersion{
			Name:    cluster.GetServerTLSSecretName(),
			Version: cluster.Status.SecretsResourceVersion.ServerSecretVersion,
		}
		updatedStatus.Secrets.ServerCA = apiv1.SecretVersion{
			Name:    cluster.GetServerCASecretName(),
			Version: cluster.Status.SecretsResourceVersion.ServerCASecretVersion,
		}
		updatedStatus.Secrets.ClientCA = apiv1.SecretVersion{
			Name:    cluster.GetClientCASecretName(),
			Version: cluster.Status.SecretsResourceVersion.ClientCASecretVersion,
		}
	}

	if resources.Deployment != nil {
		updatedStatus.Instances = resources.Deployment.Status.Replicas
	}

	// then update the status if anything changed
	if !reflect.DeepEqual(pooler.Status, updatedStatus) {
		pooler.Status = *updatedStatus
		return r.Status().Update(ctx, pooler)
	}

	return nil
}
