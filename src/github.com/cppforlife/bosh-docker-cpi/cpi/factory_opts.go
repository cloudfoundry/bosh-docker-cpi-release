package cpi

import (
	"strings"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	"github.com/cppforlife/bosh-cpi-go/apiv1"
)

type FactoryOpts struct {
	Docker DockerOpts
	Agent  apiv1.AgentOptions
}

type DockerOpts struct {
	Host       string
	APIVersion string

	CACert     string
	Cert       string
	PrivateKey string
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

func (o DockerOpts) RequiresTLS() bool {
	return !strings.HasPrefix(o.Host, "unix://")
}

func (o DockerOpts) Validate() error {
	if o.Host == "" {
		return bosherr.Error("Must provide non-empty Host")
	}

	if o.APIVersion == "" {
		return bosherr.Error("Must provide non-empty APIVersion")
	}

	if o.RequiresTLS() {
		if len(o.CACert) == 0 {
			return bosherr.Error("Must provide non-empty CACert")
		}

		if len(o.Cert) == 0 {
			return bosherr.Error("Must provide non-empty Cert")
		}

		if len(o.PrivateKey) == 0 {
			return bosherr.Error("Must provide non-empty PrivateKey")
		}
	}

	return nil
}
