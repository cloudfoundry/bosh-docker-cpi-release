package cpi

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	"github.com/cppforlife/bosh-cpi-go/apiv1"

	bdisk "github.com/cppforlife/bosh-docker-cpi/disk"
	bvm "github.com/cppforlife/bosh-docker-cpi/vm"
)

type AttachDiskMethod struct {
	vmFinder   bvm.Finder
	diskFinder bdisk.Finder
}

func NewAttachDiskMethod(vmFinder bvm.Finder, diskFinder bdisk.Finder) AttachDiskMethod {
	return AttachDiskMethod{
		vmFinder:   vmFinder,
		diskFinder: diskFinder,
	}
}

func (a AttachDiskMethod) AttachDisk(vmCID apiv1.VMCID, diskCID apiv1.DiskCID) error {
	vm, err := a.vmFinder.Find(vmCID)
	if err != nil {
		return bosherr.WrapErrorf(err, "Finding VM '%s'", vmCID)
	}

	disk, err := a.diskFinder.Find(diskCID)
	if err != nil {
		return bosherr.WrapErrorf(err, "Finding disk '%s'", diskCID)
	}

	err = vm.AttachDisk(disk)
	if err != nil {
		return bosherr.WrapErrorf(err, "Attaching disk '%s' to VM '%s'", diskCID, vmCID)
	}

	return nil
}
