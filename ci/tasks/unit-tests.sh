#!/usr/bin/env bash

set -e

check_go_version ${PWD}/bosh-cpi-src

cd ${PWD}/bosh-cpi-src/src/bosh-docker-cpi
go run github.com/onsi/ginkgo/v2/ginkgo -r .
