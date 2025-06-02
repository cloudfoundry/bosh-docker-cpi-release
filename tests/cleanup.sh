#!/bin/bash

set -e

# Docker CPI Test Cleanup Script
# This script resets and clears all test artifacts and Docker resources
# created during Docker CPI testing.

echo "=====> Starting Docker CPI test cleanup"

# Configuration
WORKSPACE_PATH=${WORKSPACE_PATH:-~/workspace}
DOCKER_NETWORK=${DOCKER_NETWORK:-bosh-docker-test}

# Detect OS for Docker socket path
OS_TYPE=$(uname -s)
if [[ "$OS_TYPE" == "Darwin" ]]; then
    DOCKER_URL="unix://$HOME/.docker/run/docker.sock"
else
    DOCKER_URL="unix:///var/run/docker.sock"
fi

echo "Configuration:"
echo "  OS: $OS_TYPE"
echo "  Docker URL: $DOCKER_URL"
echo "  Docker Network: $DOCKER_NETWORK"
echo "  Workspace: $WORKSPACE_PATH"
echo ""

# 1. Stop and Clean BOSH Environment
echo "-----> Cleaning BOSH environment"

# Set BOSH environment if director is running
if [[ -f "creds.yml" ]] && [[ -f "state.json" ]]; then
    echo "       Found existing BOSH director, attempting graceful cleanup"
    
    # Try to connect to existing director
    export BOSH_ENVIRONMENT=10.245.0.11
    export BOSH_CA_CERT="$(bosh int creds.yml --path /director_ssl/ca 2>/dev/null || echo '')"
    export BOSH_CLIENT=admin
    export BOSH_CLIENT_SECRET="$(bosh int creds.yml --path /admin_password 2>/dev/null || echo '')"
    
    # Stop any running deployments
    echo "       Deleting zookeeper deployment (if exists)"
    bosh -n -d zookeeper delete-deployment --force 2>/dev/null || true
    
    # Clean up BOSH artifacts
    echo "       Cleaning up BOSH artifacts"
    bosh -n clean-up --all 2>/dev/null || true
    
    # Delete the BOSH director environment
    echo "       Deleting BOSH director environment"
    bosh delete-env ${WORKSPACE_PATH}/bosh-deployment/bosh.yml \
        -o ${WORKSPACE_PATH}/bosh-deployment/docker/cpi.yml \
        -o ${WORKSPACE_PATH}/bosh-deployment/jumpbox-user.yml \
        -o ../manifests/dev.yml \
        -o ../manifests/ops-docker-bpm-compatibility.yml \
        -o ../manifests/local-docker.yml \
        --state=state.json \
        --vars-store=creds.yml \
        -v docker_cpi_path=$PWD/cpi \
        -v director_name=docker \
        -v internal_cidr=10.245.0.0/16 \
        -v internal_gw=10.245.0.1 \
        -v internal_ip=10.245.0.11 \
        -v docker_host="${DOCKER_URL}" \
        -v docker_tls={} \
        -v network=${DOCKER_NETWORK} 2>/dev/null || true
else
    echo "       No existing BOSH director found, skipping graceful cleanup"
fi

# Unset BOSH environment variables
unset BOSH_ENVIRONMENT BOSH_CA_CERT BOSH_CLIENT BOSH_CLIENT_SECRET

# 2. Clean Docker Containers and Volumes
echo "-----> Cleaning Docker containers and volumes"

echo "       Stopping BOSH-related containers"
# Stop and remove all BOSH-related containers (prefixed with c- or containing bosh)
docker ps -a --filter "name=c-" --format "{{.Names}}" 2>/dev/null | xargs -r docker stop 2>/dev/null || true
docker ps -a --filter "name=c-" --format "{{.Names}}" 2>/dev/null | xargs -r docker rm -f 2>/dev/null || true

# Also check for containers with bosh in the name
docker ps -a --filter "name=bosh" --format "{{.Names}}" 2>/dev/null | xargs -r docker stop 2>/dev/null || true
docker ps -a --filter "name=bosh" --format "{{.Names}}" 2>/dev/null | xargs -r docker rm -f 2>/dev/null || true

echo "       Removing orphaned containers"
docker container prune -f 2>/dev/null || true

echo "       Removing BOSH Docker volumes"
# Remove BOSH Docker volumes (prefixed with vol-)
docker volume ls --filter "name=vol-" --format "{{.Name}}" 2>/dev/null | xargs -r docker volume rm 2>/dev/null || true
docker volume prune -f 2>/dev/null || true

# 3. Clean Docker Networks
echo "-----> Cleaning Docker networks"

echo "       Removing test network: $DOCKER_NETWORK"
docker network rm "$DOCKER_NETWORK" 2>/dev/null || true

echo "       Cleaning up unused networks"
docker network prune -f 2>/dev/null || true

# 4. Remove Test Files
echo "-----> Removing test artifacts"

echo "       Removing CPI binary and state files"
rm -f cpi creds.yml state.json

echo "       Removing backup files"
rm -f run.sh.bak

# reserved_ips.yml is now in manifests/ directory, no need to clean up here

# 5. Optional: Clean Docker Images
if [[ "${CLEANUP_IMAGES:-false}" == "true" ]]; then
    echo "-----> Cleaning Docker images (CLEANUP_IMAGES=true)"
    
    echo "       Removing unused Docker images"
    docker image prune -f 2>/dev/null || true
    
    echo "       Removing BOSH stemcell images"
    docker images --filter "reference=*stemcell*" --format "{{.Repository}}:{{.Tag}}" 2>/dev/null | xargs -r docker rmi 2>/dev/null || true
    docker images --filter "reference=bosh.io/stemcells*" --format "{{.Repository}}:{{.Tag}}" 2>/dev/null | xargs -r docker rmi 2>/dev/null || true
else
    echo "-----> Skipping Docker image cleanup (set CLEANUP_IMAGES=true to enable)"
fi

echo ""
echo "=====> Docker CPI test cleanup completed successfully"
echo ""
echo "To run a fresh test, execute:"
echo "  cd tests && ./run.sh"
echo ""
echo "To also clean Docker images next time, run:"
echo "  CLEANUP_IMAGES=true ./cleanup.sh"