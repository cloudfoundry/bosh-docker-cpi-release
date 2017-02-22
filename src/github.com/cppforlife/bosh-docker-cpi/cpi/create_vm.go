package cpi

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	"github.com/cppforlife/bosh-cpi-go/apiv1"

	bstem "github.com/cppforlife/bosh-docker-cpi/stemcell"
	bvm "github.com/cppforlife/bosh-docker-cpi/vm"
)

type CreateVMMethod struct {
	stemcellFinder bstem.Finder
	vmCreator      bvm.Creator
}

func NewCreateVMMethod(stemcellFinder bstem.Finder, vmCreator bvm.Creator) CreateVMMethod {
	return CreateVMMethod{
		stemcellFinder: stemcellFinder,
		vmCreator:      vmCreator,
	}
}

func (a CreateVMMethod) CreateVM(
	agentID apiv1.AgentID, stemcellCID apiv1.StemcellCID, cloudProps apiv1.VMCloudProps,
	networks apiv1.Networks, diskCIDs []apiv1.DiskCID, env apiv1.VMEnv) (apiv1.VMCID, error) {

	stemcell, err := a.stemcellFinder.Find(stemcellCID)
	if err != nil {
		return apiv1.VMCID{}, bosherr.WrapErrorf(err, "Finding stemcell '%s'", stemcellCID)
	}

	vm, err := a.vmCreator.Create(agentID, stemcell, cloudProps, networks, diskCIDs, env)
	if err != nil {
		return apiv1.VMCID{}, bosherr.WrapErrorf(err, "Creating VM with agent ID '%s'", agentID)
	}

	return vm.ID(), nil
}
