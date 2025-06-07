#!/usr/bin/env bash

set -e


cd ${PWD}/bosh-docker-cpi-release/

cpi_path=$(realpath ../bosh-cpi-dev-artifacts/release.tgz)

function generate_certs() {
  local certs_dir
  certs_dir="${1}"

  pushd "${certs_dir}" > /dev/null
    cat <<EOF > ./bosh-vars.yml
---
variables:
- name: docker_ca
  type: certificate
  options:
    is_ca: true
    common_name: ca
- name: docker_tls
  type: certificate
  options:
    extended_key_usage: [server_auth]
    common_name: $OUTER_CONTAINER_IP
    alternative_names: [$OUTER_CONTAINER_IP]
    ca: docker_ca
- name: client_docker_tls
  type: certificate
  options:
    extended_key_usage: [client_auth]
    common_name: $OUTER_CONTAINER_IP
    alternative_names: [$OUTER_CONTAINER_IP]
    ca: docker_ca
EOF

   bosh int ./bosh-vars.yml --vars-store=./certs.yml
   bosh int ./certs.yml --path=/docker_ca/ca > ./ca.pem
   bosh int ./certs.yml --path=/docker_tls/certificate > ./server-cert.pem
   bosh int ./certs.yml --path=/docker_tls/private_key > ./server-key.pem
   bosh int ./certs.yml --path=/client_docker_tls/certificate > ./cert.pem
   bosh int ./certs.yml --path=/client_docker_tls/private_key > ./key.pem
    # generate certs in json format
    #
   ruby -e 'puts File.read("./ca.pem").split("\n").join("\\n")' > $certs_dir/ca_json_safe.pem
   ruby -e 'puts File.read("./cert.pem").split("\n").join("\\n")' > $certs_dir/client_certificate_json_safe.pem
   ruby -e 'puts File.read("./key.pem").split("\n").join("\\n")' > $certs_dir/client_private_key_json_safe.pem
  popd > /dev/null
}

function check_systemd_compatibility() {
  # Check if systemd is available and get version
  if command -v systemctl >/dev/null 2>&1; then
    local systemd_version=$(systemctl --version | head -1 | awk '{print $2}')
    echo "SystemD version: $systemd_version"
    
    # Check if version is compatible (244+ recommended for full cgroupsv2)
    if [ -n "$systemd_version" ] && [ "$systemd_version" -ge 244 ]; then
      echo "✓ SystemD version $systemd_version is fully compatible with cgroupsv2"
    elif [ -n "$systemd_version" ] && [ "$systemd_version" -ge 232 ]; then
      echo "⚠ SystemD version $systemd_version has basic cgroupsv2 support (244+ recommended)"
    else
      echo "⚠ SystemD version $systemd_version may have limited cgroupsv2 support"
    fi
  else
    echo "⚠ SystemD not detected - containers will use alternative init system"
  fi
}

function setup_cgroups_v2() {
  local retry_count=0
  local max_retries=3
  
  # Check systemd compatibility first
  check_systemd_compatibility
  
  # Check if cgroupsv2 is available
  if [ ! -f /sys/fs/cgroup/cgroup.controllers ]; then
    echo "ERROR: cgroupsv2 not available. Please enable cgroupsv2 on the host system."
    echo "Required: Linux kernel 4.15+ with cgroupsv2 enabled"
    echo ""
    echo "Troubleshooting steps:"
    echo "1. Check kernel version: uname -r (should be 4.15+)"
    echo "2. Check kernel cmdline: cat /proc/cmdline"
    echo "3. Add 'systemd.unified_cgroup_hierarchy=1' to kernel parameters"
    echo "4. Verify with: stat -fc %T /sys/fs/cgroup (should show 'cgroup2fs')"
    exit 1
  fi
  
  # Ensure cgroup mount point exists (usually handled by systemd)
  if ! mountpoint -q /sys/fs/cgroup; then
    echo "cgroupsv2 filesystem not mounted, attempting to mount..."
    
    # Create mount point if it doesn't exist
    mkdir -p /sys/fs/cgroup
    
    # Try to mount with retries
    while [ $retry_count -lt $max_retries ]; do
      if mount -t cgroup2 none /sys/fs/cgroup 2>/tmp/cgroup_mount_error.log; then
        echo "Successfully mounted cgroupsv2 filesystem"
        break
      else
        retry_count=$((retry_count + 1))
        echo "Failed to mount cgroupsv2 (attempt $retry_count/$max_retries)"
        cat /tmp/cgroup_mount_error.log
        
        if [ $retry_count -eq $max_retries ]; then
          echo ""
          echo "ERROR: Failed to mount cgroupsv2 after $max_retries attempts"
          echo "Possible causes:"
          echo "1. Kernel doesn't support cgroupsv2"
          echo "2. cgroupsv2 already mounted elsewhere"
          echo "3. Permission issues"
          echo ""
          echo "Debug information:"
          mount | grep cgroup || echo "No cgroup mounts found"
          ls -la /sys/fs/cgroup/ 2>/dev/null || echo "Cannot list /sys/fs/cgroup"
          exit 1
        fi
        
        sleep 2
      fi
    done
  fi
  
  # Verify cgroupsv2 is properly mounted and accessible
  if [ ! -f /sys/fs/cgroup/cgroup.controllers ]; then
    echo "ERROR: cgroupsv2 mounted but cgroup.controllers file not found"
    echo "This may indicate a hybrid cgroup setup or mounting issue"
    exit 1
  fi
  
  # Check available controllers
  local controllers=$(cat /sys/fs/cgroup/cgroup.controllers 2>/dev/null || echo "none")
  echo "cgroupsv2 detected and configured"
  echo "Available controllers: $controllers"
  
  # Verify essential controllers are available
  if [[ ! "$controllers" =~ "memory" ]] || [[ ! "$controllers" =~ "cpu" ]]; then
    echo "WARNING: Essential controllers (cpu, memory) may not be available"
    echo "Container resource limits may not work as expected"
  fi
  
  # Check if we're in a container (nested scenario)
  if [ -f /.dockerenv ] || grep -q docker /proc/1/cgroup 2>/dev/null; then
    echo "NOTE: Running inside a container, cgroup delegation may be limited"
  fi
}

