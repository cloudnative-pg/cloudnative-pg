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

// Package syncsequences contains the implementation of the
// kubectl cnpg subscription sync-sequences command
package syncsequences

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/logical"
)

const sqlGetSequences = `
WITH seqs AS (
    SELECT 
        sequencename AS sq_name,
        schemaname AS sq_namespace,
        last_value AS sq_value,
        CURRENT_TIMESTAMP AS ts
    FROM pg_catalog.pg_sequences s
)
SELECT pg_catalog.json_agg(seqs) FROM seqs
`

// SequenceStatus represent the status of a sequence in a certain moment
type SequenceStatus struct {
	// The name of the sequence
	Name string `json:"sq_name"`

	// The namespace where the sequence is defined
	Namespace string `json:"sq_namespace"`

	// The last value emitted from the sequence
	Value *int `json:"sq_value"`
}

// QualifiedName gets the qualified name of this sequence
func (status *SequenceStatus) QualifiedName() string {
	return fmt.Sprintf(
		"%s.%s",
		pgx.Identifier{status.Namespace}.Sanitize(),
		pgx.Identifier{status.Name}.Sanitize(),
	)
}

// SequenceMap is a map between a qualified sequence name
// and its current value
type SequenceMap map[string]*int

// GetSequenceStatus gets the status of the sequences while being connected to
// a pod of a cluster to the specified connection string
func GetSequenceStatus(ctx context.Context, clusterName string, connectionString string) (SequenceMap, error) {
	output, err := logical.RunSQLWithOutput(ctx, clusterName, connectionString, sqlGetSequences)
	if err != nil {
		return nil, fmt.Errorf("while executing query: %w", err)
	}
	if len(strings.TrimSpace(string(output))) == 0 {
		return nil, nil
	}

	var records []SequenceStatus
	if err := json.Unmarshal(output, &records); err != nil {
		return nil, fmt.Errorf("while decoding JSON output: %w", err)
	}

	result := make(SequenceMap)
	for i := range records {
		result[records[i].QualifiedName()] = records[i].Value
	}

	return result, nil
}
