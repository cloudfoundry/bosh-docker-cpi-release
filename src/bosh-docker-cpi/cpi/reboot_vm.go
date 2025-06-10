package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

// RebootVMMethod handles VM reboot operations
type RebootVMMethod struct{}

// NewRebootVMMethod creates a new RebootVMMethod
func NewRebootVMMethod() RebootVMMethod {
	return RebootVMMethod{}
}

// RebootVM reboots a VM (not implemented)
func (a RebootVMMethod) RebootVM(_ apiv1.VMCID) error {
	return nil
}
