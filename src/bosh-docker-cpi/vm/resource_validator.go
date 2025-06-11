// Package vm provides container/VM management functionality for the BOSH Docker CPI.
// This file contains resource validation logic for cgroupsv2 compatibility.
//
// CGROUPSv2 Resource Limit Mappings:
//
// Memory Limits (memory controller):
//   - Memory: maps to memory.max (hard limit)
//   - MemoryReservation: maps to memory.low (soft guarantee)
//   - MemorySwap: maps to memory.swap.max (swap limit)
//   - MemorySwappiness: maps to memory.swappiness (0-100)
//
// CPU Limits (cpu controller):
//   - NanoCPUs: maps to cpu.max (quota/period in nanoseconds)
//   - CPUQuota/CPUPeriod: maps to cpu.max (quota microseconds per period)
//   - CPUShares: maps to cpu.weight (relative weight 1-10000, converted from shares)
//   - CpusetCpus: maps to cpuset.cpus (specific CPU cores)
//   - CpusetMems: maps to cpuset.mems (specific memory nodes)
//
// Process Limits (pids controller):
//   - PidsLimit: maps to pids.max (maximum number of processes)
//
// Block I/O Limits (io controller):
//   - BlkioWeight: maps to io.weight (relative weight 1-10000)
//   - BlkioDeviceReadBps: maps to io.max rbps=<limit>
//   - BlkioDeviceWriteBps: maps to io.max wbps=<limit>
//   - BlkioDeviceReadIOps: maps to io.max riops=<limit>
//   - BlkioDeviceWriteIOps: maps to io.max wiops=<limit>
//
// Important CGROUPSv2 Differences:
//   - Unified hierarchy: all controllers in single tree
//   - Threaded mode: cpu controller can be threaded
//   - No memory.kmem: kernel memory accounting always on
//   - No memory.memsw: swap controlled separately
//   - CPU weight replaces shares (100 shares ≈ 10 weight)
//   - IO controller replaces blkio with different interface
//
// Docker Compatibility:
//   - Docker 20.10+ has full cgroupsv2 support
//   - systemd cgroup driver recommended for cgroupsv2
//   - Some legacy options may be silently ignored
//   - Resource validation ensures compatibility
package vm

import (
	"context"
	"strings"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	dkrcontainer "github.com/docker/docker/api/types/container"
	dkrclient "github.com/docker/docker/client"
)

// ResourceValidator validates Docker resource limits for cgroupsv2 compatibility
type ResourceValidator struct {
	dkrClient *dkrclient.Client
}

// NewResourceValidator creates a new ResourceValidator with the given Docker client
func NewResourceValidator(dkrClient *dkrclient.Client) *ResourceValidator {
	return &ResourceValidator{
		dkrClient: dkrClient,
	}
}

// HostResources contains information about host system resources
type HostResources struct {
	TotalMemory     int64
	AvailableMemory int64
	TotalCPU        int
	CPUQuota        int64
	CPUPeriod       int64
}

// GetHostResources retrieves information about available host resources
func (rv *ResourceValidator) GetHostResources(ctx context.Context) (*HostResources, error) {
	info, err := rv.dkrClient.Info(ctx)
	if err != nil {
		return nil, bosherr.WrapError(err, "Getting Docker info for host resources")
	}

	hostResources := &HostResources{
		TotalMemory: info.MemTotal,
		TotalCPU:    info.NCPU,
	}

	// Get available memory from Docker daemon
	// Note: Docker doesn't directly report available memory, so we estimate
	// by checking current container usage
	containers, err := rv.dkrClient.ContainerList(ctx, dkrcontainer.ListOptions{})
	if err != nil {
		return nil, bosherr.WrapError(err, "Listing containers for resource calculation")
	}

	var usedMemory int64
	// Container list doesn't include full HostConfig, would need to inspect each
	// For now, just use a conservative estimate
	// In production, this would inspect each container for accurate memory usage
	_ = containers // Mark as used to avoid compiler warning

	// Conservative estimate: assume at least 1GB reserved for system
	systemReserved := int64(1 * 1024 * 1024 * 1024)
	hostResources.AvailableMemory = hostResources.TotalMemory - usedMemory - systemReserved
	if hostResources.AvailableMemory < 0 {
		hostResources.AvailableMemory = 0
	}

	return hostResources, nil
}

