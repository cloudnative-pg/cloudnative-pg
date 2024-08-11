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

package instance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	pgurl "github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

// ErrDatabaseNotFound is raised when a database have not been found
var ErrDatabaseNotFound = errors.New("database not found")

// PgDatabase represents a PostgreSQL Database
type PgDatabase struct {
	// The owner
	Owner string `json:"owner"`

	// The encoding (cannot be changed)
	Encoding string `json:"encoding"`

	// True when the database is a template
	IsTemplate *bool `json:"isTemplate"`

	// True when connections to this database are allowed
	AllowConnections *bool `json:"allowConnections"`

	// Connection limit, -1 means no limit and -2 means the
	// database is not valid
	ConnectionLimit *int `json:"connectionLimit"`

	// The default tablespace of this database
	Tablespace string `json:"tablespace"`
}

func pgDatabaseURL(pod *corev1.Pod, dbname string) string {
	path := fmt.Sprintf("/pg/database/%s", url.PathEscape(dbname))
	return pgurl.Build(
		GetStatusSchemeFromPod(pod).ToString(), pod.Status.PodIP, path, pgurl.StatusPort)
}

func (r *statusClient) PostDatabase(ctx context.Context, pod *corev1.Pod, dbname string, data PgDatabase) error {
	return r.rawDatabaseEntrypoint(ctx, pod, http.MethodPost, dbname, data)
}

func (r *statusClient) rawDatabaseEntrypoint(
	ctx context.Context,
	pod *corev1.Pod,
	method string,
	dbname string,
	data PgDatabase,
) error {
	contextLogger := log.FromContext(ctx)

	var requestBody bytes.Buffer
	if err := json.NewEncoder(&requestBody).Encode(data); err != nil {
		return err
	}

	statusURL := pgDatabaseURL(pod, dbname)
	req, err := http.NewRequestWithContext(ctx, method, statusURL, &requestBody)
	if err != nil {
		return err
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			contextLogger.Error(err, "while closing body")
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusNotFound {
		return ErrDatabaseNotFound
	}
	if resp.StatusCode != 200 {
		return &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	return nil
}
