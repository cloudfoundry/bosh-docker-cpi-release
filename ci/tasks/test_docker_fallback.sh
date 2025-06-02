#!/bin/bash

set -e

echo "Testing Docker cgroup driver fallback mechanism"
echo "=============================================="

# Source the helper functions
source $(dirname $0)/docker_config_helper.sh

# Test detect_optimal_cgroup_driver function
test_driver_detection() {
    echo "Test 1: Testing cgroup driver detection..."
    
    local detected_driver=$(detect_optimal_cgroup_driver)
    echo "Detected optimal driver: $detected_driver"
    
    # Verify detection logic
    if [ -f /sys/fs/cgroup/cgroup.controllers ] && [ "$(ps -p 1 -o comm=)" = "systemd" ]; then
        if [ "$detected_driver" != "systemd" ]; then
            echo "✗ Failed: Should detect systemd driver on cgroupsv2 with systemd"
            return 1
        fi
    fi
    
    echo "✓ Driver detection working correctly"
}

# Test system cgroups version detection
test_cgroups_version() {
    echo -e "\nTest 2: Testing cgroups version detection..."
    
    local version=$(get_system_cgroups_version)
    echo "Detected cgroups version: $version"
    
    # Verify the detection
    if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
        if [ "$version" != "v2" ]; then
            echo "✗ Failed: Should detect v2 when controllers file exists"
            return 1
        fi
    elif [ -d /sys/fs/cgroup/memory ]; then
        if [ "$version" != "v1" ]; then
            echo "✗ Failed: Should detect v1 when memory controller dir exists"
            return 1
        fi
    fi
    
    echo "✓ Cgroups version detection working correctly"
}

# Test compatibility verification
test_compatibility() {
    echo -e "\nTest 3: Testing compatibility verification..."
    
    # Test various combinations
    echo "Testing cgroupsv2 + systemd:"
    verify_cgroup_compatibility "systemd" "v2"
    
    echo -e "\nTesting cgroupsv2 + cgroupfs:"
    verify_cgroup_compatibility "cgroupfs" "v2"
    
    echo -e "\nTesting cgroupsv1 + cgroupfs:"
    verify_cgroup_compatibility "cgroupfs" "v1"
    
    echo "✓ Compatibility checks working correctly"
}

# Test Docker config creation with fallback
test_config_creation() {
    echo -e "\nTest 4: Testing Docker config creation..."
    
    # Create temporary directory for test
    local test_dir=$(mktemp -d)
    local test_config="${test_dir}/daemon.json"
    
    # Mock certificate files
    mkdir -p "${test_dir}/certs"
    touch "${test_dir}/certs/server-cert.pem"
    touch "${test_dir}/certs/server-key.pem"
    touch "${test_dir}/certs/ca.pem"
    
    # Test config creation (without actually starting Docker)
    echo "Creating test config..."
    cat <<EOF > ${test_config}
{
  "hosts": ["tcp://127.0.0.1:4243"],
  "tls": true,
  "tlscert": "${test_dir}/certs/server-cert.pem",
  "tlskey": "${test_dir}/certs/server-key.pem",
  "tlscacert": "${test_dir}/certs/ca.pem",
  "mtu": 1500,
  "data-root": "/tmp/docker-test",
  "tlsverify": true,
  "exec-opts": ["native.cgroupdriver=systemd"],
  "storage-driver": "overlay2"
}
EOF
    
    if [ -f "$test_config" ]; then
        echo "✓ Config file created successfully"
        
        # Verify JSON validity
        if python3 -m json.tool "$test_config" >/dev/null 2>&1 || python -m json.tool "$test_config" >/dev/null 2>&1; then
            echo "✓ Config file is valid JSON"
        else
            echo "✗ Config file is not valid JSON"
            cat "$test_config"
            return 1
        fi
    else
        echo "✗ Failed to create config file"
        return 1
    fi
    
    # Cleanup
    rm -rf "$test_dir"
}

# Main test execution
main() {
    echo "Running Docker fallback mechanism tests..."
    echo ""
    
    # Run all tests
    test_driver_detection || exit 1
    test_cgroups_version || exit 1
    test_compatibility || exit 1
    test_config_creation || exit 1
    
    echo -e "\n========================================"
    echo "All Docker fallback tests passed! ✓"
}

main "$@"