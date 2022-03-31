package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"

	bvm "bosh-docker-cpi/vm"
)

type GetDisksMethod struct {
	vmFinder bvm.Finder
}

func NewGetDisksMethod(vmFinder bvm.Finder) GetDisksMethod {
	return GetDisksMethod{vmFinder}
}

func (a GetDisksMethod) GetDisks(cid apiv1.VMCID) ([]apiv1.DiskCID, error) {
	// todo implement
	return nil, nil
}
