# Getting Started with BOSH Docker CPI

This guide will help you deploy your first BOSH director using the Docker CPI. The Docker CPI allows you to use Docker containers as VMs and Docker volumes as persistent disks, making it ideal for development and testing environments.

## Prerequisites

Before you begin, ensure your system meets these requirements:

### System Requirements

1. **Operating System**: Linux with cgroupsv2 support (Ubuntu 20.04+, Debian 11+, Fedora 31+)
2. **Docker**: Version 20.10 or later with systemd cgroup driver
3. **Kernel**: Linux 4.15+ with cgroupsv2 enabled
4. **systemd**: Version 244+ (recommended)

### Quick Validation

Run these commands to verify your system is ready:

```bash
# Check cgroupsv2 availability
ls /sys/fs/cgroup/cgroup.controllers
# Expected output: cpu cpuset io memory hugetlb pids rdma misc

# Check Docker version and cgroup driver
docker info | grep -E "(Server Version|Cgroup Driver)"
# Expected: Server Version: 20.10+ and Cgroup Driver: systemd

# Verify Docker is accessible
docker run hello-world
```

### Required Tools

Install these tools if not already present:

```bash
# BOSH CLI
curl -Lo /usr/local/bin/bosh https://github.com/cloudfoundry/bosh-cli/releases/latest/download/bosh-cli-linux-amd64
chmod +x /usr/local/bin/bosh

# Other dependencies
sudo apt-get update
sudo apt-get install -y build-essential git curl jq
```

## Quick Start with Makefile

The fastest way to test the Docker CPI is using the provided Makefile:

```bash
# Clone the repository
git clone https://github.com/cloudfoundry/bosh-docker-cpi-release.git
cd bosh-docker-cpi-release

# Run a full integration test (creates director, deploys Zookeeper)
make test

# Or run a quick test (only creates director)
make test-quick

# Clean up when done
make clean
```

The Makefile handles all the complex setup automatically, including:
- Creating a Docker network
- Building the CPI release
- Deploying a BOSH director
- Running integration tests

## Manual Setup

For more control over the deployment process, follow these manual steps:

### 1. Workspace Setup

Create a workspace and clone required repositories:

```bash
# Create workspace
export WORKSPACE_PATH=~/bosh-docker-workspace
mkdir -p $WORKSPACE_PATH
cd $WORKSPACE_PATH

# Clone required repositories
git clone https://github.com/cloudfoundry/bosh-deployment.git
git clone https://github.com/cppforlife/docker-deployment.git
git clone https://github.com/cloudfoundry/bosh-docker-cpi-release.git
```

### 2. Docker Network Configuration

The Docker CPI requires a custom network that supports manual IP assignment:

```bash
# Create a custom bridge network
docker network create bosh-docker \
  --driver bridge \
  --subnet=10.245.0.0/16 \
  --gateway=10.245.0.1

# Verify the network
docker network inspect bosh-docker
```

### 3. Build the CPI Release

```bash
cd $WORKSPACE_PATH/bosh-docker-cpi-release

# Create the release
bosh create-release --force --tarball=/tmp/bosh-docker-cpi.tgz
```

## First BOSH Director Deployment

### 1. Create the Director

Use the following command to create a BOSH director with Docker CPI:

```bash
cd $WORKSPACE_PATH/bosh-docker-cpi-release/tests

bosh create-env $WORKSPACE_PATH/bosh-deployment/bosh.yml \
  -o $WORKSPACE_PATH/bosh-deployment/docker/cpi.yml \
  -o $WORKSPACE_PATH/bosh-deployment/jumpbox-user.yml \
  -o ../manifests/dev.yml \
  -o ../manifests/ops-remove-hardcoded-stemcell.yml \
  -o ../manifests/ops-override-stemcell.yml \
  -o ../manifests/ops-docker-minimal.yml \
  -o ../manifests/local-docker.yml \
  -o ../manifests/ops-docker-socket-stable-path.yml \
  -o ../manifests/ops-docker-socket-permissions.yml \
  --state=state.json \
  --vars-store=creds.yml \
  -v docker_cpi_path=/tmp/bosh-docker-cpi.tgz \
  -v director_name=docker \
  -v internal_cidr=10.245.0.0/16 \
  -v internal_gw=10.245.0.1 \
  -v internal_ip=10.245.0.11 \
  -v docker_host=unix:///var/run/docker.sock \
  -v docker_tls={} \
  -v network=bosh-docker \
  -v stemcell_url="https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-noble?v=latest" \
  -v stemcell_sha1=""
```

