#!/usr/bin/env bash
set -eu -o pipefail

REPO_ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )/../.." && pwd )"
REPO_PARENT="$( cd "${REPO_ROOT}/.." && pwd )"

if [[ -n "${DEBUG:-}" ]]; then
  set -x
  export BOSH_LOG_LEVEL=debug
  export BOSH_LOG_PATH="${BOSH_LOG_PATH:-${REPO_PARENT}/bosh-debug.log}"
fi

BOSH_DEPLOYMENT_PATH="${REPO_PARENT}/bosh-deployment"

export BOSH_DIRECTOR_IP="10.245.0.11"
export BOSH_ENVIRONMENT="docker-director"

export DNS_IP="8.8.8.8"

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
   ruby -e 'puts File.read("./ca.pem").split("\n").join("\\n")' > "${certs_dir}/ca_json_safe.pem"
   ruby -e 'puts File.read("./cert.pem").split("\n").join("\\n")' > "${certs_dir}/client_certificate_json_safe.pem"
   ruby -e 'puts File.read("./key.pem").split("\n").join("\\n")' > "${certs_dir}/client_private_key_json_safe.pem"
  popd > /dev/null
}

function sanitize_cgroups() {
  mkdir -p /sys/fs/cgroup
  mountpoint -q /sys/fs/cgroup || \
    mount -t tmpfs -o uid=0,gid=0,mode=0755 cgroup /sys/fs/cgroup

  if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
    # cgroups v2: enable nesting (based on moby/moby hack/dind)
    mkdir -p /sys/fs/cgroup/init
    # Loop to handle races from concurrent process creation (e.g. docker exec)
    while ! {
      xargs -rn1 < /sys/fs/cgroup/cgroup.procs > /sys/fs/cgroup/init/cgroup.procs 2>/dev/null || :
      sed -e 's/ / +/g' -e 's/^/+/' < /sys/fs/cgroup/cgroup.controllers \
        > /sys/fs/cgroup/cgroup.subtree_control
    }; do true; done
    return
  fi

  mount -o remount,rw /sys/fs/cgroup

  sed -e 1d /proc/cgroups | while read -r sys enabled; do
    if [ "$enabled" != "1" ]; then
      # subsystem disabled; skip
      continue
    fi

    grouping="$(cut -d: -f2 < /proc/self/cgroup | grep "\\<$sys\\>")"
    if [ -z "$grouping" ]; then
      # subsystem not mounted anywhere; mount it on its own
      grouping="$sys"
    fi

    mountpoint="/sys/fs/cgroup/$grouping"

    mkdir -p "$mountpoint"

    # clear out existing mount to make sure new one is read-write
    if mountpoint -q "$mountpoint"; then
      umount "$mountpoint"
    fi

    mount -n -t cgroup -o "$grouping" cgroup "$mountpoint"

    if [ "$grouping" != "$sys" ]; then
      if [ -L "/sys/fs/cgroup/$sys" ]; then
        rm "/sys/fs/cgroup/$sys"
      fi

      ln -s "$mountpoint" "/sys/fs/cgroup/$sys"
    fi
  done
}

function stop_docker() {
  service docker stop
}

function start_docker() {
  local certs_dir
  certs_dir="${1}"
  generate_certs "${certs_dir}"
  mkdir -p /var/log
  mkdir -p /var/run

  sanitize_cgroups

  # systemd inside nested Docker containers requires shared mount propagation
  mount --make-rshared /

  # ensure systemd cgroup is present (cgroups v1 only)
  if [ ! -f /sys/fs/cgroup/cgroup.controllers ]; then
    mkdir -p /sys/fs/cgroup/systemd
    if ! mountpoint -q /sys/fs/cgroup/systemd ; then
      mount -t cgroup -o none,name=systemd cgroup /sys/fs/cgroup/systemd
    fi
  fi

  # check for /proc/sys being mounted readonly, as systemd does
  if grep '/proc/sys\s\+\w\+\s\+ro,' /proc/mounts >/dev/null; then
    mount -o remount,rw /proc/sys
  fi

  local mtu
  mtu=$(cat "/sys/class/net/$(ip route get "${DNS_IP}"|awk '{ print $5 }')/mtu")

  [[ ! -d /etc/docker ]] && mkdir /etc/docker
  cat <<EOF > /etc/docker/daemon.json
{
  "hosts": ["${DOCKER_HOST}"],
  "tls": true,
  "tlscert": "${certs_dir}/server-cert.pem",
  "tlskey": "${certs_dir}/server-key.pem",
  "tlscacert": "${certs_dir}/ca.pem",
  "mtu": ${mtu},
  "data-root": "/scratch/docker",
  "tlsverify": true
}
EOF

  trap stop_docker EXIT

  service docker start

  rc=1
  for i in $(seq 1 100); do
    echo "waiting for docker to come up... (${i})"
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

  echo "${certs_dir}"
}

