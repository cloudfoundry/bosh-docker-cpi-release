package vm

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"

	bdisk "bosh-docker-cpi/disk"
	bstem "bosh-docker-cpi/stemcell"
)

type Creator interface {
	Create(apiv1.AgentID, bstem.Stemcell, apiv1.VMCloudProps,
		apiv1.Networks, []apiv1.DiskCID, apiv1.VMEnv) (VM, error)
}

var _ Creator = Factory{}

type Finder interface {
	Find(apiv1.VMCID) (VM, error)
}

var _ Finder = Factory{}

type VM interface {
	ID() apiv1.VMCID

	Delete() error
	Exists() (bool, error)

	AttachDisk(bdisk.Disk) (apiv1.DiskHint, error)
	DetachDisk(bdisk.Disk) error
}

var _ VM = Container{}

type AgentEnvService interface {
	// Fetch will return an error if Update was not called beforehand
	Fetch() (apiv1.AgentEnv, error)
	Update(apiv1.AgentEnv) error
}

var _ AgentEnvService = fsAgentEnvService{}
