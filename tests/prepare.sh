#!/bin/bash

# Don't use set -e here as we want to continue checking all dependencies
# even if some are missing

# Source test utilities
source "$(dirname "$0")/test_utils.sh"

header "BOSH Docker CPI - Preparing Environment"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Track if all dependencies are met
ALL_DEPS_MET=true

# Function to check if a command exists
check_command() {
    local cmd=$1
    local min_version=$2
    local version_cmd=$3
    local install_hint=$4
    
    if command -v "$cmd" &> /dev/null; then
        if [ -n "$version_cmd" ]; then
            version_output=$(eval "$version_cmd" 2>&1 || echo "unknown")
            success "$cmd is installed" "Version: $version_output"
        else
            success "$cmd is installed"
        fi
        return 0
    else
        error "$cmd is not installed"
        echo "       Install hint: $install_hint"
        ALL_DEPS_MET=false
        return 1
    fi
}

# Function to check Go version
check_go_version() {
    if command -v go &> /dev/null; then
        go_version=$(go version | awk '{print $3}' | sed 's/go//')
        required_version="1.21"
        
        if [ "$(printf '%s\n' "$required_version" "$go_version" | sort -V | head -n1)" = "$required_version" ]; then
            success "Go is installed" "Version: $go_version (>= $required_version)"
        else
            error "Go version is too old" "Version: $go_version (need >= $required_version)"
            echo "       Install hint: Download from https://golang.org/dl/"
            ALL_DEPS_MET=false
        fi
    else
        error "Go is not installed"
        echo "       Install hint: Download from https://golang.org/dl/"
        ALL_DEPS_MET=false
    fi
}

# Function to check Docker
check_docker() {
    if command -v docker &> /dev/null; then
        if docker version &> /dev/null; then
            docker_version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
            success "Docker is installed and running" "Version: $docker_version"
            
            # Check Docker socket permissions
            docker_socket="/var/run/docker.sock"
            DOCKER_SOCKET_ISSUE=false
            
            if [ -S "$docker_socket" ]; then
                socket_perms=$(stat -c "%a" "$docker_socket" 2>/dev/null || stat -f "%Op" "$docker_socket" 2>/dev/null)
                socket_group=$(stat -c "%G" "$docker_socket" 2>/dev/null || stat -f "%Sg" "$docker_socket" 2>/dev/null)
                
                # Check if user is in docker group
                if groups | grep -q docker; then
                    success "User is in docker group"
                else
                    warning "User is not in docker group"
                    echo "       Add user to docker group: sudo usermod -aG docker $USER"
                    echo "       Then log out and back in for changes to take effect"
                    DOCKER_SOCKET_ISSUE=true
                fi
                
                # Check socket permissions
                if [ "$socket_perms" = "660" ] || [ "$socket_perms" = "666" ]; then
                    success "Docker socket has correct permissions" "$socket_perms"
                    
                    # Even with correct permissions, test if containers can access the socket
                    # This catches issues with user namespace remapping and other edge cases
                    if ! docker run --rm -v /var/run/docker.sock:/docker.sock alpine:latest sh -c 'test -w /docker.sock' 2>/dev/null; then
                        warning "Containers may not be able to access Docker socket"
                        echo "       This can happen with user namespace remapping or security policies"
                        DOCKER_SOCKET_ISSUE=true
                    fi
                else
                    warning "Docker socket has unusual permissions" "$socket_perms"
                    DOCKER_SOCKET_ISSUE=true
                fi
            else
                error "Docker socket not found at $docker_socket"
                ALL_DEPS_MET=false
            fi
            
            # Check cgroup driver
            cgroup_driver=$(docker info 2>/dev/null | grep -i "Cgroup Driver:" | awk '{print $3}')
            if [ "$cgroup_driver" = "systemd" ]; then
                success "Docker using systemd cgroup driver"
            else
                warning "Docker not using systemd cgroup driver" "Current: $cgroup_driver"
            fi
            
            # Check cgroup version
            cgroup_version=$(docker info 2>/dev/null | grep -i "Cgroup Version:" | awk '{print $3}')
            if [ "$cgroup_version" = "2" ]; then
                success "Docker using cgroupsv2"
            elif [ -z "$cgroup_version" ]; then
                warning "Cannot determine Docker cgroup version" "Docker version may be < 20.10"
            else
                error "Docker not using cgroupsv2" "Version: $cgroup_version"
                ALL_DEPS_MET=false
            fi
        else
            error "Docker is installed but not running"
            echo "       Start Docker daemon: sudo systemctl start docker"
            ALL_DEPS_MET=false
        fi
    else
        error "Docker is not installed"
        echo "       Install hint: https://docs.docker.com/get-docker/"
        ALL_DEPS_MET=false
    fi
}

