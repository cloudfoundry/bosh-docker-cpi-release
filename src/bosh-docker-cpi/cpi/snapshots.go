package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

type Snapshots struct{}

func NewSnapshots() Snapshots {
	return Snapshots{}
}

func (s Snapshots) SnapshotDisk(_ apiv1.DiskCID, _ apiv1.DiskMeta) (apiv1.SnapshotCID, error) {
	return apiv1.SnapshotCID{}, nil
}

func (s Snapshots) DeleteSnapshot(_ apiv1.SnapshotCID) error {
	return nil
}
