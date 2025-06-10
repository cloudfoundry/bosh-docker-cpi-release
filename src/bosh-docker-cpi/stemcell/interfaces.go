package stemcell

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

// Importer imports stemcells from external sources
type Importer interface {
	ImportFromPath(string) (Stemcell, error)
}

// Finder finds stemcells by ID
type Finder interface {
	Find(apiv1.StemcellCID) (Stemcell, error)
}

// Stemcell represents a bootable VM image
type Stemcell interface {
	ID() apiv1.StemcellCID

	Delete() error
}
