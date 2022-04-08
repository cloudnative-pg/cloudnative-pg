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

package execlog

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Writing to a LogWriter", func() {
	l := LogWriter{Logger: log.GetLogger()}
	When("it is passed nil", func() {
		n, err := l.Write(nil)
		It("does not crash", func() {
			Expect(n).To(Equal(0))
			Expect(err).To(BeNil())
		})
	})
})
