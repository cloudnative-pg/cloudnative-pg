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

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"slices"
	"sort"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	contextutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils/context"
)

const (
	defaultRequestTimeout = 30 * time.Second
	noRequestTimeout      = 0
)

// requestRetry is the default backoff used to query the instance manager
// for the status of each PostgreSQL instance.
var requestRetry = wait.Backoff{
	Steps:    5,
	Duration: 10 * time.Millisecond,
	Factor:   5.0,
	Jitter:   0.1,
}

// InstanceClient a http client capable of querying the instance HTTP endpoints
type InstanceClient interface {
	// GetStatusFromInstances gets the replication status from the PostgreSQL instances,
	// the returned list is sorted in order to have the primary as the first element
	// and the other instances in their election order
	GetStatusFromInstances(
		ctx context.Context,
		pods corev1.PodList,
	) postgres.PostgresqlStatusList

	// GetPgControlDataFromInstance obtains the pg_controldata from the instance HTTP endpoint
	GetPgControlDataFromInstance(
		ctx context.Context,
		pod *corev1.Pod,
	) (string, error)

	// UpgradeInstanceManager upgrades the instance manager to the passed availableArchitecture
	UpgradeInstanceManager(
		ctx context.Context,
		pod *corev1.Pod,
		availableArchitecture *utils.AvailableArchitecture,
	) error

	// ArchivePartialWAL trigger the archiver for the latest partial WAL
	// file created in a specific Pod
	ArchivePartialWAL(context.Context, *corev1.Pod) (string, error)
}

type instanceClientImpl struct {
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

// extractInstancesStatus extracts the status of the underlying PostgreSQL instance from
// the requested Pod, via the instance manager. In case of failure, errors are passed
// in the result list
func (r instanceClientImpl) extractInstancesStatus(
	ctx context.Context,
	activePods []corev1.Pod,
) postgres.PostgresqlStatusList {
	var result postgres.PostgresqlStatusList

	cluster, ok := ctx.Value(contextutils.ContextKeyCluster).(*apiv1.Cluster)
	if ok && cluster != nil {
		result.IsReplicaCluster = cluster.IsReplica()
		result.CurrentPrimary = cluster.Status.CurrentPrimary
	}

	for idx := range activePods {
		instanceStatus := r.getReplicaStatusFromPodViaHTTP(ctx, activePods[idx])
		result.Items = append(result.Items, instanceStatus)
	}
	return result
}

// getReplicaStatusFromPodViaHTTP retrieves the status of PostgreSQL pod via HTTP, retrying
// the request if some communication error is encountered
func (r *instanceClientImpl) getReplicaStatusFromPodViaHTTP(
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

func (r *instanceClientImpl) GetStatusFromInstances(
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
				"podName", status.Items[idx].Pod.Name,
				"error", status.Items[idx].Error.Error())
		}
	}
	return status
}

func (r *instanceClientImpl) GetPgControlDataFromInstance(
	ctx context.Context,
	pod *corev1.Pod,
) (string, error) {
	contextLogger := log.FromContext(ctx)

	scheme := GetStatusSchemeFromPod(pod)
	httpURL := url.Build(scheme.ToString(), pod.Status.PodIP, url.PathPGControlData, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpURL, nil)
	if err != nil {
		return "", err
	}
	r.Timeout = defaultRequestTimeout
	resp, err := r.Do(req) //nolint:gosec // URL built from internal pod IP
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

	if resp.StatusCode != http.StatusOK {
		return "", &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	type pgControldataResponse struct {
		Data  string `json:"data,omitempty"`
		Error error  `json:"error,omitempty"`
	}

	var result pgControldataResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	return result.Data, result.Error
}

// UpgradeInstanceManager upgrades the instance manager to the passed availableArchitecture
func (r *instanceClientImpl) UpgradeInstanceManager(
	ctx context.Context,
	pod *corev1.Pod,
	availableArchitecture *utils.AvailableArchitecture,
) error {
	contextLogger := log.FromContext(ctx)

	binaryFileStream, err := availableArchitecture.FileStream()
	if err != nil {
		return err
	}
	defer func() {
		if binaryErr := binaryFileStream.Close(); binaryErr != nil {
			contextLogger.Error(err, "while closing the binaryFileStream")
		}
	}()

	scheme := GetStatusSchemeFromPod(pod)
	updateURL := url.Build(scheme.ToString(), pod.Status.PodIP, url.PathUpdate, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, updateURL, nil)
	if err != nil {
		return err
	}
	req.Body = binaryFileStream

	r.Timeout = noRequestTimeout
	resp, err := r.Do(req) //nolint:gosec // URL built from internal pod IP
	// This is the desired response. The instance manager will
	// synchronously update and this call won't return.
	if isEOF(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusOK {
		// Currently the instance manager should never return StatusOK
		return errors.New("instance manager has returned an unexpected status code")
	}

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if err = resp.Body.Close(); err != nil {
		return err
	}

	return fmt.Errorf("the instance manager upgrade path returned the following error: '%s", string(body))
}

func isEOF(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err.(*neturl.Error).Err, io.EOF)
}

// rawInstanceStatusRequest retrieves the status of PostgreSQL pods via an HTTP request with GET method.
func (r *instanceClientImpl) rawInstanceStatusRequest(
	ctx context.Context,
	pod corev1.Pod,
) (result postgres.PostgresqlStatus) {
	scheme := GetStatusSchemeFromPod(&pod)
	statusURL := url.Build(scheme.ToString(), pod.Status.PodIP, url.PathPgStatus, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		result.Error = err
		return result
	}

	r.Timeout = defaultRequestTimeout
	resp, err := r.Do(req) //nolint:gosec // URL built from internal pod IP
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

	if resp.StatusCode != http.StatusOK {
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

// HTTPScheme identifies a valid scheme: http, https
type HTTPScheme string

const (
	schemeHTTP  HTTPScheme = "http"
	schemeHTTPS HTTPScheme = "https"
)

// IsHTTPS returns true if schemeHTTPS
func (h HTTPScheme) IsHTTPS() bool {
	return h == schemeHTTPS
}

// ToString returns the scheme as a string value
func (h HTTPScheme) ToString() string {
	return string(h)
}

// GetStatusSchemeFromPod detects if a Pod is exposing the status via HTTP or HTTPS
func GetStatusSchemeFromPod(pod *corev1.Pod) HTTPScheme {
	// Fall back to comparing the container environment configuration
	for _, container := range pod.Spec.Containers {
		// we go to the next array element if it isn't the postgres container
		if container.Name != specs.PostgresContainerName {
			continue
		}

		if slices.Contains(container.Command, "--status-port-tls") {
			return schemeHTTPS
		}

		break
	}

	return schemeHTTP
}

func (r *instanceClientImpl) ArchivePartialWAL(ctx context.Context, pod *corev1.Pod) (string, error) {
	contextLogger := log.FromContext(ctx)

	statusURL := url.Build(
		GetStatusSchemeFromPod(pod).ToString(), pod.Status.PodIP, url.PathPgArchivePartial, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, statusURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.Do(req) //nolint:gosec // URL built from internal pod IP
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

	if resp.StatusCode != http.StatusOK {
		return "", &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	type pgArchivePartialResponse struct {
		Data string `json:"data,omitempty"`
	}

	var result pgArchivePartialResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return "", err
	}

	return result.Data, nil
}
