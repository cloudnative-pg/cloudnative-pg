/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
