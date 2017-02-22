package stemcell

import (
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	"github.com/cppforlife/bosh-cpi-go/apiv1"
	dkrclient "github.com/docker/engine-api/client"
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
