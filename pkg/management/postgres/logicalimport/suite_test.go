package logicalimport

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLogicalimport(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "logicalimport test suite")
}
