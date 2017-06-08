package vm

import (
	"context"
	"strings"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	"github.com/cppforlife/bosh-cpi-go/apiv1"
	dkrclient "github.com/docker/engine-api/client"
	dkrtypes "github.com/docker/engine-api/types"
	dkrcont "github.com/docker/engine-api/types/container"
	dkrfilters "github.com/docker/engine-api/types/filters"
	dkrnet "github.com/docker/engine-api/types/network"
	dkrstrslice "github.com/docker/engine-api/types/strslice"
	dkrnat "github.com/docker/go-connections/nat"

	bstem "github.com/cppforlife/bosh-docker-cpi/stemcell"
)

type Factory struct {
	dkrClient *dkrclient.Client
	uuidGen   boshuuid.Generator

	agentOptions apiv1.AgentOptions

	logTag string
	logger boshlog.Logger
}

func NewFactory(
	dkrClient *dkrclient.Client,
	uuidGen boshuuid.Generator,
	agentOptions apiv1.AgentOptions,
	logger boshlog.Logger,
) Factory {
	return Factory{
		dkrClient: dkrClient,
		uuidGen:   uuidGen,

		agentOptions: agentOptions,

		logTag: "vm.Factory",
		logger: logger,
	}
}

func (f Factory) Create(agentID apiv1.AgentID, stemcell bstem.Stemcell,
	cloudProps apiv1.VMCloudProps, networks apiv1.Networks,
	diskCIDs []apiv1.DiskCID, env apiv1.VMEnv) (VM, error) {

	if len(networks) == 0 {
		return Container{}, bosherr.Error("Expected exactly one network; received zero")
	}

	for _, net := range networks {
		net.SetPreconfigured()
	}

	network := networks.Default()

	var vmProps VMProps

	err := cloudProps.As(&vmProps)
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Unmarshaling VM properties")
	}

	var netProps NetProps

	err = network.CloudProps().As(&netProps)
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Unmarshaling network properties")
	}

	if len(netProps.Name) == 0 {
		return Container{}, bosherr.WrapError(err, "Expected network to specify 'name'")
	}

	idStr, err := f.uuidGen.Generate()
	if err != nil {
		return nil, bosherr.WrapError(err, "Generating container ID")
	}

	id := apiv1.NewVMCID(idStr)

	containerConfig := &dkrcont.Config{
		Image:        stemcell.ID().AsString(),
		ExposedPorts: map[dkrnat.Port]struct{}{}, // todo what ports?

		// todo hacky umount to avoid conflicting with bosh dns updates
		// todo why perms are wrong on /var/vcap/data
		// todo dont need to create /var/vcap/store
		Cmd: dkrstrslice.StrSlice{"bash", "-c", `
      umount /etc/resolv.conf && \
      umount /etc/hosts && \
      umount /etc/hostname && \
      rm -rf /var/vcap/data/sys && \
      mkdir -p /var/vcap/data/sys && \
      mkdir -p /var/vcap/store && \
      exec env -i /usr/sbin/runsvdir-start
    `},

		Env: []string{"reschedule:on-node-failure"},
	}

	if len(diskCIDs) > 0 {
		node, err := f.possiblyFindNodeWithDisk(diskCIDs[0])
		if err != nil {
			return Container{}, bosherr.WrapError(err, "Finding node for disk")
		}

		if len(node) > 0 {
			// Hard schedule on the same node that has the disk
			// todo hopefully swarm handles this functionality in future?
			containerConfig.Env = append(containerConfig.Env, "constraint:node=="+node)
		}
	}

	vmProps.HostConfig.Privileged = true
	vmProps.HostConfig.PublishAllPorts = true

	for _, port := range vmProps.ExposedPorts {
		containerConfig.ExposedPorts[dkrnat.Port(port)] = struct{}{}
	}

	vmProps = f.cleanMounts(vmProps)

	endPtConfig := &dkrnet.EndpointSettings{
		IPAMConfig: &dkrnet.EndpointIPAMConfig{
			IPv4Address: network.IP(),
		},
	}

	netConfig := &dkrnet.NetworkingConfig{
		EndpointsConfig: map[string]*dkrnet.EndpointSettings{
			netProps.Name: endPtConfig,
		},
	}

	f.logger.Debug(f.logTag, "Creating container %#v, host %#v", containerConfig, &vmProps.HostConfig)

	container, err := f.dkrClient.ContainerCreate(
		context.TODO(), containerConfig, &vmProps.HostConfig, netConfig, id.AsString())
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Creating container")
	}

	f.logger.Debug(f.logTag, "Connecting container '%s' to network '%s' with opts %#v",
		container.ID, netProps.Name, endPtConfig)

	// todo attach additional networks
	// err = f.dkrClient.NetworkConnect(context.TODO(), netProps.Name, id.AsString(), endPtConfig)
	// if err != nil {
	//  return Container{}, bosherr.WrapError(err, "Connecting container to network")
	// }

	err = f.dkrClient.ContainerStart(context.TODO(), id.AsString(), dkrtypes.ContainerStartOptions{})
	if err != nil {
		f.cleanUpContainer(container)
		return Container{}, bosherr.WrapError(err, "Starting container")
	}

	agentEnv := apiv1.AgentEnvFactory{}.ForVM(agentID, id, networks, env, f.agentOptions)
	agentEnv.AttachSystemDisk("")

	fileService := NewFileService(f.dkrClient, id, f.logger)
	agentEnvService := NewFSAgentEnvService(fileService, f.logger)

	err = agentEnvService.Update(agentEnv)
	if err != nil {
		f.cleanUpContainer(container)
		return Container{}, bosherr.WrapError(err, "Updating container's agent env")
	}

	return NewContainer(id, f.dkrClient, agentEnvService, f.logger), nil
}

