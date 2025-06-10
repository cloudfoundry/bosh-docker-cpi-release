package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

// Snapshots handles disk snapshot operations
type Snapshots struct{}

// NewSnapshots creates a new Snapshots instance
func NewSnapshots() Snapshots {
	return Snapshots{}
}

// SnapshotDisk creates a snapshot of a disk (not implemented)
func (s Snapshots) SnapshotDisk(_ apiv1.DiskCID, _ apiv1.DiskMeta) (apiv1.SnapshotCID, error) {
	return apiv1.SnapshotCID{}, nil
}

// DeleteSnapshot deletes a disk snapshot (not implemented)
func (s Snapshots) DeleteSnapshot(_ apiv1.SnapshotCID) error {
	return nil
}
