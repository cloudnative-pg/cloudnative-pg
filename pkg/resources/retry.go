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

package resources

import (
	"context"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RetryAlways is a function that always returns true on any error encountered
func RetryAlways(_ error) bool { return true }

// RetryWithRefreshedResource updates the resource before invoking the cb
func RetryWithRefreshedResource(
	ctx context.Context,
	cli client.Client,
	resource client.Object,
	cb func() error,
) error {
	return retry.OnError(retry.DefaultBackoff, RetryAlways, func() error {
		if err := cli.Get(ctx, client.ObjectKeyFromObject(resource), resource); err != nil {
			return err
		}

		return cb()
	})
}
