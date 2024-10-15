package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

type Disks struct{}

func NewDisks() Disks {
	return Disks{}
}

func (d Disks) SetDiskMetadata(_ apiv1.DiskCID, _ apiv1.DiskMeta) error {
	return nil
}

func (d Disks) ResizeDisk(_ apiv1.DiskCID, _ int) error {
	return nil
}
