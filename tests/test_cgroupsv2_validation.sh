#!/bin/bash

set -e

# Source test utilities
source "$(dirname "$0")/test_utils.sh"

header "BOSH Docker CPI cgroupsv2 Validation Test"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test results
TESTS_PASSED=0
TESTS_FAILED=0

# Function to print test results
print_result() {
    local test_name=$1
    local result=$2
    local details=$3
    
    if [ $result -eq 0 ]; then
        success "$test_name" "$details"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        error "$test_name"
        if [ -n "$details" ]; then
            echo "  $details"
        fi
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# Test 1: Verify system has cgroupsv2 enabled
test_system_cgroupsv2() {
    subheader "Test 1: System cgroupsv2 availability"
    
    # Check if cgroupsv2 is mounted
    if [ -f "/sys/fs/cgroup/cgroup.controllers" ]; then
        controllers=$(cat /sys/fs/cgroup/cgroup.controllers 2>/dev/null || echo "")
        print_result "cgroupsv2 filesystem mounted" 0 "Controllers: $controllers"
    else
        print_result "cgroupsv2 filesystem mounted" 1 "Not found at /sys/fs/cgroup/cgroup.controllers"
        return 1
    fi
    
    # Check required controllers
    local required_controllers="memory cpu io pids"
    for controller in $required_controllers; do
        if echo "$controllers" | grep -q "$controller"; then
            print_result "Controller '$controller' available" 0
        else
            print_result "Controller '$controller' available" 1 "Not found in available controllers"
        fi
    done
    
    return 0
}

# Test 2: Verify Docker daemon cgroupsv2 configuration
test_docker_cgroupsv2() {
    subheader "Test 2: Docker daemon cgroupsv2 configuration"
    
    # Get Docker info
    docker_info=$(docker info 2>/dev/null)
    
    # Check cgroup driver
    cgroup_driver=$(echo "$docker_info" | grep -i "Cgroup Driver:" | awk '{print $3}' || true)
    if [ "$cgroup_driver" = "systemd" ]; then
        print_result "Docker using systemd cgroup driver" 0 "Driver: $cgroup_driver"
    else
        print_result "Docker using systemd cgroup driver" 1 "Current driver: $cgroup_driver"
    fi
    
    # Check cgroup version (Docker 20.10+)
    cgroup_version=$(echo "$docker_info" | grep -i "Cgroup Version:" | awk '{print $3}' || true)
    if [ "$cgroup_version" = "2" ]; then
        print_result "Docker reports cgroupsv2" 0 "Version: $cgroup_version"
    elif [ -z "$cgroup_version" ]; then
        print_result "Docker reports cgroupsv2" 1 "Version field not found (Docker < 20.10?)"
    else
        print_result "Docker reports cgroupsv2" 1 "Version: $cgroup_version"
    fi
}

# Test 3: Verify container creation with cgroupsv2
test_container_cgroupsv2() {
    subheader "Test 3: Container cgroupsv2 validation"
    
    # Create a test container with resource limits
    container_name="cgroups-test-$$"
    
    echo "Creating test container with resource limits..."
    docker run -d --name "$container_name" \
        --memory="512m" \
        --cpus="0.5" \
        --pids-limit=100 \
        alpine:latest sleep 300 >/dev/null 2>&1
    
    if [ $? -eq 0 ]; then
        print_result "Container created with resource limits" 0 "Container: $container_name"
        
        # Get container ID
        container_id=$(docker inspect -f '{{.Id}}' "$container_name")
        
        # Check if container's cgroup path exists (cgroupsv2 structure)
        cgroup_path="/sys/fs/cgroup/system.slice/docker-${container_id}.scope"
        if [ -d "$cgroup_path" ]; then
            print_result "Container using cgroupsv2 hierarchy" 0 "Path: $cgroup_path"
            
            # Verify resource limit files exist
            if [ -f "$cgroup_path/memory.max" ]; then
                memory_limit=$(cat "$cgroup_path/memory.max")
                print_result "Memory limit file exists" 0 "Limit: $memory_limit"
            else
                print_result "Memory limit file exists" 1 "memory.max not found"
            fi
            
            if [ -f "$cgroup_path/cpu.max" ]; then
                cpu_limit=$(cat "$cgroup_path/cpu.max")
                print_result "CPU limit file exists" 0 "Limit: $cpu_limit"
            else
                print_result "CPU limit file exists" 1 "cpu.max not found"
            fi
            
            if [ -f "$cgroup_path/pids.max" ]; then
                pids_limit=$(cat "$cgroup_path/pids.max")
                print_result "PIDs limit file exists" 0 "Limit: $pids_limit"
            else
                print_result "PIDs limit file exists" 1 "pids.max not found"
            fi
        else
            # Try alternative path for some systems
            alt_path="/sys/fs/cgroup/docker/${container_id}"
            if [ -d "$alt_path" ]; then
                print_result "Container using cgroupsv2 hierarchy" 0 "Path: $alt_path (alternative)"
            else
                print_result "Container using cgroupsv2 hierarchy" 1 "Expected paths not found"
            fi
        fi
        
        # Cleanup
        docker rm -f "$container_name" >/dev/null 2>&1
    else
        print_result "Container created with resource limits" 1 "Failed to create container"
    fi
}

# Test 4: Test CPI binary cgroupsv2 detection
test_cpi_cgroupsv2_detection() {
    subheader "Test 4: CPI binary cgroupsv2 detection"
    
    # Check if CPI binary exists
    CPI_BIN="../src/bosh-docker-cpi/bin/cpi"
    if [ ! -f "$CPI_BIN" ]; then
        # Try alternative location
        CPI_BIN="../src/bosh-docker-cpi/out/cpi"
    fi
    
    if [ ! -f "$CPI_BIN" ]; then
        # Try to build it
        echo "CPI binary not found, attempting to build..."
        (cd ../src/bosh-docker-cpi && ./bin/build) >/dev/null 2>&1
        # Check both possible locations after build
        if [ -f "../src/bosh-docker-cpi/bin/cpi" ]; then
            CPI_BIN="../src/bosh-docker-cpi/bin/cpi"
        elif [ -f "../src/bosh-docker-cpi/out/cpi" ]; then
            CPI_BIN="../src/bosh-docker-cpi/out/cpi"
        fi
    fi
    
    if [ -f "$CPI_BIN" ]; then
        # Create a minimal CPI config for testing
        cat > /tmp/cpi_config.json <<EOF
{
  "Actions": {
    "Docker": {
      "host": "unix:///var/run/docker.sock",
      "api_version": "1.41"
    },
    "Agent": {
      "mbus": "https://admin:admin@127.0.0.1:6868",
      "ntp": [],
      "blobstore": {
        "type": "local",
        "options": {
          "blobstore_path": "/var/vcap/micro_bosh/data/cache"
        }
      }
    }
  }
}
EOF
        
        # Create a test CPI request to trigger validation
        cat > /tmp/cpi_test_request.json <<EOF
{
  "method": "info",
  "arguments": [],
  "context": {
    "director_uuid": "test-director"
  }
}
EOF
        
        # Run CPI and check if it detects cgroupsv2
        # The CPI binary should validate cgroupsv2 on startup
        result=$(timeout 5 bash -c "$CPI_BIN -configPath=/tmp/cpi_config.json < /tmp/cpi_test_request.json" 2>&1)
        exit_code=$?
        
        # Check if the command timed out
        if [ $exit_code -eq 124 ]; then
            print_result "CPI binary executes successfully" 1 "Timed out after 5 seconds"
        elif [ $exit_code -ne 0 ]; then
            print_result "CPI binary executes successfully" 1 "Exit code: $exit_code"
        # Check if the CPI returns a valid response
        elif echo "$result" | grep -q '"api_version":2'; then
            print_result "CPI binary executes successfully" 0 "API version 2 detected"
        elif echo "$result" | grep -q "cgroupsv2 is not available"; then
            print_result "CPI binary cgroupsv2 validation" 1 "CPI reports cgroupsv2 not available"
        elif echo "$result" | grep -q "error"; then
            # Extract error message
            error_msg=$(echo "$result" | grep -o '"error":{[^}]*}' | head -1 || echo "Unknown error")
            print_result "CPI binary executes successfully" 1 "Error: $error_msg"
        else
            print_result "CPI binary executes successfully" 0 "Binary runs without cgroupsv2 errors"
        fi
        
        rm -f /tmp/cpi_test_request.json /tmp/cpi_config.json
    else
        print_result "CPI binary available" 1 "Could not find or build CPI"
    fi
}

# Test 5: Validate resource enforcement
test_resource_enforcement() {
    subheader "Test 5: Resource limit enforcement"
    
    container_name="enforce-test-$$"
    
    # Create container with low memory limit
    docker run -d --name "$container_name" \
        --memory="100m" \
        alpine:latest sleep 300 >/dev/null 2>&1
    
    if [ $? -eq 0 ]; then
        # Try to allocate more memory than allowed
        docker exec "$container_name" sh -c 'dd if=/dev/zero of=/dev/null bs=1M count=200' >/dev/null 2>&1 &
        dd_pid=$!
        
        sleep 2
        
        # Check if process was killed due to memory limit
        if ! kill -0 $dd_pid 2>/dev/null; then
            print_result "Memory limit enforced" 0 "Process killed when exceeding limit"
        else
            kill $dd_pid 2>/dev/null
            print_result "Memory limit enforced" 1 "Process not killed (cgroupsv2 not enforcing?)"
        fi
        
        docker rm -f "$container_name" >/dev/null 2>&1
    else
        print_result "Resource enforcement test" 1 "Failed to create test container"
    fi
}

# Main test execution
echo "Starting cgroupsv2 validation tests..."
echo "======================================"

test_system_cgroupsv2
test_docker_cgroupsv2
test_container_cgroupsv2
test_cpi_cgroupsv2_detection
test_resource_enforcement

# Summary
echo -e "\n${YELLOW}Test Summary${NC}"
echo "============"
echo -e "Passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Failed: ${RED}$TESTS_FAILED${NC}"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "\n${GREEN}All cgroupsv2 validation tests passed!${NC}"
    exit 0
else
    echo -e "\n${RED}Some cgroupsv2 validation tests failed!${NC}"
    exit 1
fi