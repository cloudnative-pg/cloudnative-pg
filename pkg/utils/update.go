/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"context"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateStatusAndRetry updates a certain object in the k8s database,
// retrying when conflicts are detected
func UpdateStatusAndRetry(
	ctx context.Context,
	client client.StatusClient,
	obj client.Object,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// TODO: we should refresh the object here before trying
		// setting the status again
		return client.Status().Update(ctx, obj)
	})
}