# Function to check BOSH CLI
check_bosh() {
    if command -v bosh &> /dev/null; then
        bosh_version=$(bosh --version | head -1)
        success "BOSH CLI is installed" "Version: $bosh_version"
    else
        error "BOSH CLI is not installed"
        echo "       Install hint: https://bosh.io/docs/cli-v2-install/"
        ALL_DEPS_MET=false
    fi
}

# Function to check system requirements
check_system_requirements() {
    subheader "System Requirements"
    
    # Check cgroupsv2
    if [ -f "/sys/fs/cgroup/cgroup.controllers" ]; then
        controllers=$(cat /sys/fs/cgroup/cgroup.controllers)
        success "cgroupsv2 is available" "Controllers: $controllers"
    else
        error "cgroupsv2 is not available"
        echo "       Your system needs cgroupsv2 support for this CPI"
        ALL_DEPS_MET=false
    fi
    
    # Check kernel version
    kernel_version=$(uname -r)
    success "Kernel version" "$kernel_version"
}

# Function to check Git LFS
check_git_lfs() {
    if command -v git-lfs &> /dev/null; then
        git_lfs_version=$(git-lfs version | awk '{print $1}')
        success "Git LFS is installed" "Version: $git_lfs_version"
        
        # Check if LFS files are pulled
        if [ -f "../final_blobs/c05654c1-4323-4672-5199-40afcf122d50" ]; then
            file_size=$(stat -c%s "../final_blobs/c05654c1-4323-4672-5199-40afcf122d50" 2>/dev/null || stat -f%z "../final_blobs/c05654c1-4323-4672-5199-40afcf122d50" 2>/dev/null || echo "0")
            if [ "$file_size" -gt 1000 ]; then
                success "Git LFS files are pulled"
            else
                warning "Git LFS files not fully pulled"
                echo "       Run: git lfs pull"
            fi
        fi
    else
        warning "Git LFS is not installed"
        echo "       Install hint: https://git-lfs.github.com/"
        echo "       Without Git LFS, release creation may fail"
    fi
}

# Function to check and setup workspace
check_workspace() {
    subheader "Workspace Setup"
    
    # Default to tests/tmp for workspace
    WORKSPACE_PATH="${WORKSPACE_PATH:-$(pwd)/tmp}"
    
    echo "Using workspace: $WORKSPACE_PATH"
    
    # Create workspace directory if it doesn't exist
    if [ ! -d "$WORKSPACE_PATH" ]; then
        echo "Creating workspace directory..."
        mkdir -p "$WORKSPACE_PATH"
    fi
    
    # Check and clone bosh-deployment
    if [ -d "${WORKSPACE_PATH}/bosh-deployment" ]; then
        success "bosh-deployment repository found"
        # Update to latest
        echo "       Updating bosh-deployment..."
        (cd "${WORKSPACE_PATH}/bosh-deployment" && git pull --quiet)
    else
        warning "bosh-deployment repository not found"
        echo "       Cloning bosh-deployment..."
        if git clone https://github.com/cloudfoundry/bosh-deployment.git "${WORKSPACE_PATH}/bosh-deployment"; then
            success "bosh-deployment cloned successfully"
        else
            error "Failed to clone bosh-deployment"
            ALL_DEPS_MET=false
        fi
    fi
    
    # Check and clone docker-deployment
    if [ -d "${WORKSPACE_PATH}/docker-deployment" ]; then
        success "docker-deployment repository found"
        # Update to latest
        echo "       Updating docker-deployment..."
        (cd "${WORKSPACE_PATH}/docker-deployment" && git pull --quiet)
    else
        warning "docker-deployment repository not found"
        echo "       Cloning docker-deployment..."
        if git clone https://github.com/cppforlife/docker-deployment.git "${WORKSPACE_PATH}/docker-deployment"; then
            success "docker-deployment cloned successfully"
        else
            error "Failed to clone docker-deployment"
            ALL_DEPS_MET=false
        fi
    fi
}

