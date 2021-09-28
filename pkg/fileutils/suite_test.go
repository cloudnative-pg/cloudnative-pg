/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package fileutils

import (
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var tempDir1, tempDir2 string

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestConfigFile(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "File Utilities Suite")
}

var _ = BeforeSuite(func() {
	var err error
	tempDir1, err = ioutil.TempDir(os.TempDir(), "fileutils_")
	Expect(err).To(BeNil())
	tempDir2, err = ioutil.TempDir(os.TempDir(), "fileutils_")
	Expect(err).To(BeNil())
})

var _ = AfterSuite(func() {
	err := os.RemoveAll(tempDir1)
	Expect(err).To(BeNil())
	err = os.RemoveAll(tempDir2)
	Expect(err).To(BeNil())
})
