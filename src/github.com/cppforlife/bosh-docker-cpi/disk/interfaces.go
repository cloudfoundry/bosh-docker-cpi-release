package disk

import (
	"github.com/cppforlife/bosh-cpi-go/apiv1"
)

type Creator interface {
	Create(int, *apiv1.VMCID) (Disk, error)
}

type Finder interface {
	Find(apiv1.DiskCID) (Disk, error)
}

type Disk interface {
	ID() apiv1.DiskCID

	Delete() error
	Exists() (bool, error)
}
