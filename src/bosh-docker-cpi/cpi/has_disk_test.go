package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/disk/diskfakes"
)

var _ = Describe("HasDiskMethod", func() {
	var (
		fakeFinder *diskfakes.FakeFinder
		fakeDisk   *diskfakes.FakeDisk
		method     cpi.HasDiskMethod
		diskCID    apiv1.DiskCID
	)

	BeforeEach(func() {
		fakeFinder = &diskfakes.FakeFinder{}
		fakeDisk = &diskfakes.FakeDisk{}
		method = cpi.NewHasDiskMethod(fakeFinder)
		diskCID = apiv1.NewDiskCID("fake-disk-id")
	})

	It("returns true when the disk exists", func() {
		fakeFinder.FindReturns(fakeDisk, nil)
		fakeDisk.ExistsReturns(true, nil)

		found, err := method.HasDisk(diskCID)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())

		Expect(fakeFinder.FindCallCount()).To(Equal(1))
		Expect(fakeFinder.FindArgsForCall(0)).To(Equal(diskCID))
	})

	It("returns false when the disk does not exist", func() {
		fakeFinder.FindReturns(fakeDisk, nil)
		fakeDisk.ExistsReturns(false, nil)

		found, err := method.HasDisk(diskCID)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
	})

	It("returns error when finding the disk fails", func() {
		fakeFinder.FindReturns(nil, errors.New("find-error"))

		_, err := method.HasDisk(diskCID)
		Expect(err).To(MatchError(ContainSubstring("Finding disk")))
		Expect(err).To(MatchError(ContainSubstring("find-error")))
	})

	It("returns error when checking existence fails", func() {
		fakeFinder.FindReturns(fakeDisk, nil)
		fakeDisk.ExistsReturns(false, errors.New("exists-error"))

		_, err := method.HasDisk(diskCID)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("exists-error")))
	})
})
