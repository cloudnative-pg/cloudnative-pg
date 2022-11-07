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

package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// Branch is an object returned by gitHub query
type Branch struct {
	Name string `json:"name,omitempty"`
}

func getLatestOperatorVersion(ctx context.Context) (string, error) {
	url := "https://api.github.com/repos/cloudnative-pg/artifacts/branches"
	body, err := executeGetRequest(ctx, url)
	if err != nil {
		return "", err
	}

	var tags []Branch
	if err := json.Unmarshal(body, &tags); err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no branches found")
	}

	// we order the slice in reverse order, so the latest version is the first element
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name > tags[j].Name
	})

	return tags[0].Name, nil
}

func executeGetRequest(ctx context.Context, url string) ([]byte, error) {
	contextLogger := log.FromContext(ctx)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		contextLogger.Error(err, "Error while visiting url", "url", url)
	}
	defer func() {
		err = resp.Body.Close()
		if err != nil {
			contextLogger.Error(err, "Can't close the connection",
				"url", url,
				"statusCode", resp.StatusCode,
			)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		contextLogger.Error(err, "Error while reading status response body",
			"url", url,
			"statusCode", resp.StatusCode,
		)
		return nil, err
	}
	if resp.StatusCode > 299 {
		return nil, fmt.Errorf("statusCode=%v while visiting url: %v",
			resp.StatusCode, url)
	}
	return body, nil
}
