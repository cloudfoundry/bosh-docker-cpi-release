package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/disk/diskfakes"
)

var _ = Describe("DeleteDiskMethod", func() {
	var (
		fakeFinder *diskfakes.FakeFinder
		fakeDisk   *diskfakes.FakeDisk
		method     cpi.DeleteDiskMethod
		diskCID    apiv1.DiskCID
	)

	BeforeEach(func() {
		fakeFinder = &diskfakes.FakeFinder{}
		fakeDisk = &diskfakes.FakeDisk{}
		method = cpi.NewDeleteDiskMethod(fakeFinder)
		diskCID = apiv1.NewDiskCID("fake-disk-id")
	})

	It("finds and deletes the disk", func() {
		fakeFinder.FindReturns(fakeDisk, nil)
		fakeDisk.DeleteReturns(nil)

		err := method.DeleteDisk(diskCID)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeFinder.FindCallCount()).To(Equal(1))
		Expect(fakeFinder.FindArgsForCall(0)).To(Equal(diskCID))
		Expect(fakeDisk.DeleteCallCount()).To(Equal(1))
	})

	It("returns error when finding the disk fails", func() {
		fakeFinder.FindReturns(nil, errors.New("find-error"))

		err := method.DeleteDisk(diskCID)
		Expect(err).To(MatchError(ContainSubstring("Finding disk")))
		Expect(err).To(MatchError(ContainSubstring("find-error")))
	})

	It("returns error when deleting the disk fails", func() {
		fakeFinder.FindReturns(fakeDisk, nil)
		fakeDisk.DeleteReturns(errors.New("delete-error"))

		err := method.DeleteDisk(diskCID)
		Expect(err).To(MatchError(ContainSubstring("Deleting disk")))
		Expect(err).To(MatchError(ContainSubstring("delete-error")))
	})
})
