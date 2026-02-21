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

var _ = Describe("AttachDiskMethod", func() {
	var (
		fakeVMFinder   *vmfakes.FakeFinder
		fakeVM         *vmfakes.FakeVM
		fakeDiskFinder *diskfakes.FakeFinder
		fakeDisk       *diskfakes.FakeDisk
		method         cpi.AttachDiskMethod
		vmCID          apiv1.VMCID
		diskCID        apiv1.DiskCID
	)

	BeforeEach(func() {
		fakeVMFinder = &vmfakes.FakeFinder{}
		fakeVM = &vmfakes.FakeVM{}
		fakeDiskFinder = &diskfakes.FakeFinder{}
		fakeDisk = &diskfakes.FakeDisk{}
		method = cpi.NewAttachDiskMethod(fakeVMFinder, fakeDiskFinder)
		vmCID = apiv1.NewVMCID("fake-vm-id")
		diskCID = apiv1.NewDiskCID("fake-disk-id")
	})

	Describe("AttachDiskV2", func() {
		It("finds the VM and disk, then attaches the disk", func() {
			expectedHint := apiv1.NewDiskHintFromString("/warden-cpi-dev/fake-disk-id")
			fakeVMFinder.FindReturns(fakeVM, nil)
			fakeDiskFinder.FindReturns(fakeDisk, nil)
			fakeVM.AttachDiskReturns(expectedHint, nil)

			hint, err := method.AttachDiskV2(vmCID, diskCID)
			Expect(err).NotTo(HaveOccurred())
			Expect(hint).To(Equal(expectedHint))

			Expect(fakeVMFinder.FindCallCount()).To(Equal(1))
			Expect(fakeVMFinder.FindArgsForCall(0)).To(Equal(vmCID))

			Expect(fakeDiskFinder.FindCallCount()).To(Equal(1))
			Expect(fakeDiskFinder.FindArgsForCall(0)).To(Equal(diskCID))

			Expect(fakeVM.AttachDiskCallCount()).To(Equal(1))
			Expect(fakeVM.AttachDiskArgsForCall(0)).To(Equal(fakeDisk))
		})

		It("returns error when finding the VM fails", func() {
			fakeVMFinder.FindReturns(nil, errors.New("vm-find-error"))

			_, err := method.AttachDiskV2(vmCID, diskCID)
			Expect(err).To(MatchError(ContainSubstring("Finding VM")))
			Expect(err).To(MatchError(ContainSubstring("vm-find-error")))
		})

		It("returns error when finding the disk fails", func() {
			fakeVMFinder.FindReturns(fakeVM, nil)
			fakeDiskFinder.FindReturns(nil, errors.New("disk-find-error"))

			_, err := method.AttachDiskV2(vmCID, diskCID)
			Expect(err).To(MatchError(ContainSubstring("Finding disk")))
			Expect(err).To(MatchError(ContainSubstring("disk-find-error")))
		})

		It("returns error when attaching the disk fails", func() {
			fakeVMFinder.FindReturns(fakeVM, nil)
			fakeDiskFinder.FindReturns(fakeDisk, nil)
			fakeVM.AttachDiskReturns(apiv1.DiskHint{}, errors.New("attach-error"))

			_, err := method.AttachDiskV2(vmCID, diskCID)
			Expect(err).To(MatchError(ContainSubstring("Attaching disk")))
			Expect(err).To(MatchError(ContainSubstring("attach-error")))
		})
	})

	Describe("AttachDisk", func() {
		It("delegates to AttachDiskV2", func() {
			fakeVMFinder.FindReturns(fakeVM, nil)
			fakeDiskFinder.FindReturns(fakeDisk, nil)
			fakeVM.AttachDiskReturns(apiv1.NewDiskHintFromString("/path"), nil)

			err := method.AttachDisk(vmCID, diskCID)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
