package vm

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"bosh-docker-cpi/config"
	bstem "bosh-docker-cpi/stemcell"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	dkrcont "github.com/docker/docker/api/types/container"
	dkrnet "github.com/docker/docker/api/types/network"
	dkrstrslice "github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/api/types/volume"
	dkrclient "github.com/docker/docker/client"
	dkrnat "github.com/docker/go-connections/nat"
)

// Factory creates and finds VMs as Docker containers
type Factory struct {
	dkrClient *dkrclient.Client
	uuidGen   boshuuid.Generator

	agentOptions apiv1.AgentOptions

	logTag string
	logger boshlog.Logger
	Config config.Config
}

// NewFactory creates a new VM factory with the given dependencies
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

// Create creates a new VM as a Docker container
func (f Factory) Create(agentID apiv1.AgentID, stemcell bstem.Stemcell,
	cloudProps apiv1.VMCloudProps, networks apiv1.Networks,
	diskCIDs []apiv1.DiskCID, env apiv1.VMEnv) (VM, error) {

	var vmProps Props

	err := cloudProps.As(&vmProps)
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Unmarshaling VM properties")
	}

	// Create context for validation operations
	validateCtx, validateCancel := ContextWithTimeout(ShortDockerTimeout)
	defer validateCancel()

	// Validate resource limits before creating container
	validator := NewResourceValidator(f.dkrClient)
	err = validator.ValidateVMProps(validateCtx, &vmProps)
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Validating VM resource limits")
	}

	// Detect stemcell information
	stemcellInfo, err := DetectStemcellInfo(validateCtx, f.dkrClient, stemcell.ID().AsString(), f.logger)
	if err != nil {
		f.logger.Warn(f.logTag, "Failed to detect stemcell info, assuming runit: %s", err.Error())
		// Default to runit if detection fails
		stemcellInfo = &StemcellInfo{UseSystemd: false}
	}

	// Override with config if explicitly set
	useSystemd := stemcellInfo.UseSystemd
	if f.Config.StartContainersWithSystemD {
		useSystemd = true
	}

	// Validate systemd mode if enabled
	err = validator.ValidateSystemdMode(validateCtx, useSystemd)
	if err != nil {
		f.logger.Warn(f.logTag, "SystemD mode validation warning: %s", err.Error())
		// Don't fail, just warn - let Docker handle the actual compatibility
	}

	networksHandler := NewNetworks(f.dkrClient, f.uuidGen, networks)
	networkInitBashCmd, netConfig, err := networksHandler.Enable()
	if err != nil {
		return nil, bosherr.WrapError(err, "Enabling networks")
	}

	// Check if we're on Docker Desktop for enhanced networking
	isDockerDesktop := networksHandler.IsDockerDesktop()

	idStr, err := f.uuidGen.Generate()
	if err != nil {
		return nil, bosherr.WrapError(err, "Generating container ID")
	}

	idStr = "c-" + idStr

	id := apiv1.NewVMCID(idStr)

	containerConfig := &dkrcont.Config{
		Image:        stemcell.ID().AsString(),
		ExposedPorts: map[dkrnat.Port]struct{}{}, // todo what ports?
		Env: []string{
			"reschedule:on-node-failure",
			"BOSH_DOCKER_CPI=true",
			"BPM_ENABLE_PRIVILEGED=true",
		},
	}

	if useSystemd {
		containerConfig.Entrypoint = dkrstrslice.StrSlice{"/sbin/init"}
		f.logger.Debug(f.logTag, "Using systemd init for container (stemcell: %s %s)", stemcellInfo.OSCodename, stemcellInfo.OSVersion)
	} else {
		// todo hacky umount to avoid conflicting with bosh dns updates
		// todo why perms are wrong on /var/vcap/data
		// todo dont need to create /var/vcap/store
		initCommand := GetInitCommand(stemcellInfo)
		containerConfig.Cmd = dkrstrslice.StrSlice{"bash", "-c", strings.Join([]string{
			`umount /etc/resolv.conf`,
			`umount /etc/hosts`,
			`umount /etc/hostname`,
			networkInitBashCmd,
			`rm -rf /var/vcap/data/sys`,
			`mkdir -p /var/vcap/data/sys`,
			`mkdir -p /var/vcap/store`,
			"[ -f /var/vcap/bosh/bin/sync-time ] && sed -i 's/chronyc/# chronyc/g' /var/vcap/bosh/bin/sync-time || true",
			fmt.Sprintf(`exec env -i %s`, initCommand),
		}, " && ")}
		f.logger.Debug(f.logTag, "Using runit init (%s) for container", initCommand)
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

	vmProps.HostConfig.Privileged = true //nolint:staticcheck

	// Disable seccomp for BPM compatibility inside containers
	// BPM (BOSH Process Manager) uses runc with seccomp filters that conflict
	// with Docker's own seccomp filtering when running nested containers
	if vmProps.SecurityOpt == nil {
		vmProps.SecurityOpt = []string{}
	}
	vmProps.SecurityOpt = append(vmProps.SecurityOpt, "seccomp=unconfined")

	// Configure networking based on Docker environment
	if isDockerDesktop {
		// For Docker Desktop, use bridge networking with specific port forwarding
		vmProps.HostConfig.PublishAllPorts = true //nolint:staticcheck
		// Add explicit port binding for BOSH agent to ensure connectivity
		if vmProps.HostConfig.PortBindings == nil {
			vmProps.HostConfig.PortBindings = make(map[dkrnat.Port][]dkrnat.PortBinding)
		}
		// Map container port 6868 to host port 6868 for direct access
		agentPort := dkrnat.Port("6868/tcp")
		vmProps.HostConfig.PortBindings[agentPort] = []dkrnat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: "6868"},
		}
		f.logger.Debug(f.logTag, "Using bridge networking with Docker Desktop port forwarding")
	} else {
		// Use bridge networking with port publishing for native Docker
		vmProps.HostConfig.PublishAllPorts = true //nolint:staticcheck
		f.logger.Debug(f.logTag, "Using bridge networking with port publishing")
	}

	// Log Docker Desktop configuration
	if isDockerDesktop {
		for _, network := range networks {
			f.logger.Debug(f.logTag, "Docker Desktop detected - container %s will have port 6868 forwarded to host", network.IP())
		}
	}

	// Log applied resource limits for debugging and auditing
	if vmProps.HostConfig.Memory > 0 {
		f.logger.Debug(f.logTag, "Applying memory limit: %d bytes (%.2f GB)",
			vmProps.HostConfig.Memory, float64(vmProps.HostConfig.Memory)/(1024*1024*1024))
	}
	if vmProps.HostConfig.NanoCPUs > 0 {
		f.logger.Debug(f.logTag, "Applying CPU limit: %d nanocpus (%.2f CPUs)",
			vmProps.HostConfig.NanoCPUs, float64(vmProps.HostConfig.NanoCPUs)/1e9)
	}
	if vmProps.HostConfig.CPUShares > 0 {
		f.logger.Debug(f.logTag, "Applying CPU shares: %d", vmProps.HostConfig.CPUShares)
	}
	if vmProps.HostConfig.CPUQuota > 0 && vmProps.HostConfig.CPUPeriod > 0 {
		f.logger.Debug(f.logTag, "Applying CPU quota: %d/%d (%.2f%% of CPU)",
			vmProps.HostConfig.CPUQuota, vmProps.HostConfig.CPUPeriod,
			float64(vmProps.HostConfig.CPUQuota)/float64(vmProps.HostConfig.CPUPeriod)*100)
	}
	if vmProps.HostConfig.PidsLimit != nil && *vmProps.HostConfig.PidsLimit > 0 {
		f.logger.Debug(f.logTag, "Applying PIDs limit: %d", *vmProps.HostConfig.PidsLimit)
	}
	if vmProps.HostConfig.MemoryReservation > 0 {
		f.logger.Debug(f.logTag, "Applying memory reservation: %d bytes (%.2f GB)",
			vmProps.HostConfig.MemoryReservation, float64(vmProps.HostConfig.MemoryReservation)/(1024*1024*1024))
	}
	if vmProps.HostConfig.MemorySwap > 0 {
		f.logger.Debug(f.logTag, "Applying memory+swap limit: %d bytes (%.2f GB)",
			vmProps.HostConfig.MemorySwap, float64(vmProps.HostConfig.MemorySwap)/(1024*1024*1024))
	}
	if vmProps.HostConfig.BlkioWeight > 0 {
		f.logger.Debug(f.logTag, "Applying block I/O weight: %d", vmProps.HostConfig.BlkioWeight)
	}
	if len(vmProps.HostConfig.BlkioDeviceReadBps) > 0 || len(vmProps.HostConfig.BlkioDeviceWriteBps) > 0 {
		f.logger.Debug(f.logTag, "Applying block I/O bandwidth limits")
	}
	if len(vmProps.HostConfig.BlkioDeviceReadIOps) > 0 || len(vmProps.HostConfig.BlkioDeviceWriteIOps) > 0 {
		f.logger.Debug(f.logTag, "Applying block I/O IOPS limits")
	}

	for _, port := range vmProps.ExposedPorts {
		containerConfig.ExposedPorts[dkrnat.Port(port)] = struct{}{}
	}

	vmProps, err = f.cleanMounts(vmProps)
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Cleaning mount configurations")
	}

	// Preserve any existing binds and add the ephemeral disk
	vmProps.HostConfig.Binds = append(vmProps.HostConfig.Binds, EphemeralDiskCID{id}.AsString()+":/var/vcap/data/") //nolint:staticcheck

	f.logger.Debug(f.logTag, "Creating container %#v, host %#v", containerConfig, &vmProps.HostConfig)

	// Split network settings for bridge networking
	var additionalEndPtConfigs map[string]*dkrnet.EndpointSettings
	netConfig, additionalEndPtConfigs = splitNetworkSettings(netConfig)

	vmProps.Platform.OS = "linux"           //nolint:staticcheck
	vmProps.Platform.Architecture = "amd64" //nolint:staticcheck

	// Create context for container creation
	createCtx, createCancel := ContextWithTimeout(DefaultDockerTimeout)
	defer createCancel()

	container, err := f.dkrClient.ContainerCreate(
		createCtx, containerConfig, &vmProps.HostConfig, netConfig, &vmProps.Platform, id.AsString())
	if err != nil {
		return Container{}, bosherr.WrapError(err, "Creating container")
	}

	f.logger.Debug(f.logTag,
		"Connecting container '%s' to networks with opts %#v", container.ID, netConfig)

	// Create context for network operations
	networkCtx, networkCancel := ContextWithTimeout(DefaultDockerTimeout)
	defer networkCancel()

	for name, endPtConfig := range additionalEndPtConfigs {
		err := f.dkrClient.NetworkConnect(networkCtx, name, id.AsString(), endPtConfig)
		if err != nil {
			return Container{}, bosherr.WrapErrorf(err, "Connecting container to network '%s'", name)
		}
	}

	// Create context for starting container
	startCtx, startCancel := ContextWithTimeout(DefaultDockerTimeout)
	defer startCancel()

	err = f.dkrClient.ContainerStart(startCtx, id.AsString(), dkrcont.StartOptions{})
	if err != nil {
		cleanupErr := f.cleanUpContainer(container)
		if cleanupErr != nil {
			return Container{}, bosherr.WrapError(err,
				fmt.Sprintf("Starting container (cleanup also failed: %s)", cleanupErr))
		}
		return Container{}, bosherr.WrapError(err, "Starting container")
	}

	if useSystemd {
		// Create context for exec operations
		execCtx, execCancel := ContextWithTimeout(DefaultDockerTimeout)
		defer execCancel()

		execProcess, err := f.dkrClient.ContainerExecCreate(execCtx, id.AsString(), dkrcont.ExecOptions{Cmd: []string{"bash", "-c", "umount /etc/hosts"}})
		if err != nil {
			cleanupErr := f.cleanUpContainer(container)
			if cleanupErr != nil {
				return Container{}, bosherr.WrapError(err,
					fmt.Sprintf("Creating exec (cleanup also failed: %s)", cleanupErr))
			}
			return Container{}, bosherr.WrapError(err, "Creating exec")
		}

		err = f.dkrClient.ContainerExecStart(execCtx, execProcess.ID, dkrcont.ExecStartOptions{})
		if err != nil {
			cleanupErr := f.cleanUpContainer(container)
			if cleanupErr != nil {
				return Container{}, bosherr.WrapError(err,
					fmt.Sprintf("Starting exec (cleanup also failed: %s)", cleanupErr))
			}
			return Container{}, bosherr.WrapError(err, "Starting exec")
		}
	}

	agentEnv := apiv1.AgentEnvFactory{}.ForVM(agentID, id, networks, env, f.agentOptions)
	agentEnv.AttachSystemDisk(apiv1.NewDiskHintFromString(""))

	fileService := NewFileService(f.dkrClient, id, f.logger)
	agentEnvService := NewFSAgentEnvService(fileService, f.logger)

	err = agentEnvService.Update(agentEnv)
	if err != nil {
		cleanupErr := f.cleanUpContainer(container)
		if cleanupErr != nil {
			return Container{}, bosherr.WrapError(err,
				fmt.Sprintf("Updating container's agent env (cleanup also failed: %s)", cleanupErr))
		}
		return Container{}, bosherr.WrapError(err, "Updating container's agent env")
	}

	// Workaround: The BOSH CPI Go library doesn't include blobstore provider in AgentOptions,
	// which causes the agent to fail with "executable bosh-blobstore- not found".
	// We need to patch the agent environment file after it's written.
	// TODO: This should be fixed in the BOSH CPI Go library to properly support blobstore configuration
	blobstoreProvider := "dav" // Hardcoded for now since the Go library doesn't expose this
	if blobstoreProvider != "" {
		fixBlobstoreCtx, fixBlobstoreCancel := ContextWithTimeout(ShortDockerTimeout)
		defer fixBlobstoreCancel()

		fixCmd := fmt.Sprintf(`python3 -c "
import json
with open('/var/vcap/bosh/warden-cpi-agent-env.json', 'r') as f:
    data = json.load(f)
if 'env' in data and 'bosh' in data['env'] and 'blobstores' in data['env']['bosh']:
    for bs in data['env']['bosh']['blobstores']:
        # Only set provider if it's missing
        if 'provider' not in bs or not bs['provider']:
            bs['provider'] = '%s'
        # Fix endpoint URL to include scheme if missing
        if 'options' in bs and 'endpoint' in bs['options']:
            endpoint = bs['options']['endpoint']
            if endpoint and not endpoint.startswith('http://') and not endpoint.startswith('https://'):
                # Assume HTTPS for standard BOSH blobstore port
                if ':25250' in endpoint:
                    bs['options']['endpoint'] = 'https://' + endpoint
                else:
                    bs['options']['endpoint'] = 'http://' + endpoint
with open('/var/vcap/bosh/warden-cpi-agent-env.json', 'w') as f:
    json.dump(data, f)

# Write back the config
with open('/var/vcap/bosh/warden-cpi-agent-env.json', 'w') as f:
    json.dump(data, f)
"`, blobstoreProvider)

		exec, err := f.dkrClient.ContainerExecCreate(fixBlobstoreCtx, container.ID, dkrcont.ExecOptions{
			AttachStderr: false,
			AttachStdout: false,
			Cmd:          []string{"sh", "-c", fixCmd},
		})
		if err != nil {
			f.logger.Warn(f.logTag, "Failed to create exec for blobstore fix: %s", err)
		} else {
			err = f.dkrClient.ContainerExecStart(fixBlobstoreCtx, exec.ID, dkrcont.ExecStartOptions{})
			if err != nil {
				f.logger.Warn(f.logTag, "Failed to fix blobstore provider: %s", err)
			}
		}
	}

	// Add a blobstore readiness check with retry mechanism
	blobstoreReadinessCtx, blobstoreReadinessCancel := ContextWithTimeout(DefaultDockerTimeout)
	defer blobstoreReadinessCancel()

	// Create a script that waits for blobstore to be ready
	blobstoreWaitScript := `#!/bin/bash
# Wait for blobstore to be ready with exponential backoff
BLOBSTORE_ENDPOINT=$(python3 -c "
import json
with open('/var/vcap/bosh/warden-cpi-agent-env.json', 'r') as f:
    data = json.load(f)
for bs in data.get('env', {}).get('bosh', {}).get('blobstores', []):
    if 'options' in bs and 'endpoint' in bs['options']:
        print(bs['options']['endpoint'])
        break
")

if [ -z "$BLOBSTORE_ENDPOINT" ]; then
    echo "No blobstore endpoint found"
    exit 0
fi

echo "Waiting for blobstore at $BLOBSTORE_ENDPOINT to be ready..."

# Try multiple times with exponential backoff
for i in {1..5}; do
    # Use curl with insecure flag for self-signed certificates
    if curl -k -s --connect-timeout 2 --max-time 5 "$BLOBSTORE_ENDPOINT" >/dev/null 2>&1; then
        echo "Blobstore is ready"
        exit 0
    fi
    
    # Exponential backoff: 2, 4, 8, 16, 32 seconds
    SLEEP_TIME=$((2 ** i))
    echo "Blobstore not ready, waiting ${SLEEP_TIME}s before retry..."
    sleep $SLEEP_TIME
done

echo "Warning: Blobstore may not be ready after retries"
`

	// Write and execute the blobstore readiness check script
	writeScriptExec, err := f.dkrClient.ContainerExecCreate(blobstoreReadinessCtx, container.ID, dkrcont.ExecOptions{
		AttachStderr: false,
		AttachStdout: false,
		Cmd:          []string{"sh", "-c", fmt.Sprintf("echo '%s' > /tmp/wait-for-blobstore.sh && chmod +x /tmp/wait-for-blobstore.sh && /tmp/wait-for-blobstore.sh", blobstoreWaitScript)},
	})
	if err != nil {
		f.logger.Warn(f.logTag, "Failed to create exec for blobstore readiness check: %s", err)
	} else {
		err = f.dkrClient.ContainerExecStart(blobstoreReadinessCtx, writeScriptExec.ID, dkrcont.ExecStartOptions{})
		if err != nil {
			f.logger.Warn(f.logTag, "Failed to run blobstore readiness check: %s", err)
		}
	}

	return NewContainer(id, f.dkrClient, agentEnvService, f.logger), nil
}

