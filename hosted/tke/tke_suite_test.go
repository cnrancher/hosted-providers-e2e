package tke

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTke(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tke Suite")
}
