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

package istio

import (
	"context"
	"errors"
	"net/http"
	"os"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v12 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// WaitKubernetesAPIServer will return error in case Kubernetes API
// is not reachable
func WaitKubernetesAPIServer(ctx context.Context, client client.Client, clusterObjectKey client.ObjectKey) error {
	var cluster v12.Cluster
	readinessCheckRetry := wait.Backoff{
		Steps:    5,
		Duration: 10 * time.Millisecond,
		Factor:   5.0,
		Jitter:   0.1,
	}
	if err := retry.OnError(readinessCheckRetry, func(err error) bool { return true }, func() (err error) {
		return client.Get(ctx, clusterObjectKey, &cluster)
	}); err != nil {
		return err
	}

	return nil
}

// QuitIstioProxy triggers the quitquitquit endpoint of Istio
func QuitIstioProxy() error {
	istioProxyQuitEndpoint := "http://localhost:15000/quitquitquit"
	clientHTTP := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := clientHTTP.Post(istioProxyQuitEndpoint, "", nil)
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return nil
	case os.IsTimeout(err):
		return nil
	case err != nil:
		return err
	}
	defer func() {
		if err = resp.Body.Close(); err != nil {
			log.Error(err, "Calling possible istio-proxy container quitquitquit endpoint")
		}
	}()

	return nil
}