# Main preparation flow
echo "Checking development environment dependencies..."
echo

subheader "Required Dependencies"

# Check all required commands
check_command "bosh" "" "bosh --version | head -1" "https://bosh.io/docs/cli-v2-install/"
check_command "docker" "" "" "https://docs.docker.com/get-docker/"
check_command "jq" "" "jq --version" "sudo apt-get install jq"
check_command "ruby" "2.6" "ruby --version" "sudo apt-get install ruby"
check_go_version
check_command "make" "" "make --version | head -1" "sudo apt-get install make"

echo

subheader "Optional Dependencies"

check_git_lfs
check_command "yq" "" "yq --version" "https://github.com/mikefarah/yq"
check_command "shellcheck" "" "shellcheck --version | head -1" "sudo apt-get install shellcheck"

echo

# Check system requirements
check_system_requirements

echo

# Check Docker specifics
subheader "Docker Configuration"
check_docker

echo

# Check workspace
check_workspace

echo

# Summary
if [ "$ALL_DEPS_MET" = true ]; then
    echo -e "${GREEN}✓ All required dependencies are installed!${NC}"
    echo
    
    # Check if there's a Docker socket issue
    if [ "$DOCKER_SOCKET_ISSUE" = true ]; then
        echo -e "${YELLOW}⚠ Docker socket access issues detected${NC}"
        echo
        echo "The BOSH director container may not be able to access the Docker socket."
        echo "If you encounter permission errors during deployment, try:"
        echo
        echo -e "  ${GREEN}DOCKER_SOCKET_WORLD_WRITABLE=true make test${NC}"
        echo
        echo "This uses a less secure but more compatible configuration."
        echo "Only use this in development environments."
        echo
    fi
    
    echo "You can now run:"
    echo "  make test       - Run full integration tests"
    echo "  make test-quick - Run quick director creation test"
    echo "  make build      - Build the CPI binary"
    echo "  make dev-release - Create a development release"
    exit 0
else
    echo -e "${RED}✗ Some dependencies are missing!${NC}"
    echo
    
    # Check if there's a Docker socket issue even with missing deps
    if [ "$DOCKER_SOCKET_ISSUE" = true ]; then
        echo -e "${YELLOW}⚠ Docker socket access issues detected${NC}"
        echo
        echo "When running tests, you may need to use:"
        echo -e "  ${GREEN}DOCKER_SOCKET_WORLD_WRITABLE=true make test${NC}"
        echo
    fi
    
    # Check if only Ruby is missing (common case)
    if ! command -v ruby &> /dev/null && command -v bosh &> /dev/null && command -v docker &> /dev/null; then
        echo -e "${YELLOW}Note: Ruby is only required for BOSH director deployment (ERB template rendering).${NC}"
        echo "      You can still:"
        echo "      - make build     - Build the CPI binary"
        echo "      - make unit-tests - Run unit tests"
        echo
    fi
    
    echo "To run full integration tests, please install the missing dependencies and run 'make prepare' again."
    exit 1
fi