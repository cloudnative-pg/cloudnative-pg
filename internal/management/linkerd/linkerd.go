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

package linkerd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

// TryInvokeShutdownEndpoint executes a post request on the /shutdown endpoint. Returns any errors encountered if
// the service exists
func TryInvokeShutdownEndpoint(ctx context.Context) error {
	const endpoint = "http://localhost:4191/shutdown"
	logger := log.FromContext(ctx)

	clientHTTP := http.Client{Timeout: 5 * time.Second}
	resp, err := clientHTTP.Post(endpoint, "", nil)

	if errors.Is(err, syscall.ECONNREFUSED) || os.IsTimeout(err) {
		return nil
	}

	if err != nil {
		return err
	}

	if closeErr := resp.Body.Close(); closeErr != nil {
		logger.Error(closeErr, "unable to close the response body", "endpoint", endpoint)
	}

	return nil
}
