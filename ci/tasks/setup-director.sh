#!/usr/bin/env bash

set -e


cd ${PWD}/bosh-cpi-src/

dev_version=$(cat ../bosh-cpi-dev-artifacts/version)
cpi_path=../bosh-cpi-dev-artifacts/bosh-docker-cpi-${dev_version}.tgz


echo "-----> Create env"
bosh create-env ../bosh-deployment/bosh.yml \
  -o ../bosh-deployment/docker/cpi.yml \
  -o ../bosh-deployment/jumpbox-user.yml \
  -o manifests/dev.yml \
  --state=state.json \
  --vars-store=creds.yml \
  -v docker_cpi_path=${cpi_path} \
  -v director_name=docker \
  -v internal_cidr=10.245.0.0/16 \
  -v internal_gw=10.245.0.1 \
  -v internal_ip=10.245.0.11 \
  -v docker_host=tcp://192.168.50.8:4243 \
  --var-file docker_tls.certificate=<(bosh int ../docker-deployment/creds.yml --path /docker_client_ssl/certificate) \
  --var-file docker_tls.private_key=<(bosh int ../docker-deployment/creds.yml --path /docker_client_ssl/private_key) \
  --var-file docker_tls.ca=<(bosh int ../docker-deployment/creds.yml --path /docker_client_ssl/ca) \
  -v network=net3

export BOSH_ENVIRONMENT=10.245.0.11
export BOSH_CA_CERT="$(bosh int creds.yml --path /director_ssl/ca)"
export BOSH_CLIENT=admin
export BOSH_CLIENT_SECRET="$(bosh int creds.yml --path /admin_password)"
