#!/usr/bin/env bash

set -e

source ci/ci/tasks/utils.sh

check_go_version ${PWD}/bosh-docker-cpi-release

cd ${PWD}/bosh-docker-cpi-release/src/bosh-docker-cpi
go run github.com/onsi/ginkgo/v2/ginkgo -r .
