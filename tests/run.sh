#!/bin/bash

set -e # -x

cpi_path=$PWD/cpi

rm -f creds.yml

echo "-----> `date`: Create dev release"
bosh create-release --force --dir ./../ --tarball $cpi_path

echo "-----> `date`: Create env"
bosh create-env ~/workspace/bosh-deployment/bosh.yml \
  -o ~/workspace/bosh-deployment/docker/cpi.yml \
  -o ~/workspace/bosh-deployment/jumpbox-user.yml \
  -o ../manifests/dev.yml \
  --state=state.json \
  --vars-store=creds.yml \
  -v docker_cpi_path=$cpi_path \
  -v director_name=docker \
  -v internal_cidr=10.245.0.0/16 \
  -v internal_gw=10.245.0.1 \
  -v internal_ip=10.245.0.11 \
  -v docker_host=tcp://192.168.50.8:4243 \
  -l docker-creds.yml \
  -v network=net3

export BOSH_ENVIRONMENT=10.245.0.11
export BOSH_CA_CERT="$(bosh int creds.yml --path /director_ssl/ca)"
export BOSH_CLIENT=admin
export BOSH_CLIENT_SECRET="$(bosh int creds.yml --path /admin_password)"

echo "-----> `date`: Update cloud config"
bosh -n update-cloud-config ~/workspace/bosh-deployment/docker/cloud-config.yml \
  -o reserved_ips.yml \
  -v network=net3

echo "-----> `date`: Upload stemcell"
bosh -n upload-stemcell "https://bosh.io/d/stemcells/bosh-warden-boshlite-ubuntu-trusty-go_agent?v=3312.18" \
  --sha1 2e1f70938a2b6e8269a721bfb59cf799883b9cc1

echo "-----> `date`: Create env second time to test disk attachment"
bosh create-env ~/workspace/bosh-deployment/bosh.yml \
  -o ~/workspace/bosh-deployment/docker/cpi.yml \
  -o ~/workspace/bosh-deployment/jumpbox-user.yml \
  -o ../manifests/dev.yml \
  -o ./force.yml \
  --state=state.json \
  --vars-store=creds.yml \
  -v docker_cpi_path=$cpi_path \
  -v director_name=docker \
  -v internal_cidr=10.245.0.0/16 \
  -v internal_gw=10.245.0.1 \
  -v internal_ip=10.245.0.11 \
  -v docker_host=tcp://192.168.50.8:4243 \
  -l docker-creds.yml \
  -v network=net3 \
  -v force_val="`date`"

echo "-----> `date`: Delete previous deployment"
bosh -n -d zookeeper delete-deployment --force

echo "-----> `date`: Deploy"
bosh -n -d zookeeper deploy zookeeper.yml

echo "-----> `date`: Recreate all VMs"
bosh -n -d zookeeper recreate

echo "-----> `date`: Exercise deployment"
bosh -n -d zookeeper run-errand smoke_tests

echo "-----> `date`: Restart deployment"
bosh -n -d zookeeper restart

echo "-----> `date`: Report any problems"
bosh -n -d zookeeper cck --report

echo "-----> `date`: Delete random VM"
bosh -n -d zookeeper delete-vm `bosh -d zookeeper vms|sort|cut -f5|head -1`

echo "-----> `date`: Fix deleted VM"
bosh -n -d zookeeper cck --auto

echo "-----> `date`: Delete deployment"
bosh -n -d zookeeper delete-deployment

echo "-----> `date`: Clean up disks, etc."
bosh -n -d zookeeper clean-up --all

echo "-----> `date`: Deleting env"
bosh delete-env ~/workspace/bosh-deployment/bosh.yml \
  -o ~/workspace/bosh-deployment/docker/cpi.yml \
  -o ~/workspace/bosh-deployment/jumpbox-user.yml \
  -o ../manifests/dev.yml \
  --state=state.json \
  --vars-store=creds.yml \
  -v docker_cpi_path=$cpi_path \
  -v director_name=docker \
  -v internal_cidr=10.245.0.0/16 \
  -v internal_gw=10.245.0.1 \
  -v internal_ip=10.245.0.11 \
  -v docker_host=tcp://192.168.50.8:4243 \
  -l docker-creds.yml \
  -v network=net3

echo "-----> `date`: Done"
