package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

// CalculateVMCloudPropertiesMethod handles calculating VM cloud properties
type CalculateVMCloudPropertiesMethod struct{}

// NewCalculateVMCloudPropertiesMethod creates a new CalculateVMCloudPropertiesMethod
func NewCalculateVMCloudPropertiesMethod() CalculateVMCloudPropertiesMethod {
	return CalculateVMCloudPropertiesMethod{}
}

// CalculateVMCloudProperties calculates cloud properties for VM resources
func (a CalculateVMCloudPropertiesMethod) CalculateVMCloudProperties(_ apiv1.VMResources) (apiv1.VMCloudProps, error) {
	return apiv1.NewVMCloudPropsFromMap(map[string]interface{}{}), nil
}