func (f Factory) Find(id apiv1.VMCID) (VM, error) {
	fileService := NewFileService(f.dkrClient, id, f.logger)
	agentEnvService := NewFSAgentEnvService(fileService, f.logger)
	return NewContainer(id, f.dkrClient, agentEnvService, f.logger), nil
}

func (f Factory) cleanUpContainer(container dkrtypes.ContainerCreateResponse) {
	// todo be more reselient at removal see Container#Delete()
	rmOpts := dkrtypes.ContainerRemoveOptions{Force: true}

	err := f.dkrClient.ContainerRemove(context.TODO(), container.ID, rmOpts)
	if err != nil {
		f.logger.Error(f.logTag, "Failed destroying container '%s': %s", container.ID, err.Error())
	}
}

func (f Factory) possiblyFindNodeWithDisk(diskID apiv1.DiskCID) (string, error) {
	resp, err := f.dkrClient.VolumeList(context.TODO(), dkrfilters.NewArgs())
	if err != nil {
		return "", bosherr.WrapError(err, "Listing volumes")
	}

	for _, vol := range resp.Volumes {
		// Check if volume is available on any node
		if strings.Contains(vol.Name, "/"+diskID.AsString()) {
			return strings.SplitN(vol.Name, "/", 2)[0], nil
		}

		// Check if volume is available locally (no node prefix)
		if vol.Name == diskID.AsString() {
			return "", nil
		}
	}

	// Did not find volume on any nodes
	return "", nil
}

func (f Factory) cleanMounts(vmProps VMProps) VMProps {
	const unixSock = "unix://"

	for i, _ := range vmProps.HostConfig.Mounts {
		// Strip off unix socker from sources for convenience of configuration
		if strings.HasPrefix(vmProps.HostConfig.Mounts[i].Source, unixSock) {
			vmProps.HostConfig.Mounts[i].Source =
				strings.TrimPrefix(vmProps.HostConfig.Mounts[i].Source, unixSock)
		}
	}

	return vmProps
}
