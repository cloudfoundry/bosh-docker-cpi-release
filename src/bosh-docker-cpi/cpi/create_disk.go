package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bdisk "bosh-docker-cpi/disk"
)

type CreateDiskMethod struct {
	diskCreator bdisk.Creator
}

func NewCreateDiskMethod(diskCreator bdisk.Creator) CreateDiskMethod {
	return CreateDiskMethod{diskCreator: diskCreator}
}

func (a CreateDiskMethod) CreateDisk(
	size int, _ apiv1.DiskCloudProps, vmCID *apiv1.VMCID) (apiv1.DiskCID, error) {

	disk, err := a.diskCreator.Create(size, vmCID)
	if err != nil {
		return apiv1.DiskCID{}, bosherr.WrapErrorf(err, "Creating disk of size '%d'", size)
	}

	return disk.ID(), nil
}
