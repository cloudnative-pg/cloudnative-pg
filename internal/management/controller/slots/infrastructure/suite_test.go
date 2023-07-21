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

package infrastructure

import (
	"database/sql"
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Internal Management Controller Slots Infrastructure Suite")
}

// mockPooler is a mock implementation of the Pooler interface
type mockPooler struct {
	db *sql.DB
}

func (mp *mockPooler) Connection(_ string) (*sql.DB, error) {
	if mp.db == nil {
		return nil, errors.New("connection error")
	}
	return mp.db, nil
}

func (mp *mockPooler) GetDsn(_ string) string {
	return "mocked DSN"
}

func (mp *mockPooler) ShutdownConnections() {
	// no-op in mock
}
