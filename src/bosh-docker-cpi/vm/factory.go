package vm

import (
	"context"
	"strings"

	"bosh-docker-cpi/config"
	bstem "bosh-docker-cpi/stemcell"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	dkrcont "github.com/docker/docker/api/types/container"
	dkrstrslice "github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/api/types/volume"
	dkrclient "github.com/docker/docker/client"
	dkrnat "github.com/docker/go-connections/nat"
)

type Factory struct {
	dkrClient *dkrclient.Client
	uuidGen   boshuuid.Generator

	agentOptions apiv1.AgentOptions

	logTag string
	logger boshlog.Logger
	Config config.Config
}

func NewFactory(
	dkrClient *dkrclient.Client,
	uuidGen boshuuid.Generator,
	agentOptions apiv1.AgentOptions,
	logger boshlog.Logger,
	cfg config.Config,
) Factory {
	return Factory{
		dkrClient: dkrClient,
		uuidGen:   uuidGen,

		agentOptions: agentOptions,

		logTag: "vm.Factory",
		logger: logger,
		Config: cfg,
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

	startContainersWithSystemD := f.Config.StartContainersWithSystemD
	if vmProps.ForceStartWithSystemD {
		startContainersWithSystemD = true
	}
	if vmProps.ForceStartWithoutSystemD {
		startContainersWithSystemD = false
	}

	lxcfsEnabled := f.Config.EnableLXCFSSupport
	if vmProps.ForceLXCFSEnabled {
		lxcfsEnabled = true
	}
	if vmProps.ForceLXCFSDisabled {
		lxcfsEnabled = false
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
		Env:          []string{"reschedule:on-node-failure"},
	}

	// todo hacky umount to avoid conflicting with bosh dns updates
	// todo why perms are wrong on /var/vcap/data
	// todo dont need to create /var/vcap/store

	commonInitCmds := []string{
		`umount /etc/resolv.conf`,
		`umount /etc/hosts`,
		`umount /etc/hostname`,
		networkInitBashCmd,
		`rm -rf /var/vcap/data/sys`,
		`mkdir -p /var/vcap/data/sys`,
		`mkdir -p /var/vcap/store`,
		"sed -i 's/chronyc/# chronyc/g' /var/vcap/bosh/bin/sync-time",
	}

	if startContainersWithSystemD {
		// only load minimal set of systemd units / services
		// https://github.com/asg1612/docker-systemd/blob/master/Dockerfile
		removeNonCriticalSystemdServices := `find /etc/systemd/system ` +
			`/lib/systemd/system -path '*.wants/*'` +
			`-not -name '*journald*' -not -name '*logrotate*'` +
			`-not -name '*systemd-tmpfiles*' -not -name '*systemd-user-sessions*' ` +
			`-not -name '*runit*' -not -name '*bosh-agent*' -not -name '*ssh*' -exec rm \{} \;`

		systemdInitCmds := []string{
			`rm -rf /etc/sv/{ssh,cron}`,
			`rm -rf /etc/service/{ssh,cron}`,
			removeNonCriticalSystemdServices,
			`exec /sbin/init`,
		}

		containerConfig.Cmd = dkrstrslice.StrSlice{"bash", "-c", strings.Join(append(commonInitCmds, systemdInitCmds...), " && ")}
	} else {
		containerConfig.Cmd = dkrstrslice.StrSlice{"bash", "-c", strings.Join(append(commonInitCmds, `exec env -i /usr/sbin/runsvdir-start`), " && ")}
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

	vmProps.HostConfig.Privileged = true      //nolint:staticcheck
	vmProps.HostConfig.PublishAllPorts = true //nolint:staticcheck

	for _, port := range vmProps.ExposedPorts {
		containerConfig.ExposedPorts[dkrnat.Port(port)] = struct{}{}
	}

	vmProps = f.cleanMounts(vmProps)

	binds := []string{
		EphemeralDiskCID{id}.AsString() + ":/var/vcap/data/",
		// Ensure kernel modules can be matched/loaded if host kernel != stemcell kernel
		"/lib/modules:/usr/lib/modules",
	} //nolint:staticcheck

	if lxcfsEnabled {
		binds = append(binds, []string{
			"/var/lib/lxcfs/proc/meminfo:/proc/meminfo:rw",
		}...) //nolint:staticcheck
	}

	vmProps.HostConfig.Binds = binds //nolint:staticcheck

	f.logger.Debug(f.logTag, "Creating container %#v, host %#v", containerConfig, &vmProps.HostConfig)

	netConfig, additionalEndPtConfigs := splitNetworkSettings(netConfig)

	vmProps.Platform.OS = "linux"           //nolint:staticcheck
	vmProps.Platform.Architecture = "amd64" //nolint:staticcheck

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

	err = f.dkrClient.ContainerStart(context.TODO(), id.AsString(), dkrcont.StartOptions{})
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

func (f Factory) cleanUpContainer(container dkrcont.CreateResponse) {
	// todo be more reselient at removal see Container#Delete()
	rmOpts := dkrcont.RemoveOptions{Force: true}

	err := f.dkrClient.ContainerRemove(context.TODO(), container.ID, rmOpts)
	if err != nil {
		f.logger.Error(f.logTag, "Failed destroying container '%s': %s", container.ID, err.Error())
	}
}

func (f Factory) possiblyFindNodeWithDisk(diskID apiv1.DiskCID) (string, error) {
	resp, err := f.dkrClient.VolumeList(context.TODO(), volume.ListOptions{})
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

	for i := range vmProps.HostConfig.Mounts { //nolint:staticcheck
		// Strip off unix socket from sources for convenience of configuration
		if strings.HasPrefix(vmProps.HostConfig.Mounts[i].Source, unixSock) { //nolint:staticcheck
			vmProps.HostConfig.Mounts[i].Source =
				strings.TrimPrefix(vmProps.HostConfig.Mounts[i].Source, unixSock)
		}
	}

	return vmProps
}
