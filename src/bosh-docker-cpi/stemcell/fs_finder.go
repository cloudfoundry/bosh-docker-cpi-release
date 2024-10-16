package stemcell

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrclient "github.com/docker/docker/client"
)

type FSFinder struct {
	dkrClient *dkrclient.Client

	logger boshlog.Logger
}

func NewFSFinder(dkrClient *dkrclient.Client, logger boshlog.Logger) FSFinder {
	return FSFinder{dkrClient: dkrClient, logger: logger}
}

func (f FSFinder) Find(id apiv1.StemcellCID) (Stemcell, error) {
	return NewImage(id, f.dkrClient, f.logger), nil
}
