package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bdisk "bosh-docker-cpi/disk"
)

// HasDiskMethod handles checking if disks exist
type HasDiskMethod struct {
	diskFinder bdisk.Finder
}

// NewHasDiskMethod creates a new HasDiskMethod with the given disk finder
func NewHasDiskMethod(diskFinder bdisk.Finder) HasDiskMethod {
	return HasDiskMethod{diskFinder: diskFinder}
}

// HasDisk checks if a disk exists
func (a HasDiskMethod) HasDisk(cid apiv1.DiskCID) (bool, error) {
	disk, err := a.diskFinder.Find(cid)
	if err != nil {
		return false, bosherr.WrapErrorf(err, "Finding disk '%s'", cid)
	}

	return disk.Exists()
}