function verify_docker_cgroups_v2() {
  echo "Verifying Docker cgroupsv2 configuration..."
  
  # Source helper functions if not already sourced
  if ! type get_system_cgroups_version >/dev/null 2>&1; then
    source $(dirname $0)/docker_config_helper.sh 2>/dev/null || true
  fi
  
  # Check if cgroupsv2 is still properly mounted
  if ! mountpoint -q /sys/fs/cgroup || [ ! -f /sys/fs/cgroup/cgroup.controllers ]; then
    echo "ERROR: cgroupsv2 mount was lost after Docker startup"
    echo "This can happen if Docker reconfigured the cgroup mounts"
    mount | grep cgroup
    exit 1
  fi
  
  # Get Docker's cgroup configuration
  local docker_info=$(docker info 2>/dev/null)
  
  # Check cgroup driver
  local cgroup_driver=$(echo "$docker_info" | grep -i "cgroup driver" | awk '{print $NF}')
  if [ -z "$cgroup_driver" ]; then
    echo "WARNING: Unable to determine Docker's cgroup driver"
  else
    echo "Docker is using $cgroup_driver cgroup driver"
    
    # Check compatibility
    local system_version=$(get_system_cgroups_version 2>/dev/null || echo "v2")
    if type verify_cgroup_compatibility >/dev/null 2>&1; then
      verify_cgroup_compatibility "$cgroup_driver" "$system_version"
    fi
  fi
  
  # Check cgroup version
  local cgroup_version=$(echo "$docker_info" | grep -i "cgroup version" | awk '{print $NF}')
  if [ -n "$cgroup_version" ]; then
    if [ "$cgroup_version" = "2" ]; then
      echo "✓ Docker detected cgroupsv2"
    elif [ "$cgroup_version" = "1" ]; then
      echo "ERROR: Docker detected cgroupsv1 but system has cgroupsv2"
      echo "This indicates a configuration mismatch"
      exit 1
    fi
  fi
  
  # Test creating a container with resource limits
  echo "Testing container creation with resource limits..."
  local test_container=$(docker run -d --rm \
    --memory="100m" \
    --cpus="0.5" \
    --name="cgroup-test-$$" \
    busybox sleep 30 2>&1)
  
  if [ $? -eq 0 ]; then
    echo "✓ Successfully created container with resource limits"
    
    # Verify the limits were applied
    local container_info=$(docker inspect "cgroup-test-$$" 2>/dev/null)
    if [ $? -eq 0 ]; then
      local memory_limit=$(echo "$container_info" | grep -i "memory" | grep "104857600" | wc -l)
      if [ $memory_limit -gt 0 ]; then
        echo "✓ Memory limits properly applied"
      else
        echo "WARNING: Memory limits may not be properly applied"
      fi
    fi
    
    # Clean up test container
    docker stop "cgroup-test-$$" >/dev/null 2>&1 || true
  else
    echo "WARNING: Failed to create container with resource limits"
    echo "Error: $test_container"
  fi
  
  echo "cgroupsv2 verification complete"
}

function stop_docker() {
  service docker stop
}