This process takes 5-10 minutes. You'll see Docker containers being created and configured.

### 2. Configure Director Access

Once the director is created, set up your environment:

```bash
# Set director environment variables
export BOSH_ENVIRONMENT=10.245.0.11
export BOSH_CA_CERT="$(bosh int creds.yml --path /director_ssl/ca)"
export BOSH_CLIENT=admin
export BOSH_CLIENT_SECRET="$(bosh int creds.yml --path /admin_password)"

# Verify connection
bosh env
```

### 3. Update Cloud Config

Configure the cloud properties for deployments:

```bash
bosh update-cloud-config $WORKSPACE_PATH/bosh-deployment/docker/cloud-config.yml \
  -v network=bosh-docker
```

### 4. Upload a Stemcell

```bash
# Upload latest Ubuntu Noble stemcell
bosh upload-stemcell https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-noble?v=latest

# Or for Ubuntu Jammy (22.04)
bosh upload-stemcell https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-jammy-go_agent?v=latest
```

## Deploy a Sample Application

Deploy Zookeeper as a test:

```bash
# Deploy Zookeeper
bosh -d zookeeper deploy <(wget -O- https://raw.githubusercontent.com/cppforlife/zookeeper-release/master/manifests/zookeeper.yml)

# Run smoke tests
bosh -d zookeeper run-errand smoke-tests

# Check VMs
bosh -d zookeeper vms
```

## Common Operations Files

The Docker CPI includes several operations files for different scenarios:

- **`ops-docker-minimal.yml`**: Basic Docker compatibility settings
- **`ops-docker-socket-permissions.yml`**: Grants privileged mode for Docker socket access
- **`ops-docker-socket-world-writable.yml`**: Makes Docker socket world-writable (development only!)
- **`local-docker.yml`**: Removes TLS configuration for local Docker
- **`ops-override-stemcell.yml`**: Allows custom stemcell selection
- **`increase-timeouts.yml`**: Extends timeouts for slower environments

## Stemcell Selection

### Ubuntu Noble (24.04) - Recommended

```bash
# Latest Noble stemcell
-v stemcell_url="https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-noble?v=latest"
```

**Note**: Noble requires systemd mode. The CPI automatically detects and configures this.

### Ubuntu Jammy (22.04)

```bash
# Latest Jammy stemcell
-v stemcell_url="https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-jammy-go_agent?v=latest"
```

### Specific Versions

```bash
# Specific version
-v stemcell_url="https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-noble?v=1.571"
```

## Clean Up

To remove the director and clean up:

```bash
# Delete the director
bosh delete-env $WORKSPACE_PATH/bosh-deployment/bosh.yml \
  [... same ops files as create-env ...] \
  --state=state.json \
  --vars-store=creds.yml \
  [... same variables ...]

# Remove the Docker network (optional)
docker network rm bosh-docker

# Clean up state files
rm -f state.json creds.yml
```

## Next Steps

- Review [Troubleshooting Guide](troubleshooting.md) for common issues
- Read [Architecture Documentation](architecture.md) for technical details
- Explore the `manifests/` directory for more operations files
- Try deploying other BOSH releases

## Tips for Success

1. **Always verify prerequisites** before starting
2. **Use the Makefile** for quick testing
3. **Save your `state.json` and `creds.yml`** files - they're needed for updates/deletion
4. **Monitor Docker resources** - the CPI creates many containers
5. **Check logs** in `tests/logs/` when using the Makefile

Remember that the Docker CPI is designed for development and testing. For production deployments, use a cloud-specific CPI (AWS, GCP, Azure, vSphere, etc.).