package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

type Disks struct{}

func NewDisks() Disks {
	return Disks{}
}

func (d Disks) SetDiskMetadata(cid apiv1.DiskCID, meta apiv1.DiskMeta) error {
	return nil
}

func (d Disks) ResizeDisk(cid apiv1.DiskCID, size int) error {
	return nil
}
