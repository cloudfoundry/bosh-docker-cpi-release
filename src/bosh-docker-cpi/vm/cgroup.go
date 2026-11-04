package vm

import (
	"context"
	"io"
	"path"
	"strings"
	"time"

	"bosh-docker-cpi/config"

	"github.com/cloudfoundry/bosh-cpi-go/apiv1"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrcont "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/versions"
	dkrclient "github.com/docker/docker/client"
)

// cgroupV2Env tells systemd it runs in a container; set only for cgroup_v2 VMs.
const cgroupV2Env = "container=docker"

// cgroupV2Label marks containers created with a private cgroup namespace so that
// recreating them (disk attach/detach) keeps the cgroup_v2 behaviour. Unlike the
// environment, it cannot be inherited from the image.
const cgroupV2Label = "bosh-docker-cpi.cgroup_v2"

// cgroupV2PreflightMessage prefixes every preflight failure; it is also how a
// failed preflight is recognised in the container's output.
const cgroupV2PreflightMessage = "cgroup_v2 preflight failed"

// cgroupV2Preflight runs as the first start command, before systemd moves PID 1
// into init.scope. Docker/runc provides the namespace-rooted cgroup mount and
// privileged mode makes it writable; never replace it with a host bind mount.
// It fails closed, naming the check, on a non-cgroup2 mount, a host namespace,
// missing memory delegation, or a read-only cgroup mount.
const cgroupV2Preflight = `(fail() { echo "` + cgroupV2PreflightMessage + `: $1" >&2; exit 1; }; ` +
	`test "$(stat -f -c %T /sys/fs/cgroup)" = cgroup2fs || fail '/sys/fs/cgroup is not a cgroup2 mount'; ` +
	`grep -qx '0::/' /proc/self/cgroup || fail 'container is not in a private cgroup namespace'; ` +
	`test -f /sys/fs/cgroup/memory.max || fail 'memory.max is missing, memory controller is not delegated'; ` +
	`grep -qw memory /sys/fs/cgroup/cgroup.controllers || fail 'memory controller is not available'; ` +
	`test -w /sys/fs/cgroup/cgroup.procs || fail 'cgroup.procs is not writable, cgroup mount is read-only'; ` +
	`test -w /sys/fs/cgroup/cgroup.subtree_control || fail 'cgroup.subtree_control is not writable, cgroup mount is read-only')`

// cgroupPreflightGrace is how long to wait for a container to exit after start
// before concluding its preflight passed. The preflight is the first command,
// so a failure exits within milliseconds.
var cgroupPreflightGrace = 300 * time.Millisecond

func validateCgroupV2(props Props, enabled, systemd bool) error {
	if !enabled {
		return nil
	}
	if !systemd {
		return bosherr.Errorf("cgroup_v2 requires start_containers_with_systemd (or force_start_with_systemd)")
	}
	if mode := props.HostConfig.CgroupnsMode; mode != "" && mode != dkrcont.CgroupnsModePrivate { //nolint:staticcheck
		return bosherr.Errorf("cgroup_v2 requires CgroupnsMode %q, got %q; remove CgroupnsMode or set it to %q",
			dkrcont.CgroupnsModePrivate, mode, dkrcont.CgroupnsModePrivate)
	}
	// Props.Binds is not checked: Create replaces HostConfig.Binds with its own list.
	// VolumesFrom can import mounts which are invisible in this create request,
	// so they cannot be checked against the cgroup paths.
	if len(props.VolumesFrom) != 0 { //nolint:staticcheck
		return bosherr.Errorf("cgroup_v2 does not support VolumesFrom (%v) because imported mounts cannot be checked for /sys/fs/cgroup conflicts; replace them with explicit Mounts",
			props.VolumesFrom) //nolint:staticcheck
	}
	for _, mount := range props.Mounts {
		if cgroupOverlap(mount.Source) || cgroupOverlap(mount.Target) {
			return bosherr.Errorf("cgroup_v2 conflicts with mount %q -> %q", mount.Source, mount.Target)
		}
	}
	for target := range props.Tmpfs {
		if cgroupOverlap(target) {
			return bosherr.Errorf("cgroup_v2 conflicts with tmpfs %q", target)
		}
	}
	return nil
}

