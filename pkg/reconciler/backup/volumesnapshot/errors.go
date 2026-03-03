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
	"regexp"
	"strconv"
	"strings"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
)

var (
	retryableStatusCodes = []int{408, 429, 500, 502, 503, 504}
	httpStatusCodeRegex  = regexp.MustCompile(`HTTPStatusCode:\s(\d{3})`)
)

// isErrorRetryable detects is an error is retryable or not.
//
// Important: this function is intended for detecting errors that
// occur during communication between the operator and the Kubernetes
// API server, as well as between the operator and the instance
// manager.
// It is not designed to check errors raised by the CSI driver and
// exposed by the CSI snapshotter sidecar.
func isNetworkErrorRetryable(err error) bool {
	return apierrs.IsServerTimeout(err) || apierrs.IsConflict(err) || apierrs.IsInternalError(err) ||
		errors.Is(err, context.DeadlineExceeded)
}

// isCSIErrorMessageRetriable detects if a certain error message
// raised by the CSI driver corresponds to a retriable error or
// not.
//
// It relies on heuristics, as this information is not available in
// the Kubernetes VolumeSnapshot API, and the CSI driver does not
// expose it either.
func isCSIErrorMessageRetriable(msg string) bool {
	isRetryableFuncs := []func(string) bool{
		isExplicitlyRetriableError,
		isRetryableHTTPError,
		isConflictError,
		isContextDeadlineExceededError,
		isOCIConflictError,
	}

	for _, isRetryableFunc := range isRetryableFuncs {
		if isRetryableFunc(msg) {
			return true
		}
	}

	return false
}

// isContextDeadlineExceededError detects context deadline exceeded errors
// These are timeouts that may be retried by the Kubernetes CSI controller
func isContextDeadlineExceededError(msg string) bool {
	return strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timed out")
}

// isConflictError detects optimistic locking errors
func isConflictError(msg string) bool {
	// Obviously this is a heuristic, but unfortunately we don't have
	// the information we need.
	// We're trying to handle the cases where the external-snapshotter
	// controller failed on a conflict with the following error:
	//
	// > the object has been modified; please apply your changes to the
	// > latest version and try again

	return strings.Contains(msg, "the object has been modified")
}

// isExplicitlyRetriableError detects explicitly retriable errors as raised
// by the Azure CSI driver. These errors contain the "Retriable: true"
// string.
func isExplicitlyRetriableError(msg string) bool {
	return strings.Contains(msg, "Retriable: true")
}

// isRetryableHTTPError, will return a retry on the following status codes:
// - 408: Request Timeout
// - 429: Too Many Requests
// - 500: Internal Server Error
// - 502: Bad Gateway
// - 503: Service Unavailable
// - 504: Gateway Timeout
func isRetryableHTTPError(msg string) bool {
	if matches := httpStatusCodeRegex.FindStringSubmatch(msg); len(matches) == 2 {
		if code, err := strconv.Atoi(matches[1]); err == nil {
			for _, retryableCode := range retryableStatusCodes {
				if code == retryableCode {
					return true
				}
			}
		}
	}

	return false
}

// isOCIConflictError detects OCI conflict errors
func isOCIConflictError(msg string) bool {
	// The OCI CSI driver returns a 409 Conflict error when a backup is already in progress.
	// This happens due to race conditions or retries.
	// The error message typically contains "Error returned by Blockstorage Service. Http Status Code: 409. Error Code: Conflict."
	//
	// We check for the presence of these substrings to identify the error.
	return strings.Contains(msg, "Error returned by Blockstorage Service") &&
		strings.Contains(msg, "Http Status Code: 409") &&
		strings.Contains(msg, "Error Code: Conflict")
}
