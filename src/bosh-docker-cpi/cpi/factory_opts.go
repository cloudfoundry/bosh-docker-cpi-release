package cpi

import (
	"strings"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
)

type FactoryOpts struct {
	Docker DockerOpts
	Agent  apiv1.AgentOptions
}

type DockerOpts struct {
	Host       string
	APIVersion string `json:"api_version"`
	TLS        DockerOptsTLS
}

type DockerOptsTLS struct {
	CA          string
	Certificate string
	PrivateKey  string `json:"private_key"`
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
		if len(o.TLS.CA) == 0 {
			return bosherr.Error("Must provide non-empty CA")
		}

		if len(o.TLS.Certificate) == 0 {
			return bosherr.Error("Must provide non-empty Certificate")
		}

		if len(o.TLS.PrivateKey) == 0 {
			return bosherr.Error("Must provide non-empty PrivateKey")
		}
	}

	return nil
}