function start_docker() {
  # docker will fail starting with the new iptables. it throws:
  # iptables v1.8.7 (nf_tables): Could not fetch rule set generation id: ....
  update-alternatives --set iptables /usr/sbin/iptables-legacy
  generate_certs $1
  mkdir -p /var/log
  mkdir -p /var/run

  setup_cgroups_v2


  # check for /proc/sys being mounted readonly, as systemd does
  if grep '/proc/sys\s\+\w\+\s\+ro,' /proc/mounts >/dev/null; then
    mount -o remount,rw /proc/sys
  fi

  local mtu=$(cat /sys/class/net/$(ip route get 8.8.8.8|awk '{ print $5 }')/mtu)

  # Source helper functions
  source $(dirname $0)/docker_config_helper.sh
  
  [[ ! -d /etc/docker ]] && mkdir /etc/docker
  
  # Create Docker config with automatic cgroup driver detection and fallback
  create_docker_config_with_fallback "/etc/docker/daemon.json" "${certs_dir}" "${DOCKER_HOST}" "${mtu}"

  trap stop_docker EXIT

  service docker start

  export DOCKER_TLS_VERIFY=1
  export DOCKER_CERT_PATH=$1

  rc=1
  for i in $(seq 1 100); do
    echo waiting for docker to come up...
    sleep 1
    set +e
    docker info
    rc=$?
    set -e
    if [ "$rc" -eq "0" ]; then
        break
    fi
  done

  if [ "$rc" -ne "0" ]; then
    exit 1
  fi

  # Verify cgroupsv2 and Docker configuration
  verify_docker_cgroups_v2

  echo $certs_dir
}

function main() {
  export OUTER_CONTAINER_IP=$(ruby -rsocket -e 'puts Socket.ip_address_list
                          .reject { |addr| !addr.ip? || addr.ipv4_loopback? || addr.ipv6? }
                          .map { |addr| addr.ip_address }')

  export DOCKER_HOST="tcp://${OUTER_CONTAINER_IP}:4243"

  local certs_dir
  certs_dir=$(mktemp -d)
  start_docker "${certs_dir}"

  local local_bosh_dir
  local_bosh_dir="/tmp/local-bosh/director"

  docker network create -d bridge --subnet=10.245.0.0/16 director_network

  export BOSH_DIRECTOR_IP="10.245.0.11"
  export BOSH_ENVIRONMENT="docker-director"

  mkdir -p ${local_bosh_dir}

  command bosh int ../bosh-deployment/bosh.yml \
    -o ../bosh-deployment/docker/cpi.yml \
    -o ../bosh-deployment/jumpbox-user.yml \
    -o manifests/dev.yml \
    -o manifests/ops-blobstore-bind-all.yml \
    -o manifests/ops-docker-socket-stable-path.yml \
    -o manifests/ops-docker-socket-permissions.yml \
    -v director_name=docker \
    -v docker_cpi_path=$cpi_path \
    -v internal_cidr=10.245.0.0/16 \
    -v internal_gw=10.245.0.1 \
    -v internal_ip="${BOSH_DIRECTOR_IP}" \
    -v docker_host="${DOCKER_HOST}" \
    -v network=director_network \
    -v docker_tls="{\"ca\": \"$(cat ${certs_dir}/ca_json_safe.pem)\",\"certificate\": \"$(cat ${certs_dir}/client_certificate_json_safe.pem)\",\"private_key\": \"$(cat ${certs_dir}/client_private_key_json_safe.pem)\"}" \
    ${@} > "${local_bosh_dir}/bosh-director.yml"

  command bosh create-env "${local_bosh_dir}/bosh-director.yml" \
          --vars-store="${local_bosh_dir}/creds.yml" \
          --state="${local_bosh_dir}/state.json"

  bosh int "${local_bosh_dir}/creds.yml" --path /director_ssl/ca > "${local_bosh_dir}/ca.crt"
  bosh -e "${BOSH_DIRECTOR_IP}" --ca-cert "${local_bosh_dir}/ca.crt" alias-env "${BOSH_ENVIRONMENT}"

  cat <<EOF > "${local_bosh_dir}/env"
  export BOSH_ENVIRONMENT="${BOSH_ENVIRONMENT}"
  export BOSH_CLIENT=admin
  export BOSH_CLIENT_SECRET=`bosh int "${local_bosh_dir}/creds.yml" --path /admin_password`
  export BOSH_CA_CERT="${local_bosh_dir}/ca.crt"

EOF

  source "${local_bosh_dir}/env"
  bosh -n update-cloud-config ../bosh-deployment/docker/cloud-config.yml -v network=director_network

}

# Only run main if not being sourced for testing
if [ -z "$SOURCING_FOR_TEST" ]; then
  main $@
fi
