package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/disk/diskfakes"
	"bosh-docker-cpi/vm/vmfakes"
)

var _ = Describe("DetachDiskMethod", func() {
	var (
		fakeVMFinder   *vmfakes.FakeFinder
		fakeVM         *vmfakes.FakeVM
		fakeDiskFinder *diskfakes.FakeFinder
		fakeDisk       *diskfakes.FakeDisk
		method         cpi.DetachDiskMethod
		vmCID          apiv1.VMCID
		diskCID        apiv1.DiskCID
	)

	BeforeEach(func() {
		fakeVMFinder = &vmfakes.FakeFinder{}
		fakeVM = &vmfakes.FakeVM{}
		fakeDiskFinder = &diskfakes.FakeFinder{}
		fakeDisk = &diskfakes.FakeDisk{}
		method = cpi.NewDetachDiskMethod(fakeVMFinder, fakeDiskFinder)
		vmCID = apiv1.NewVMCID("fake-vm-id")
		diskCID = apiv1.NewDiskCID("fake-disk-id")
	})

	It("finds the VM and disk, then detaches the disk", func() {
		fakeVMFinder.FindReturns(fakeVM, nil)
		fakeDiskFinder.FindReturns(fakeDisk, nil)
		fakeVM.DetachDiskReturns(nil)

		err := method.DetachDisk(vmCID, diskCID)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeVMFinder.FindCallCount()).To(Equal(1))
		Expect(fakeVMFinder.FindArgsForCall(0)).To(Equal(vmCID))

		Expect(fakeDiskFinder.FindCallCount()).To(Equal(1))
		Expect(fakeDiskFinder.FindArgsForCall(0)).To(Equal(diskCID))

		Expect(fakeVM.DetachDiskCallCount()).To(Equal(1))
		Expect(fakeVM.DetachDiskArgsForCall(0)).To(Equal(fakeDisk))
	})

	It("returns error when finding the VM fails", func() {
		fakeVMFinder.FindReturns(nil, errors.New("vm-find-error"))

		err := method.DetachDisk(vmCID, diskCID)
		Expect(err).To(MatchError(ContainSubstring("Finding VM")))
		Expect(err).To(MatchError(ContainSubstring("vm-find-error")))
	})

	It("returns error when finding the disk fails", func() {
		fakeVMFinder.FindReturns(fakeVM, nil)
		fakeDiskFinder.FindReturns(nil, errors.New("disk-find-error"))

		err := method.DetachDisk(vmCID, diskCID)
		Expect(err).To(MatchError(ContainSubstring("Finding disk")))
		Expect(err).To(MatchError(ContainSubstring("disk-find-error")))
	})

	It("returns error when detaching the disk fails", func() {
		fakeVMFinder.FindReturns(fakeVM, nil)
		fakeDiskFinder.FindReturns(fakeDisk, nil)
		fakeVM.DetachDiskReturns(errors.New("detach-error"))

		err := method.DetachDisk(vmCID, diskCID)
		Expect(err).To(MatchError(ContainSubstring("Detaching disk")))
		Expect(err).To(MatchError(ContainSubstring("detach-error")))
	})
})
