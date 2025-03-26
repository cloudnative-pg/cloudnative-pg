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

package istio

import (
	"context"
	"errors"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

// TryInvokeQuitEndpoint executes a post request on the /quitquitquit endpoint. Returns any errors encountered if
// the service exists
func TryInvokeQuitEndpoint(ctx context.Context) error {
	const endpoint = "http://localhost:15000/quitquitquit"
	logger := log.FromContext(ctx).WithName("try_invoke_quit_quit_endpoint")

	clientHTTP := http.Client{Timeout: 5 * time.Second}
	resp, err := clientHTTP.Post(endpoint, "", nil)
	if errors.Is(err, syscall.ECONNREFUSED) || os.IsTimeout(err) {
		logger.Debug("received ECONNREFUSED, ignoring the error", "endpoint", endpoint)
		return nil
	}
	if err != nil {
		logger.Error(err, "while invoking the /quitquitquit endpoint", "endpoint", endpoint)
		return err
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		logger.Error(closeErr, "unable to close the response body", "endpoint", endpoint)
	}

	return nil
}
