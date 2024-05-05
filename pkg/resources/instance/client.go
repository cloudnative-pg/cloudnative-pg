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

package instance

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// requestRetry is the default backoff used to query the instance manager
// for the status of each PostgreSQL instance.
var requestRetry = wait.Backoff{
	Steps:    5,
	Duration: 10 * time.Millisecond,
	Factor:   5.0,
	Jitter:   0.1,
}

// StatusClient a http client capable of querying the instance HTTP endpoints
type StatusClient struct {
	*http.Client
}

// An StatusError reports an unsuccessful attempt to retrieve an instance status
type StatusError struct {
	StatusCode int
	Body       string
}

func (i StatusError) Error() string {
	return fmt.Sprintf("error status code: %v, body: %v", i.StatusCode, i.Body)
}

// NewStatusClient returns a client capable of querying the instance HTTP endpoints
func NewStatusClient() *StatusClient {
	const connectionTimeout = 2 * time.Second
	const requestTimeout = 30 * time.Second

	// We want a connection timeout to prevent waiting for the default
	// TCP connection timeout (30 seconds) on lost SYN packets
	dialer := &net.Dialer{
		Timeout: connectionTimeout,
	}
	timeoutClient := &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				tlsConfig, ok := ctx.Value(utils.ContextKeyTLSConfig).(*tls.Config)
				if !ok || tlsConfig == nil {
					return nil, fmt.Errorf("missing TLSConfig object in context")
				}
				tlsDialer := tls.Dialer{
					NetDialer: dialer,
					Config:    tlsConfig,
				}
				return tlsDialer.DialContext(ctx, network, addr)
			},
		},
		Timeout: requestTimeout,
	}

	return &StatusClient{timeoutClient}
}

// extractInstancesStatus extracts the status of the underlying PostgreSQL instance from
// the requested Pod, via the instance manager. In case of failure, errors are passed
// in the result list
func (r *StatusClient) extractInstancesStatus(
	ctx context.Context,
	activePods []corev1.Pod,
) postgres.PostgresqlStatusList {
	var result postgres.PostgresqlStatusList

	for idx := range activePods {
		instanceStatus := r.getReplicaStatusFromPodViaHTTP(ctx, activePods[idx])
		result.Items = append(result.Items, instanceStatus)
	}
	return result
}

// getReplicaStatusFromPodViaHTTP retrieves the status of PostgreSQL pod via HTTP, retrying
// the request if some communication error is encountered
func (r *StatusClient) getReplicaStatusFromPodViaHTTP(
	ctx context.Context,
	pod corev1.Pod,
) (result postgres.PostgresqlStatus) {
	isErrorRetryable := func(err error) bool {
		contextLog := log.FromContext(ctx)

		// If it's a timeout, we do not want to retry
		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			return false
		}

		// If the pod answered with a not ok status, it is pointless to retry
		var statuserror StatusError
		if errors.As(err, &statuserror) {
			return false
		}

		contextLog.Debug("Error while requesting the status of an instance, retrying",
			"pod", pod.Name,
			"error", err)
		return true
	}

	// The retry here is to support restarting the instance manager during
	// online upgrades. It is not intended to wait for recovering from any
	// other remote failure.
	_ = retry.OnError(requestRetry, isErrorRetryable, func() error {
		result = r.rawInstanceStatusRequest(ctx, pod)
		return result.Error
	})

	result.AddPod(pod)

	return result
}

// GetStatusFromInstances gets the replication status from the PostgreSQL instances,
// the returned list is sorted in order to have the primary as the first element
// and the other instances in their election order
func (r *StatusClient) GetStatusFromInstances(
	ctx context.Context,
	pods corev1.PodList,
) postgres.PostgresqlStatusList {
	// Only work on Pods which can still become active in the future
	filteredPods := utils.FilterActivePods(pods.Items)
	if len(filteredPods) == 0 {
		// No instances to control
		return postgres.PostgresqlStatusList{}
	}

	status := r.extractInstancesStatus(ctx, filteredPods)
	sort.Sort(&status)
	for idx := range status.Items {
		if status.Items[idx].Error != nil {
			log.FromContext(ctx).Info("Cannot extract Pod status",
				"name", status.Items[idx].Pod.Name,
				"error", status.Items[idx].Error.Error())
		}
	}
	return status
}

// GetPgControlDataFromInstance obtains the pg_controldata from the instance HTTP endpoint
func (r *StatusClient) GetPgControlDataFromInstance(
	ctx context.Context,
	pod *corev1.Pod,
) (string, error) {
	contextLogger := log.FromContext(ctx)

	scheme := GetStatusSchemeFromPod(pod)
	httpURL := url.Build(scheme, pod.Status.PodIP, url.PathPGControlData, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, "GET", httpURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := r.Client.Do(req)
	if err != nil {
		return "", err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			contextLogger.Error(err, "while closing body")
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	type pgControldataResponse struct {
		Data  string `json:"data,omitempty"`
		Error error  `json:"error,omitempty"`
	}

	var result pgControldataResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		result.Error = err
		return "", err
	}

	return result.Data, result.Error
}

// rawInstanceStatusRequest retrieves the status of PostgreSQL pods via an HTTP request with GET method.
func (r *StatusClient) rawInstanceStatusRequest(
	ctx context.Context,
	pod corev1.Pod,
) (result postgres.PostgresqlStatus) {
	scheme := GetStatusSchemeFromPod(&pod)
	statusURL := url.Build(scheme, pod.Status.PodIP, url.PathPgStatus, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		result.Error = err
		return result
	}

	resp, err := r.Client.Do(req)
	if err != nil {
		result.Error = err
		return result
	}

	defer func() {
		err = resp.Body.Close()
		if err != nil && result.Error == nil {
			result.Error = err
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = err
		return result
	}

	if resp.StatusCode != 200 {
		result.Error = &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
		return result
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		result.Error = err
		return result
	}

	return result
}

// GetStatusSchemeFromPod detects if a Pod is esposint the status via HTTP or HTTPS
func GetStatusSchemeFromPod(pod *corev1.Pod) string {
	// Fall back to comparing the container environment configuration
	for _, container := range pod.Spec.Containers {
		// we go to the next array element if it isn't the postgres container
		if container.Name != specs.PostgresContainerName {
			continue
		}

		if slices.Contains(container.Command, "--tls-status") {
			return "https"
		}

		break
	}

	return "http"
}
