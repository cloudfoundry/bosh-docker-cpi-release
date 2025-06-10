package vm

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"

	bdisk "bosh-docker-cpi/disk"
	bstem "bosh-docker-cpi/stemcell"
)

// Creator creates VMs
type Creator interface {
	Create(apiv1.AgentID, bstem.Stemcell, apiv1.VMCloudProps,
		apiv1.Networks, []apiv1.DiskCID, apiv1.VMEnv) (VM, error)
}

var _ Creator = Factory{}

// Finder finds VMs by ID
type Finder interface {
	Find(apiv1.VMCID) (VM, error)
}

var _ Finder = Factory{}

// VM represents a virtual machine
type VM interface {
	ID() apiv1.VMCID

	Delete() error
	Exists() (bool, error)

	AttachDisk(bdisk.Disk) (apiv1.DiskHint, error)
	DetachDisk(bdisk.Disk) error
}

var _ VM = Container{}

// AgentEnvService manages BOSH agent environment
type AgentEnvService interface {
	// Fetch will return an error if Update was not called beforehand
	Fetch() (apiv1.AgentEnv, error)
	Update(apiv1.AgentEnv) error
}

var _ AgentEnvService = fsAgentEnvService{}
