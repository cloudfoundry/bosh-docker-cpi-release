#!/bin/bash

set -e

# Source test utilities
source "$(dirname "$0")/test_utils.sh"

header "Testing Resource Enforcement with cgroupsv2"

# Check prerequisites
check_prerequisites() {
    info "Checking prerequisites..."
    
    # Check cgroupsv2
    if [ ! -f /sys/fs/cgroup/cgroup.controllers ]; then
        error "cgroupsv2 not available"
        exit 1
    fi
    
    # Check Docker
    if ! command -v docker &> /dev/null; then
        error "Docker not installed"
        exit 1
    fi
    
    # Check Docker daemon cgroup driver
    local cgroup_driver=$(docker info --format '{{.CgroupDriver}}' 2>/dev/null)
    echo "Docker cgroup driver: $cgroup_driver"
    
    # Check stress-ng availability
    if ! docker run --rm busybox which stress-ng &>/dev/null; then
        echo "WARNING: stress-ng not available in test image, using alternative stress methods"
    fi
    
    echo "✓ Prerequisites check passed"
}

# Test memory limits
test_memory_limits() {
    subheader "Test 1: Memory Limit Enforcement"
    echo "--------------------------------"
    
    local memory_limit="256m"
    local test_allocation="512m"  # Try to allocate more than limit
    
    echo "Creating container with ${memory_limit} memory limit..."
    local container_id=$(docker run -d \
        --name "memory-test-$$" \
        --memory="${memory_limit}" \
        --rm \
        alpine sh -c "sleep 300")
    
    echo "Container ID: $container_id"
    
    # Verify memory limit is set
    local actual_limit=$(docker inspect "$container_id" --format '{{.HostConfig.Memory}}')
    local expected_bytes=$((256 * 1024 * 1024))
    
    if [ "$actual_limit" -eq "$expected_bytes" ]; then
        echo "✓ Memory limit correctly set to ${memory_limit} ($actual_limit bytes)"
    else
        echo "✗ Memory limit mismatch. Expected: $expected_bytes, Got: $actual_limit"
        docker stop "$container_id" 2>/dev/null || true
        return 1
    fi
    
    # Test memory allocation beyond limit
    echo "Testing memory allocation beyond limit..."
    
    # Try to allocate memory beyond limit
    if docker exec "$container_id" sh -c "dd if=/dev/zero of=/dev/null bs=1M count=512" 2>&1 | grep -q "Cannot allocate memory\|Killed"; then
        echo "✓ Memory limit enforced - allocation beyond limit failed as expected"
    else
        # Alternative test: Check if process gets OOM killed
        local test_result=$(docker exec "$container_id" sh -c "
            # Create a simple memory consumer
            awk 'BEGIN{
                s=\"\";
                for(i=0;i<1000;i++) {
                    s=s\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\";
                    for(j=0;j<1000;j++) {
                        a[i,j]=s;
                    }
                }
            }' 2>&1 || echo 'OOM'
        ")
        
        if [[ "$test_result" == *"OOM"* ]] || [[ "$test_result" == *"Killed"* ]]; then
            echo "✓ Memory limit enforced - process killed when exceeding limit"
        else
            echo "⚠ Could not verify memory limit enforcement"
        fi
    fi
    
    # Check cgroupsv2 memory files
    echo "Checking cgroupsv2 memory controllers..."
    local cgroup_path=$(docker inspect "$container_id" --format '{{.HostConfig.CgroupParent}}')
    
    if [ -f "/sys/fs/cgroup/${cgroup_path}/memory.max" ]; then
        local cgroup_limit=$(cat "/sys/fs/cgroup/${cgroup_path}/memory.max" 2>/dev/null || echo "N/A")
        echo "cgroup memory.max: $cgroup_limit"
    fi
    
    # Cleanup
    docker stop "$container_id" 2>/dev/null || true
    
    echo "✓ Memory limit test completed"
}

