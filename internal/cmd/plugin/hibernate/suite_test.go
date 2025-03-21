package hibernate_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHibernate(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hibernate Suite")
}
