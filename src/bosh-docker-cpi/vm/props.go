package vm

import (
	dkrcont "github.com/docker/docker/api/types/container"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

type Props struct {
	ExposedPorts []string `json:"ports"` // [6868/tcp]

	// Allow all Docker options
	// ./src/github.com/docker/engine-api/types/container/host_config.go
	dkrcont.HostConfig `json:",inline"`
	specs.Platform
}

type NetProps struct {
	Name   string
	Driver string

	EnableIPv6 bool `json:"enable_ipv6"` // useful for dynamic networks since they don't specify subnet
}
