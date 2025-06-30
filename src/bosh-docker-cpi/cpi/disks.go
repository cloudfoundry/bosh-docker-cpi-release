package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

// Disks handles disk metadata operations
type Disks struct{}

// NewDisks creates a new Disks instance
func NewDisks() Disks {
	return Disks{}
}

// SetDiskMetadata sets metadata for a disk (not implemented)
func (d Disks) SetDiskMetadata(_ apiv1.DiskCID, _ apiv1.DiskMeta) error {
	return nil
}

// ResizeDisk resizes a disk (not implemented)
func (d Disks) ResizeDisk(_ apiv1.DiskCID, _ int) error {
	return nil
}
