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

package local

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

// ClusterClient is the interface to interact with the uncategorized endpoints
type ClusterClient interface {
	// SetWALArchiveStatusCondition sets the wal-archive status condition.
	// An empty errMessage means that the archive process was successful.
	// Returns any error encountered during the request.
	SetWALArchiveStatusCondition(ctx context.Context, errMessage string) error
}

// clusterClientImpl a client to interact with the uncategorized endpoints
type clusterClientImpl struct {
	cli *http.Client
}

func (c *clusterClientImpl) SetWALArchiveStatusCondition(ctx context.Context, errMessage string) error {
	contextLogger := log.FromContext(ctx).WithValues("endpoint", url.PathWALArchiveStatusCondition)

	asr := webserver.ArchiveStatusRequest{
		Error: errMessage,
	}

	encoded, err := json.Marshal(&asr)
	if err != nil {
		return err
	}

	resp, err := http.Post(
		url.Local(url.PathWALArchiveStatusCondition, url.LocalPort),
		"application/json",
		bytes.NewBuffer(encoded),
	)
	if err != nil {
		return err
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			contextLogger.Error(err, "while closing response body")
		}
	}()

	return nil
}
