package disk

import (
	"context"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrclient "github.com/docker/docker/client"
	cerrdefs "github.com/docker/docker/errdefs"
)

// Volume represents a Docker volume as a persistent disk
type Volume struct {
	id apiv1.DiskCID

	dkrClient *dkrclient.Client

	logger boshlog.Logger
}

// NewVolume creates a new Volume with the given ID and Docker client
func NewVolume(id apiv1.DiskCID, dkrClient *dkrclient.Client, logger boshlog.Logger) Volume {
	return Volume{id: id, dkrClient: dkrClient, logger: logger}
}

// ID returns the disk ID
func (s Volume) ID() apiv1.DiskCID { return s.id }

// Delete removes the volume
func (s Volume) Delete() error {
	s.logger.Debug("Volume", "Deleting disk '%s'", s.id)

	err := s.dkrClient.VolumeRemove(context.TODO(), s.id.AsString(), true)
	if err != nil {
		return bosherr.WrapErrorf(err, "Deleting volume")
	}

	return nil
}

// Exists checks if the volume exists
func (s Volume) Exists() (bool, error) {
	_, err := s.dkrClient.VolumeInspect(context.TODO(), s.id.AsString())
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return false, nil
		}

		return false, bosherr.WrapError(err, "Finding volume")
	}

	return true, nil
}
