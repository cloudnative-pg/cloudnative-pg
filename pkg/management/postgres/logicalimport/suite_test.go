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

package logicalimport

import (
	"database/sql"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLogicalimport(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "logicalimport test suite")
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
