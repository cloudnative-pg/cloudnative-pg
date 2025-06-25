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

package postgres

import (
	"database/sql"
	"testing"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPostgres(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PostgreSQL instance manager test suite")
}

// mockInstance implements the minimum required functionality to test ConfigureNewInstance
type mockInstance struct {
	superUserDB *sql.DB
	templateDB  *sql.DB
	appDB       *sql.DB
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

func (m *mockInstance) GetSuperUserDB() (*sql.DB, error) {
	return m.superUserDB, nil
}

func (m *mockInstance) GetTemplateDB() (*sql.DB, error) {
	return m.templateDB, nil
}

func (m *mockInstance) ConnectionPool() pool.Pooler {
	return &fakePooler{db: m.appDB}
}