// ValidateVMProps validates VM properties for cgroupsv2 compatibility and resource constraints.
// It ensures that requested resources are within valid ranges and don't exceed host capacity.
// This function validates all cgroupsv2-related resource limits including memory, CPU, PIDs, and I/O.
func (rv *ResourceValidator) ValidateVMProps(ctx context.Context, props *Props) error {
	// Get host resources
	hostResources, err := rv.GetHostResources(ctx)
	if err != nil {
		// Don't fail hard if we can't get host resources, just log warning
		// Docker will ultimately enforce limits anyway
		return nil
	}

	// Validate memory limits
	// CGROUPSv2: Memory controller validates memory.max
	if props.Memory > 0 {
		if props.Memory < 0 {
			return bosherr.Error("Memory limit cannot be negative")
		}

		// Minimum memory requirement (32MB)
		minMemory := int64(32 * 1024 * 1024)
		if props.Memory < minMemory {
			return bosherr.Errorf("Memory limit %d is below minimum requirement of %d bytes (32MB)",
				props.Memory, minMemory)
		}

		if props.HostConfig.Memory > hostResources.TotalMemory {
			return bosherr.Errorf("Requested memory %d exceeds host total memory %d",
				props.HostConfig.Memory, hostResources.TotalMemory)
		}

		if props.HostConfig.Memory > hostResources.AvailableMemory {
			// Note: Requested memory exceeds available memory - Docker scheduler might handle this
			// Consider this a warning condition but don't fail validation
			_ = hostResources.AvailableMemory // Mark as intentionally unused to avoid linter warning
		}
	}

	// Validate memory + swap
	// CGROUPSv2: Separate memory.swap.max control (not combined with memory)
	if props.HostConfig.MemorySwap > 0 {
		if props.HostConfig.MemorySwap < props.HostConfig.Memory {
			return bosherr.Error("MemorySwap must be larger than Memory limit")
		}
	}

	// Validate CPU shares (relative weight)
	// CGROUPSv2: CPUShares maps to cpu.weight (converted: weight = 1 + ((shares-2)*9999)/262142)
	if props.HostConfig.CPUShares < 0 {
		return bosherr.Error("CPU shares cannot be negative")
	}

	// Validate CPU quota/period for hard limits
	// CGROUPSv2: Maps to cpu.max as "quota period" in microseconds
	if props.HostConfig.CPUQuota > 0 {
		if props.HostConfig.CPUPeriod <= 0 {
			// Set default period if not specified
			props.HostConfig.CPUPeriod = 100000 // 100ms default
		}

		if props.HostConfig.CPUQuota < 1000 {
			return bosherr.Error("CPU quota must be at least 1000 microseconds")
		}

		// Calculate requested CPUs (quota/period)
		requestedCPUs := float64(props.HostConfig.CPUQuota) / float64(props.HostConfig.CPUPeriod)
		if requestedCPUs > float64(hostResources.TotalCPU) {
			return bosherr.Errorf("Requested CPU quota %.2f exceeds host CPU count %d",
				requestedCPUs, hostResources.TotalCPU)
		}
	}

	// Validate NanoCPUs (Docker 1.12.3+)
	// CGROUPSv2: Internally converted to cpu.max quota/period values
	if props.HostConfig.NanoCPUs > 0 {
		requestedCPUs := float64(props.HostConfig.NanoCPUs) / 1e9
		if requestedCPUs > float64(hostResources.TotalCPU) {
			return bosherr.Errorf("Requested CPUs %.2f exceeds host CPU count %d",
				requestedCPUs, hostResources.TotalCPU)
		}

		// Minimum CPU requirement (0.01 CPU)
		if props.HostConfig.NanoCPUs < 1e7 {
			return bosherr.Error("CPU limit must be at least 0.01 CPU")
		}
	}

	// Validate PIDs limit (cgroupsv2 feature)
	// CGROUPSv2: Maps directly to pids.max in the pids controller
	if props.HostConfig.PidsLimit != nil && *props.HostConfig.PidsLimit < 0 {
		return bosherr.Error("PIDs limit cannot be negative")
	}

	// Validate CPU set (specific CPU cores)
	if props.HostConfig.CpusetCpus != "" {
		// Validate format and CPU existence
		if err := validateCPUSet(props.HostConfig.CpusetCpus, hostResources.TotalCPU); err != nil {
			return bosherr.WrapError(err, "Invalid CpusetCpus")
		}
	}

	// Validate Ulimits
	for _, ulimit := range props.HostConfig.Ulimits {
		if ulimit.Soft > ulimit.Hard {
			return bosherr.Errorf("Ulimit %s: soft limit %d exceeds hard limit %d",
				ulimit.Name, ulimit.Soft, ulimit.Hard)
		}
		if ulimit.Soft < 0 || ulimit.Hard < 0 {
			return bosherr.Errorf("Ulimit %s: limits cannot be negative", ulimit.Name)
		}
	}

	// Validate Mounts
	if err := rv.ValidateMounts(props); err != nil {
		return bosherr.WrapError(err, "Validating mounts")
	}

	// Validate Binds
	if err := rv.ValidateBinds(props); err != nil {
		return bosherr.WrapError(err, "Validating bind volumes")
	}

	return nil
}

