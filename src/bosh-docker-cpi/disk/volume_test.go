package disk_test

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "bosh-docker-cpi/disk"
)

var _ = Describe("Volume", func() {
	Describe("ID", func() {
		It("returns the disk CID it was created with", func() {
			diskCID := apiv1.NewDiskCID("vol-abc123")
			vol := NewVolume(diskCID, nil, nil)
			Expect(vol.ID()).To(Equal(diskCID))
		})
	})
})
