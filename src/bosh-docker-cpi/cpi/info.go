package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

// InfoMethod handles CPI info requests
type InfoMethod struct{}

// NewInfoMethod creates a new InfoMethod
func NewInfoMethod() InfoMethod {
	return InfoMethod{}
}

// Info returns CPI information including supported stemcell formats and API version
func (a InfoMethod) Info() (apiv1.Info, error) {
	return apiv1.Info{
		StemcellFormats: []string{"warden-tar", "general-tar"},
		APIVersion:      2,
	}, nil
}
