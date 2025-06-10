package disk

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

// Creator creates persistent disks
type Creator interface {
	Create(int, *apiv1.VMCID) (Disk, error)
}

// Finder finds persistent disks by ID
type Finder interface {
	Find(apiv1.DiskCID) (Disk, error)
}

// Disk represents a persistent disk volume
type Disk interface {
	ID() apiv1.DiskCID

	Delete() error
	Exists() (bool, error)
}
