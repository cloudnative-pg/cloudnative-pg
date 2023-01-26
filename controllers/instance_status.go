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

package controllers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

type instanceStatusClient struct {
	*http.Client
}

func newInstanceStatusClient() *instanceStatusClient {
	const connectionTimeout = 2 * time.Second
	const requestTimeout = 30 * time.Second

	// We want a connection timeout to prevent waiting for the default
	// TCP connection timeout (30 seconds) on lost SYN packets
	timeoutClient := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: connectionTimeout,
			}).DialContext,
		},
		Timeout: requestTimeout,
	}

	return &instanceStatusClient{timeoutClient}
}

// extractInstancesStatus extracts the status of the underlying PostgreSQL instance from
// the requested Pod, via the instance manager. In case of failure, errors are passed
// in the result list
func (r *instanceStatusClient) extractInstancesStatus(
	ctx context.Context,
	activePods []v1.Pod,
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
func (r *instanceStatusClient) getReplicaStatusFromPodViaHTTP(
	ctx context.Context,
	pod v1.Pod,
) (result postgres.PostgresqlStatus) {
	isErrorRetryable := func(err error) bool {
		contextLog := log.FromContext(ctx)

		// If it's a timeout, we do not want to retry
		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			return false
		}

		// If the pod answered with a not ok status, it is pointless to retry
		var instanceStatusError InstanceStatusError
		if errors.As(err, &instanceStatusError) {
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
	_ = retry.OnError(StatusRequestRetry, isErrorRetryable, func() error {
		result = rawInstanceStatusRequest(ctx, r.Client, pod)
		return result.Error
	})

	result.AddPod(pod)

	return result
}

// getStatusFromInstances gets the replication status from the PostgreSQL instances,
// the returned list is sorted in order to have the primary as the first element
// and the other instances in their election order
func (r *instanceStatusClient) getStatusFromInstances(
	ctx context.Context,
	pods v1.PodList,
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
