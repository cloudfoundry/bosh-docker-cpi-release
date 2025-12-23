#!/usr/bin/env bash

set -euo pipefail

# Script to create a light stemcell given an image URL
# Usage: ./create-light-stemcell.sh <image_url> [output_file]
# Example: ./create-light-stemcell.sh ghcr.io/cloudfoundry/ubuntu-noble-stemcell:1.165 light-stemcell.tgz

if [ $# -lt 1 ]; then
    echo "Usage: $0 <image_url> [output_file]"
    echo "Example: $0 ghcr.io/cloudfoundry/ubuntu-noble-stemcell:1.165 light-stemcell.tgz"
    exit 1
fi

IMAGE_URL="$1"
OUTPUT_FILE="${2:-light-stemcell.tgz}"

# Extract image details
# Format can be: registry/repo:tag or registry/repo@sha256:digest
IMAGE_NAME=$(echo "$IMAGE_URL" | sed 's|.*/||' | sed 's|[@:].*||')
IMAGE_TAG=$(echo "$IMAGE_URL" | grep -o ':[^@]*$' | sed 's/^://' || echo "latest")

# Extract OS from image name (e.g., ubuntu-noble-stemcell -> ubuntu-noble)
OS_NAME=$(echo "$IMAGE_NAME" | sed 's/-stemcell$//')

echo "Creating light stemcell for image: $IMAGE_URL"
echo "Image name: $IMAGE_NAME"
echo "Image tag: $IMAGE_TAG"
echo "OS: $OS_NAME"

# Create a temporary directory
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Create stemcell metadata in BOSH standard format (stemcell.MF)
# Name format: bosh-<infrastructure>-<os>
# For Docker CPI, use "docker" as the infrastructure
# Use stemcell_formats: ["docker-light"] and cloud_properties structure
cat > "$TEMP_DIR/stemcell.MF" <<'STEMCELL_MF'
---
name: bosh-docker-$OS_NAME
version: "$IMAGE_TAG"
api_version: 3
bosh_protocol: '1'
# Note: sha1 is the hash of an empty string, kept as a legacy placeholder value.
# Light stemcells use SHA256 verification for the actual image, not SHA1 for the metadata file.
sha1: da39a3ee5e6b4b0d3255bfef95601890afd80709
operating_system: $OS_NAME
stemcell_formats:
- docker-light
cloud_properties:
  image_reference: "$IMAGE_URL"
STEMCELL_MF

# Substitute variables in the template
sed -i '' "s/\$OS_NAME/$OS_NAME/g" "$TEMP_DIR/stemcell.MF"
sed -i '' "s|\$IMAGE_TAG|$IMAGE_TAG|g" "$TEMP_DIR/stemcell.MF"
sed -i '' "s|\$IMAGE_URL|$IMAGE_URL|g" "$TEMP_DIR/stemcell.MF"

# Optionally pull the image and get its SHA256 digest if Docker is available
if command -v docker &> /dev/null; then
    echo "Docker found, attempting to pull image and extract digest..."
    
    # Try to pull the image (may fail for private registries without auth)
    if docker pull "$IMAGE_URL" 2>/dev/null; then
        # Get the image digest
        DIGEST=$(docker inspect "$IMAGE_URL" --format='{{index .RepoDigests 0}}' 2>/dev/null | grep -o 'sha256:[a-f0-9]*' || echo "")
        
        if [ -n "$DIGEST" ]; then
            echo "Found image digest: $DIGEST"
            echo "  digest: \"$DIGEST\"" >> "$TEMP_DIR/stemcell.MF"
        else
            echo "Warning: Could not extract digest from image"
        fi
    else
        echo "Warning: Could not pull image. Proceeding without digest verification."
        echo "Note: For private registries, ensure you're authenticated with 'docker login'"
    fi
else
    echo "Docker not available. Creating light stemcell without digest verification."
fi

# Create empty image file (required by BOSH stemcell format)
touch "$TEMP_DIR/image"

echo ""
echo "Stemcell metadata:"
cat "$TEMP_DIR/stemcell.MF"
echo ""

# Create tar.gz archive with the metadata file and empty image file
echo "Creating archive: $OUTPUT_FILE"
tar -czf "$OUTPUT_FILE" -C "$TEMP_DIR" stemcell.MF image

# Show the result
echo ""
echo "✅ Light stemcell created successfully: $OUTPUT_FILE"
echo "Size: $(du -h "$OUTPUT_FILE" | cut -f1)"
echo ""
echo "You can upload this stemcell to BOSH with:"
echo "  bosh upload-stemcell $OUTPUT_FILE"
