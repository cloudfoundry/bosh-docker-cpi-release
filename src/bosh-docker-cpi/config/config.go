package config

import (
	"encoding/json"
	"strings"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
)

// Config holds configuration settings for the Docker CPI
type Config struct {
	Actions FactoryOpts

	StartContainersWithSystemD bool `json:"start_containers_with_systemd"`
}

// DockerOpts contains Docker connection configuration
type DockerOpts struct {
	Host       string
	APIVersion string `json:"api_version"`
	TLS        DockerOptsTLS
}

// RequiresTLS returns true if TLS is required for this Docker connection
func (o DockerOpts) RequiresTLS() bool {
	return !strings.HasPrefix(o.Host, "unix://")
}

// Validate checks that the Docker options are valid
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

// DockerOptsTLS contains TLS configuration for Docker connections
type DockerOptsTLS struct {
	CA          string
	Certificate string
	PrivateKey  string `json:"private_key"`
}

// FactoryOpts contains options for creating CPI factories
type FactoryOpts struct {
	Docker DockerOpts
	Agent  apiv1.AgentOptions
}

// Validate checks that the factory options are valid
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

// NewConfigFromPath loads configuration from a file path
func NewConfigFromPath(path string, fs boshsys.FileSystem) (Config, error) {
	var config Config

	bytes, err := fs.ReadFile(path)
	if err != nil {
		return config, bosherr.WrapErrorf(err, "Reading config '%s'", path)
	}

	err = json.Unmarshal(bytes, &config)
	if err != nil {
		return config, bosherr.WrapError(err, "Unmarshalling config")
	}

	err = config.Validate()
	if err != nil {
		return config, bosherr.WrapError(err, "Validating config")
	}

	return config, nil
}

// Validate checks that the configuration is valid
func (c Config) Validate() error {
	err := c.Actions.Validate()
	if err != nil {
		return bosherr.WrapError(err, "Validating Actions configuration")
	}

	return nil
}
