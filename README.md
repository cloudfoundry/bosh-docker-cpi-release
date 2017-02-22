## bosh-docker-cpi-release

This is a BOSH release for the Docker CPI. It can be used against single or multi-host Docker configurations.

Known limitations:

- requires Docker network to have ability to assign IP addresses
  - necessary for bootstrapping Director
- does not work with deployments that try to attach persistent disk
  - works during `bosh create-env` but not in `bosh deploy`
  - will be fixed in the Director when we wait for Agent to be responsive after attach_disk CPI call

## Development

- integration tests: `cd tests && ./run.sh`
- unit tests: `./src/github.com/cppforlife/bosh-docker-cpi/bin/test`

## TODO

- root & ephemeral disk size limits
- persistent disk attach after container is created
- AZ tagging
- efficient stemcell import for swarm
- drain of containers when host is going down
- expose ports
- network name vs cloud_properties
- [cf] gorouter tcp tuning
  - running_in_container needs to check for docker
- [cf] postgres needs /var/vcap/store
