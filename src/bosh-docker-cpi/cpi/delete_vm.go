package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bvm "bosh-docker-cpi/vm"
)

type DeleteVMMethod struct {
	vmFinder bvm.Finder
}

func NewDeleteVMMethod(vmFinder bvm.Finder) DeleteVMMethod {
	return DeleteVMMethod{vmFinder: vmFinder}
}

func (a DeleteVMMethod) DeleteVM(cid apiv1.VMCID) error {
	vm, err := a.vmFinder.Find(cid)
	if err != nil {
		return bosherr.WrapErrorf(err, "Finding vm '%s'", cid)
	}

	err = vm.Delete()
	if err != nil {
		return bosherr.WrapErrorf(err, "Deleting vm '%s'", cid)
	}

	return nil
}
