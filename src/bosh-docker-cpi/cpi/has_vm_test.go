package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/vm/vmfakes"
)

var _ = Describe("HasVMMethod", func() {
	var (
		fakeFinder *vmfakes.FakeFinder
		fakeVM     *vmfakes.FakeVM
		method     cpi.HasVMMethod
		vmCID      apiv1.VMCID
	)

	BeforeEach(func() {
		fakeFinder = &vmfakes.FakeFinder{}
		fakeVM = &vmfakes.FakeVM{}
		method = cpi.NewHasVMMethod(fakeFinder)
		vmCID = apiv1.NewVMCID("fake-vm-id")
	})

	It("returns true when the VM exists", func() {
		fakeFinder.FindReturns(fakeVM, nil)
		fakeVM.ExistsReturns(true, nil)

		found, err := method.HasVM(vmCID)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())

		Expect(fakeFinder.FindCallCount()).To(Equal(1))
		Expect(fakeFinder.FindArgsForCall(0)).To(Equal(vmCID))
	})

	It("returns false when the VM does not exist", func() {
		fakeFinder.FindReturns(fakeVM, nil)
		fakeVM.ExistsReturns(false, nil)

		found, err := method.HasVM(vmCID)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
	})

	It("returns error when finding the VM fails", func() {
		fakeFinder.FindReturns(nil, errors.New("find-error"))

		_, err := method.HasVM(vmCID)
		Expect(err).To(MatchError(ContainSubstring("Finding VM")))
		Expect(err).To(MatchError(ContainSubstring("find-error")))
	})

	It("returns error when checking existence fails", func() {
		fakeFinder.FindReturns(fakeVM, nil)
		fakeVM.ExistsReturns(false, errors.New("exists-error"))

		_, err := method.HasVM(vmCID)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("exists-error")))
	})
})
