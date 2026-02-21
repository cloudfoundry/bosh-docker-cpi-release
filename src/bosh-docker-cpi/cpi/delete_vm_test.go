package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/vm/vmfakes"
)

var _ = Describe("DeleteVMMethod", func() {
	var (
		fakeFinder *vmfakes.FakeFinder
		fakeVM     *vmfakes.FakeVM
		method     cpi.DeleteVMMethod
		vmCID      apiv1.VMCID
	)

	BeforeEach(func() {
		fakeFinder = &vmfakes.FakeFinder{}
		fakeVM = &vmfakes.FakeVM{}
		method = cpi.NewDeleteVMMethod(fakeFinder)
		vmCID = apiv1.NewVMCID("fake-vm-id")
	})

	It("finds and deletes the VM", func() {
		fakeFinder.FindReturns(fakeVM, nil)
		fakeVM.DeleteReturns(nil)

		err := method.DeleteVM(vmCID)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeFinder.FindCallCount()).To(Equal(1))
		Expect(fakeFinder.FindArgsForCall(0)).To(Equal(vmCID))
		Expect(fakeVM.DeleteCallCount()).To(Equal(1))
	})

	It("returns error when finding the VM fails", func() {
		fakeFinder.FindReturns(nil, errors.New("find-error"))

		err := method.DeleteVM(vmCID)
		Expect(err).To(MatchError(ContainSubstring("Finding vm")))
		Expect(err).To(MatchError(ContainSubstring("find-error")))
	})

	It("returns error when deleting the VM fails", func() {
		fakeFinder.FindReturns(fakeVM, nil)
		fakeVM.DeleteReturns(errors.New("delete-error"))

		err := method.DeleteVM(vmCID)
		Expect(err).To(MatchError(ContainSubstring("Deleting vm")))
		Expect(err).To(MatchError(ContainSubstring("delete-error")))
	})
})
