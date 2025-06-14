package vm

import (
	"context"
	"strings"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrcont "github.com/docker/docker/api/types/container"
	dkrclient "github.com/docker/docker/client"
)

// StemcellInfo contains information about the stemcell
type StemcellInfo struct {
	OSVersion  string
	OSCodename string
	UseSystemd bool
}

// DetectStemcellInfo detects information about a stemcell image
func DetectStemcellInfo(ctx context.Context, dkrClient *dkrclient.Client, imageID string, logger boshlog.Logger) (*StemcellInfo, error) {
	logTag := "vm.StemcellDetector"

	logger.Debug(logTag, "Detecting stemcell info for image %s", imageID)

	// Create a temporary container to check the OS version
	containerConfig := &dkrcont.Config{
		Image:        imageID,
		Entrypoint:   []string{"/bin/bash"},
		Cmd:          []string{"-c", "cat /etc/os-release 2>/dev/null || echo 'OS_RELEASE_NOT_FOUND'"},
		AttachStdout: true,
		AttachStderr: true,
	}

	resp, err := dkrClient.ContainerCreate(ctx, containerConfig, nil, nil, nil, "")
	if err != nil {
		return nil, bosherr.WrapError(err, "Creating temporary container for stemcell detection")
	}

	// Ensure cleanup
	defer func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), ShortDockerTimeout)
		defer cancel()
		if err := dkrClient.ContainerRemove(removeCtx, resp.ID, dkrcont.RemoveOptions{Force: true}); err != nil {
			// Log but don't fail - this is cleanup
			logger.Debug(logTag, "Failed to remove temporary container %s: %s", resp.ID, err)
		}
	}()

	// Start the container
	if err := dkrClient.ContainerStart(ctx, resp.ID, dkrcont.StartOptions{}); err != nil {
		return nil, bosherr.WrapError(err, "Starting temporary container")
	}

	// Wait for the container to finish
	statusCh, errCh := dkrClient.ContainerWait(ctx, resp.ID, dkrcont.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return nil, bosherr.WrapError(err, "Waiting for container")
		}
	case <-statusCh:
	}

	// Get the logs
	logsOptions := dkrcont.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	}
	logs, err := dkrClient.ContainerLogs(ctx, resp.ID, logsOptions)
	if err != nil {
		return nil, bosherr.WrapError(err, "Getting container logs")
	}
	defer func() {
		if err := logs.Close(); err != nil {
			// Log close error if needed
			logger.Debug(logTag, "Failed to close container logs: %s", err)
		}
	}()

	// Read the output
	buf := make([]byte, 4096)
	n, err := logs.Read(buf)
	if err != nil && err.Error() != "EOF" {
		// Handle read error if not EOF
		logger.Debug(logTag, "Error reading container logs: %s", err)
	}
	output := string(buf[:n])

	// Parse the output
	info := &StemcellInfo{}

	if strings.Contains(output, "OS_RELEASE_NOT_FOUND") {
		// Fallback: assume it's an older stemcell that uses runit
		logger.Debug(logTag, "No /etc/os-release found, assuming runit-based stemcell")
		info.UseSystemd = false
		return info, nil
	}

	// Parse os-release file
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")

		switch key {
		case "VERSION_ID":
			info.OSVersion = value
		case "VERSION_CODENAME":
			info.OSCodename = value
		}
	}

	logger.Debug(logTag, "Detected OS: version=%s, codename=%s", info.OSVersion, info.OSCodename)

	// Determine init system based on OS version
	// Ubuntu 24.04 (Noble) and later use systemd
	// Earlier versions use runit
	if info.OSCodename == "noble" || info.OSVersion >= "24.04" {
		logger.Debug(logTag, "Detected Ubuntu Noble or later, will use systemd")
		info.UseSystemd = true
	} else {
		logger.Debug(logTag, "Detected pre-Noble Ubuntu, will use runit")
		info.UseSystemd = false
	}

	// Also check if runsvdir-start exists as a fallback
	checkCmd := "test -f /usr/sbin/runsvdir-start && echo 'RUNSVDIR_EXISTS' || echo 'RUNSVDIR_MISSING'"
	checkConfig := &dkrcont.Config{
		Image:        imageID,
		Entrypoint:   []string{"/bin/bash"},
		Cmd:          []string{"-c", checkCmd},
		AttachStdout: true,
	}

	checkResp, err := dkrClient.ContainerCreate(ctx, checkConfig, nil, nil, nil, "")
	if err == nil {
		defer func() {
			removeCtx, cancel := context.WithTimeout(context.Background(), ShortDockerTimeout)
			defer cancel()
			if err := dkrClient.ContainerRemove(removeCtx, checkResp.ID, dkrcont.RemoveOptions{Force: true}); err != nil {
				// Log but don't fail - this is cleanup
				logger.Debug(logTag, "Failed to remove systemd check container %s: %s", checkResp.ID, err)
			}
		}()

		if err := dkrClient.ContainerStart(ctx, checkResp.ID, dkrcont.StartOptions{}); err == nil {
			statusCh, errCh := dkrClient.ContainerWait(ctx, checkResp.ID, dkrcont.WaitConditionNotRunning)
			select {
			case <-errCh:
			case <-statusCh:
			}

			checkLogs, err := dkrClient.ContainerLogs(ctx, checkResp.ID, logsOptions)
			if err == nil {
				defer func() {
					if err := checkLogs.Close(); err != nil {
						// Log close error if needed
						logger.Debug(logTag, "Failed to close systemd check logs: %s", err)
					}
				}()
				checkBuf := make([]byte, 1024)
				n, err := checkLogs.Read(checkBuf)
				if err != nil && err.Error() != "EOF" {
					// Handle read error if not EOF
					logger.Debug(logTag, "Error reading systemd check logs: %s", err)
				}
				checkOutput := string(checkBuf[:n])

				if strings.Contains(checkOutput, "RUNSVDIR_MISSING") && !info.UseSystemd {
					// If runsvdir is missing but we expected it, force systemd
					logger.Warn(logTag, "runsvdir-start not found in stemcell, forcing systemd mode")
					info.UseSystemd = true
				}
			}
		}
	}

	return info, nil
}

// GetInitCommand returns the appropriate init command for the stemcell
func GetInitCommand(stemcellInfo *StemcellInfo) string {
	if stemcellInfo.UseSystemd {
		return "/sbin/init"
	}
	return "/usr/sbin/runsvdir-start"
}
