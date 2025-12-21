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
				err := crudClient.List(ctx, objectList, opts...)
				if err != nil {
					return err
				}
				return nil
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
) error {
	err := retry.New(
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay)).
		Do(
			func() error {
				err := crudClient.Get(ctx, objectKey, object)
				if err != nil {
					return err
				}
				return nil
			},
		)
	return err
}