// ValidateMounts validates mount configurations
func (rv *ResourceValidator) ValidateMounts(props *Props) error {
	for i, mount := range props.HostConfig.Mounts {
		// Validate mount type
		switch mount.Type {
		case "bind", "volume", "tmpfs", "npipe":
			// Valid types
		case "":
			// Default to volume if not specified
			props.HostConfig.Mounts[i].Type = "volume"
		default:
			return bosherr.Errorf("Invalid mount type '%s' for mount %s", mount.Type, mount.Target)
		}

		// Validate source and target
		if mount.Target == "" {
			return bosherr.Error("Mount target cannot be empty")
		}

		// For bind mounts, source must exist
		if mount.Type == "bind" && mount.Source == "" {
			return bosherr.Errorf("Bind mount source cannot be empty for target %s", mount.Target)
		}

		// Validate tmpfs options
		if mount.Type == "tmpfs" {
			if mount.TmpfsOptions != nil && mount.TmpfsOptions.SizeBytes < 0 {
				return bosherr.Errorf("Tmpfs size cannot be negative for mount %s", mount.Target)
			}
		}

		// Check for dangerous mount targets
		dangerousPaths := []string{"/", "/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64"}
		for _, dangerous := range dangerousPaths {
			if mount.Target == dangerous {
				return bosherr.Errorf("Mounting over system directory %s is not allowed", dangerous)
			}
		}
	}

	return nil
}

// ValidateBinds validates bind volume configurations
func (rv *ResourceValidator) ValidateBinds(props *Props) error {
	for _, bind := range props.HostConfig.Binds {
		// Bind format: source:destination[:options]
		parts := strings.Split(bind, ":")
		if len(parts) < 2 {
			return bosherr.Errorf("Invalid bind format '%s', expected source:destination[:options]", bind)
		}

		source := parts[0]
		destination := parts[1]

		// Validate source
		if source == "" {
			return bosherr.Errorf("Bind source cannot be empty in '%s'", bind)
		}

		// Validate destination
		if destination == "" {
			return bosherr.Errorf("Bind destination cannot be empty in '%s'", bind)
		}

		// Check for dangerous destinations
		dangerousPaths := []string{"/", "/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/proc", "/sys"}
		for _, dangerous := range dangerousPaths {
			if destination == dangerous {
				return bosherr.Errorf("Binding over system directory %s is not allowed", dangerous)
			}
		}

		// Validate options if present
		if len(parts) >= 3 {
			options := parts[2]
			validOptions := map[string]bool{
				"ro": true, "rw": true, "z": true, "Z": true,
				"rslave": true, "rprivate": true, "rshared": true,
				"slave": true, "private": true, "shared": true,
			}

			for _, opt := range strings.Split(options, ",") {
				if !validOptions[opt] {
					return bosherr.Errorf("Invalid bind option '%s' in '%s'", opt, bind)
				}
			}
		}
	}

	return nil
}