// Find returns a VM by ID
func (f Factory) Find(id apiv1.VMCID) (VM, error) {
	fileService := NewFileService(f.dkrClient, id, f.logger)
	agentEnvService := NewFSAgentEnvService(fileService, f.logger)
	return NewContainer(id, f.dkrClient, agentEnvService, f.logger), nil
}

func (f Factory) cleanUpContainer(container dkrcont.CreateResponse) error {
	f.logger.Debug(f.logTag, "Cleaning up container '%s'", container.ID)

	// Create context for cleanup operations
	cleanupCtx, cleanupCancel := ContextWithTimeout(DefaultDockerTimeout)
	defer cleanupCancel()

	// First, try to stop the container gracefully
	stopTimeout := 10 // seconds
	err := f.dkrClient.ContainerStop(cleanupCtx, container.ID, dkrcont.StopOptions{
		Timeout: &stopTimeout,
	})
	if err != nil && !dkrclient.IsErrNotFound(err) {
		f.logger.Warn(f.logTag, "Failed to stop container '%s' gracefully: %s", container.ID, err.Error())
	}

	// Now remove the container with retries
	rmOpts := dkrcont.RemoveOptions{Force: true, RemoveVolumes: true}

	var lastErr error
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		// Create a new context for each retry to avoid timeout accumulation
		retryCtx, retryCancel := ContextWithTimeout(ShortDockerTimeout)

		err = f.dkrClient.ContainerRemove(retryCtx, container.ID, rmOpts)
		retryCancel() // Clean up context immediately

		if err == nil {
			f.logger.Debug(f.logTag, "Successfully cleaned up container '%s'", container.ID)
			return nil
		}

		if dkrclient.IsErrNotFound(err) {
			// Container already removed
			return nil
		}

		lastErr = err
		f.logger.Warn(f.logTag, "Attempt %d/%d: Failed to remove container '%s': %s",
			i+1, maxRetries, container.ID, err.Error())

		// Special handling for common errors
		if strings.Contains(err.Error(), "device or resource busy") {
			// Wait longer for resources to be released
			time.Sleep(time.Duration(i+1) * time.Second)
		} else if strings.Contains(err.Error(), "Driver aufs failed to remove root filesystem") {
			// AUFS specific issue - might succeed on retry
			time.Sleep(500 * time.Millisecond)
		}
	}

	// If we get here, all retries failed
	errMsg := fmt.Sprintf("Failed to clean up container '%s' after %d attempts: %s",
		container.ID, maxRetries, lastErr.Error())
	f.logger.Error(f.logTag, errMsg)

	// Also try to clean up the ephemeral disk volume
	ephDiskID := EphemeralDiskCID{apiv1.NewVMCID(container.ID)}.AsString()
	volumeCtx, volumeCancel := ContextWithTimeout(ShortDockerTimeout)
	defer volumeCancel()

	volErr := f.dkrClient.VolumeRemove(volumeCtx, ephDiskID, true)
	if volErr != nil && !dkrclient.IsErrNotFound(volErr) {
		f.logger.Warn(f.logTag, "Failed to remove ephemeral volume '%s': %s",
			ephDiskID, volErr.Error())
	}

	return bosherr.Error(errMsg)
}

