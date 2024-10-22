#!/usr/bin/env bash

set -e

cpi_release_name="bosh-docker-cpi"
semver=`cat version-semver/number`
image_path=$PWD/bosh-cpi-src/${cpi_release_name}-${semver}.tgz

pushd bosh-cpi-src
  echo "Using BOSH CLI version..."
  bosh --version

  echo "Exposing release semver to bosh-docker-cpi"
  echo ${semver} > "src/bosh-docker-cpi/release"

  # We have to use the --force flag because we just added the `src/bosh-docker-cpi/release` file
  echo "Creating CPI BOSH Release..."
  bosh create-release --name=${cpi_release_name} --version=${semver} --tarball=${image_path} --force
popd

mv ${image_path} candidate/