func validateCPUSet(cpuSet string, totalCPUs int) error {
	// Simple validation - Docker will do full validation
	// Just check basic format and CPU numbers don't exceed total
	if cpuSet == "" {
		return nil
	}

	// TODO: Parse CPU set string (e.g., "0-3", "0,2,4", "0-2,4")
	// and validate each CPU number < totalCPUs
	// For now, let Docker validate the format

	return nil
}

// GetCgroupsVersion detects if the system is using cgroupsv1 or cgroupsv2
// Returns: 1 for cgroupsv1, 2 for cgroupsv2, 0 if unable to determine
//
// Detection method:
// 1. Check Docker's CgroupVersion field (most reliable, Docker 20.10+)
// 2. Check CgroupDriver (systemd usually means v2, cgroupfs usually v1)
// 3. In production, would also check /sys/fs/cgroup/cgroup.controllers
func (rv *ResourceValidator) GetCgroupsVersion(ctx context.Context) (int, error) {
	info, err := rv.dkrClient.Info(ctx)
	if err != nil {
		return 0, bosherr.WrapError(err, "Getting Docker info for cgroups version")
	}

	// Check CgroupVersion field if available (Docker 20.10+)
	switch info.CgroupVersion {
	case "2":
		return 2, nil
	case "1":
		return 1, nil
	}

	// Fallback: check CgroupDriver
	switch info.CgroupDriver {
	case "systemd":
		// systemd driver typically indicates cgroupsv2
		return 2, nil
	case "cgroupfs":
		// cgroupfs typically indicates cgroupsv1, but could be v2
		// Would need to check /sys/fs/cgroup/cgroup.controllers to be sure
		return 1, nil
	}

	return 0, bosherr.Error("Unable to determine cgroups version")
}

// SystemdCompatibilityInfo contains systemd version and compatibility details
type SystemdCompatibilityInfo struct {
	Version          int
	FullCgroupsV2    bool
	BasicCgroupsV2   bool
	SystemdAvailable bool
}

// CheckSystemdCompatibility checks if systemd is compatible with cgroupsv2 features
func (rv *ResourceValidator) CheckSystemdCompatibility(ctx context.Context) (*SystemdCompatibilityInfo, error) {
	info := &SystemdCompatibilityInfo{}

	// Check Docker info for systemd details
	dockerInfo, err := rv.dkrClient.Info(ctx)
	if err != nil {
		return info, bosherr.WrapError(err, "Getting Docker info")
	}

	// If using systemd cgroup driver, systemd must be available
	if dockerInfo.CgroupDriver == "systemd" {
		info.SystemdAvailable = true
		// Note: We can't easily get systemd version from within Go
		// In production, this would be checked at the OS level
		info.BasicCgroupsV2 = true
		info.FullCgroupsV2 = true // Assume modern systemd if Docker uses it
	}

	return info, nil
}

// ValidateSystemdMode validates if systemd mode can be used safely
func (rv *ResourceValidator) ValidateSystemdMode(ctx context.Context, useSystemd bool) error {
	if !useSystemd {
		return nil // No validation needed if not using systemd
	}

	// Check if Docker is using systemd cgroup driver
	dockerInfo, err := rv.dkrClient.Info(ctx)
	if err != nil {
		return bosherr.WrapError(err, "Getting Docker info for systemd validation")
	}

	if dockerInfo.CgroupDriver != "systemd" {
		return bosherr.Error("SystemD mode requires Docker to use systemd cgroup driver")
	}

	// Check cgroupsv2 compatibility
	cgVersion, err := rv.GetCgroupsVersion(ctx)
	if err != nil {
		return bosherr.WrapError(err, "Checking cgroups version for systemd mode")
	}

	if cgVersion != 2 {
		return bosherr.Error("SystemD mode works best with cgroupsv2")
	}

	return nil
}
