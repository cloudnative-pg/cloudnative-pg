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

package fileutils

import (
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var tempDir1, tempDir2, tempDir3 string

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestConfigFile(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "File Utilities Suite")
}

var _ = BeforeSuite(func() {
	var err error
	tempDir1, err = ioutil.TempDir(os.TempDir(), "fileutils1_")
	Expect(err).To(BeNil())
	tempDir2, err = ioutil.TempDir(os.TempDir(), "fileutils2_")
	Expect(err).To(BeNil())
	tempDir3, err = ioutil.TempDir(os.TempDir(), "fileutils3_")
	Expect(err).To(BeNil())
})

var _ = AfterSuite(func() {
	err := os.RemoveAll(tempDir1)
	Expect(err).To(BeNil())
	err = os.RemoveAll(tempDir2)
	Expect(err).To(BeNil())
	err = os.RemoveAll(tempDir3)
	Expect(err).To(BeNil())
})
