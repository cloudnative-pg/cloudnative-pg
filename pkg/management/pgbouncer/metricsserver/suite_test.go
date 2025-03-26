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

package metricsserver

import (
	"database/sql"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	lastCollectionErrorKey   = "cnpg_pgbouncer_last_collection_error"
	pgBouncerUpKey           = "cnpg_pgbouncer_up"
	collectionErrorsTotalKey = "cnpg_pgbouncer_collection_errors_total"
)

func TestMetricsserver(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PGBouncer Metricsserver Suite")
}

type fakePooler struct {
	db *sql.DB
}

func (f fakePooler) Connection(_ string) (*sql.DB, error) {
	return f.db, nil
}

func (f fakePooler) GetDsn(dbName string) string {
	return dbName
}

func (f fakePooler) ShutdownConnections() {
}

type nameGetter interface {
	GetName() string
}

// getMetric is used to avoid having the direct dependency on: github.com/prometheus/client_model library
func getMetric[T nameGetter](
	metrics []T,
	metricName string,
) T {
	var t T
	for _, metric := range metrics {
		if metric.GetName() == metricName {
			return metric
		}
	}

	return t
}