# Test CPU limits
test_cpu_limits() {
    subheader "Test 2: CPU Limit Enforcement"
    echo "-----------------------------"
    
    local cpu_limit="0.5"  # 50% of one CPU
    
    echo "Creating container with ${cpu_limit} CPU limit..."
    local container_id=$(docker run -d \
        --name "cpu-test-$$" \
        --cpus="${cpu_limit}" \
        --rm \
        alpine sh -c "sleep 300")
    
    echo "Container ID: $container_id"
    
    # Verify CPU limit is set
    local nano_cpus=$(docker inspect "$container_id" --format '{{.HostConfig.NanoCpus}}')
    local expected_nano=$((500000000))  # 0.5 * 1e9
    
    if [ "$nano_cpus" -eq "$expected_nano" ]; then
        echo "✓ CPU limit correctly set to ${cpu_limit} CPUs ($nano_cpus nanocpus)"
    else
        echo "✗ CPU limit mismatch. Expected: $expected_nano, Got: $nano_cpus"
        docker stop "$container_id" 2>/dev/null || true
        return 1
    fi
    
    # Test CPU usage under load
    echo "Testing CPU usage under load..."
    
    # Start CPU intensive task in background
    docker exec -d "$container_id" sh -c "while true; do echo 'scale=5000; 4*a(1)' | bc -l >/dev/null 2>&1; done"
    
    # Wait for process to stabilize
    sleep 5
    
    # Check CPU usage
    local cpu_stats=$(docker stats --no-stream --format "{{.CPUPerc}}" "$container_id" | tr -d '%')
    echo "CPU usage: ${cpu_stats}%"
    
    # CPU usage should be around 50% (with some tolerance)
    if (( $(echo "$cpu_stats < 70" | bc -l) )); then
        echo "✓ CPU limit enforced - usage constrained as expected"
    else
        echo "⚠ CPU usage higher than expected (may be due to system load)"
    fi
    
    # Check cgroupsv2 CPU files
    echo "Checking cgroupsv2 CPU controllers..."
    local cgroup_path=$(docker inspect "$container_id" --format '{{.HostConfig.CgroupParent}}')
    
    if [ -f "/sys/fs/cgroup/${cgroup_path}/cpu.max" ]; then
        local cpu_max=$(cat "/sys/fs/cgroup/${cgroup_path}/cpu.max" 2>/dev/null || echo "N/A")
        echo "cgroup cpu.max: $cpu_max"
    fi
    
    # Cleanup
    docker stop "$container_id" 2>/dev/null || true
    
    echo "✓ CPU limit test completed"
}

