package cpi_test

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bosh-docker-cpi/cpi"
)

var _ = Describe("RebootVMMethod", func() {
	It("returns nil", func() {
		method := cpi.NewRebootVMMethod()
		err := method.RebootVM(apiv1.NewVMCID("any-vm"))
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("SetVMMetadataMethod", func() {
	It("returns nil", func() {
		method := cpi.NewSetVMMetadataMethod()
		err := method.SetVMMetadata(apiv1.NewVMCID("any-vm"), apiv1.VMMeta{})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("CalculateVMCloudPropertiesMethod", func() {
	It("returns empty cloud properties without error", func() {
		method := cpi.NewCalculateVMCloudPropertiesMethod()
		props, err := method.CalculateVMCloudProperties(apiv1.VMResources{})
		Expect(err).NotTo(HaveOccurred())
		Expect(props).NotTo(BeNil())
	})
})

var _ = Describe("GetDisksMethod", func() {
	It("returns nil without error", func() {
		method := cpi.NewGetDisksMethod(nil)
		disks, err := method.GetDisks(apiv1.NewVMCID("any-vm"))
		Expect(err).NotTo(HaveOccurred())
		Expect(disks).To(BeNil())
	})
})

var _ = Describe("Disks", func() {
	var disks cpi.Disks

	BeforeEach(func() {
		disks = cpi.NewDisks()
	})

	Describe("SetDiskMetadata", func() {
		It("returns nil", func() {
			err := disks.SetDiskMetadata(apiv1.NewDiskCID("any-disk"), apiv1.DiskMeta{})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("ResizeDisk", func() {
		It("returns nil", func() {
			err := disks.ResizeDisk(apiv1.NewDiskCID("any-disk"), 2048)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("Snapshots", func() {
	var snapshots cpi.Snapshots

	BeforeEach(func() {
		snapshots = cpi.NewSnapshots()
	})

	Describe("SnapshotDisk", func() {
		It("returns empty snapshot CID without error", func() {
			cid, err := snapshots.SnapshotDisk(apiv1.NewDiskCID("any-disk"), apiv1.DiskMeta{})
			Expect(err).NotTo(HaveOccurred())
			Expect(cid).To(Equal(apiv1.SnapshotCID{}))
		})
	})

	Describe("DeleteSnapshot", func() {
		It("returns nil", func() {
			err := snapshots.DeleteSnapshot(apiv1.SnapshotCID{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
