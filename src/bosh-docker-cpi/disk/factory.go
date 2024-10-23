package disk

import (
	"context"
	"encoding/json"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	dkrvoltypes "github.com/docker/docker/api/types/volume"
	dkrclient "github.com/docker/docker/client"
)

type Factory struct {
	dkrClient *dkrclient.Client
	uuidGen   boshuuid.Generator

	logTag string
	logger boshlog.Logger
}

func NewFactory(
	dkrClient *dkrclient.Client,
	uuidGen boshuuid.Generator,
	logger boshlog.Logger,
) Factory {
	return Factory{
		dkrClient: dkrClient,
		uuidGen:   uuidGen,

		logTag: "disk.Factory",
		logger: logger,
	}
}

func (f Factory) Create(size int, vmCID *apiv1.VMCID) (Disk, error) {
	f.logger.Debug(f.logTag, "Creating disk of size '%d'", size)

	id, err := f.uuidGen.Generate()
	if err != nil {
		return nil, bosherr.WrapError(err, "Generating disk ID")
	}

	id = "vol-" + id

	// todo allow other drivers
	opts := dkrvoltypes.CreateOptions{
		Name:   id,
		Driver: "local",
	}

	if vmCID != nil {
		node, err := f.possiblyFindNodeWithContainer(*vmCID)
		if err != nil {
			return nil, bosherr.WrapError(err, "Finding node for container")
		}

		if len(node) > 0 {
			// Choose specific node for local disk creation
			opts.Name = node + "/" + opts.Name
		}
	}

	_, err = f.dkrClient.VolumeCreate(context.TODO(), opts)
	if err != nil {
		return nil, bosherr.WrapError(err, "Creating volume")
	}

	return NewVolume(apiv1.NewDiskCID(id), f.dkrClient, f.logger), nil
}

func (f Factory) Find(id apiv1.DiskCID) (Disk, error) {
	return NewVolume(id, f.dkrClient, f.logger), nil
}

func (f Factory) possiblyFindNodeWithContainer(vmCID apiv1.VMCID) (string, error) {
	_, rawResp, err := f.dkrClient.ContainerInspectWithRaw(context.TODO(), vmCID.AsString(), false)
	if err != nil {
		return "", bosherr.WrapError(err, "Inspecting container")
	}

	var resp containerResp

	err = json.Unmarshal(rawResp, &resp)
	if err != nil {
		return "", bosherr.WrapError(err, "Unmarshalling raw container resp")
	}

	return resp.Node.Name, nil
}

type containerResp struct {
	Node nodeResp
}

type nodeResp struct {
	Name string
}