// Check ancestors as well as descendants: a recursive /sys bind at another
// destination also exposes host cgroups. Paths refer to the daemon host, so
// resolving symlinks on the CPI appliance would give the wrong answer.
func cgroupOverlap(p string) bool {
	if !path.IsAbs(p) {
		return false
	}
	p = path.Clean(p)
	const root = "/sys/fs/cgroup"
	return p == "/" || p == root || strings.HasPrefix(root, p+"/") || strings.HasPrefix(p, root+"/")
}

// minCgroupnsAPIVersion is the first Docker API version honouring HostConfig.CgroupnsMode
// and reporting Info.CgroupVersion.
const minCgroupnsAPIVersion = "1.41"

// cgroupV2Enabled resolves the CPI-wide default and the per-VM override. A default
// of true only applies to systemd VMs, so runit VMs keep working; an explicit
// per-VM true is still validated and rejected without systemd.
func cgroupV2Enabled(cfg config.Config, props Props, systemd bool) bool {
	if props.CgroupV2 != nil {
		return *props.CgroupV2
	}
	return cfg.CgroupV2 && systemd
}

// checkCgroupV2Host reports whether the Docker host runs cgroup v2 (private namespace)
// or v1 (legacy path). Unknown host capabilities and API errors must not silently
// downgrade. The container preflight checks writable mounts and memory delegation on v2.
func (f Factory) checkCgroupV2Host(ctx context.Context) (bool, error) {
	if clientVersion := f.dkrClient.ClientVersion(); versions.LessThan(clientVersion, minCgroupnsAPIVersion) {
		return false, bosherr.Errorf("cgroup_v2 requires Docker API >= %s for host detection, CPI client is pinned to %s", minCgroupnsAPIVersion, clientVersion)
	}

	// A daemon older than the pinned client version rejects this request itself.
	info, err := f.dkrClient.Info(ctx)
	if err != nil {
		return false, bosherr.WrapError(err, "Checking Docker daemon cgroup version for cgroup_v2")
	}
	switch info.CgroupVersion {
	case "1":
		return false, nil
	case "2":
		return true, nil
	default:
		return false, bosherr.Errorf("cgroup_v2 cannot detect a supported Docker host cgroup version, daemon reports %q", info.CgroupVersion)
	}
}

// cgroupPreflightFailure waits briefly for a just-started cgroup_v2 container to
// exit. A container still running after the grace period passed its preflight;
// one that exited failed during startup, and its output is returned so the
// operator sees the cause rather than a later generic upload error.
func cgroupPreflightFailure(dkrClient *dkrclient.Client, logger boshlog.Logger, logTag string, id apiv1.VMCID) error {
	ctx, cancel := context.WithTimeout(context.Background(), cgroupPreflightGrace)
	defer cancel()

	var exitCode int64
	statusCh, errCh := dkrClient.ContainerWait(ctx, id.AsString(), dkrcont.WaitConditionNotRunning)
	select {
	case status := <-statusCh:
		exitCode = status.StatusCode
	case err := <-errCh:
		if ctx.Err() != nil {
			return nil
		}
		logger.Warn(logTag, "Could not check whether container '%s' passed the cgroup_v2 preflight: %s", id.AsString(), err)
		return nil
	}

	output := "output unavailable"
	logs, err := dkrClient.ContainerLogs(context.TODO(), id.AsString(), dkrcont.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		logger.Warn(logTag, "Could not read logs of exited container '%s': %s", id.AsString(), err)
	} else {
		defer logs.Close() //nolint:errcheck
		raw, _ := io.ReadAll(io.LimitReader(logs, 4096))
		// Non-TTY logs are multiplexed with binary frame headers; keep printable text.
		output = strings.TrimSpace(strings.Map(func(r rune) rune {
			if r == '\n' || r >= ' ' && r < 0x7f {
				return r
			}
			return -1
		}, string(raw)))
	}

	if strings.Contains(output, cgroupV2PreflightMessage) {
		return bosherr.Errorf("%s (container exited with code %d)", output, exitCode)
	}
	return bosherr.Errorf("cgroup_v2 container exited during startup with code %d: %s", exitCode, output)
}
