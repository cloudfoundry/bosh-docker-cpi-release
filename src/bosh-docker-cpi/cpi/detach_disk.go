package cpi

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"

	bdisk "bosh-docker-cpi/disk"
	bvm "bosh-docker-cpi/vm"
)

type DetachDiskMethod struct {
	vmFinder   bvm.Finder
	diskFinder bdisk.Finder
}

func NewDetachDiskMethod(vmFinder bvm.Finder, diskFinder bdisk.Finder) DetachDiskMethod {
	return DetachDiskMethod{
		vmFinder:   vmFinder,
		diskFinder: diskFinder,
	}
}

func (a DetachDiskMethod) DetachDisk(vmCID apiv1.VMCID, diskCID apiv1.DiskCID) error {
	vm, err := a.vmFinder.Find(vmCID)
	if err != nil {
		return bosherr.WrapErrorf(err, "Finding VM '%s'", vmCID)
	}

	disk, err := a.diskFinder.Find(diskCID)
	if err != nil {
		return bosherr.WrapErrorf(err, "Finding disk '%s'", diskCID)
	}

	err = vm.DetachDisk(disk)
	if err != nil {
		return bosherr.WrapErrorf(err, "Detaching disk '%s' to VM '%s'", diskCID, vmCID)
	}

	return nil
}
