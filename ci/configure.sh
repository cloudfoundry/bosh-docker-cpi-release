#!/usr/bin/env bash

set -eu

script_dir="$( cd "$( dirname "$0" )" && pwd )"

fly -t bosh set-pipeline \
    -p bosh-docker-cpi \
    -c ${script_dir}/pipeline.yml
