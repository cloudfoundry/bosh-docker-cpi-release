package vm

import (
	"context"
	"strings"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	dkrtypes "github.com/docker/docker/api/types"
	dkrcont "github.com/docker/docker/api/types/container"
	dkrfilters "github.com/docker/docker/api/types/filters"
	dkrstrslice "github.com/docker/docker/api/types/strslice"
	dkrclient "github.com/docker/docker/client"
	dkrnat "github.com/docker/go-connections/nat"

	bstem "bosh-docker-cpi/stemcell"
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

	var vmProps Props

	err := cloudProps.As(&vmProps)
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Unmarshaling VM properties")
	}

	networkInitBashCmd, netConfig, err := NewNetworks(f.dkrClient, f.uuidGen, networks).Enable()
	if err != nil {
		return nil, bosherr.WrapError(err, "Enabling networks")
	}

	idStr, err := f.uuidGen.Generate()
	if err != nil {
		return nil, bosherr.WrapError(err, "Generating container ID")
	}

	idStr = "c-" + idStr

	id := apiv1.NewVMCID(idStr)

	containerConfig := &dkrcont.Config{
		Image:        stemcell.ID().AsString(),
		ExposedPorts: map[dkrnat.Port]struct{}{}, // todo what ports?

		// todo hacky umount to avoid conflicting with bosh dns updates
		// todo why perms are wrong on /var/vcap/data
		// todo dont need to create /var/vcap/store
		Cmd: dkrstrslice.StrSlice{"bash", "-c", strings.Join([]string{
			`umount /etc/resolv.conf`,
			`umount /etc/hosts`,
			`umount /etc/hostname`,
			networkInitBashCmd,
			`rm -rf /var/vcap/data/sys`,
			`mkdir -p /var/vcap/data/sys`,
			`mkdir -p /var/vcap/store`,
			"sed -i 's/chronyc/# chronyc/g' /var/vcap/bosh/bin/sync-time",
			`exec env -i /usr/sbin/runsvdir-start`,
		}, " && ")},

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

	vmProps.HostConfig.Binds = []string{EphemeralDiskCID{id}.AsString() + ":/var/vcap/data/"}

	f.logger.Debug(f.logTag, "Creating container %#v, host %#v", containerConfig, &vmProps.HostConfig)

	netConfig, additionalEndPtConfigs := splitNetworkSettings(netConfig)

	vmProps.Platform.OS = "linux"
	vmProps.Platform.Architecture = "amd64"

	container, err := f.dkrClient.ContainerCreate(
		context.TODO(), containerConfig, &vmProps.HostConfig, netConfig, &vmProps.Platform, id.AsString())
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Creating container")
	}

	f.logger.Debug(f.logTag,
		"Connecting container '%s' to networks with opts %#v", container.ID, netConfig)

	for name, endPtConfig := range additionalEndPtConfigs {
		err := f.dkrClient.NetworkConnect(context.TODO(), name, id.AsString(), endPtConfig)
		if err != nil {
			return Container{}, bosherr.WrapErrorf(err, "Connecting container to network '%s'", name)
		}
	}

	err = f.dkrClient.ContainerStart(context.TODO(), id.AsString(), dkrtypes.ContainerStartOptions{})
	if err != nil {
		f.cleanUpContainer(container)
		return Container{}, bosherr.WrapError(err, "Starting container")
	}

	agentEnv := apiv1.AgentEnvFactory{}.ForVM(agentID, id, networks, env, f.agentOptions)
	agentEnv.AttachSystemDisk(apiv1.NewDiskHintFromString(""))

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

func (f Factory) cleanUpContainer(container dkrcont.ContainerCreateCreatedBody) {
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

func (f Factory) cleanMounts(vmProps Props) Props {
	const unixSock = "unix://"

	for i := range vmProps.HostConfig.Mounts {
		// Strip off unix socker from sources for convenience of configuration
		if strings.HasPrefix(vmProps.HostConfig.Mounts[i].Source, unixSock) { //nolint:gosimple
			vmProps.HostConfig.Mounts[i].Source =
				strings.TrimPrefix(vmProps.HostConfig.Mounts[i].Source, unixSock)
		}
	}

	return vmProps
}
