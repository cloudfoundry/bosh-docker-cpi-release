package vm

import (
	"context"
	"fmt"
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

	// Umount Docker's bind-mounted /etc/resolv.conf, /etc/hosts, and /etc/hostname
	// so the BOSH agent and BOSH DNS can manage these files freely.
	// After unmounting /etc/resolv.conf, write a new one with DNS servers from
	// the network spec so that processes have working DNS resolution before the
	// BOSH agent takes over.
	preStartCommands := []string{
		`umount /etc/resolv.conf`,
		populateResolveConf(networks),
		`umount /etc/hosts`,
		`umount /etc/hostname`,
		networkInitBashCmd,
		`rm -rf /var/vcap/data/sys`,
		`mkdir -p /var/vcap/data/sys`,
		`mkdir -p /var/vcap/store`,
		"sed -i 's/chronyc/# chronyc/g' /var/vcap/bosh/bin/sync-time",
	}

	var startContainerCommands []string

	if startContainersWithSystemD {
		// only load minimal set of systemd units / services
		// https://github.com/asg1612/docker-systemd/blob/master/Dockerfile
		deleteUnwantedUnitsCommand := strings.Join([]string{
			`find`,
			`/etc/systemd/system`,
			`/lib/systemd/system`,
			`-path '*.wants/*' `,
			`-not -name '*bosh-agent*'`,
			`-not -name '*journald*'`,
			`-not -name '*logrotate*' `,
			`-not -name '*runit*'`,
			`-not -name '*ssh*'`,
			`-not -name '*systemd-user-sessions*'`,
			`-not -name '*systemd-tmpfiles*'`,
			`-exec rm \{} \;`,
		}, " ")

		// cgroups v2 nesting: systemd as PID 1 needs subtree_control delegation
		// so it can create child cgroups for services. With cgroupns=private,
		// the container sees /sys/fs/cgroup/ as its cgroup root. We move
		// existing processes into an "init" child and enable all available
		// controllers on subtree_control (same pattern as moby/moby hack/dind).
		// #region agent log — debug[58375b]: cgroup nesting with echo diagnostics
		cgroupNestingCmd := strings.Join([]string{
			`echo "DEBUG[58375b] cgroup-nesting: start" >&2;`,
			`if [ -f /sys/fs/cgroup/cgroup.controllers ]; then`,
			`echo "DEBUG[58375b] cgroup-nesting: cgroupv2, before procs=$(cat /sys/fs/cgroup/cgroup.procs 2>/dev/null | tr '\n' ',') subtree=$(cat /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null)" >&2;`,
			`mkdir -p /sys/fs/cgroup/init;`,
			`_NEST_I=0;`,
			`while ! {`,
			`xargs -rn1 < /sys/fs/cgroup/cgroup.procs > /sys/fs/cgroup/init/cgroup.procs 2>/dev/null || :;`,
			`sed -e 's/ / +/g' -e 's/^/+/' < /sys/fs/cgroup/cgroup.controllers > /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null;`,
			`}; do _NEST_I=$((_NEST_I+1)); if [ $_NEST_I -ge 50 ]; then echo "DEBUG[58375b] cgroup-nesting: giving up after 50 iterations" >&2; break; fi; done;`,
			`echo "DEBUG[58375b] cgroup-nesting: done iter=$_NEST_I subtree=$(cat /sys/fs/cgroup/cgroup.subtree_control 2>/dev/null)" >&2;`,
			`else echo "DEBUG[58375b] cgroup-nesting: not cgroupv2" >&2;`,
			`fi`,
		}, " ")
		// #endregion agent log

		preStartCommands = append(preStartCommands, []string{
			`rm -rf /etc/sv/{ssh,cron}`,
			`rm -rf /etc/service/{ssh,cron}`,
			deleteUnwantedUnitsCommand,
			cgroupNestingCmd,
		}...)

		startContainerCommands = append(preStartCommands,
			`echo "DEBUG[58375b] about to exec /sbin/init" >&2`,
			`exec /sbin/init`)
	} else {
		preStartCommands = append(preStartCommands, []string{}...)

		startContainerCommands = append(preStartCommands, `exec env -i /usr/sbin/runsvdir-start`)
	}

	containerConfig.Cmd = dkrstrslice.StrSlice{"bash", "-c", strings.Join(startContainerCommands, " && ")}

	// #region agent log — debug[58375b]: log startup config for hypothesis A/B
	f.logger.Debug(f.logTag, "DEBUG[58375b] startContainersWithSystemD=%v cgroupnsMode=%s containerCmd=%s",
		startContainersWithSystemD, vmProps.HostConfig.CgroupnsMode, strings.Join(startContainerCommands, " && "))
	// #endregion agent log

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

	if startContainersWithSystemD {
		// Use a private cgroup namespace so systemd sees itself at the cgroup
		// root (/). With cgroupns=host, systemd would see a deep path like
		// /docker/<id>/init and fail to manage cgroups properly.
		vmProps.HostConfig.CgroupnsMode = dkrcont.CgroupnsModePrivate //nolint:staticcheck
	}

	for _, port := range vmProps.ExposedPorts {
		containerConfig.ExposedPorts[dkrnat.Port(port)] = struct{}{}
	}

	vmProps = f.cleanMounts(vmProps)

	binds := []string{
		fmt.Sprintf("%s:/var/vcap/data/", EphemeralDiskCID{id}.AsString()),
		"/lib/modules:/usr/lib/modules", // make host kernel modules accessible
	}

	// With cgroupns=private, the kernel provides a cgroup2 mount at
	// /sys/fs/cgroup scoped to the container's namespace automatically.
	// No explicit bind mount is needed (and would defeat the namespace).

	if lxcfsEnabled {
		binds = append(binds, "/var/lib/lxcfs/proc/meminfo:/proc/meminfo:rw")
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

	// #region agent log — debug[58375b]: verify container is running after start (hypothesis A/C/E)
	inspect, inspErr := f.dkrClient.ContainerInspect(context.TODO(), id.AsString())
	if inspErr == nil {
		f.logger.Debug(f.logTag, "DEBUG[58375b] container %s post-start: status=%s running=%v exitCode=%d pid=%d",
			id.AsString(), inspect.State.Status, inspect.State.Running, inspect.State.ExitCode, inspect.State.Pid)
	} else {
		f.logger.Debug(f.logTag, "DEBUG[58375b] container %s post-start inspect error: %v", id.AsString(), inspErr)
	}
	// #endregion agent log

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
	// todo be more resilient at removal see Container#Delete()
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

func populateResolveConf(networks apiv1.Networks) string {
	var nameserverEntries []string
	for _, network := range networks {
		for _, dnsServer := range network.DNS() {
			nameserverEntries = append(nameserverEntries, fmt.Sprintf(`"nameserver %s"`, dnsServer))
		}
	}

	if len(nameserverEntries) == 0 {
		return ":" // no-op bash command
	}

	return fmt.Sprintf(`printf '%%s\n' %s > /etc/resolv.conf`, strings.Join(nameserverEntries, " "))
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