function main() {
  OUTER_CONTAINER_IP=$(
    ip addr \
    | grep 'inet ' \
    | grep -v -E ' (127\.|172\.|10\.245)' \
    | cut -d/ -f 1 \
    | cut -d' ' -f6
  )
  export OUTER_CONTAINER_IP

  if [[ "${OUTER_CONTAINER_IP}" == *$'\n'* ]] ; then
    echo "OUTER_CONTAINER_IP had more than one ip: '${OUTER_CONTAINER_IP}'" >&2
    exit 1
  fi

  local certs_dir
  certs_dir=$(mktemp -d)

  local local_bosh_dir
  local_bosh_dir="/tmp/local-bosh/director"
  mkdir -p ${local_bosh_dir}

  cat <<EOF > "${local_bosh_dir}/docker-env"
export DOCKER_HOST="tcp://${OUTER_CONTAINER_IP}:4243"
export DOCKER_TLS_VERIFY=1
export DOCKER_CERT_PATH="${certs_dir}"

EOF
  echo "Source '${local_bosh_dir}/docker-env' to run docker" >&2
  source "${local_bosh_dir}/docker-env"

  start_docker "${certs_dir}"

  local docker_network_name="director_network"
  local docker_network_cidr="10.245.0.0/16"
  if docker network ls | grep -q "${docker_network_name}"; then
    echo "A docker network named '${docker_network_name}' already exists, skipping creation" >&2
  else
    docker network create -d bridge --subnet="${docker_network_cidr}" "${docker_network_name}"
  fi

  cat <<EOF > "${local_bosh_dir}/docker_tls.json"
{
  "ca": "$(cat "${certs_dir}/ca_json_safe.pem")",
  "certificate": "$(cat "${certs_dir}/client_certificate_json_safe.pem")",
  "private_key": "$(cat "${certs_dir}/client_private_key_json_safe.pem")"
}

EOF

  bosh int "${BOSH_DEPLOYMENT_PATH}/bosh.yml" \
    -o "${BOSH_DEPLOYMENT_PATH}/docker/cpi.yml" \
    -o "${BOSH_DEPLOYMENT_PATH}/jumpbox-user.yml" \
    -o "${REPO_ROOT}/manifests/dev.yml" \
    -v director_name=docker \
    -v internal_cidr="${docker_network_cidr}" \
    -v internal_gw=10.245.0.1 \
    -v internal_ip="${BOSH_DIRECTOR_IP}" \
    -v docker_host="${DOCKER_HOST}" \
    -v network="${docker_network_name}" \
    -v docker_tls="$(cat "${local_bosh_dir}/docker_tls.json")" \
    -v docker_cpi_path="${REPO_PARENT}/bosh-cpi-dev-artifacts/release.tgz" \
    "${@}" > "${local_bosh_dir}/bosh-director.yml"

  if ! bosh create-env "${local_bosh_dir}/bosh-director.yml" \
      --vars-store="${local_bosh_dir}/creds.yml" \
      --state="${local_bosh_dir}/state.json"; then

    echo ""
    echo "=== CREATE-ENV FAILED - COLLECTING DIAGNOSTICS ==="
    echo ""

    echo "--- docker ps (all containers, including stopped) ---"
    docker ps -a --format "table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Ports}}" || true

    for cid in $(docker ps -a -q); do
      cname=$(docker inspect --format '{{.Name}}' "${cid}" | sed 's|^/||')
      cstatus=$(docker inspect --format '{{.State.Status}}' "${cid}")
      echo ""
      echo "=== Container: ${cname} (${cid}) - Status: ${cstatus} ==="

      echo "--- Container state ---"
      docker inspect --format 'ExitCode={{.State.ExitCode}} OOMKilled={{.State.OOMKilled}} Error={{.State.Error}} StartedAt={{.State.StartedAt}} FinishedAt={{.State.FinishedAt}}' "${cid}" 2>&1 || true

      echo "--- Container HostConfig ---"
      docker inspect --format 'Privileged={{.HostConfig.Privileged}} CgroupnsMode={{.HostConfig.CgroupnsMode}}' "${cid}" 2>&1 || true

      echo "--- Container logs ---"
      docker logs "${cid}" 2>&1 || true

      if [ "${cstatus}" != "running" ]; then
        local image
        image=$(docker inspect --format '{{.Config.Image}}' "${cid}" 2>/dev/null)
        local binds
        binds=$(docker inspect --format '{{json .HostConfig.Binds}}' "${cid}" 2>/dev/null)
        local network_mode
        network_mode=$(docker inspect --format '{{.HostConfig.NetworkMode}}' "${cid}" 2>/dev/null)
        echo "--- Failed container config: Image=${image} Binds=${binds} NetworkMode=${network_mode} ---"

        if [ -n "${image}" ]; then
          echo ""
          echo "--- Test 1: exact same command as the failed container ---"
          local orig_cmd
          orig_cmd=$(docker inspect --format '{{json .Config.Cmd}}' "${cid}" 2>/dev/null)
          echo "Command: ${orig_cmd}"
          docker run --rm -d --name diag-exact \
            --privileged --cgroupns=host \
            -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
            -v /lib/modules:/usr/lib/modules \
            --network "${network_mode}" \
            --ip 10.245.0.99 \
            "${image}" \
            bash -c 'set -x; umount /etc/resolv.conf; echo "umount resolv rc=$?"; umount /etc/hosts; echo "umount hosts rc=$?"; umount /etc/hostname; echo "umount hostname rc=$?"; echo "prestart-done"; exec /sbin/init' 2>&1 || true
          sleep 5
          echo "--- diag-exact status ---"
          docker ps -a --filter name=diag-exact --format "table {{.ID}}\t{{.Names}}\t{{.Status}}" 2>&1 || true
          echo "--- diag-exact logs ---"
          docker logs diag-exact 2>&1 || true
          diag_status=$(docker inspect --format '{{.State.Status}}' diag-exact 2>/dev/null || echo "unknown")
          if [ "${diag_status}" = "running" ]; then
            echo "--- diag-exact: systemd running OK ---"
            docker exec diag-exact ps aux 2>&1 | head -10 || true
          else
            echo "--- diag-exact: container NOT running (status=${diag_status}) ---"
            docker inspect --format 'ExitCode={{.State.ExitCode}} OOMKilled={{.State.OOMKilled}} Error={{.State.Error}}' diag-exact 2>&1 || true
          fi
          docker rm -f diag-exact 2>/dev/null || true

          echo ""
          echo "--- Test 2: minimal /sbin/init (no umounts, no network) ---"
          docker run --rm -d --name diag-minimal \
            --privileged --cgroupns=host \
            -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
            "${image}" \
            bash -c 'echo minimal-prestart-ok && exec /sbin/init' 2>&1 || true
          sleep 5
          echo "--- diag-minimal status ---"
          docker ps -a --filter name=diag-minimal --format "table {{.ID}}\t{{.Names}}\t{{.Status}}" 2>&1 || true
          echo "--- diag-minimal logs ---"
          docker logs diag-minimal 2>&1 || true
          docker rm -f diag-minimal 2>/dev/null || true
        fi
      fi
    done

    echo ""
    echo "--- dmesg (last 50 lines) ---"
    dmesg 2>&1 | tail -50 || true

    echo ""
    echo "--- /sys/fs/cgroup status ---"
    ls -la /sys/fs/cgroup/ 2>&1 || true
    cat /sys/fs/cgroup/cgroup.subtree_control 2>&1 || true
    cat /sys/fs/cgroup/cgroup.controllers 2>&1 || true

    echo ""
    echo "=== END CREATE-ENV DIAGNOSTICS ==="
    exit 1
  fi

  bosh int "${local_bosh_dir}/creds.yml" --path /director_ssl/ca > "${local_bosh_dir}/ca.crt"
  bosh_client_secret="$(bosh int "${local_bosh_dir}/creds.yml" --path /admin_password)"

  bosh -e "${BOSH_DIRECTOR_IP}" --ca-cert "${local_bosh_dir}/ca.crt" alias-env "${BOSH_ENVIRONMENT}"

  cat <<EOF > "${local_bosh_dir}/env"
  export BOSH_DIRECTOR_IP="${BOSH_DIRECTOR_IP}"
  export BOSH_ENVIRONMENT="${BOSH_ENVIRONMENT}"
  export BOSH_CLIENT=admin
  export BOSH_CLIENT_SECRET=${bosh_client_secret}
  export BOSH_CA_CERT="${local_bosh_dir}/ca.crt"

EOF

  echo "Source '${local_bosh_dir}/env' to run bosh" >&2
  source "${local_bosh_dir}/env"

  bosh -n update-cloud-config "${BOSH_DEPLOYMENT_PATH}/docker/cloud-config.yml" \
    -v network="${docker_network_name}"

  stemcell_file="$(find "${REPO_PARENT}/stemcell" -maxdepth 1 -path '*.tgz')"
  bosh -n upload-stemcell "${stemcell_file}"

  deployment_name="integration-test"

  docker events --format '{{.Time}} {{.Type}} {{.Action}} {{.Actor.Attributes.name}} {{.Actor.ID}}' > /tmp/docker-events.log 2>&1 &
  local docker_events_pid=$!

  if ! bosh deploy --non-interactive \
    --deployment "${deployment_name}" \
    "${REPO_ROOT}/ci/tasks/integration-test-manifest.yml" \
     --var deployment_name="${deployment_name}"; then

    kill "${docker_events_pid}" 2>/dev/null || true
    wait "${docker_events_pid}" 2>/dev/null || true

    echo ""
    echo "=== DEPLOYMENT FAILED - COLLECTING DIAGNOSTICS ==="
    echo ""

    echo "--- Docker events during deploy ---"
    cat /tmp/docker-events.log 2>&1 || true

    echo ""
    echo "--- docker ps (all containers, including stopped) ---"
    docker ps -a --format "table {{.ID}}\t{{.Names}}\t{{.Status}}\t{{.Ports}}" || true

    local director_cid=""
    for cid in $(docker ps -a -q); do
      cname=$(docker inspect --format '{{.Name}}' "${cid}" | sed 's|^/||')
      cstatus=$(docker inspect --format '{{.State.Status}}' "${cid}")
      echo ""
      echo "=== Container: ${cname} (${cid}) - Status: ${cstatus} ==="

      echo "--- Container state ---"
      docker inspect --format 'ExitCode={{.State.ExitCode}} OOMKilled={{.State.OOMKilled}} Error={{.State.Error}} StartedAt={{.State.StartedAt}} FinishedAt={{.State.FinishedAt}}' "${cid}" 2>&1 || true

      echo "--- Container HostConfig ---"
      docker inspect --format 'Privileged={{.HostConfig.Privileged}} CgroupnsMode={{.HostConfig.CgroupnsMode}}' "${cid}" 2>&1 || true

      echo "--- Container logs (last 50 lines) ---"
      docker logs --tail 50 "${cid}" 2>&1 || true

      if [ "${cstatus}" = "running" ]; then
        director_cid="${cid}"

        echo "--- Processes ---"
        docker exec "${cid}" ps aux 2>&1 | head -30 || true

        echo "--- BOSH agent log (last 30 lines) ---"
        docker exec "${cid}" bash -c 'tail -30 /var/vcap/bosh/log/current 2>/dev/null || echo "no agent log found"' 2>&1 || true
      fi
    done

    if [ -n "${director_cid}" ]; then
      echo ""
      echo "--- Director CPI logs (docker_cpi errors/warnings) ---"
      docker exec "${director_cid}" bash -c 'grep -i "error\|warn\|fail\|timeout\|create_vm\|delete_vm" /var/vcap/sys/log/cpi/*.log 2>/dev/null | tail -50 || echo "no CPI logs found"' 2>&1 || true

      echo ""
      echo "--- Director task debug log (last 100 lines) ---"
      docker exec "${director_cid}" bash -c 'ls -t /var/vcap/data/sys/log/director/*.debug 2>/dev/null | head -1 | xargs tail -100 2>/dev/null || echo "no director debug log found"' 2>&1 || true
    fi

    echo ""
    echo "--- dmesg (last 30 lines) ---"
    dmesg 2>&1 | tail -30 || true

    echo ""
    echo "=== END DIAGNOSTICS ==="
    exit 1
  fi

  kill "${docker_events_pid}" 2>/dev/null || true
  wait "${docker_events_pid}" 2>/dev/null || true
}

main "${@}"
