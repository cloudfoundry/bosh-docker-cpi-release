package vm

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrtypes "github.com/docker/docker/api/types"
	dkrfilters "github.com/docker/docker/api/types/filters"
	dkrnet "github.com/docker/docker/api/types/network"
	dkrclient "github.com/docker/docker/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"

	bdisk "bosh-docker-cpi/disk"
)

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

		rmOpts := dkrtypes.ContainerRemoveOptions{Force: true}

		// todo handle 'device or resource busy' error?
		err = c.dkrClient.ContainerRemove(context.TODO(), c.id.AsString(), rmOpts)
		if err != nil {
			// todo how to best handle rootfs removal e.g.:
			// ... remove root filesystem xxx-removing: device or resource busy
			// ... remove root filesystem xxx: layer not retained
			if !strings.Contains(err.Error(), "Driver aufs failed to remove root filesystem") {
				return err
			}
		}
	}

	err = c.dkrClient.VolumeRemove(context.TODO(), EphemeralDiskCID{c.id}.AsString(), true)
	if err != nil {
		if !dkrclient.IsErrNotFound(err) {
			return bosherr.WrapErrorf(err, "Deleting ephemeral volume")
		}
	}

	return nil
}

func (c Container) Exists() (bool, error) {
	_, err := c.dkrClient.ContainerInspect(context.TODO(), c.id.AsString())
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
		lastErr = c.dkrClient.ContainerKill(context.TODO(), c.id.AsString(), "KILL")
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
	updateSettings, err := fileService.Download("/var/vcap/bosh/update_settings.json")
	if err != nil {
		return apiv1.DiskHint{}, bosherr.WrapError(err, "Fetching update_settings.json")
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
		err = fileService.Upload("/var/vcap/bosh/update_settings.json", updateSettings)
		if err != nil {
			return apiv1.DiskHint{}, bosherr.WrapError(err, "Restoring update_settings.json")
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
	updateSettings, err := fileService.Download("/var/vcap/bosh/update_settings.json")
	if err != nil {
		return bosherr.WrapError(err, "Fetching update_settings.json")
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
		err = fileService.Upload("/var/vcap/bosh/update_settings.json", updateSettings)
		if err != nil {
			return bosherr.WrapError(err, "Restoring update_settings.json")
		}
	}

	return nil
}

func (c Container) restartByRecreating(diskID apiv1.DiskCID, diskPath string) error {
	conf, err := c.dkrClient.ContainerInspect(context.TODO(), c.id.AsString())
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

	_, err = c.dkrClient.ContainerCreate(
		context.TODO(), conf.Config, conf.HostConfig, netConfig, &platform, c.id.AsString())
	if err != nil {
		return bosherr.WrapError(err, "Creating container")
	}

	for name, endPtConfig := range additionalEndPtConfigs {
		err := c.dkrClient.NetworkConnect(context.TODO(), name, c.id.AsString(), endPtConfig)
		if err != nil {
			return bosherr.WrapErrorf(err, "Connecting container to network '%s'", name)
		}
	}

	err = c.dkrClient.ContainerStart(
		context.TODO(), c.id.AsString(), dkrtypes.ContainerStartOptions{})
	if err != nil {
		c.Delete()
		return bosherr.WrapError(err, "Starting container")
	}

	return nil
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

func (Container) copyNetworks(conf dkrtypes.ContainerJSON) *dkrnet.NetworkingConfig {
	netConfig := &dkrnet.NetworkingConfig{
		EndpointsConfig: map[string]*dkrnet.EndpointSettings{},
	}

	for netName, endPtConfig := range conf.NetworkSettings.Networks {
		netConfig.EndpointsConfig[netName] = endPtConfig
	}

	return netConfig
}

// todo connectNetworks is not used
func (c Container) connectNetworks(conf dkrtypes.ContainerJSON) error {
	for _, endPtConfig := range conf.NetworkSettings.Networks {
		_, err := c.dkrClient.NetworkInspect(context.TODO(), endPtConfig.NetworkID, dkrtypes.NetworkInspectOptions{})
		if err != nil {
			if dkrclient.IsErrNotFound(err) {
				continue
			}

			// Bridge networks cannot be inspected
			// todo should be fixed in swarm api?
			return bosherr.Errorf("Did not find network '%s'", endPtConfig.NetworkID)
		}

		err = c.dkrClient.NetworkConnect(
			context.TODO(), endPtConfig.NetworkID, c.id.AsString(), endPtConfig)
		if err != nil {
			return bosherr.WrapError(err, "Connecting container to network")
		}
	}

	return nil
}

func (c Container) findNodeWithDisk(diskID apiv1.DiskCID) (string, error) {
	resp, err := c.dkrClient.VolumeList(context.TODO(), dkrfilters.NewArgs())
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
