package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

type SetVMMetadataMethod struct{}

func NewSetVMMetadataMethod() SetVMMetadataMethod {
	return SetVMMetadataMethod{}
}

func (a SetVMMetadataMethod) SetVMMetadata(_ apiv1.VMCID, _ apiv1.VMMeta) error { // TODO: can properties be set on the container
	return nil
}
