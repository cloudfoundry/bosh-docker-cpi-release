#!/usr/bin/env bash

set -e

source ci/ci/tasks/utils.sh

check_go_version ${PWD}/bosh-docker-cpi-release

${PWD}/bosh-docker-cpi-release/scripts/test-unit
