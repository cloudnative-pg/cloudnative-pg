package walarchive

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWalarchive(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Walarchive Suite")
}
