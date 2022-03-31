package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bdisk "bosh-docker-cpi/disk"
	bvm "bosh-docker-cpi/vm"
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
	_, err := a.AttachDiskV2(vmCID, diskCID)
	return err
}

func (a AttachDiskMethod) AttachDiskV2(vmCID apiv1.VMCID, diskCID apiv1.DiskCID) (apiv1.DiskHint, error) {
	vm, err := a.vmFinder.Find(vmCID)
	if err != nil {
		return apiv1.DiskHint{}, bosherr.WrapErrorf(err, "Finding VM '%s'", vmCID)
	}

	disk, err := a.diskFinder.Find(diskCID)
	if err != nil {
		return apiv1.DiskHint{}, bosherr.WrapErrorf(err, "Finding disk '%s'", diskCID)
	}

	diskHint, err := vm.AttachDisk(disk)
	if err != nil {
		return apiv1.DiskHint{}, bosherr.WrapErrorf(err, "Attaching disk '%s' to VM '%s'", diskCID, vmCID)
	}

	return diskHint, nil
}
