package cpi

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	"github.com/cppforlife/bosh-cpi-go/apiv1"

	bdisk "github.com/cppforlife/bosh-docker-cpi/disk"
)

type DeleteDiskMethod struct {
	diskFinder bdisk.Finder
}

func NewDeleteDiskMethod(diskFinder bdisk.Finder) DeleteDiskMethod {
	return DeleteDiskMethod{diskFinder: diskFinder}
}

func (a DeleteDiskMethod) DeleteDisk(diskCID apiv1.DiskCID) error {
	disk, err := a.diskFinder.Find(diskCID)
	if err != nil {
		return bosherr.WrapErrorf(err, "Finding disk '%s'", diskCID)
	}

	err = disk.Delete()
	if err != nil {
		return bosherr.WrapErrorf(err, "Deleting disk '%s'", diskCID)
	}

	return nil
}
