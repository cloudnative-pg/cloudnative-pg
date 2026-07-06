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

package volumesnapshot

import (
	"context"
	"errors"
	"net"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
)

// isNetworkErrorRetryable detects if an error is retryable or not.
//
// Important: this function is intended for detecting errors that
// occur during communication between the operator and the Kubernetes
// API server, as well as between the operator and the instance
// manager.
// It is not designed to check errors raised by the CSI driver and
// exposed by the CSI snapshotter sidecar.
func isNetworkErrorRetryable(err error) bool {
	// A transport-level failure to reach the instance manager surfaces as a
	// net.Error. The HTTP client wraps every such failure in a *net/url.Error,
	// which itself satisfies net.Error, so this matches all of them: dial
	// timeout, connection refused or reset, DNS failure, and TLS or certificate
	// errors. In each case the connection never produced an authenticated
	// response, so requeuing is safe. It matters most in the finalize step,
	// where the snapshots are already provisioned and a single failure would
	// otherwise discard an otherwise complete backup.
	//
	// This is intentionally the opposite of the in-request retry in
	// remote.getReplicaStatusFromPodViaHTTP, which treats a net.Error timeout as
	// non-retryable: that path retries within a single reconcile, whereas here we
	// requeue the whole reconcile.
	var netErr net.Error

	return apierrs.IsServerTimeout(err) || apierrs.IsConflict(err) || apierrs.IsInternalError(err) ||
		errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr)
}
