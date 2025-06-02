#!/bin/bash

set -e

echo "Testing BOSH Docker CPI SystemD Container Mode"
echo "============================================="

# Function to check if systemd is PID 1 in container
check_systemd_pid1() {
    local container_id=$1
    local pid1=$(docker exec $container_id ps aux | grep -E "^\s*1\s+" | awk '{print $11}')
    
    if [[ "$pid1" == *"systemd"* ]] || [[ "$pid1" == *"/sbin/init"* ]]; then
        echo "✓ SystemD is PID 1 in container"
        return 0
    else
        echo "✗ SystemD is NOT PID 1 in container (found: $pid1)"
        return 1
    fi
}

# Function to verify systemd services are running
check_systemd_services() {
    local container_id=$1
    
    echo "Checking systemd services..."
    docker exec $container_id systemctl status --no-pager || true
    
    # Check if basic systemd targets are active
    if docker exec $container_id systemctl is-active multi-user.target >/dev/null 2>&1; then
        echo "✓ multi-user.target is active"
    else
        echo "✗ multi-user.target is not active"
        return 1
    fi
}

# Function to test resource limits with systemd
test_resource_limits() {
    local container_id=$1
    
    echo "Testing resource limits..."
    
    # Check if cgroup controllers are available
    docker exec $container_id cat /sys/fs/cgroup/cgroup.controllers || {
        echo "✗ Cannot read cgroup controllers"
        return 1
    }
    
    # Check memory limit
    local memory_max=$(docker exec $container_id cat /sys/fs/cgroup/memory.max 2>/dev/null || echo "not found")
    echo "Memory limit: $memory_max"
    
    # Check CPU limit
    local cpu_max=$(docker exec $container_id cat /sys/fs/cgroup/cpu.max 2>/dev/null || echo "not found")
    echo "CPU limit: $cpu_max"
}

# Function to test systemd shutdown
test_systemd_shutdown() {
    local container_id=$1
    
    echo "Testing systemd shutdown..."
    
    # Send SIGTERM to systemd
    docker kill -s TERM $container_id
    
    # Wait for graceful shutdown (max 30 seconds)
    local count=0
    while [ $count -lt 30 ]; do
        if ! docker ps | grep -q $container_id; then
            echo "✓ Container shut down gracefully"
            return 0
        fi
        sleep 1
        ((count++))
    done
    
    echo "✗ Container did not shut down gracefully"
    return 1
}

# Main test flow
main() {
    # Check prerequisites
    if [ ! -f /sys/fs/cgroup/cgroup.controllers ]; then
        echo "ERROR: cgroupsv2 not available on host system"
        exit 1
    fi
    
    # Verify Docker is using systemd cgroup driver
    if ! docker info | grep -q "Cgroup Driver: systemd"; then
        echo "WARNING: Docker is not using systemd cgroup driver"
    fi
    
    # Create a test container with systemd
    echo "Creating test container with systemd..."
    
    # This would use the actual BOSH Docker CPI to create a container
    # For now, we'll create a test container manually
    CONTAINER_ID=$(docker run -d \
        --privileged \
        --cgroupns=host \
        -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
        --cap-add SYS_ADMIN \
        --cap-add SYS_INIT \
        --memory="1g" \
        --cpus="0.5" \
        centos:8 /sbin/init)
    
    echo "Container ID: $CONTAINER_ID"
    
    # Wait for systemd to initialize
    echo "Waiting for systemd to initialize..."
    sleep 10
    
    # Run tests
    echo ""
    echo "Running SystemD tests..."
    echo "----------------------"
    
    check_systemd_pid1 $CONTAINER_ID || exit 1
    check_systemd_services $CONTAINER_ID || exit 1
    test_resource_limits $CONTAINER_ID || exit 1
    
    # Test shutdown (this will stop the container)
    test_systemd_shutdown $CONTAINER_ID || exit 1
    
    echo ""
    echo "All SystemD tests passed! ✓"
}

# Run main function
main "$@"