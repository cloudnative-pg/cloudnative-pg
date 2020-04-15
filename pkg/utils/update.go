/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package utils

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateStatusAndRetry update a certain backup in the k8s database
// retrying when conflicts are detected
func UpdateStatusAndRetry(
	ctx context.Context,
	client client.StatusClient,
	obj runtime.Object,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return client.Status().Update(ctx, obj)
	})
}