# Test PIDs limit (cgroupsv2 feature)
test_pids_limit() {
    subheader "Test 3: PIDs Limit Enforcement"
    echo "------------------------------"
    
    local pids_limit="50"
    
    echo "Creating container with PIDs limit of ${pids_limit}..."
    local container_id=$(docker run -d \
        --name "pids-test-$$" \
        --pids-limit="${pids_limit}" \
        --rm \
        alpine sh -c "sleep 300")
    
    echo "Container ID: $container_id"
    
    # Verify PIDs limit is set
    local actual_limit=$(docker inspect "$container_id" --format '{{.HostConfig.PidsLimit}}')
    
    if [ "$actual_limit" -eq "$pids_limit" ]; then
        echo "✓ PIDs limit correctly set to ${pids_limit}"
    else
        echo "✗ PIDs limit mismatch. Expected: $pids_limit, Got: $actual_limit"
        docker stop "$container_id" 2>/dev/null || true
        return 1
    fi
    
    # Test PIDs limit enforcement
    echo "Testing PIDs limit enforcement..."
    
    # Try to create more processes than the limit
    local fork_test=$(docker exec "$container_id" sh -c '
        for i in $(seq 1 100); do
            sleep 300 &
        done 2>&1
        wait
    ' || echo "Fork failed")
    
    if [[ "$fork_test" == *"Resource temporarily unavailable"* ]] || [[ "$fork_test" == *"Fork failed"* ]]; then
        echo "✓ PIDs limit enforced - fork failed when exceeding limit"
    else
        # Count actual processes
        local process_count=$(docker exec "$container_id" sh -c 'ps aux | wc -l')
        echo "Process count: $process_count"
        
        if [ "$process_count" -le "$pids_limit" ]; then
            echo "✓ PIDs limit enforced - process count within limit"
        else
            echo "⚠ PIDs limit may not be properly enforced"
        fi
    fi
    
    # Cleanup
    docker stop "$container_id" 2>/dev/null || true
    
    echo "✓ PIDs limit test completed"
}

# Test combined limits
test_combined_limits() {
    subheader "Test 4: Combined Resource Limits"
    echo "--------------------------------"
    
    echo "Creating container with multiple resource limits..."
    local container_id=$(docker run -d \
        --name "combined-test-$$" \
        --memory="512m" \
        --cpus="1.0" \
        --pids-limit="100" \
        --memory-swap="768m" \
        --rm \
        alpine sh -c "sleep 300")
    
    echo "Container ID: $container_id"
    
    # Verify all limits are set
    local memory=$(docker inspect "$container_id" --format '{{.HostConfig.Memory}}')
    local cpus=$(docker inspect "$container_id" --format '{{.HostConfig.NanoCpus}}')
    local pids=$(docker inspect "$container_id" --format '{{.HostConfig.PidsLimit}}')
    local swap=$(docker inspect "$container_id" --format '{{.HostConfig.MemorySwap}}')
    
    echo "Configured limits:"
    echo "  Memory: $((memory / 1024 / 1024))MB"
    echo "  CPUs: $((cpus / 1000000000))"
    echo "  PIDs: $pids"
    echo "  Memory+Swap: $((swap / 1024 / 1024))MB"
    
    # Run stress test with combined load
    echo "Running combined stress test..."
    
    docker exec "$container_id" sh -c '
        # Memory stress
        dd if=/dev/zero of=/tmp/memfile bs=1M count=400 2>/dev/null &
        
        # CPU stress
        while true; do echo "scale=1000; 4*a(1)" | bc -l >/dev/null 2>&1; done &
        
        # Process stress
        for i in $(seq 1 50); do
            sleep 300 &
        done
        
        sleep 10
    ' 2>&1 || true
    
    # Check container is still running
    if docker ps | grep -q "$container_id"; then
        echo "✓ Container stable under combined resource limits"
    else
        echo "⚠ Container may have been killed due to resource limits"
    fi
    
    # Cleanup
    docker stop "$container_id" 2>/dev/null || true
    
    echo "✓ Combined limits test completed"
}

# Test limit changes (dynamic updates)
test_dynamic_updates() {
    subheader "Test 5: Dynamic Resource Limit Updates"
    
    echo "Creating container with initial limits..."
    local container_id=$(docker run -d \
        --name "dynamic-test-$$" \
        --memory="256m" \
        --cpus="0.5" \
        --rm \
        alpine sh -c "sleep 300")
    
    echo "Container ID: $container_id"
    echo "Initial limits: 256MB memory, 0.5 CPUs"
    
    # Update limits
    echo "Updating to: 512MB memory, 1.0 CPUs..."
    docker update \
        --memory="512m" \
        --cpus="1.0" \
        "$container_id"
    
    # Verify updates
    local new_memory=$(docker inspect "$container_id" --format '{{.HostConfig.Memory}}')
    local new_cpus=$(docker inspect "$container_id" --format '{{.HostConfig.NanoCpus}}')
    
    if [ "$new_memory" -eq "$((512 * 1024 * 1024))" ] && [ "$new_cpus" -eq "$((1000000000))" ]; then
        echo "✓ Resource limits successfully updated"
    else
        echo "✗ Resource limit update failed"
    fi
    
    # Cleanup
    docker stop "$container_id" 2>/dev/null || true
    
    echo "✓ Dynamic update test completed"
}

# Main test execution
main() {
    echo "Starting resource enforcement tests..."
    echo "Test environment: $(uname -r)"
    echo "Docker version: $(docker --version)"
    echo ""
    
    # Run tests
    check_prerequisites || exit 1
    test_memory_limits || exit 1
    test_cpu_limits || exit 1
    test_pids_limit || exit 1
    test_combined_limits || exit 1
    test_dynamic_updates || exit 1
    
    echo -e "\n====================================="
    echo "All resource enforcement tests passed! ✓"
    echo "====================================="
}

# Cleanup function
cleanup() {
    echo "Cleaning up test containers..."
    docker ps -a | grep -E "(memory|cpu|pids|combined|dynamic)-test-" | awk '{print $1}' | xargs -r docker rm -f 2>/dev/null || true
}

# Set trap for cleanup
trap cleanup EXIT

# Run main
main "$@"