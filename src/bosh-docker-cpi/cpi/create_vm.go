package cpi

import (
	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"

	bstem "bosh-docker-cpi/stemcell"
	bvm "bosh-docker-cpi/vm"
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

	vmCID, _, err := a.CreateVMV2(agentID, stemcellCID, cloudProps, networks, diskCIDs, env)
	return vmCID, err
}


func (a CreateVMMethod) CreateVMV2(
	agentID apiv1.AgentID, stemcellCID apiv1.StemcellCID, cloudProps apiv1.VMCloudProps,
	networks apiv1.Networks, diskCIDs []apiv1.DiskCID, env apiv1.VMEnv) (apiv1.VMCID, apiv1.Networks, error) {

	stemcell, err := a.stemcellFinder.Find(stemcellCID)
	if err != nil {
		return apiv1.VMCID{}, networks, bosherr.WrapErrorf(err, "Finding stemcell '%s'", stemcellCID)
	}

	vm, err := a.vmCreator.Create(agentID, stemcell, cloudProps, networks, diskCIDs, env)
	if err != nil {
		return apiv1.VMCID{}, networks, bosherr.WrapErrorf(err, "Creating VM with agent ID '%s'", agentID)
	}

	return vm.ID(), networks, nil
}
