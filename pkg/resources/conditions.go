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

package resources

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// readinessCheckRetry is used to wait until the API server is reachable
var readinessCheckRetry = wait.Backoff{
	Steps:    5,
	Duration: 10 * time.Millisecond,
	Factor:   5.0,
	Jitter:   0.1,
}

// RetryAlways is a function that always returns true on any error encountered
func RetryAlways(err error) bool { return true }

// WaitKubernetesAPIServer will wait for the kubernetes API server to by ready.
// Returns any error if it can't be reached.
func WaitKubernetesAPIServer(ctx context.Context, clusterObjectKey client.ObjectKey) error {
	logger := log.FromContext(ctx)

	cli, err := management.NewControllerRuntimeClient()
	if err != nil {
		logger.Error(err, "error while creating a standalone Kubernetes client")
		return err
	}

	if err := retry.OnError(readinessCheckRetry, RetryAlways, func() (err error) {
		return cli.Get(ctx, clusterObjectKey, &v1.Cluster{})
	}); err != nil {
		const message = "error while waiting for the API server to be reachable"
		logger.Error(err, message)
		return fmt.Errorf("%s: %w", message, err)
	}

	return nil
}
