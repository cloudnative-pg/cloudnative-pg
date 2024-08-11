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
	"context"
	"fmt"
	"net/http"
	"net/url"

	corev1 "k8s.io/api/core/v1"
)

type PgPublication struct {
	// The name inside PostgreSQL
	Name string `json:"name"`

	// The owner
	Owner string `json:"owner,omitempty"`

	// Parameters
	Parameters map[string]string `json:"parameters,omitempty"`

	// Publication target
	Target PgPublicationTarget `json:"target,omitempty"`
}

type PgPublicationTarget struct {
	// All tables should be publicated
	AllTables *PgPublicationTargetAllTables `json:"allTables,omitempty"`

	// Just the following schema objects
	Objects []PgPublicationTargetObject `json:"objects,omitempty"`
}

// PgPublicationTargetAllTables means all tables should be publicated
type PgPublicationTargetAllTables struct {
}

// PublicationObject is an object to publicate
type PgPublicationTargetObject struct {
	// The schema to publicate
	Schema string `json:"schema,omitempty"`

	// A list of table expressions
	TableExpression []string `json:"tableExpression,omitempty"`
}

func (r *statusClient) PostPublication(ctx context.Context, pod *corev1.Pod, dbname string, data PgPublication) error {
	path := fmt.Sprintf(
		"/pg/database/%s/publication/%s",
		url.PathEscape(dbname),
		url.PathEscape(data.Name),
	)
	return r.rawEntrypoint(ctx, pod, http.MethodPost, path, data)
}

