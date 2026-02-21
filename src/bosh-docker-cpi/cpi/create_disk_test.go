package cpi_test

import (
	"errors"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
	"bosh-docker-cpi/disk/diskfakes"
)

var _ = Describe("CreateDiskMethod", func() {
	var (
		fakeCreator *diskfakes.FakeCreator
		fakeDisk    *diskfakes.FakeDisk
		method      cpi.CreateDiskMethod
	)

	BeforeEach(func() {
		fakeCreator = &diskfakes.FakeCreator{}
		fakeDisk = &diskfakes.FakeDisk{}
		method = cpi.NewCreateDiskMethod(fakeCreator)
	})

	It("creates a disk and returns its CID", func() {
		expectedCID := apiv1.NewDiskCID("vol-new-disk")
		fakeDisk.IDReturns(expectedCID)
		fakeCreator.CreateReturns(fakeDisk, nil)

		vmCID := apiv1.NewVMCID("vm-123")
		cid, err := method.CreateDisk(1024, nil, &vmCID)
		Expect(err).NotTo(HaveOccurred())
		Expect(cid).To(Equal(expectedCID))

		Expect(fakeCreator.CreateCallCount()).To(Equal(1))
		size, passedVMCID := fakeCreator.CreateArgsForCall(0)
		Expect(size).To(Equal(1024))
		Expect(passedVMCID).To(Equal(&vmCID))
	})

	It("passes nil vmCID when not provided", func() {
		fakeDisk.IDReturns(apiv1.NewDiskCID("vol-new-disk"))
		fakeCreator.CreateReturns(fakeDisk, nil)

		_, err := method.CreateDisk(2048, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		_, passedVMCID := fakeCreator.CreateArgsForCall(0)
		Expect(passedVMCID).To(BeNil())
	})

	It("returns error when creating the disk fails", func() {
		fakeCreator.CreateReturns(nil, errors.New("create-error"))

		_, err := method.CreateDisk(1024, nil, nil)
		Expect(err).To(MatchError(ContainSubstring("Creating disk of size")))
		Expect(err).To(MatchError(ContainSubstring("create-error")))
	})
})
