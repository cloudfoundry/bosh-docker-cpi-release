package vm_test

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "bosh-docker-cpi/vm"
)

var _ = Describe("Container", func() {
	Describe("ID", func() {
		It("returns the VM CID it was created with", func() {
			vmCID := apiv1.NewVMCID("c-test-vm")
			container := NewContainer(vmCID, nil, nil, nil)
			Expect(container.ID()).To(Equal(vmCID))
		})
	})
})
