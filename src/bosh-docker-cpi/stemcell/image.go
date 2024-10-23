package stemcell

import (
	"context"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrimages "github.com/docker/docker/api/types/image"
	dkrclient "github.com/docker/docker/client"
)

type Image struct {
	id apiv1.StemcellCID

	dkrClient *dkrclient.Client

	logger boshlog.Logger
}

func NewImage(id apiv1.StemcellCID, dkrClient *dkrclient.Client, logger boshlog.Logger) Image {
	return Image{id, dkrClient, logger}
}

func (s Image) ID() apiv1.StemcellCID { return s.id }

func (s Image) Delete() error {
	s.logger.Debug("Image", "Deleting stemcell '%s'", s.id)

	// todo remove forcefully?
	_, err := s.dkrClient.ImageRemove(context.TODO(), s.id.AsString(), dkrimages.RemoveOptions{Force: true})
	if err != nil {
		return bosherr.WrapErrorf(err, "Deleting stemcell image")
	}

	return nil
}
