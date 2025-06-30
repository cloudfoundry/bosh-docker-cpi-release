#!/bin/bash

set -e

# Source test utilities
source "$(dirname "$0")/test_utils.sh"

# Setup log directory and logfile with timestamp
LOG_DIR="logs"
mkdir -p "$LOG_DIR"
LOGFILE="$LOG_DIR/test-run-$(date +%Y%m%d-%H%M%S).log"
echo "Test run started at $(date)" > "$LOGFILE"
echo "Logging output to: $LOGFILE"

# Use exec to redirect all output to tee with unbuffered output
exec > >(stdbuf -o0 -e0 tee -a "$LOGFILE")
exec 2>&1

# This script runs integration tests for the Docker CPI
# 
# Environment variables:
#   WORKSPACE_PATH - Path to workspace containing bosh-deployment and docker-deployment repos (default: ~/workspace)
#   DOCKER_URL     - Docker daemon URL (default: auto-detected based on OS)
#   DOCKER_NETWORK - Docker network name (default: net3 for Linux, bridge for macOS)
#   USE_LOCAL_DOCKER - Force use of local Docker even on Linux (default: false)
#   STEMCELL_OS    - Stemcell OS to use: jammy, noble (default: noble)
#   STEMCELL_VERSION - Stemcell version to use (default: latest)
#
# Prerequisites:
#   1. Clone required repositories:
#      mkdir -p $WORKSPACE_PATH
#      cd $WORKSPACE_PATH
#      git clone https://github.com/cloudfoundry/bosh-deployment.git
#      git clone https://github.com/cppforlife/docker-deployment.git
#   
#   2. For Linux with remote Docker - Deploy Docker with TLS:
#      cd $WORKSPACE_PATH/docker-deployment
#      bosh create-env docker.yml --state=state.json --vars-store=creds.yml -v network=$DOCKER_NETWORK
#
#   3. For macOS - Ensure Docker Desktop is running

# Detect OS and set appropriate defaults
OS_TYPE=$(uname -s)
USE_LOCAL_DOCKER=${USE_LOCAL_DOCKER:-false}
STEMCELL_OS=${STEMCELL_OS:-noble}
STEMCELL_VERSION=${STEMCELL_VERSION:-latest}

# Setup workspace path
if [[ -z "$WORKSPACE_PATH" ]]; then
    # Use local tmp directory if WORKSPACE_PATH not set
    WORKSPACE_PATH="$PWD/tmp"
    echo "WORKSPACE_PATH not set, using local directory: $WORKSPACE_PATH"
    
    # Ensure tmp directory exists
    mkdir -p "$WORKSPACE_PATH"
    
    # Clone required repositories if they don't exist
    if [[ ! -d "$WORKSPACE_PATH/bosh-deployment" ]]; then
        echo "Cloning bosh-deployment..."
        git clone https://github.com/cloudfoundry/bosh-deployment.git "$WORKSPACE_PATH/bosh-deployment"
    else
        echo "Using existing bosh-deployment repository"
    fi
    
    if [[ ! -d "$WORKSPACE_PATH/docker-deployment" ]]; then
        echo "Cloning docker-deployment..."
        git clone https://github.com/cppforlife/docker-deployment.git "$WORKSPACE_PATH/docker-deployment"
    else
        echo "Using existing docker-deployment repository"
    fi
else
    echo "Using WORKSPACE_PATH: $WORKSPACE_PATH"
fi

