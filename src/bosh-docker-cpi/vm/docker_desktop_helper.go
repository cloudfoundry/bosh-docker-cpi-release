package vm

import (
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	dkrclient "github.com/docker/docker/client"
)

// Regex for validating IP addresses and preventing command injection
var validIPRegex = regexp.MustCompile(`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`)

// validateIP validates that the input is a safe IP address
func validateIP(ip string) error {
	if !validIPRegex.MatchString(ip) {
		return bosherr.Errorf("Invalid IP address format: %s", ip)
	}

	// Parse as net.IP for additional validation
	if parsed := net.ParseIP(ip); parsed == nil {
		return bosherr.Errorf("Invalid IP address: %s", ip)
	}

	return nil
}

// validatePort validates that the port is in valid range
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return bosherr.Errorf("Invalid port number: %d", port)
	}
	return nil
}

// DockerDesktopHelper manages networking issues specific to Docker Desktop
type DockerDesktopHelper struct {
	dkrClient *dkrclient.Client
	logger    boshlog.Logger
}

// NewDockerDesktopHelper creates a new helper for Docker Desktop networking
func NewDockerDesktopHelper(dkrClient *dkrclient.Client, logger boshlog.Logger) *DockerDesktopHelper {
	return &DockerDesktopHelper{
		dkrClient: dkrClient,
		logger:    logger,
	}
}

// SetupNetworkForwarding sets up network forwarding for Docker Desktop
// This ensures BOSH can connect to the container IP by creating a route/alias
func (h *DockerDesktopHelper) SetupNetworkForwarding(containerIP string, hostPort int) error {
	if runtime.GOOS != "darwin" {
		return nil // Only needed on macOS Docker Desktop
	}

	h.logger.Debug("DockerDesktopHelper", "Setting up network forwarding for %s:%d", containerIP, hostPort)

	// Method 1: Try to add the container IP as an alias to lo0
	err := h.addLoopbackAlias(containerIP)
	if err != nil {
		h.logger.Warn("DockerDesktopHelper", "Failed to add loopback alias: %s", err.Error())

		// Method 2: Set up port forwarding using socat
		return h.setupPortForwarding(containerIP, hostPort)
	}

	return nil
}

// addLoopbackAlias adds the container IP as an alias to the loopback interface
func (h *DockerDesktopHelper) addLoopbackAlias(containerIP string) error {
	// Validate IP to prevent command injection
	if err := validateIP(containerIP); err != nil {
		return bosherr.WrapError(err, "Invalid container IP for loopback alias")
	}

	cmd := exec.Command("sudo", "ifconfig", "lo0", "alias", containerIP, "netmask", "255.255.255.255")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return bosherr.WrapErrorf(err, "Adding loopback alias for %s: %s", containerIP, string(output))
	}

	h.logger.Debug("DockerDesktopHelper", "Added loopback alias for %s", containerIP)
	return nil
}

// setupPortForwarding sets up port forwarding using socat
func (h *DockerDesktopHelper) setupPortForwarding(containerIP string, hostPort int) error {
	// Validate inputs to prevent command injection
	if err := validateIP(containerIP); err != nil {
		return bosherr.WrapError(err, "Invalid container IP")
	}
	if err := validatePort(hostPort); err != nil {
		return bosherr.WrapError(err, "Invalid host port")
	}

	h.logger.Debug("DockerDesktopHelper", "Setting up socat forwarding from %s:%d to localhost:%d",
		containerIP, hostPort, hostPort)

	// Find an available local IP that we can bind to
	localIP, err := h.getAvailableLocalIP()
	if err != nil {
		return bosherr.WrapError(err, "Finding available local IP")
	}

	// Validate the local IP as well
	if err := validateIP(localIP); err != nil {
		return bosherr.WrapError(err, "Invalid local IP")
	}

	// Use socat to forward traffic
	// #nosec G204 - hostPort and localIP have been validated above
	cmd := exec.Command("socat",
		"TCP-LISTEN:"+strconv.Itoa(hostPort)+",bind="+localIP+",fork",
		"TCP:localhost:"+strconv.Itoa(hostPort))

	err = cmd.Start()
	if err != nil {
		return bosherr.WrapErrorf(err, "Starting socat forwarder")
	}

	h.logger.Debug("DockerDesktopHelper", "Started socat forwarder (PID: %d) from %s:%d to localhost:%d",
		cmd.Process.Pid, localIP, hostPort, hostPort)

	return nil
}

// getAvailableLocalIP finds a local IP address that we can bind to
func (h *DockerDesktopHelper) getAvailableLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "127.0.0.1", nil // fallback to localhost
}

// CleanupNetworkForwarding removes network forwarding setup
func (h *DockerDesktopHelper) CleanupNetworkForwarding(containerIP string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}

	// Validate IP before using in any commands
	if err := validateIP(containerIP); err != nil {
		h.logger.Warn("DockerDesktopHelper", "Invalid container IP for cleanup: %s", err.Error())
		return nil
	}

	h.logger.Debug("DockerDesktopHelper", "Cleaning up network forwarding for %s", containerIP)

	// Remove loopback alias
	cmd := exec.Command("sudo", "ifconfig", "lo0", "-alias", containerIP)
	output, err := cmd.CombinedOutput()
	if err != nil {
		h.logger.Warn("DockerDesktopHelper", "Failed to remove loopback alias: %s: %s",
			err.Error(), string(output))
	}

	// Kill any socat processes (this is crude but effective for testing)
	// IP already validated above
	// #nosec G204 - containerIP has been validated as a safe IP address
	if err := exec.Command("pkill", "-f", "socat.*"+containerIP).Run(); err != nil {
		h.logger.Debug("DockerDesktopHelper", "Failed to kill socat processes: %s", err.Error())
	}

	return nil
}

// WaitForAgent waits for the BOSH agent to become available
func (h *DockerDesktopHelper) WaitForAgent(agentIP string, agentPort int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(agentIP, strconv.Itoa(agentPort)), 2*time.Second)
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				h.logger.Debug("DockerDesktopHelper", "Failed to close connection: %s", closeErr.Error())
			}
			h.logger.Debug("DockerDesktopHelper", "Agent is reachable at %s:%d", agentIP, agentPort)
			return nil
		}

		h.logger.Debug("DockerDesktopHelper", "Agent not yet reachable at %s:%d: %s", agentIP, agentPort, err.Error())
		time.Sleep(1 * time.Second)
	}

	return bosherr.Errorf("Agent at %s:%d not reachable after %v", agentIP, agentPort, timeout)
}
