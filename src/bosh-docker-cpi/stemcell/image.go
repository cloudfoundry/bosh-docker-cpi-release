package stemcell

import (
	"context"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrimages "github.com/docker/docker/api/types/image"
	dkrclient "github.com/docker/docker/client"
)

// Image represents a stemcell as a Docker image
type Image struct {
	id apiv1.StemcellCID

	dkrClient *dkrclient.Client

	logger boshlog.Logger
}

// NewImage creates a new Image with the given ID, Docker client, and logger
func NewImage(id apiv1.StemcellCID, dkrClient *dkrclient.Client, logger boshlog.Logger) Image {
	return Image{id, dkrClient, logger}
}

// ID returns the stemcell ID
func (s Image) ID() apiv1.StemcellCID { return s.id }

// Delete removes the stemcell image
func (s Image) Delete() error {
	s.logger.Debug("Image", "Deleting stemcell '%s'", s.id)

	// todo remove forcefully?
	_, err := s.dkrClient.ImageRemove(context.TODO(), s.id.AsString(), dkrimages.RemoveOptions{Force: true})
	if err != nil {
		return bosherr.WrapErrorf(err, "Deleting stemcell image")
	}

	return nil
}
