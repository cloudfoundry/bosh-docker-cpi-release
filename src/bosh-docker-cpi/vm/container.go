package vm

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	bdisk "bosh-docker-cpi/disk"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrtypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dkrnet "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	dkrclient "github.com/docker/docker/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

const UpdateSettingsPath = "/var/vcap/bosh/update_settings.json"
const DnsRecordsPath = "/var/vcap/instance/dns/records.json"

type Container struct {
	id apiv1.VMCID

	dkrClient       *dkrclient.Client
	agentEnvService AgentEnvService

	logger boshlog.Logger
}

type EphemeralDiskCID struct {
	id apiv1.VMCID
}

func (c EphemeralDiskCID) AsString() string { return "vol-eph-" + c.id.AsString() }

func NewContainer(
	id apiv1.VMCID,
	dkrClient *dkrclient.Client,
	agentEnvService AgentEnvService,
	logger boshlog.Logger,
) Container {
	return Container{
		id: id,

		dkrClient:       dkrClient,
		agentEnvService: agentEnvService,

		logger: logger,
	}
}

func (c Container) ID() apiv1.VMCID { return c.id }

func (c Container) Delete() error {
	exists, err := c.Exists()
	if err != nil {
		return err
	}

	if exists {
		err := c.tryKilling() // todo just make this idempotent
		if err != nil {
			return err
		}

		rmOpts := container.RemoveOptions{Force: true}

		// Create context for removal operations
		removeCtx, removeCancel := ContextWithTimeout(DefaultDockerTimeout)
		defer removeCancel()

		// todo handle 'device or resource busy' error?
		err = c.dkrClient.ContainerRemove(removeCtx, c.id.AsString(), rmOpts)
		if err != nil {
			// todo how to best handle rootfs removal e.g.:
			// ... remove root filesystem xxx-removing: device or resource busy
			// ... remove root filesystem xxx: layer not retained
			if !strings.Contains(err.Error(), "Driver aufs failed to remove root filesystem") {
				return err
			}
		}
	}

	volumeCtx, volumeCancel := ContextWithTimeout(ShortDockerTimeout)
	defer volumeCancel()

	err = c.dkrClient.VolumeRemove(volumeCtx, EphemeralDiskCID{c.id}.AsString(), true)
	if err != nil {
		if !dkrclient.IsErrNotFound(err) {
			return bosherr.WrapErrorf(err, "Deleting ephemeral volume")
		}
	}

	return nil
}

func (c Container) Exists() (bool, error) {
	inspectCtx, inspectCancel := ContextWithTimeout(ShortDockerTimeout)
	defer inspectCancel()

	_, err := c.dkrClient.ContainerInspect(inspectCtx, c.id.AsString())
	if err != nil {
		if dkrclient.IsErrNotFound(err) {
			return false, nil
		}

		return false, bosherr.WrapError(err, "Inspecting container")
	}

	return true, nil
}

