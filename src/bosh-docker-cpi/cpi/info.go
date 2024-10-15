package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
)

type InfoMethod struct{}

func NewInfoMethod() InfoMethod {
	return InfoMethod{}
}

func (a InfoMethod) Info() (apiv1.Info, error) {
	return apiv1.Info{
		StemcellFormats: []string{"warden-tar", "general-tar"},
		APIVersion:      2,
	}, nil
}
