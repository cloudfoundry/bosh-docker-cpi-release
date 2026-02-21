package stemcell

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

//go:generate go tool counterfeiter -generate

//counterfeiter:generate . Importer

type Importer interface {
	ImportFromPath(string) (Stemcell, error)
}

//counterfeiter:generate . Finder

type Finder interface {
	Find(apiv1.StemcellCID) (Stemcell, error)
}

//counterfeiter:generate . Stemcell

type Stemcell interface {
	ID() apiv1.StemcellCID

	Delete() error
}
