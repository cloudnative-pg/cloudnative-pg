/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"reflect"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// updateResourceStatus fill the status of the pooler
func (r *PoolerReconciler) updateResourceStatus(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	existingPoolerStatus := pooler.Status

	if resources.Configuration != nil {
		pooler.Status.ConfigResourceVersion = resources.Configuration.ResourceVersion
	} else {
		pooler.Status.ConfigResourceVersion = ""
	}

	if !reflect.DeepEqual(existingPoolerStatus, pooler.Status) {
		return r.Status().Update(ctx, pooler)
	}

	return nil
}
