#!/bin/bash

# Helper functions for Docker daemon configuration with cgroupsv2

function detect_optimal_cgroup_driver() {
  # Detect the best cgroup driver based on system configuration
  
  # Check if systemd is PID 1
  if [ "$(ps -p 1 -o comm=)" = "systemd" ]; then
    # Check if cgroupsv2 is available
    if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
      echo "systemd"
      return 0
    fi
  fi
  
  # Fallback to cgroupfs
  echo "cgroupfs"
  return 0
}

function create_docker_config_with_fallback() {
  local config_path=$1
  local certs_dir=$2
  local docker_host=$3
  local mtu=$4
  local preferred_driver="systemd"
  
  # First, try to create config with systemd driver
  cat <<EOF > ${config_path}
{
  "hosts": ["${docker_host}"],
  "tls": true,
  "tlscert": "${certs_dir}/server-cert.pem",
  "tlskey": "${certs_dir}/server-key.pem",
  "tlscacert": "${certs_dir}/ca.pem",
  "mtu": ${mtu},
  "data-root": "/scratch/docker",
  "tlsverify": true,
  "exec-opts": ["native.cgroupdriver=${preferred_driver}"],
  "storage-driver": "overlay2"
}
EOF
  
  # Test if Docker can start with this config
  echo "Testing Docker daemon with ${preferred_driver} cgroup driver..."
  
  # Save current Docker state
  local docker_was_running=false
  if systemctl is-active --quiet docker || service docker status >/dev/null 2>&1; then
    docker_was_running=true
    service docker stop || systemctl stop docker
  fi
  
  # Try to start Docker with the config
  if timeout 30 dockerd --config-file=${config_path} --pidfile=/tmp/test-docker.pid >/tmp/docker-test.log 2>&1 & then
    local test_pid=$!
    sleep 5
    
    # Check if Docker started successfully
    if kill -0 $test_pid 2>/dev/null; then
      # Docker started, kill it
      kill $test_pid 2>/dev/null || true
      wait $test_pid 2>/dev/null || true
      echo "✓ Docker daemon works with ${preferred_driver} driver"
      return 0
    fi
  fi
  
  # If systemd driver failed, try cgroupfs
  echo "WARNING: ${preferred_driver} driver failed, trying cgroupfs fallback..."
  
  preferred_driver="cgroupfs"
  cat <<EOF > ${config_path}
{
  "hosts": ["${docker_host}"],
  "tls": true,
  "tlscert": "${certs_dir}/server-cert.pem",
  "tlskey": "${certs_dir}/server-key.pem",
  "tlscacert": "${certs_dir}/ca.pem",
  "mtu": ${mtu},
  "data-root": "/scratch/docker",
  "tlsverify": true,
  "exec-opts": ["native.cgroupdriver=${preferred_driver}"],
  "storage-driver": "overlay2"
}
EOF
  
  echo "Docker daemon will use ${preferred_driver} cgroup driver"
  
  # Restore Docker state if it was running
  if [ "$docker_was_running" = true ]; then
    service docker start || systemctl start docker
  fi
  
  return 0
}

function verify_cgroup_compatibility() {
  local docker_driver=$1
  local system_cgroups=$2
  
  if [ "$system_cgroups" = "v2" ] && [ "$docker_driver" = "systemd" ]; then
    echo "✓ Optimal configuration: cgroupsv2 with systemd driver"
    return 0
  elif [ "$system_cgroups" = "v2" ] && [ "$docker_driver" = "cgroupfs" ]; then
    echo "⚠ Sub-optimal: cgroupsv2 with cgroupfs driver (consider using systemd)"
    return 0
  elif [ "$system_cgroups" = "v1" ] && [ "$docker_driver" = "cgroupfs" ]; then
    echo "✓ Compatible: cgroupsv1 with cgroupfs driver"
    return 0
  else
    echo "⚠ Potential compatibility issue: ${system_cgroups} with ${docker_driver} driver"
    return 1
  fi
}

function get_system_cgroups_version() {
  if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
    echo "v2"
  elif [ -d /sys/fs/cgroup/memory ]; then
    echo "v1"
  else
    echo "unknown"
  fi
}

# Export functions for use in other scripts
export -f detect_optimal_cgroup_driver
export -f create_docker_config_with_fallback
export -f verify_cgroup_compatibility
export -f get_system_cgroups_version