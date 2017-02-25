package cpi

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	"github.com/cppforlife/bosh-cpi-go/apiv1"
)

type FactoryOpts struct {
	Docker DockerOpts
	Agent  apiv1.AgentOptions
}

type DockerOpts struct {
	Host       string
	CACert     string
	APIVersion string
	CertFile   string
	KeyFile    string
}

func (o FactoryOpts) Validate() error {
	err := o.Docker.Validate()
	if err != nil {
		return bosherr.WrapError(err, "Validating Docker configuration")
	}

	err = o.Agent.Validate()
	if err != nil {
		return bosherr.WrapError(err, "Validating Agent configuration")
	}

	return nil
}

func (o DockerOpts) Validate() error {
	if o.Host == "" {
		return bosherr.Error("Must provide non-empty Host")
	}

	if o.APIVersion == "" {
		return bosherr.Error("Must provide non-empty APIVersion")
	}

	if len(o.CACert) == 0 {
		return bosherr.Error("Must provide non-empty CACert")
	}

	return nil
}
