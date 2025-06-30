package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"

	bvm "bosh-docker-cpi/vm"
)

// GetDisksMethod handles retrieving disks attached to VMs
type GetDisksMethod struct {
	vmFinder bvm.Finder
}

// NewGetDisksMethod creates a new GetDisksMethod with the given VM finder
func NewGetDisksMethod(vmFinder bvm.Finder) GetDisksMethod {
	return GetDisksMethod{vmFinder}
}

// GetDisks returns the list of disks attached to a VM (not implemented)
func (a GetDisksMethod) GetDisks(_ apiv1.VMCID) ([]apiv1.DiskCID, error) { // TODO implement
	return nil, nil
}