# Set Docker defaults based on OS
if [[ "$OS_TYPE" == "Darwin" ]] || [[ "$USE_LOCAL_DOCKER" == "true" ]]; then
    # macOS or forced local Docker
    echo "Detected macOS or USE_LOCAL_DOCKER=true - using local Docker"
    if [[ "$OS_TYPE" == "Darwin" ]]; then
        # On macOS, Docker Desktop uses a different socket location
        DOCKER_URL=${DOCKER_URL:-unix://$HOME/.docker/run/docker.sock}
    else
        # On Linux with local Docker
        DOCKER_URL=${DOCKER_URL:-unix:///var/run/docker.sock}
    fi
    # Use custom network since bridge doesn't support manual IP assignment
    DOCKER_NETWORK=${DOCKER_NETWORK:-bosh-docker-test}
    USE_TLS=false
else
    # Linux - use local Docker socket (most common setup)
    echo "Detected Linux - using local Docker"
    DOCKER_URL=${DOCKER_URL:-unix:///var/run/docker.sock}
    DOCKER_NETWORK=${DOCKER_NETWORK:-bosh-docker-test}
    USE_TLS=false
fi

# Override TLS if using unix socket
if [[ "$DOCKER_URL" == unix://* ]]; then
    USE_TLS=false
fi

echo "Configuration:"
echo "  OS: $OS_TYPE"
echo "  Docker URL: $DOCKER_URL"
echo "  Docker Network: $DOCKER_NETWORK"
echo "  Use TLS: $USE_TLS"
echo "  Stemcell OS: $STEMCELL_OS"
echo "  Stemcell Version: $STEMCELL_VERSION"

# Validate Docker connectivity
echo ""
echo "Validating Docker connectivity..."
if [[ "$DOCKER_URL" == unix://* ]]; then
    # Extract socket path from unix URL
    SOCKET_PATH=${DOCKER_URL#unix://}
    if [[ ! -S "$SOCKET_PATH" ]]; then
        echo "ERROR: Docker socket not found at $SOCKET_PATH"
        echo "Please ensure Docker Desktop is running"
        exit 1
    fi
    # Test Docker connectivity
    if ! DOCKER_HOST="$DOCKER_URL" docker version >/dev/null 2>&1; then
        echo "ERROR: Cannot connect to Docker daemon at $DOCKER_URL"
        echo "Please ensure Docker Desktop is running and accessible"
        exit 1
    fi
else
    # For TCP connections, just try to connect
    if ! DOCKER_HOST="$DOCKER_URL" docker version >/dev/null 2>&1; then
        echo "ERROR: Cannot connect to Docker daemon at $DOCKER_URL"
        echo "Please ensure docker-deployment is running"
        exit 1
    fi
fi
echo "Docker connectivity validated successfully"

cpi_path=$PWD/cpi

rm -f creds.yml

# Create Docker network if using local Docker
if [[ "$USE_TLS" == "false" ]]; then
    header "Managing Docker network: $DOCKER_NETWORK"
    # Check if network already exists
    if docker network inspect "$DOCKER_NETWORK" >/dev/null 2>&1; then
        echo "       Network $DOCKER_NETWORK already exists"
    else
        echo "       Creating network $DOCKER_NETWORK with subnet 10.245.0.0/16"
        docker network create "$DOCKER_NETWORK" --driver bridge --subnet=10.245.0.0/16 --gateway=10.245.0.1
    fi
    echo "       Network $DOCKER_NETWORK ready"
fi

header "Create dev release"
bosh create-release --force --dir ./../ --tarball $cpi_path

# Helper function to build create-env command with optional TLS
build_create_env_cmd() {
    local action=$1
    shift
    
    local cmd="bosh $action ${WORKSPACE_PATH}/bosh-deployment/bosh.yml"
    cmd="$cmd -o ${WORKSPACE_PATH}/bosh-deployment/docker/cpi.yml"
    cmd="$cmd -o ${WORKSPACE_PATH}/bosh-deployment/jumpbox-user.yml"
    cmd="$cmd -o ../manifests/dev.yml"
    
    # Remove hardcoded stemcell from docker/cpi.yml
    cmd="$cmd -o ../manifests/ops-remove-hardcoded-stemcell.yml"
    
    # Override with our stemcell
    cmd="$cmd -o ../manifests/ops-override-stemcell.yml"
    
    # Add Docker BPM compatibility ops file for all Docker deployments (minimal approach)
    cmd="$cmd -o ../manifests/ops-docker-minimal.yml"
    
    # Add timeout increases for slower Docker environments
    cmd="$cmd -o ../manifests/increase-timeouts.yml"
    
    # NOTE: Do NOT configure agent blobstore for create-env!
    # During create-env, the BOSH CLI provides its own local blobstore.
    # Compilation VMs need to use this local blobstore, not the director's (which doesn't exist yet).
    # cmd="$cmd -o ../manifests/ops-configure-agent-blobstore.yml"
    
    # Make blobstore listen on all interfaces for Docker networking
    cmd="$cmd -o ../manifests/ops-blobstore-bind-all.yml"
    
    # Add local-docker ops file when TLS is disabled
    if [[ "$USE_TLS" == "false" ]]; then
        cmd="$cmd -o ../manifests/local-docker.yml"
        cmd="$cmd -o ../manifests/ops-docker-socket-stable-path.yml"
        cmd="$cmd -o ../manifests/ops-docker-socket-permissions.yml"
        
        # Use world-writable socket permissions if requested (less secure)
        if [[ "${DOCKER_SOCKET_WORLD_WRITABLE:-false}" == "true" ]]; then
            cmd="$cmd -o ../manifests/ops-docker-socket-world-writable.yml"
        fi
    fi
    
    # Add noble-specific fixes
    if [[ "$STEMCELL_OS" == "noble" ]]; then
        cmd="$cmd -o ../manifests/ops-fix-docker-socket-noble.yml"
    fi
    
    cmd="$cmd --state=state.json"
    cmd="$cmd --vars-store=creds.yml"
    cmd="$cmd -v docker_cpi_path=$cpi_path"
    cmd="$cmd -v director_name=docker"
    cmd="$cmd -v internal_cidr=10.245.0.0/16"
    cmd="$cmd -v internal_gw=10.245.0.1"
    cmd="$cmd -v internal_ip=10.245.0.11"
    cmd="$cmd -v docker_host=${DOCKER_URL}"
    
    if [[ "$USE_TLS" == "true" ]]; then
        if [[ ! -f "${WORKSPACE_PATH}/docker-deployment/creds.yml" ]]; then
            echo "ERROR: TLS enabled but ${WORKSPACE_PATH}/docker-deployment/creds.yml not found"
            echo "Please deploy docker-deployment first or set USE_LOCAL_DOCKER=true"
            exit 1
        fi
        cmd="$cmd --var-file docker_tls.certificate=<(bosh int ${WORKSPACE_PATH}/docker-deployment/creds.yml --path /docker_client_ssl/certificate)"
        cmd="$cmd --var-file docker_tls.private_key=<(bosh int ${WORKSPACE_PATH}/docker-deployment/creds.yml --path /docker_client_ssl/private_key)"
        cmd="$cmd --var-file docker_tls.ca=<(bosh int ${WORKSPACE_PATH}/docker-deployment/creds.yml --path /docker_client_ssl/ca)"
    else
        # Provide empty TLS values for local Docker
        cmd="$cmd -v docker_tls={}"
    fi
    
    cmd="$cmd -v network=${DOCKER_NETWORK}"
    
    # Add any additional arguments
    for arg in "$@"; do
        cmd="$cmd $arg"
    done
    
    echo "$cmd"
}

header "Validating Docker socket permissions"
# Check Docker socket permissions
DOCKER_SOCKET="/var/run/docker.sock"
if [ -S "$DOCKER_SOCKET" ]; then
    socket_perms=$(stat -c "%a" "$DOCKER_SOCKET" 2>/dev/null)
    socket_owner=$(stat -c "%U:%G" "$DOCKER_SOCKET" 2>/dev/null)
    echo "       Docker socket: $DOCKER_SOCKET (perms: $socket_perms, owner: $socket_owner)"
    
    # Test if we can access Docker
    if ! docker version &>/dev/null; then
        echo "       ✗ Cannot access Docker daemon"
        echo "       This may cause issues when the BOSH director tries to access Docker"
        echo "       Consider:"
        echo "         - Adding your user to the docker group: sudo usermod -aG docker $USER"
        echo "         - Using rootless Docker"
        echo "         - Running with DOCKER_SOCKET_WORLD_WRITABLE=true (less secure)"
    else
        echo "       ✓ Docker daemon is accessible"
    fi
    
    # Auto-detect if we need world-writable permissions
    # Check if socket is not world-readable/writable (last digit < 6)
    if [[ -z "${DOCKER_SOCKET_WORLD_WRITABLE}" ]]; then
        last_perm_digit=${socket_perms: -1}
        if [[ "$last_perm_digit" -lt 6 ]]; then
            echo "       ⚠ Docker socket has restrictive permissions ($socket_perms)"
            echo "       The BOSH director (running as 'vcap' user) needs Docker access"
            echo "       Auto-enabling DOCKER_SOCKET_WORLD_WRITABLE=true for this test run"
            export DOCKER_SOCKET_WORLD_WRITABLE=true
        fi
    fi
else
    echo "       ✗ Docker socket not found at $DOCKER_SOCKET"
    exit 1
fi

header "Validating cgroupsv2 support"
# Check if we should skip validation (for testing purposes)
if [[ "${SKIP_CGROUPSV2_CHECK:-false}" == "true" ]]; then
    echo "       Skipping cgroupsv2 validation (SKIP_CGROUPSV2_CHECK=true)"
else
    # Run cgroupsv2 validation test before proceeding
    if [[ -f "./test_cgroupsv2_validation.sh" ]]; then
        echo "       Running cgroupsv2 validation..."
        
        # Run the test and capture exit code separately to avoid issues with set -e
        set +e
        ./test_cgroupsv2_validation.sh > /tmp/cgroupsv2_test.out 2>&1
        test_exit_code=$?
        set -e
        
        if [ $test_exit_code -eq 0 ]; then
            echo "       ✓ cgroupsv2 validation passed"
        else
            echo "       ✗ cgroupsv2 validation failed (exit code: $test_exit_code)"
            # Show first few lines of output for debugging
            head -10 /tmp/cgroupsv2_test.out | sed 's/^/       /'
            echo "       The Docker CPI requires cgroupsv2 support. Please ensure:"
            echo "       - Your system has cgroupsv2 enabled"
            echo "       - Docker is using the systemd cgroup driver"
            echo "       - Required cgroup controllers are available"
            # For now, just warn but don't exit since cgroupsv2 is actually working
            echo "       WARNING: Continuing despite validation failure (cgroupsv2 appears to be working)"
        fi
    else
        echo "       Warning: cgroupsv2 validation script not found"
    fi
fi

header "Create env"

# Show warning if using world-writable Docker socket
if [[ "${DOCKER_SOCKET_WORLD_WRITABLE:-false}" == "true" ]]; then
    echo "       WARNING: Using world-writable Docker socket permissions (less secure)"
    echo "       This is only recommended for development environments"
    echo
fi
# Enable debug output if requested
if [[ "${DEBUG:-false}" == "true" ]]; then
    export BOSH_LOG_LEVEL=debug
    echo "       Debug mode enabled"
fi

# Force BOSH to show progress
export BOSH_LOG_LEVEL=${BOSH_LOG_LEVEL:-info}

# Map stemcell OS to full name before using it
case "$STEMCELL_OS" in
    jammy)
        STEMCELL_OS_FULL="ubuntu-jammy"
        STEMCELL_NAME="bosh-warden-boshlite-ubuntu-jammy-go_agent"
        ;;
    noble)
        STEMCELL_OS_FULL="ubuntu-noble"
        STEMCELL_NAME="bosh-warden-boshlite-ubuntu-noble"  # Note: no go_agent suffix for noble
        ;;
    *)
        echo "ERROR: Unsupported stemcell OS: $STEMCELL_OS (supported: jammy, noble)"
        exit 1
        ;;
esac

# Query latest version if needed
if [[ "$STEMCELL_VERSION" == "latest" ]]; then
    echo "       Getting latest ${STEMCELL_OS_FULL} stemcell version..."
    
    # Query the API
    API_URL="https://bosh.io/api/v1/stemcells/${STEMCELL_NAME}"
    API_RESPONSE=$(curl -s "$API_URL")
    
    if [[ -z "$API_RESPONSE" ]]; then
        echo "ERROR: Empty response from bosh.io API"
        exit 1
    fi
    
    # Extract version - look for versions that are just numbers (e.g., "1.571" not "1.571.1")
    STEMCELL_VERSION=$(echo "$API_RESPONSE" | \
        jq -r '.[] | select(.version | test("^[0-9]+\\.[0-9]+$")) | .version' 2>/dev/null | \
        sort -V | tail -1)
    
    # For noble, also check single digit versions like "1.2"
    if [[ -z "$STEMCELL_VERSION" ]] && [[ "$STEMCELL_OS" == "noble" ]]; then
        STEMCELL_VERSION=$(echo "$API_RESPONSE" | \
            jq -r '.[] | .version' 2>/dev/null | \
            grep -E '^[0-9]+\.[0-9]+$' | \
            sort -V | tail -1)
    fi
    
    if [[ -z "$STEMCELL_VERSION" ]]; then
        echo "ERROR: Failed to query latest stemcell version"
        echo "       API Response sample:"
        echo "$API_RESPONSE" | jq '.[0:2]' 2>/dev/null || echo "Could not parse API response"
        exit 1
    fi
    echo "       Latest version: $STEMCELL_VERSION"
    
    # Get stemcell SHA1
    STEMCELL_SHA1=$(echo "$API_RESPONSE" | \
        jq -r --arg version "$STEMCELL_VERSION" '.[] | select(.version == $version) | .regular.sha1' 2>/dev/null)
else
    # For specific version, we need to get the SHA1
    echo "       Getting SHA1 for ${STEMCELL_OS_FULL} v${STEMCELL_VERSION}..."
    API_RESPONSE=$(curl -s "https://bosh.io/api/v1/stemcells/${STEMCELL_NAME}")
    STEMCELL_SHA1=$(echo "$API_RESPONSE" | \
        jq -r --arg version "$STEMCELL_VERSION" '.[] | select(.version == $version) | .regular.sha1' 2>/dev/null)
fi

# Setup stemcell cache directory for create-env
STEMCELL_CACHE_DIR="${WORKSPACE_PATH}/stemcells/${STEMCELL_OS}/${STEMCELL_VERSION}"
mkdir -p "$STEMCELL_CACHE_DIR"

# Build stemcell filename
STEMCELL_FILENAME="${STEMCELL_NAME}-${STEMCELL_VERSION}.tgz"
CACHED_STEMCELL_PATH="${STEMCELL_CACHE_DIR}/${STEMCELL_FILENAME}"

# Check if stemcell is already cached
if [[ -f "$CACHED_STEMCELL_PATH" ]]; then
    echo "       Found cached stemcell at: $CACHED_STEMCELL_PATH"
    
    # Verify SHA1 if available
    if [[ -n "$STEMCELL_SHA1" ]] && [[ "$STEMCELL_SHA1" != "null" ]]; then
        echo "       Verifying cached stemcell SHA1..."
        CACHED_SHA1=$(sha1sum "$CACHED_STEMCELL_PATH" | cut -d' ' -f1)
        if [[ "$CACHED_SHA1" != "$STEMCELL_SHA1" ]]; then
            echo "       WARNING: Cached stemcell SHA1 mismatch (expected: $STEMCELL_SHA1, got: $CACHED_SHA1)"
            echo "       Removing corrupted cached stemcell..."
            rm -f "$CACHED_STEMCELL_PATH"
        else
            echo "       Cached stemcell SHA1 verified"
        fi
    fi
fi

# Download stemcell if not cached
if [[ ! -f "$CACHED_STEMCELL_PATH" ]]; then
    echo "       Downloading stemcell to cache..."
    DOWNLOAD_URL="https://bosh.io/d/stemcells/${STEMCELL_NAME}?v=${STEMCELL_VERSION}"
    
    # Download to temp file first
    TEMP_FILE="${CACHED_STEMCELL_PATH}.tmp"
    if curl -L -o "$TEMP_FILE" "$DOWNLOAD_URL"; then
        # Verify SHA1 if available
        if [[ -n "$STEMCELL_SHA1" ]] && [[ "$STEMCELL_SHA1" != "null" ]]; then
            DOWNLOADED_SHA1=$(sha1sum "$TEMP_FILE" | cut -d' ' -f1)
            if [[ "$DOWNLOADED_SHA1" != "$STEMCELL_SHA1" ]]; then
                echo "ERROR: Downloaded stemcell SHA1 mismatch (expected: $STEMCELL_SHA1, got: $DOWNLOADED_SHA1)"
                rm -f "$TEMP_FILE"
                exit 1
            fi
        fi
        mv "$TEMP_FILE" "$CACHED_STEMCELL_PATH"
        echo "       Stemcell cached successfully"
    else
        echo "ERROR: Failed to download stemcell"
        rm -f "$TEMP_FILE"
        exit 1
    fi
fi

# Use cached stemcell for create-env
STEMCELL_URL="file://${CACHED_STEMCELL_PATH}"
echo "       Using stemcell: ${STEMCELL_OS_FULL} v${STEMCELL_VERSION}"
echo "       Starting BOSH director deployment (this takes 5-10 minutes)..."
echo ""

# Build the create-env command
if [[ -n "$STEMCELL_SHA1" ]] && [[ "$STEMCELL_SHA1" != "null" ]]; then
    CREATE_ENV_CMD="$(build_create_env_cmd create-env -v stemcell_url=\"$STEMCELL_URL\" -v stemcell_sha1=\"$STEMCELL_SHA1\")"
else
    echo "       WARNING: No SHA1 available, proceeding without verification"
    CREATE_ENV_CMD="$(build_create_env_cmd create-env -v stemcell_url=\"$STEMCELL_URL\" -v stemcell_sha1=\"\")"
fi

# Use script command to allocate a pseudo-TTY so BOSH shows progress
# The script command makes programs think they're running in a terminal
if command -v script >/dev/null 2>&1; then
    # Linux version of script
    if [[ "$OS_TYPE" == "Linux" ]]; then
        script -qefc "$CREATE_ENV_CMD" /dev/null
    else
        # macOS version of script has different flags
        script -q /dev/null "$CREATE_ENV_CMD"
    fi
else
    # Fallback if script is not available
    echo "       Note: Progress output disabled (install 'script' command for progress)"
    eval "$CREATE_ENV_CMD"
fi

export BOSH_ENVIRONMENT=10.245.0.11
export BOSH_CA_CERT="$(bosh int creds.yml --path /director_ssl/ca)"
export BOSH_CLIENT=admin
export BOSH_CLIENT_SECRET="$(bosh int creds.yml --path /admin_password)"

# Wait for director to be ready
header "Waiting for director to be ready..."
echo "       Checking director services status..."
start_time=$(date +%s)
timeout=600  # 10 minutes timeout
last_status=""
dots=0

while true; do
    # Check if we can connect to the director
    if bosh env >/dev/null 2>&1; then
        echo ""
        echo "       ✓ Director is ready!"
        break
    fi
    
    # Show progress by checking container status
    if [[ -n "${BOSH_VM_CID:-}" ]] || [[ -f state.json ]]; then
        # Try to get the container ID from state.json
        if [[ -z "${BOSH_VM_CID:-}" ]] && [[ -f state.json ]]; then
            BOSH_VM_CID=$(bosh int state.json --path /current_vm_cid 2>/dev/null || echo "")
        fi
        
        # Check monit status inside the container for progress indication
        if [[ -n "$BOSH_VM_CID" ]] && docker ps -q -f "name=$BOSH_VM_CID" >/dev/null 2>&1; then
            current_status=$(docker exec "$BOSH_VM_CID" /var/vcap/bosh/bin/monit summary 2>/dev/null | grep -E "(initializing|running|Does not exist)" | wc -l || echo "0")
            if [[ "$current_status" != "$last_status" ]] && [[ "$current_status" != "0" ]]; then
                echo ""
                echo "       Services starting: $(docker exec "$BOSH_VM_CID" /var/vcap/bosh/bin/monit summary 2>/dev/null | grep -c "running" || echo "0") running, $(docker exec "$BOSH_VM_CID" /var/vcap/bosh/bin/monit summary 2>/dev/null | grep -c "initializing" || echo "0") initializing"
                last_status="$current_status"
                dots=0
            fi
        fi
    fi
    
    current_time=$(date +%s)
    elapsed=$((current_time - start_time))
    
    if [ $elapsed -gt $timeout ]; then
        echo ""
        echo "ERROR: Director failed to become ready after 10 minutes"
        # Show diagnostic information
        if [[ -n "$BOSH_VM_CID" ]]; then
            echo "       Container status:"
            docker ps -f "name=$BOSH_VM_CID" --format "table {{.Status}}"
            echo "       Service status:"
            docker exec "$BOSH_VM_CID" /var/vcap/bosh/bin/monit summary 2>/dev/null || echo "Could not get monit status"
        fi
        exit 1
    fi
    
    # Show dots for progress, but reset line after 60 dots
    if [ $dots -ge 60 ]; then
        echo ""
        echo -n "       "
        dots=0
    fi
    echo -n "."
    ((dots++))
    
    sleep 1
done

header "Update cloud config"
bosh -n update-cloud-config ${WORKSPACE_PATH}/bosh-deployment/docker/cloud-config.yml \
  -o ../manifests/reserved_ips.yml \
  -v network=${DOCKER_NETWORK}

header "Upload stemcell"

# Map stemcell OS to full name
case "$STEMCELL_OS" in
    jammy)
        STEMCELL_OS_FULL="ubuntu-jammy"
        STEMCELL_NAME="bosh-warden-boshlite-ubuntu-jammy-go_agent"
        ;;
    noble)
        STEMCELL_OS_FULL="ubuntu-noble"
        STEMCELL_NAME="bosh-warden-boshlite-ubuntu-noble"  # Note: no go_agent suffix for noble
        ;;
    *)
        echo "ERROR: Unsupported stemcell OS: $STEMCELL_OS (supported: jammy, noble)"
        exit 1
        ;;
esac

# Query latest version if needed
if [[ "$STEMCELL_VERSION" == "latest" ]]; then
    echo "       Querying latest ${STEMCELL_OS_FULL} stemcell version..."
    
    # First check if jq is available
    if ! command -v jq &> /dev/null; then
        echo "ERROR: jq is not installed. Please install jq to query latest stemcell versions."
        echo "       On macOS: brew install jq"
        echo "       On Ubuntu/Debian: sudo apt-get install jq"
        exit 1
    fi
    
    # Query the API
    API_URL="https://bosh.io/api/v1/stemcells/${STEMCELL_NAME}"
    echo "       Fetching from: $API_URL"
    
    API_RESPONSE=$(curl -s "$API_URL")
    if [[ -z "$API_RESPONSE" ]]; then
        echo "ERROR: Empty response from bosh.io API"
        exit 1
    fi
    
    # Check if we got an error response
    if echo "$API_RESPONSE" | grep -q "error"; then
        echo "ERROR: API returned an error:"
        echo "$API_RESPONSE" | jq . 2>/dev/null || echo "$API_RESPONSE"
        exit 1
    fi
    
    # Extract version - look for versions that are just numbers (e.g., "1.571" not "1.571.1")
    STEMCELL_VERSION=$(echo "$API_RESPONSE" | \
        jq -r '.[] | select(.version | test("^[0-9]+\\.[0-9]+$")) | .version' 2>/dev/null | \
        sort -V | tail -1)
    
    if [[ -z "$STEMCELL_VERSION" ]]; then
        echo "ERROR: Failed to query latest stemcell version"
        echo "       API Response sample:"
        echo "$API_RESPONSE" | jq '.[0:2]' 2>/dev/null || echo "Could not parse API response"
        exit 1
    fi
    echo "       Latest version: $STEMCELL_VERSION"
fi

# Get stemcell SHA1
echo "       Getting SHA1 for ${STEMCELL_OS_FULL} v${STEMCELL_VERSION}..."

# Use cached response if we just queried it
if [[ -z "$API_RESPONSE" ]]; then
    API_RESPONSE=$(curl -s "https://bosh.io/api/v1/stemcells/${STEMCELL_NAME}")
fi

STEMCELL_SHA1=$(echo "$API_RESPONSE" | \
    jq -r --arg version "$STEMCELL_VERSION" '.[] | select(.version == $version) | .regular.sha1' 2>/dev/null)

# Upload cached stemcell (already downloaded during create-env)
echo "       Uploading stemcell from cache..."
if [[ -n "$STEMCELL_SHA1" ]] && [[ "$STEMCELL_SHA1" != "null" ]]; then
    bosh -n upload-stemcell "$CACHED_STEMCELL_PATH" --sha1 "$STEMCELL_SHA1"
else
    bosh -n upload-stemcell "$CACHED_STEMCELL_PATH"
fi

header "Create env second time to test persistent disk attachment"
echo "       Recreating director to test persistent disk attachment..."
echo ""

# Build the recreate command
if [[ -n "$STEMCELL_SHA1" ]] && [[ "$STEMCELL_SHA1" != "null" ]]; then
    RECREATE_CMD="$(build_create_env_cmd create-env --recreate -v stemcell_url=\"$STEMCELL_URL\" -v stemcell_sha1=\"$STEMCELL_SHA1\")"
else
    RECREATE_CMD="$(build_create_env_cmd create-env --recreate -v stemcell_url=\"$STEMCELL_URL\" -v stemcell_sha1=\"\")"
fi

# Use script command for progress
if command -v script >/dev/null 2>&1; then
    if [[ "$OS_TYPE" == "Linux" ]]; then
        script -qefc "$RECREATE_CMD" /dev/null
    else
        script -q /dev/null "$RECREATE_CMD"
    fi
else
    eval "$RECREATE_CMD"
fi

header "Delete previous deployment"
bosh -n -d zookeeper delete-deployment --force

header "Deploy"
bosh -n -d zookeeper deploy <(wget -O- https://raw.githubusercontent.com/cppforlife/zookeeper-release/master/manifests/zookeeper.yml) \
  -o <(echo "- type: replace
  path: /stemcells/0
  value:
    alias: default
    os: ${STEMCELL_OS_FULL}
    version: latest")

header "Recreate all VMs"
bosh -n -d zookeeper recreate

header "Exercise deployment"
# Skip smoke tests for Noble stemcell due to Golang 1.10 support bug with Zookeeper
if [[ "$STEMCELL_OS" == "noble" ]]; then
    echo "       WARNING: Skipping smoke tests for Noble stemcell"
    echo "       Zookeeper has a Golang 1.10 support bug that causes authentication failures on Noble"
    echo "       See error: 'panic: create: zk: could not connect to a server'"
else
    bosh -n -d zookeeper run-errand smoke-tests
fi

header "Restart deployment"
bosh -n -d zookeeper restart

header "Report any problems"
bosh -n -d zookeeper cck --report

header "Delete random VM"
bosh -n -d zookeeper delete-vm `bosh -d zookeeper vms|sort|cut -f5|head -1`

header "Fix deleted VM"
bosh -n -d zookeeper cck --auto

header "Delete deployment"
bosh -n -d zookeeper delete-deployment

header "Clean up disks, etc."
bosh -n -d zookeeper clean-up --all

header "Deleting env"
echo "       Cleaning up BOSH director..."
echo ""

# Build the delete-env command with stemcell variables
if [[ -n "$STEMCELL_SHA1" ]] && [[ "$STEMCELL_SHA1" != "null" ]]; then
    DELETE_CMD="$(build_create_env_cmd delete-env -v stemcell_url=\"$STEMCELL_URL\" -v stemcell_sha1=\"$STEMCELL_SHA1\")"
else
    DELETE_CMD="$(build_create_env_cmd delete-env -v stemcell_url=\"$STEMCELL_URL\" -v stemcell_sha1=\"\")"
fi

# Use script command for progress
if command -v script >/dev/null 2>&1; then
    if [[ "$OS_TYPE" == "Linux" ]]; then
        script -qefc "$DELETE_CMD" /dev/null
    else
        script -q /dev/null "$DELETE_CMD"
    fi
else
    eval "$DELETE_CMD"
fi

# Optionally clean up Docker network
if [[ "$USE_TLS" == "false" ]] && [[ "${CLEANUP_NETWORK:-false}" == "true" ]]; then
    echo ""
    header "Cleaning up Docker network: $DOCKER_NETWORK"
    docker network rm "$DOCKER_NETWORK" 2>/dev/null || true
    echo "       Network removed"
fi

header "Done"

# Create/update symlink to latest log (use relative path for portability)
cd "$LOG_DIR"
ln -sf "$(basename "$LOGFILE")" "test-run-latest.log"
cd ..

echo ""
echo -e "\nTest run completed at $(date)"
echo "Full log saved to: $LOGFILE"
echo "Latest log symlink: $LOG_DIR/test-run-latest.log"
