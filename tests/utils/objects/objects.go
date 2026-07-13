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

// Package objects provides functions to manage pure objects in Kubernetes
package objects

import (
	"context"
	"time"

	"github.com/avast/retry-go/v5"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// RetryAttempts maximum number of attempts when it fails in `retry`. Mainly used in `RunUncheckedRetry`
	RetryAttempts = 5

	// PollingTime polling interval (in seconds) between retries
	PollingTime = 5
)

// Create creates object in the Kubernetes cluster
func Create(
	ctx context.Context,
	crudClient client.Client,
	object client.Object,
	opts ...client.CreateOption,
) (client.Object, error) {
	err := retry.New(
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
		retry.RetryIf(func(err error) bool { return !errors.IsAlreadyExists(err) })).
		Do(
			func() error {
				return crudClient.Create(ctx, object, opts...)
			},
		)
	return object, err
}

// Delete deletes an object in the Kubernetes cluster
func Delete(
	ctx context.Context,
	crudClient client.Client,
	object client.Object,
	opts ...client.DeleteOption,
) error {
	err := retry.New(retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
		retry.RetryIf(func(err error) bool { return !errors.IsNotFound(err) })).
		Do(
			func() error {
				return crudClient.Delete(ctx, object, opts...)
			},
		)
	return err
}

// List retrieves a list of objects
func List(
	ctx context.Context,
	crudClient client.Client,
	objectList client.ObjectList,
	opts ...client.ListOption,
) error {
	err := retry.New(
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay)).
		Do(
			func() error {
				return crudClient.List(ctx, objectList, opts...)
			},
		)
	return err
}

// isRetryableWriteError reports whether err is a transient failure worth
// retrying, as opposed to a decisive rejection from the API server (a stale
// resourceVersion, a validation failure, an RBAC denial) that would recur
// unchanged on a subsequent attempt with the same request.
func isRetryableWriteError(err error) bool {
	return !errors.IsConflict(err) &&
		!errors.IsInvalid(err) &&
		!errors.IsForbidden(err) &&
		!errors.IsBadRequest(err)
}

// Patch patches an object in the Kubernetes cluster, retrying on transient
// errors. Forwarding a mutation to an admission webhook can intermittently
// fail with a 500/503 on any cluster that routes that hop through a proxy
// layer (for example an apiserver-network-proxy/konnectivity), so the patch
// is worth retrying just like objects.Get and objects.List retry reads.
func Patch(
	ctx context.Context,
	crudClient client.Client,
	object client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	err := retry.New(
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
		retry.RetryIf(isRetryableWriteError)).
		Do(
			func() error {
				return crudClient.Patch(ctx, object, patch, opts...)
			},
		)
	return err
}

// PatchStatus patches an object's status subresource in the Kubernetes cluster,
// retrying on transient errors. See Patch for the rationale behind retrying.
func PatchStatus(
	ctx context.Context,
	crudClient client.Client,
	object client.Object,
	patch client.Patch,
	opts ...client.SubResourcePatchOption,
) error {
	err := retry.New(
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
		retry.RetryIf(isRetryableWriteError)).
		Do(
			func() error {
				return crudClient.Status().Patch(ctx, object, patch, opts...)
			},
		)
	return err
}

// Get retrieves an object for the given object key from the Kubernetes Cluster
func Get(
	ctx context.Context,
	crudClient client.Client,
	objectKey client.ObjectKey,
	object client.Object,
	opts ...client.GetOption,
) error {
	err := retry.New(
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay)).
		Do(
			func() error {
				return crudClient.Get(ctx, objectKey, object, opts...)
			},
		)
	return err
}

// Update updates an object in the Kubernetes Cluster, retrying on transient
// errors. Unlike Patch, Update sends the object's resourceVersion, so a retry
// after a successful-but-unacknowledged write would otherwise surface as a
// spurious conflict rather than a no-op; isRetryableWriteError avoids
// retrying that case.
func Update(
	ctx context.Context,
	crudClient client.Client,
	object client.Object,
	opts ...client.UpdateOption,
) error {
	err := retry.New(
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
		retry.RetryIf(isRetryableWriteError)).
		Do(
			func() error {
				return crudClient.Update(ctx, object, opts...)
			},
		)
	return err
}
