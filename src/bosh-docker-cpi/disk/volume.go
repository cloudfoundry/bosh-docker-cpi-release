package disk

import (
	"context"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrclient "github.com/docker/docker/client"
)

type Volume struct {
	id apiv1.DiskCID

	dkrClient *dkrclient.Client

	logger boshlog.Logger
}

func NewVolume(id apiv1.DiskCID, dkrClient *dkrclient.Client, logger boshlog.Logger) Volume {
	return Volume{id: id, dkrClient: dkrClient, logger: logger}
}

func (s Volume) ID() apiv1.DiskCID { return s.id }

func (s Volume) Delete() error {
	s.logger.Debug("Volume", "Deleting disk '%s'", s.id)

	err := s.dkrClient.VolumeRemove(context.TODO(), s.id.AsString(), true)
	if err != nil {
		return bosherr.WrapErrorf(err, "Deleting volume")
	}

	return nil
}

func (s Volume) Exists() (bool, error) {
	_, err := s.dkrClient.VolumeInspect(context.TODO(), s.id.AsString())
	if err != nil {
		if dkrclient.IsErrNotFound(err) {
			return false, nil
		}

		return false, bosherr.WrapError(err, "Finding volume")
	}

	return true, nil
}
