#!/usr/bin/env bash
set -eu -o pipefail

REPO_ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )/../.." && pwd )"
REPO_PARENT="$( cd "${REPO_ROOT}/.." && pwd )"

if [[ -n "${DEBUG:-}" ]]; then
  set -x
  export BOSH_LOG_LEVEL=debug
  export BOSH_LOG_PATH="${BOSH_LOG_PATH:-${REPO_PARENT}/bosh-debug.log}"
fi

cpi_package_go_version="$(
  grep linux "${REPO_PARENT}"/bosh-docker-cpi-release/packages/golang-*/spec.lock \
  | awk '{print $2}' \
  | sed "s/golang-\(.*\)-linux/\1/"
)"

current=$(go version)
if [[ "$current" != *"${cpi_package_go_version}"* ]]; then
  echo "Current go version '${current}' does not match CPI's packaged go version '${cpi_package_go_version}'"
  exit 1
fi

"${REPO_PARENT}/bosh-docker-cpi-release/scripts/test-unit"