func (c Container) tryKilling() error {
	var lastErr error

	// Try multiple times to avoid 'EOF' errors
	for i := 0; i < 20; i++ {
		// Create context for each kill attempt
		killCtx, killCancel := ContextWithTimeout(ShortDockerTimeout)
		lastErr = c.dkrClient.ContainerKill(killCtx, c.id.AsString(), "KILL")
		killCancel()

		if lastErr == nil {
			return nil
		}

		if strings.Contains(lastErr.Error(), "is not running") {
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}

	return bosherr.WrapError(lastErr, "Killing container")
}

func (c Container) AttachDisk(disk bdisk.Disk) (apiv1.DiskHint, error) {
	exists, err := c.Exists()
	if err != nil {
		return apiv1.DiskHint{}, err
	}

	if !exists {
		return apiv1.DiskHint{}, bosherr.Error("VM does not exist")
	}

	agentEnv, err := c.agentEnvService.Fetch()
	if err != nil {
		return apiv1.DiskHint{}, bosherr.WrapError(err, "Fetching agent env")
	}

	fileService := NewFileService(c.dkrClient, c.id, c.logger)

	updateSettings, err := fileService.Download(UpdateSettingsPath)
	if err != nil {
		c.logger.Warn("attach-disk", "Unable to find update_settings.json skipping: %s", err)
	}
	dnsRecords, err := fileService.Download(DnsRecordsPath)
	if err != nil {
		c.logger.Warn("attach-disk", "Unable to find records.json skipping: %s", err)
	}

	path := filepath.Join("/warden-cpi-dev", disk.ID().AsString())
	diskHint := apiv1.NewDiskHintFromString(path)
	agentEnv.AttachPersistentDisk(disk.ID(), diskHint)

	err = c.restartByRecreating(disk.ID(), path)
	if err != nil {
		return apiv1.DiskHint{}, bosherr.WrapError(err, "Restarting by recreating")
	}

	err = c.agentEnvService.Update(agentEnv)
	if err != nil {
		return apiv1.DiskHint{}, bosherr.WrapError(err, "Updating agent env")
	}

	if len(updateSettings) > 0 {
		err = fileService.Upload(UpdateSettingsPath, updateSettings)
		if err != nil {
			return apiv1.DiskHint{}, bosherr.WrapError(err, "Restoring update_settings.json")
		}
	}
	if len(dnsRecords) > 0 {
		// Ensure the DNS directory exists in the new container
		dnsDir := filepath.Dir(DnsRecordsPath)
		err = c.runInContainer(fmt.Sprintf("mkdir -p %s && chown vcap:vcap %s", dnsDir, dnsDir))
		if err != nil {
			c.logger.Warn("attach-disk", "Failed to create DNS directory: %s", err)
			// Continue anyway, as DNS records are optional
		} else {
			err = fileService.Upload(DnsRecordsPath, dnsRecords)
			if err != nil {
				// Log warning but don't fail, as DNS records are optional for container operation
				c.logger.Warn("attach-disk", "Failed to restore records.json: %s", err)
			} else {
				err = c.runInContainer("chgrp vcap " + DnsRecordsPath)
				if err != nil {
					c.logger.Warn("attach-disk", "Failed to chgrp records.json: %s", err)
				}
			}
		}
	}

	return diskHint, nil
}

func (c Container) DetachDisk(disk bdisk.Disk) error {
	exists, err := c.Exists()
	if err != nil {
		return err
	}

	if !exists {
		return bosherr.Error("VM does not exist")
	}

	agentEnv, err := c.agentEnvService.Fetch()
	if err != nil {
		return bosherr.WrapError(err, "Fetching agent env")
	}

	fileService := NewFileService(c.dkrClient, c.id, c.logger)

	updateSettings, err := fileService.Download(UpdateSettingsPath)
	if err != nil {
		c.logger.Warn("detach-disk", "Unable to find update_settings.json skipping: %s", err)
	}
	dnsRecords, err := fileService.Download(DnsRecordsPath)
	if err != nil {
		c.logger.Warn("detach-disk", "Unable to find records.json skipping: %s", err)
	}

	agentEnv.DetachPersistentDisk(disk.ID())

	err = c.restartByRecreating(disk.ID(), "")
	if err != nil {
		return bosherr.WrapError(err, "Restarting by recreating")
	}

	// todo update before starting?
	err = c.agentEnvService.Update(agentEnv)
	if err != nil {
		return bosherr.WrapError(err, "Updating agent env")
	}

	if len(updateSettings) > 0 {
		err = fileService.Upload(UpdateSettingsPath, updateSettings)
		if err != nil {
			return bosherr.WrapError(err, "Restoring update_settings.json")
		}
	}
	if len(dnsRecords) > 0 {
		// Ensure the DNS directory exists in the new container
		dnsDir := filepath.Dir(DnsRecordsPath)
		err = c.runInContainer(fmt.Sprintf("mkdir -p %s && chown vcap:vcap %s", dnsDir, dnsDir))
		if err != nil {
			c.logger.Warn("detach-disk", "Failed to create DNS directory: %s", err)
			// Continue anyway, as DNS records are optional
		} else {
			err = fileService.Upload(DnsRecordsPath, dnsRecords)
			if err != nil {
				// Log warning but don't fail, as DNS records are optional for container operation
				c.logger.Warn("detach-disk", "Failed to restore records.json: %s", err)
			} else {
				err = c.runInContainer("chgrp vcap " + DnsRecordsPath)
				if err != nil {
					c.logger.Warn("detach-disk", "Failed to chgrp records.json: %s", err)
				}
			}
		}
	}

	return nil
}

func (c Container) restartByRecreating(diskID apiv1.DiskCID, diskPath string) error {
	inspectCtx, inspectCancel := ContextWithTimeout(ShortDockerTimeout)
	defer inspectCancel()

	conf, err := c.dkrClient.ContainerInspect(inspectCtx, c.id.AsString())
	if err != nil {
		return bosherr.WrapError(err, "Inspecting container")
	}

	err = c.Delete()
	if err != nil {
		return bosherr.WrapError(err, "Disposing of container before disk attachment")
	}

	node, err := c.findNodeWithDisk(diskID)
	if err != nil {
		return bosherr.WrapError(err, "Finding node for disk")
	}

	if len(node) > 0 {
		// Hard schedule on the same node that has the disk
		// todo hopefully swarm handles this functionality in future?
		conf.Config.Env = []string{"constraint:node==" + node}
	}

	conf.HostConfig.Binds = c.updateBinds(conf.HostConfig.Binds, diskID, diskPath)

	netConfig := c.copyNetworks(conf)
	netConfig, additionalEndPtConfigs := splitNetworkSettings(netConfig)
	var platform specs.Platform
	platform.OS = "linux"
	platform.Architecture = "amd64"

	createCtx, createCancel := ContextWithTimeout(DefaultDockerTimeout)
	defer createCancel()

	_, err = c.dkrClient.ContainerCreate(
		createCtx, conf.Config, conf.HostConfig, netConfig, &platform, c.id.AsString())
	if err != nil {
		return bosherr.WrapError(err, "Creating container")
	}

	for name, endPtConfig := range additionalEndPtConfigs {
		networkCtx, networkCancel := ContextWithTimeout(ShortDockerTimeout)
		err := c.dkrClient.NetworkConnect(networkCtx, name, c.id.AsString(), endPtConfig)
		networkCancel()
		if err != nil {
			return bosherr.WrapErrorf(err, "Connecting container to network '%s'", name)
		}
	}

	startCtx, startCancel := ContextWithTimeout(DefaultDockerTimeout)
	defer startCancel()

	err = c.dkrClient.ContainerStart(startCtx, c.id.AsString(), container.StartOptions{})
	if err != nil {
		c.Delete() //nolint:errcheck
		return bosherr.WrapError(err, "Starting container")
	}

	return nil
}

func (c Container) runInContainer(cmd string) error {
	execCtx, execCancel := ContextWithTimeout(ShortDockerTimeout)
	defer execCancel()

	execProcess, err := c.dkrClient.ContainerExecCreate(execCtx, c.id.AsString(), container.ExecOptions{Cmd: []string{"bash", "-c", cmd}})
	if err != nil {
		return err
	}

	startCtx, startCancel := ContextWithTimeout(ShortDockerTimeout)
	defer startCancel()

	return c.dkrClient.ContainerExecStart(startCtx, execProcess.ID, container.ExecStartOptions{})
}

func (Container) updateBinds(binds []string, diskID apiv1.DiskCID, diskPath string) []string {
	if len(diskPath) > 0 {
		return append(binds, diskID.AsString()+":"+diskPath)
	}

	for i, bind := range binds {
		if strings.HasPrefix(bind, diskID.AsString()+":") {
			binds = append(binds[:i], binds[i+1:]...)
			break
		}
	}

	return binds
}

func (Container) copyNetworks(conf dkrtypes.ContainerJSON) *dkrnet.NetworkingConfig { //nolint:staticcheck
	netConfig := &dkrnet.NetworkingConfig{
		EndpointsConfig: map[string]*dkrnet.EndpointSettings{},
	}

	for netName, endPtConfig := range conf.NetworkSettings.Networks {
		netConfig.EndpointsConfig[netName] = endPtConfig
	}

	return netConfig
}

func (c Container) findNodeWithDisk(diskID apiv1.DiskCID) (string, error) {
	listCtx, listCancel := ContextWithTimeout(ShortDockerTimeout)
	defer listCancel()

	resp, err := c.dkrClient.VolumeList(listCtx, volume.ListOptions{})
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

	return "", bosherr.Errorf("Did not find node with disk '%s'", diskID)
}
