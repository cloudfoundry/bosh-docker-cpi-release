#!/usr/bin/env bash

set -euo pipefail

# Script to build a custom instant-bosh image and run integration tests
# This script:
# 1. Compiles the CPI binary using a multi-stage Docker build
# 2. Copies the binary and job templates into a custom instant-bosh image
# 3. Starts a BOSH director using the custom image
# 4. Tests lite stemcell upload functionality

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "==> Building custom instant-bosh image for testing"
echo ""

# Check prerequisites
if ! command -v docker &> /dev/null; then
    echo "Error: docker is required but not installed"
    exit 1
fi

if ! command -v ibosh &> /dev/null; then
    echo "Error: ibosh CLI is required but not installed"
    echo "Install with: curl -fsSL https://instant-bosh.dev/install.sh | bash"
    exit 1
fi

# Build custom instant-bosh image
CUSTOM_IMAGE="bosh-docker-cpi-test:latest"

echo "Using Dockerfile at: $SCRIPT_DIR/Dockerfile.custom-ibosh"

echo "Building Docker image: $CUSTOM_IMAGE"
echo ""
docker build --build-arg CACHE_BUST=$(date +%s) -f "$SCRIPT_DIR/Dockerfile.custom-ibosh" -t "$CUSTOM_IMAGE" "$REPO_ROOT"

if [ $? -ne 0 ]; then
    echo "Error: Failed to build Docker image"
    exit 1
fi

echo ""
echo "✅ Custom instant-bosh image built successfully: $CUSTOM_IMAGE"
echo ""

# Integration test: Start BOSH director and test lite stemcell upload
echo "==> Running integration test"
echo ""

# Create a lite stemcell for testing
TEST_IMAGE="ghcr.io/cloudfoundry/ubuntu-noble-stemcell:1.165"
LITE_STEMCELL="$SCRIPT_DIR/test-lite-stemcell.tgz"

echo "Creating test lite stemcell..."
"$SCRIPT_DIR/create-light-stemcell.sh" "$TEST_IMAGE" "$LITE_STEMCELL"

if [ ! -f "$LITE_STEMCELL" ]; then
    echo "Error: Failed to create lite stemcell"
    exit 1
fi

echo ""
echo "Stopping any existing BOSH director..."
ibosh stop || true

echo ""
echo "Starting BOSH director with custom image..."
echo "Command: ibosh start --image $CUSTOM_IMAGE"
echo ""

# Start the director with our custom image
# Note: This assumes ibosh CLI is properly installed and configured
ibosh start --image "$CUSTOM_IMAGE"

if [ $? -ne 0 ]; then
    echo "Error: Failed to start BOSH director"
    exit 1
fi

# Configure BOSH CLI environment
echo ""
echo "Configuring BOSH CLI environment..."
source <(ibosh print-env)

# Try to upload the lite stemcell
echo ""
echo "Testing lite stemcell upload..."
echo "Command: bosh upload-stemcell $LITE_STEMCELL"
echo ""

if bosh upload-stemcell "$LITE_STEMCELL"; then
    echo ""
    echo "✅ Lite stemcell uploaded successfully!"
    echo ""
    
    # List stemcells to confirm
    echo "Stemcells in BOSH:"
    bosh stemcells
    
    echo ""
    echo "==> Integration test PASSED ✅"
    echo ""
    echo "The lite stemcell feature is working correctly!"
    exit 0
else
    echo ""
    echo "❌ Lite stemcell upload failed"
    echo ""
    echo "==> Integration test FAILED ❌"
    
    # Show logs for debugging
    echo ""
    echo "BOSH logs:"
    ibosh logs
    
    exit 1
fi

echo "✅ Script created successfully"
