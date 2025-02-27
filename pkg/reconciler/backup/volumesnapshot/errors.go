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

package volumesnapshot

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	retryableStatusCodes = []int{408, 429, 500, 502, 503, 504}
	httpStatusCodeRegex  = regexp.MustCompile(`HTTPStatusCode:\s(\d{3})`)
)

// isRetriableErrorMessage detects if a certain error message belongs
// to a retriable error or not. This is obviously an heuristic but
// unfortunately we don't have that information exposed in the
// Kubernetes VolumeSnapshot API and the CSI driver haven't that too.
func isRetriableErrorMessage(msg string) bool {
	isRetryableFuncs := []func(string) bool{
		isExplicitlyRetriableError,
		isRetryableHTTPError,
		isConflictError,
		isContextDeadlineExceededError,
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
