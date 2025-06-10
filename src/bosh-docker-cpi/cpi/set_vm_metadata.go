package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

// SetVMMetadataMethod handles setting VM metadata
type SetVMMetadataMethod struct{}

// NewSetVMMetadataMethod creates a new SetVMMetadataMethod
func NewSetVMMetadataMethod() SetVMMetadataMethod {
	return SetVMMetadataMethod{}
}

// SetVMMetadata sets metadata on a VM (not implemented)
func (a SetVMMetadataMethod) SetVMMetadata(_ apiv1.VMCID, _ apiv1.VMMeta) error { // TODO: can properties be set on the container
	return nil
}
