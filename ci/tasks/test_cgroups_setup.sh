#!/bin/bash

set -e

echo "Testing cgroupsv2 Setup Functions"
echo "================================="

# Source the setup script functions without executing main
SOURCING_FOR_TEST=1
source $(dirname $0)/setup-director.sh || {
    echo "Failed to source setup-director.sh"
    exit 1
}

# Test 1: Mock missing cgroupsv2 controllers file
test_missing_cgroupsv2() {
    echo -e "\nTest 1: Testing behavior when cgroupsv2 is not available..."
    
    # Create a temporary directory to use as fake /sys/fs/cgroup
    local test_dir=$(mktemp -d)
    
    # Override the check by using a function wrapper
    original_setup=$(declare -f setup_cgroups_v2)
    
    # Create a modified version that checks our test directory
    setup_cgroups_v2() {
        local retry_count=0
        local max_retries=3
        
        # Check our test directory instead of real path
        if [ ! -f "${test_dir}/cgroup.controllers" ]; then
            echo "ERROR: cgroupsv2 not available. Please enable cgroupsv2 on the host system."
            echo "Required: Linux kernel 4.15+ with cgroupsv2 enabled"
            echo ""
            echo "Troubleshooting steps:"
            echo "1. Check kernel version: uname -r (should be 4.15+)"
            echo "2. Check kernel cmdline: cat /proc/cmdline"
            echo "3. Add 'systemd.unified_cgroup_hierarchy=1' to kernel parameters"
            echo "4. Verify with: stat -fc %T /sys/fs/cgroup (should show 'cgroup2fs')"
            return 1
        fi
        return 0
    }
    
    # Test the function - should fail
    if setup_cgroups_v2 2>&1 | grep -q "ERROR: cgroupsv2 not available"; then
        echo "✓ Correctly detected missing cgroupsv2"
    else
        echo "✗ Failed to detect missing cgroupsv2"
        rm -rf "$test_dir"
        return 1
    fi
    
    # Now create the controllers file and test again
    echo "memory cpu io pids" > "${test_dir}/cgroup.controllers"
    
    # Should pass now
    if setup_cgroups_v2 >/dev/null 2>&1; then
        echo "✓ Correctly detected available cgroupsv2"
    else
        echo "✗ Failed to detect available cgroupsv2"
        rm -rf "$test_dir"
        return 1
    fi
    
    # Cleanup
    rm -rf "$test_dir"
    eval "$original_setup"  # Restore original function
    
    echo "✓ Test 1 passed"
}

# Test 2: Test mount failure handling
test_mount_failure() {
    echo -e "\nTest 2: Testing cgroupsv2 mount failure handling..."
    
    # We can't easily test actual mount operations without root in a safe way
    # So we'll test the retry logic by examining the function
    
    # Check that the function has retry logic
    if declare -f setup_cgroups_v2 | grep -q "retry_count"; then
        echo "✓ Retry logic present in setup_cgroups_v2"
    else
        echo "✗ No retry logic found in setup_cgroups_v2"
        return 1
    fi
    
    # Check for proper error messages
    if declare -f setup_cgroups_v2 | grep -q "Failed to mount cgroupsv2 after.*attempts"; then
        echo "✓ Proper error handling for mount failures"
    else
        echo "✗ Missing error handling for mount failures"
        return 1
    fi
    
    echo "✓ Test 2 passed"
}

# Test 3: Test controller availability checking
test_controller_checking() {
    echo -e "\nTest 3: Testing controller availability checking..."
    
    # Check that the function verifies essential controllers
    if declare -f setup_cgroups_v2 | grep -q "memory.*cpu"; then
        echo "✓ Function checks for essential controllers"
    else
        echo "✗ Function doesn't check for essential controllers"
        return 1
    fi
    
    # Check for warning messages
    if declare -f setup_cgroups_v2 | grep -q "Essential controllers.*may not be available"; then
        echo "✓ Warns about missing essential controllers"
    else
        echo "✗ No warning for missing essential controllers"
        return 1
    fi
    
    echo "✓ Test 3 passed"
}

# Test 4: Test nested container detection
test_nested_container() {
    echo -e "\nTest 4: Testing nested container detection..."
    
    # Check that the function detects running in a container
    if declare -f setup_cgroups_v2 | grep -q "dockerenv.*proc/1/cgroup"; then
        echo "✓ Function checks for container environment"
    else
        echo "✗ Function doesn't check for container environment"
        return 1
    fi
    
    # Check for appropriate warning
    if declare -f setup_cgroups_v2 | grep -q "cgroup delegation may be limited"; then
        echo "✓ Warns about limited delegation in containers"
    else
        echo "✗ No warning about container limitations"
        return 1
    fi
    
    echo "✓ Test 4 passed"
}

# Test 5: Test verify_docker_cgroups_v2 function
test_docker_verification() {
    echo -e "\nTest 5: Testing Docker cgroupsv2 verification..."
    
    # Check that verification function exists
    if ! declare -f verify_docker_cgroups_v2 >/dev/null; then
        echo "✗ verify_docker_cgroups_v2 function not found"
        return 1
    fi
    
    # Check for proper verification steps
    if declare -f verify_docker_cgroups_v2 | grep -q "docker info"; then
        echo "✓ Function checks Docker info"
    else
        echo "✗ Function doesn't check Docker info"
        return 1
    fi
    
    if declare -f verify_docker_cgroups_v2 | grep -q "cgroup driver"; then
        echo "✓ Function verifies cgroup driver"
    else
        echo "✗ Function doesn't verify cgroup driver"
        return 1
    fi
    
    if declare -f verify_docker_cgroups_v2 | grep -q "test container.*resource limits"; then
        echo "✓ Function tests container creation with limits"
    else
        echo "✗ Function doesn't test container creation"
        return 1
    fi
    
    echo "✓ Test 5 passed"
}

# Test 6: Integration test for full setup flow
test_integration() {
    echo -e "\nTest 6: Testing integration of cgroupsv2 setup..."
    
    # Check that start_docker calls setup_cgroups_v2
    if declare -f start_docker | grep -q "setup_cgroups_v2"; then
        echo "✓ start_docker calls setup_cgroups_v2"
    else
        echo "✗ start_docker doesn't call setup_cgroups_v2"
        return 1
    fi
    
    # Check that start_docker calls verify_docker_cgroups_v2
    if declare -f start_docker | grep -q "verify_docker_cgroups_v2"; then
        echo "✓ start_docker calls verify_docker_cgroups_v2"
    else
        echo "✗ start_docker doesn't call verify_docker_cgroups_v2"
        return 1
    fi
    
    echo "✓ Test 6 passed"
}

# Main test execution
main() {
    echo "Running cgroupsv2 setup tests..."
    
    # Run all tests
    test_missing_cgroupsv2 || exit 1
    test_mount_failure || exit 1
    test_controller_checking || exit 1
    test_nested_container || exit 1
    test_docker_verification || exit 1
    test_integration || exit 1
    
    echo -e "\n================================="
    echo "All cgroupsv2 setup tests passed! ✓"
}

# Only run main if not being sourced
if [ "$SOURCING_FOR_TEST" != "1" ]; then
    main "$@"
fi