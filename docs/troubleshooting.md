# Troubleshooting Guide for BOSH Docker CPI

This guide helps diagnose and resolve common issues when using the BOSH Docker CPI.

## Table of Contents

1. [cgroupsv2 Issues](#cgroupsv2-issues)
2. [Docker Socket Permissions](#docker-socket-permissions)
3. [Network Configuration](#network-configuration)
4. [Stemcell Compatibility](#stemcell-compatibility)
5. [Common Error Messages](#common-error-messages)
6. [Debug Techniques](#debug-techniques)

## cgroupsv2 Issues

The Docker CPI requires cgroupsv2 for proper resource management. Here's how to diagnose and fix related issues.

### Symptoms

- Error: "cgroupsv2 is not available on this system"
- Containers created without resource limits
- Resource enforcement not working
- Tests failing with cgroup-related errors

### Validation Commands

```bash
# Check if cgroupsv2 is available
ls /sys/fs/cgroup/cgroup.controllers
# Expected output: cpu cpuset io memory hugetlb pids rdma misc

# Verify Docker is using systemd cgroup driver
docker info | grep "Cgroup Driver"
# Expected: Cgroup Driver: systemd

# Check cgroup version
docker info | grep "Cgroup Version"
# Expected: Cgroup Version: 2

# Test resource limits work
docker run --rm --memory=100m --cpus=0.5 alpine echo "Limits work!"
```

### Enabling cgroupsv2

#### Ubuntu 20.04+
```bash
# Edit GRUB configuration
sudo nano /etc/default/grub
# Add to GRUB_CMDLINE_LINUX: systemd.unified_cgroup_hierarchy=1

# Update GRUB
sudo update-grub
sudo reboot
```

#### Configure Docker for systemd driver
```bash
# Create/edit Docker daemon config
sudo nano /etc/docker/daemon.json
```

Add:
```json
{
  "exec-opts": ["native.cgroupdriver=systemd"],
  "cgroup-parent": "/system.slice",
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "100m"
  }
}
```

```bash
# Restart Docker
sudo systemctl restart docker
```

### Verify cgroupsv2 is working

Run the validation test:
```bash
cd bosh-docker-cpi-release/tests
./test_cgroupsv2_validation.sh
```

## Docker Socket Permissions

Docker socket permission issues are common when the BOSH director tries to access Docker.

### Symptoms

- "permission denied while trying to connect to the Docker daemon socket"
- "Cannot connect to the Docker daemon"
- Director creation succeeds but VM creation fails

### Solutions

#### Option 1: Add user to docker group (Recommended)
```bash
# Add current user to docker group
sudo usermod -aG docker $USER

# Log out and back in, then verify
docker run hello-world
```

#### Option 2: Use privileged mode (Development)
```bash
# Add ops file during create-env
-o ../manifests/ops-docker-socket-permissions.yml
```

#### Option 3: World-writable socket (Development only!)
```bash
# Make socket world-writable (insecure!)
sudo chmod 666 /var/run/docker.sock

# Or use ops file
-o ../manifests/ops-docker-socket-world-writable.yml
```

#### Option 4: Rootless Docker
```bash
# Install rootless Docker
curl -fsSL https://get.docker.com/rootless | sh

# Set DOCKER_HOST
export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/docker.sock
```

### Debugging socket issues

```bash
# Check socket permissions
ls -la /var/run/docker.sock

# Test access as different user
sudo -u vcap docker version

# Check Docker socket location
docker context inspect | jq -r '.[0].Endpoints.docker.Host'
```

## Network Configuration

Docker network issues can prevent proper VM communication.

### Symptoms

- "Network has no available IPs"
- VMs cannot communicate with director
- Agent unreachable errors
- Bridge network doesn't support manual IP assignment

### Creating proper networks

```bash
# Remove default bridge attempt
docker network rm bosh-docker 2>/dev/null || true

# Create with proper subnet
docker network create bosh-docker \
  --driver bridge \
  --subnet=10.245.0.0/16 \
  --gateway=10.245.0.1 \
  --opt com.docker.network.bridge.name=br-bosh

# Verify configuration
docker network inspect bosh-docker | jq '.[0].IPAM.Config'
```

### Network troubleshooting

```bash
# List all networks
docker network ls

# Check network details
docker network inspect bosh-docker

# Test network connectivity
docker run --rm --network bosh-docker alpine ping -c 3 10.245.0.1

# Check for IP conflicts
docker ps --format "table {{.Names}}\t{{.Networks}}"
```

### Common network fixes

1. **IP allocation issues**
   ```bash
   # Ensure subnet is large enough
   --subnet=10.245.0.0/16  # Provides 65,534 IPs
   ```

2. **Gateway connectivity**
   ```bash
   # Verify gateway is first IP
   --gateway=10.245.0.1
   ```

3. **Multiple networks error**
   - Docker CPI only supports single network
   - Remove extra network configurations

## Stemcell Compatibility

Different stemcell versions have specific requirements.

### Noble (Ubuntu 24.04) Issues

#### Symptoms
- Containers fail to start
- "Failed to connect to bus: No such file or directory"
- systemd-related errors

#### Solution
The CPI auto-detects Noble and enables systemd mode. Ensure you're using the latest CPI version.

Manual override if needed:
```bash
# In CPI properties
start_containers_with_systemd: true
```

### Jammy to Noble Migration

1. **Check current stemcell**
   ```bash
   bosh stemcells
   ```

2. **Upload Noble stemcell**
   ```bash
   bosh upload-stemcell https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-noble?v=latest
   ```

3. **Update deployment**
   ```yaml
   stemcells:
   - alias: default
     os: ubuntu-noble
     version: latest
   ```

### Version-specific fixes

```bash
# For Noble - ensure ops file is included
-o ../manifests/ops-fix-docker-socket-noble.yml

# For older stemcells - disable systemd mode
-o ../manifests/ops-docker-bpm-compatibility.yml
```

## Common Error Messages

### "Cannot connect to Docker daemon"

```bash
# Check Docker is running
sudo systemctl status docker

# Verify socket exists
ls -la /var/run/docker.sock

# Test connection
DOCKER_HOST=unix:///var/run/docker.sock docker version
```

### "cgroupsv2 not available"

See [cgroupsv2 Issues](#cgroupsv2-issues) section above.

### "Network has no available IPs"

```bash
# Check network subnet size
docker network inspect bosh-docker | grep Subnet

# List all containers using network
docker ps --filter network=bosh-docker

# Remove stopped containers
docker container prune
```

### "Agent unreachable"

```bash
# Check container is running
docker ps | grep vm-

# Inspect container networking
docker inspect <container-id> | jq '.[0].NetworkSettings'

# Check agent logs
docker exec <container-id> tail -f /var/vcap/bosh/log/current
```

### "Compilation of package X failed"

```bash
# Increase timeouts
-o ../manifests/increase-timeouts.yml

# Check compilation VM logs
bosh -d <deployment> task <task-id> --debug

# Verify disk space
docker system df
```

## Debug Techniques

### Enable Debug Logging

```bash
# Set environment variable
export BOSH_LOG_LEVEL=debug

# Or in create-env command
BOSH_LOG_LEVEL=debug bosh create-env ...
```

### Check Container Logs

```bash
# List all BOSH-related containers
docker ps -a | grep -E "(vm-|disk-|compilation-)"

# View container logs
docker logs <container-id>

# Follow agent logs
docker exec <container-id> tail -f /var/vcap/bosh/log/current
```

### Inspect Monit Status

```bash
# Get container ID from state.json
CONTAINER_ID=$(bosh int state.json --path /current_vm_cid)

# Check monit summary
docker exec $CONTAINER_ID /var/vcap/bosh/bin/monit summary
```

### CPI Log Analysis

```bash
# During create-env, logs are in stderr
bosh create-env ... 2>&1 | tee create-env.log

# Search for CPI errors
grep -i error create-env.log
grep "ERROR" create-env.log | grep -v "SSL_ERROR_SSL"
```

### Docker Diagnostics

```bash
# Overall Docker health
docker system info

# Resource usage
docker system df

# Events monitoring
docker events --filter type=container --filter type=network

# Clean up resources
docker system prune -a
```

### Using Test Scripts

```bash
# Run specific tests
cd tests
./test_cgroupsv2_validation.sh
./test_docker_fallback.sh
./test_resource_enforcement.sh

# Enable debug mode
DEBUG=true ./run.sh
```

## Quick Fixes Checklist

1. ✓ Verify Docker is running: `sudo systemctl status docker`
2. ✓ Check cgroupsv2: `ls /sys/fs/cgroup/cgroup.controllers`
3. ✓ Verify network exists: `docker network ls | grep bosh`
4. ✓ Check disk space: `df -h` and `docker system df`
5. ✓ Validate permissions: `docker run hello-world`
6. ✓ Review logs: `tests/logs/test-run-latest.log`
7. ✓ Clean up: `docker system prune` and `make clean`

## Getting Help

If you're still experiencing issues:

1. Check the [GitHub Issues](https://github.com/cloudfoundry/bosh-docker-cpi-release/issues)
2. Enable debug logging and collect logs
3. Run validation tests and include output
4. Provide your environment details:
   ```bash
   uname -a
   docker version
   docker info
   bosh -v
   ```

Remember: The Docker CPI is designed for development/testing. Many issues that would be critical in production can be worked around with less secure options (like world-writable sockets) in development environments.