package stemcell

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrclient "github.com/docker/docker/client"
)

// FSFinder finds stemcells as Docker images
type FSFinder struct {
	dkrClient *dkrclient.Client

	logger boshlog.Logger
}

// NewFSFinder creates a new FSFinder with the given Docker client and logger
func NewFSFinder(dkrClient *dkrclient.Client, logger boshlog.Logger) FSFinder {
	return FSFinder{dkrClient: dkrClient, logger: logger}
}

// Find returns a stemcell by ID
func (f FSFinder) Find(id apiv1.StemcellCID) (Stemcell, error) {
	return NewImage(id, f.dkrClient, f.logger), nil
}
