## bosh-docker-cpi-release

This is a BOSH release for the Docker CPI. It can be used against single or multi-host Docker configurations.

## System Requirements

### Dependencies
- BOSH CLI (https://bosh.io/docs/cli-v2-install/)
- jq (command-line JSON processor)
- Docker 20.10+ (recommended: Docker 24.0+ for full cgroupsv2 support)

### Control Groups v2 (cgroupsv2)
This release requires cgroupsv2 support:
- Linux kernel 4.15+ with cgroupsv2 enabled
- systemd 244+ (recommended)

To check cgroupsv2 availability:
```bash
# Using Makefile (recommended)
make check-cgroups

# Manual check
ls /sys/fs/cgroup/cgroup.controllers
```

Known limitations:

- requires Docker network to have ability to assign IP addresses
  - necessary for bootstrapping Director
- does not work with deployments that try to attach persistent disk
  - works during `bosh create-env` but not in `bosh deploy`
  - will be fixed in the Director when we wait for Agent to be responsive after attach_disk CPI call

## Development

### Prerequisites

Check and prepare your development environment:

```bash
# Check all dependencies and prepare workspace
make prepare
```

This will:
- Verify required dependencies (BOSH CLI, Docker, jq, Ruby, Go)
- Check Docker socket permissions
- Clone required repositories (bosh-deployment, docker-deployment)
- Validate cgroupsv2 support

### Quick Start with Makefile

The project includes a comprehensive Makefile for common development tasks:

```bash
# Check environment and run all tests
make test

# Quick test (BOSH director creation only)
make test-quick

# Clean up test environment
make clean

# Deep clean (removes all BOSH data)
make clean-all

# Build CPI binary
make build

# Run unit tests only
make unit-tests

# Create development release
make dev-release

# Show all available targets
make help
```

### Environment Variables

- `WORKSPACE_PATH`: Path to workspace with bosh-deployment (default: auto-clones to `./tests/tmp/`)
- `DOCKER_NETWORK`: Docker network name (default: `bosh-docker-test`)
- `USE_LOCAL_DOCKER`: Force local Docker usage (default: `false`)
- `CLEANUP_DOWNLOADS`: Also clean ~/.bosh/downloads during cleanup (default: `false`)
- `STEMCELL_OS`: Stemcell OS to use: `jammy` or `noble` (default: `noble`)
- `STEMCELL_VERSION`: Stemcell version to use (default: `latest`)
- `DOCKER_SOCKET_WORLD_WRITABLE`: Use less secure Docker socket permissions (default: `false`)

**Example Usage:**
```bash
# Test with custom workspace
WORKSPACE_PATH=/custom/path make test

# Verify setup before testing
make verify-setup

# Clean with downloads cache removal
CLEANUP_DOWNLOADS=true make clean

# Deep clean everything
make clean-all

# Test with Ubuntu Jammy stemcell (latest version)
STEMCELL_OS=jammy make test

# Test with specific Ubuntu Noble version
STEMCELL_OS=noble STEMCELL_VERSION=1.571 make test

# Test with specific Ubuntu Jammy version
STEMCELL_OS=jammy STEMCELL_VERSION=1.524 make test

# Test with world-writable Docker socket (if permission issues occur)
DOCKER_SOCKET_WORLD_WRITABLE=true make test
```

### Docker Socket Permissions

If you encounter Docker socket permission errors during deployment:

```
permission denied while trying to connect to the Docker daemon socket at unix:///docker.sock
```

This happens when the BOSH director container cannot access the Docker socket. Solutions:

1. **Use the workaround (development only):**
   ```bash
   DOCKER_SOCKET_WORLD_WRITABLE=true make test
   ```

2. **Fix host permissions:**
   ```bash
   # Add user to docker group
   sudo usermod -aG docker $USER
   # Log out and back in for changes to take effect
   ```

3. **Use rootless Docker:**
   - See https://docs.docker.com/engine/security/rootless/

The `make prepare` command will detect and warn about potential Docker socket issues.

### Manual Commands

#### Integration Tests
The integration test script automatically detects your OS and configures Docker accordingly:

```bash
# Run with default settings (Ubuntu Noble, latest version)
make test

# Use Ubuntu Jammy stemcell
STEMCELL_OS=jammy make test

# Use specific stemcell version
STEMCELL_OS=noble STEMCELL_VERSION=1.571 make test

# Or run directly without make
cd tests && ./run.sh
```

**macOS (default)**: Uses local Docker Desktop
- Docker URL: `unix://$HOME/.docker/run/docker.sock`
- Network: `bosh-docker-test` (automatically created)
- No TLS required

**Linux (default)**: Uses local Docker socket
- Docker URL: `unix:///var/run/docker.sock`
- Network: `bosh-docker-test`
- No TLS required

**Prerequisites**:
1. Clone required repositories (optional):
   ```bash
   # If WORKSPACE_PATH is not set, the test script will automatically clone these into ./tests/tmp/
   # To manually set up in a custom location:
   mkdir -p ~/workspace && cd ~/workspace
   git clone https://github.com/cloudfoundry/bosh-deployment.git
   git clone https://github.com/cppforlife/docker-deployment.git
   ```

2. For Linux with remote Docker (legacy):
   ```bash
   cd ~/workspace/docker-deployment
   bosh create-env docker.yml --state=state.json --vars-store=creds.yml -v network=net3
   ```

#### Unit Tests
```bash
cd src/bosh-docker-cpi
./bin/test
```

#### Building
```bash
# Build CPI binary
cd src/bosh-docker-cpi && ./bin/build

# Build for Linux (for release)
cd src/bosh-docker-cpi && ./bin/build-linux-amd64

# Create development release
bosh create-release --force
```

#### Cleanup

The project provides multiple cleanup options to manage test artifacts and BOSH data:

**Basic Cleanup** (`make clean`):
- Removes Docker containers and volumes created during tests
- Removes the Docker test network
- Cleans up test artifacts (cpi, creds.yml, state.json)
- Removes the specific BOSH installation directory for the current test
- Prunes orphaned Docker resources

```bash
# Standard cleanup
make clean

# Also remove BOSH downloads cache
CLEANUP_DOWNLOADS=true make clean
```

**Deep Cleanup** (`make clean-all`):
- Performs all basic cleanup tasks
- Removes ALL BOSH installation directories (~/.bosh/installations/*)
- Removes the entire BOSH downloads cache (~/.bosh/downloads)
- Removes Docker images with 'stemcell' in the name

```bash
# Complete cleanup of all BOSH data
make clean-all
```

**Manual Cleanup**:
```bash
# Using the cleanup script directly
cd tests && ./cleanup.sh

# View what will be cleaned before running
make clean-all --dry-run
```

**Cleanup Features**:
- **Smart Installation Tracking**: Reads the installation UUID from `state.json` to clean only the relevant installation
- **Stemcell Cache Preservation**: Cached stemcells in `$WORKSPACE_PATH/stemcells/` are preserved during cleanup
- **Safety**: Uses `-f` flags and error suppression to handle missing files gracefully
- **Informative Output**: Shows what's being cleaned and reports sizes where applicable

## TODO

- disk migration
- root & ephemeral disk size limits
- persistent disk attach after container is created
- AZ tagging
- efficient stemcell import for swarm
- drain of containers when host is going down
- expose ports
- network name vs cloud_properties
- multiple networks
- [cf] gorouter tcp tuning
  - running_in_container needs to check for docker
- [cf] postgres needs /var/vcap/store
OSX:
sudo ifconfig lo0 alias 10.245.0.11 netmask 255.255.255.255