func (f Factory) possiblyFindNodeWithDisk(diskID apiv1.DiskCID) (string, error) {
	ctx, cancel := ContextWithTimeout(ShortDockerTimeout)
	defer cancel()

	resp, err := f.dkrClient.VolumeList(ctx, volume.ListOptions{})
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

func (f Factory) cleanMounts(vmProps Props) (Props, error) {
	const unixSock = "unix://"

	for i := range vmProps.HostConfig.Mounts { //nolint:staticcheck
		mount := &vmProps.HostConfig.Mounts[i]

		// Validate and clean unix socket paths
		if strings.HasPrefix(mount.Source, unixSock) { //nolint:gosimple,staticcheck
			cleanPath := strings.TrimPrefix(mount.Source, unixSock)

			// Validate the cleaned path
			if cleanPath == "" {
				return vmProps, bosherr.Errorf("Invalid unix socket path: %s", mount.Source)
			}

			// Check for path traversal attempts
			if strings.Contains(cleanPath, "..") {
				return vmProps, bosherr.Errorf("Path traversal not allowed in mount source: %s", mount.Source)
			}

			mount.Source = cleanPath
			f.logger.Debug(f.logTag, "Cleaned unix socket path from %s to %s", unixSock+cleanPath, cleanPath)
		}

		// Additional validation for all mounts
		if mount.Type == "bind" && !filepath.IsAbs(mount.Source) {
			return vmProps, bosherr.Errorf("Bind mount source must be absolute path: %s", mount.Source)
		}
	}

	return vmProps, nil
}
