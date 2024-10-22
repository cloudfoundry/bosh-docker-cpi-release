#!/usr/bin/env bash

set -e

source ci/ci/tasks/utils.sh

check_go_version ${PWD}/bosh-cpi-src

cd ${PWD}/bosh-cpi-src/src/bosh-docker-cpi
go run github.com/onsi/ginkgo/v2/ginkgo -r .
