package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bdisk "bosh-docker-cpi/disk"
)

// DeleteDiskMethod handles deleting persistent disks
type DeleteDiskMethod struct {
	diskFinder bdisk.Finder
}

// NewDeleteDiskMethod creates a new DeleteDiskMethod with the given disk finder
func NewDeleteDiskMethod(diskFinder bdisk.Finder) DeleteDiskMethod {
	return DeleteDiskMethod{diskFinder: diskFinder}
}

// DeleteDisk deletes the specified persistent disk
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
