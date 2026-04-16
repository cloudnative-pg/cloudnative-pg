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
	"fmt"
	"io"
	"net/http"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
)

// executeRequestWithError executes an http request and returns a webserver.response and any error encountered
func executeRequestWithError[T any](
	ctx context.Context,
	cli *http.Client,
	req *http.Request,
	ignoreBodyErrors bool,
) (*webserver.Response[T], error) {
	contextLogger := log.FromContext(ctx)

	resp, err := cli.Do(req) //nolint:gosec // URL built from internal pod IP
	if err != nil {
		return nil, fmt.Errorf("while executing http request: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			contextLogger.Error(err, "while closing response body")
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("while reading the response body: %w", err)
	}

	if resp.StatusCode == http.StatusInternalServerError {
		return nil, fmt.Errorf("encountered an internal server error status code 500 with body: %s", string(body))
	}

	var result webserver.Response[T]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("while unmarshalling the body, body: %s err: %w", string(body), err)
	}
	if result.Error != nil && !ignoreBodyErrors {
		return nil, fmt.Errorf("body contained an error code: %s and message: %s",
			result.Error.Code, result.Error.Message)
	}

	return &result, nil
}
