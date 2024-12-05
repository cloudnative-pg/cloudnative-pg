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

package controller

import (
	"database/sql"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// implementation of instanceInterface with fakeable DB
type fakeInstanceData struct {
	*postgres.Instance
	db *sql.DB
}

func (f *fakeInstanceData) GetSuperUserDB() (*sql.DB, error) {
	return f.db, nil
}

func (f *fakeInstanceData) GetNamedDB(_ string) (*sql.DB, error) {
	return f.db, nil
}

var _ = Describe("Conversion of PG parameters from map to string of key/value pairs", func() {
	It("returns expected well-formed list", func() {
		m := map[string]string{
			"a": "1", "b": "2",
		}
		res := toPostgresParameters(m)
		Expect(res).To(Equal(`"a" = '1', "b" = '2'`))
	})
})
