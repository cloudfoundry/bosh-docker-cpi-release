package disk

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

//go:generate go tool counterfeiter -generate

//counterfeiter:generate . Creator

type Creator interface {
	Create(int, *apiv1.VMCID) (Disk, error)
}

//counterfeiter:generate . Finder

type Finder interface {
	Find(apiv1.DiskCID) (Disk, error)
}

//counterfeiter:generate . Disk

type Disk interface {
	ID() apiv1.DiskCID

	Delete() error
	Exists() (bool, error)
}
