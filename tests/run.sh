#!/usr/bin/env bash
set -euo pipefail

if [[ -n "${DEBUG:-}" ]]; then
  set -x
  export BOSH_LOG_LEVEL=debug
fi

cpi_path=$PWD/cpi

rm -f creds.yml

echo "-----> $(date): Create dev release"
bosh create-release --force --dir ./../ --tarball "${cpi_path}"

echo "-----> $(date): Create env"
bosh create-env ~/workspace/bosh-deployment/bosh.yml \
  -o ~/workspace/bosh-deployment/docker/cpi.yml \
  -o ~/workspace/bosh-deployment/jumpbox-user.yml \
  -o ../manifests/dev.yml \
  --state=state.json \
  --vars-store=creds.yml \
  -v docker_cpi_path="${cpi_path}" \
  -v director_name=docker \
  -v internal_cidr=10.245.0.0/16 \
  -v internal_gw=10.245.0.1 \
  -v internal_ip=10.245.0.11 \
  -v docker_host=tcp://192.168.50.8:4243 \
  --var-file docker_tls.certificate=<(bosh int ~/workspace/docker-deployment/creds.yml --path /docker_client_ssl/certificate) \
  --var-file docker_tls.private_key=<(bosh int ~/workspace/docker-deployment/creds.yml --path /docker_client_ssl/private_key) \
  --var-file docker_tls.ca=<(bosh int ~/workspace/docker-deployment/creds.yml --path /docker_client_ssl/ca) \
  -v network=net3

export BOSH_ENVIRONMENT=10.245.0.11
BOSH_CA_CERT="$(bosh int creds.yml --path /director_ssl/ca)"
export BOSH_CA_CERT
export BOSH_CLIENT=admin
BOSH_CLIENT_SECRET="$(bosh int creds.yml --path /admin_password)"
export BOSH_CLIENT_SECRET

echo "-----> $(date): Update cloud config"
bosh -n update-cloud-config ~/workspace/bosh-deployment/docker/cloud-config.yml \
  -o reserved_ips.yml \
  -v network=net3

echo "-----> $(date): Upload stemcell"
bosh -n upload-stemcell "https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-trusty-go_agent?v=3445.11" \
  --sha1 d57c48cee58c71dce3707ff117ce79d01cc322ab

echo "-----> $(date): Create env second time to test persistent disk attachment"
bosh create-env ~/workspace/bosh-deployment/bosh.yml \
  -o ~/workspace/bosh-deployment/docker/cpi.yml \
  -o ~/workspace/bosh-deployment/jumpbox-user.yml \
  -o ../manifests/dev.yml \
  --state=state.json \
  --vars-store=creds.yml \
  -v docker_cpi_path="${cpi_path}" \
  -v director_name=docker \
  -v internal_cidr=10.245.0.0/16 \
  -v internal_gw=10.245.0.1 \
  -v internal_ip=10.245.0.11 \
  -v docker_host=tcp://192.168.50.8:4243 \
  --var-file docker_tls.certificate=<(bosh int ~/workspace/docker-deployment/creds.yml --path /docker_client_ssl/certificate) \
  --var-file docker_tls.private_key=<(bosh int ~/workspace/docker-deployment/creds.yml --path /docker_client_ssl/private_key) \
  --var-file docker_tls.ca=<(bosh int ~/workspace/docker-deployment/creds.yml --path /docker_client_ssl/ca) \
  -v network=net3 \
  --recreate

echo "-----> $(date): Delete previous deployment"
bosh -n -d zookeeper delete-deployment --force

echo "-----> $(date): Deploy"
bosh -n -d zookeeper deploy <(wget -O- https://raw.githubusercontent.com/cppforlife/zookeeper-release/master/manifests/zookeeper.yml)

echo "-----> $(date): Recreate all VMs"
bosh -n -d zookeeper recreate

echo "-----> $(date): Exercise deployment"
bosh -n -d zookeeper run-errand smoke-tests

echo "-----> $(date): Restart deployment"
bosh -n -d zookeeper restart

echo "-----> $(date): Report any problems"
bosh -n -d zookeeper cck --report

echo "-----> $(date): Delete random VM"
bosh -n -d zookeeper delete-vm "$(bosh -d zookeeper vms|sort|cut -f5|head -1)"

echo "-----> $(date): Fix deleted VM"
bosh -n -d zookeeper cck --auto

echo "-----> $(date): Delete deployment"
bosh -n -d zookeeper delete-deployment

echo "-----> $(date): Clean up disks, etc."
bosh -n -d zookeeper clean-up --all

echo "-----> $(date): Deleting env"
bosh delete-env ~/workspace/bosh-deployment/bosh.yml \
  -o ~/workspace/bosh-deployment/docker/cpi.yml \
  -o ~/workspace/bosh-deployment/jumpbox-user.yml \
  -o ../manifests/dev.yml \
  --state=state.json \
  --vars-store=creds.yml \
  -v docker_cpi_path="${cpi_path}" \
  -v director_name=docker \
  -v internal_cidr=10.245.0.0/16 \
  -v internal_gw=10.245.0.1 \
  -v internal_ip=10.245.0.11 \
  -v docker_host=tcp://192.168.50.8:4243 \
  -l docker-creds.yml \
  -v network=net3

echo "-----> $(date): Done"
