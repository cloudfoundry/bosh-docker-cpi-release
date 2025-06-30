package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bvm "bosh-docker-cpi/vm"
)

// DeleteVMMethod handles deleting VMs
type DeleteVMMethod struct {
	vmFinder bvm.Finder
}

// NewDeleteVMMethod creates a new DeleteVMMethod with the given VM finder
func NewDeleteVMMethod(vmFinder bvm.Finder) DeleteVMMethod {
	return DeleteVMMethod{vmFinder: vmFinder}
}

// DeleteVM deletes the specified VM
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
