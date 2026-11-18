## bosh-docker-cpi-release

This is a BOSH release for the Docker CPI. It can be used against single or multi-host Docker configurations.

Known limitations:

- requires Docker network to have ability to assign IP addresses
  - necessary for bootstrapping Director
- does not work with deployments that try to attach persistent disk
  - works during `bosh create-env` but not in `bosh deploy`
  - will be fixed in the Director when we wait for Agent to be responsive after attach_disk CPI call

## Light Stemcells

The Docker CPI supports "light" stemcells, which contain only metadata and a Docker image reference instead of a full image archive. This allows BOSH to pull images directly from container registries, reducing stemcell file size and enabling faster deployments.

### Creating Light Stemcells

Use the provided script to create a light stemcell from any Docker image:

```bash
./dev/create-light-stemcell.sh <docker-image-reference> <output-file>
```

Example:
```bash
./dev/create-light-stemcell.sh ghcr.io/cloudfoundry/ubuntu-noble-stemcell:1.165 my-stemcell.tgz
```

The script automatically:
- Extracts the OS name from the image name (e.g., `ubuntu-noble-stemcell` → `ubuntu-noble`)
- Generates proper BOSH stemcell metadata
- Creates a tarball that can be uploaded to BOSH Director

### Light Stemcell Format

A light stemcell is a tarball containing:
- `stemcell.MF` - Metadata file with:
  - `name` - Stemcell name (e.g., `bosh-docker-ubuntu-noble`)
  - `version` - Stemcell version (extracted from image tag)
  - `operating_system` - OS identifier (e.g., `ubuntu-noble`)
  - `stemcell_formats` - Set to `["docker-light"]` to indicate light stemcell
  - `cloud_properties` - IaaS-specific properties including:
    - `image_reference` - Full Docker image reference (e.g., `ghcr.io/org/image:tag`)
    - `digest` - SHA256 digest for image verification (optional)
- `image` - Empty file (required by BOSH Director format)

### Configuration

Configure light stemcell behavior in your deployment manifest:

```yaml
properties:
  docker_cpi:
    light_stemcell:
      # Disable light stemcell feature entirely (default: false)
      disable_light_stemcells: false
      
      # Require image digest verification (default: true)
      require_image_verification: true
```

### Security Considerations

- **Registry Trust**: Light stemcells can pull from any registry accessible to the Docker daemon. For production, configure Docker daemon authentication for private registries.
- **Image Verification**: By default, `require_image_verification` is `true` to ensure images match expected digests.
- **Disabling Feature**: Set `disable_light_stemcells: true` to disable the light stemcell feature entirely and only use traditional stemcells.

### CID Behavior

Light stemcells use the Docker image digest as the Cloud ID (CID), providing content-addressable and immutable references:

```bash
$ bosh stemcells
NAME                      VERSION  OS            CID
bosh-docker-ubuntu-noble  1.165    ubuntu-noble  ghcr.io/cloudfoundry/ubuntu-noble-stemcell@sha256:d4ca21a75f1ff6be382695e299257f054585143bf09762647bcb32f37be5eaf3
```

This provides immutability guarantees, ensuring the stemcell always references the exact image content. This differs from traditional stemcells which use generated UUIDs.

## Private cgroup v2 namespace for systemd containers

By default, systemd containers share the host cgroup namespace (`CgroupnsMode: host`) and, unless
`docker_cpi.mount_cgroupfs` is `false`, bind-mount the host `/sys/fs/cgroup`. With `cgroup_v2` enabled
a systemd container on a cgroup v2 host instead gets a private, writable cgroup v2 namespace and no host cgroup bind.
On a host Docker identifies as cgroup v1, it falls back to the legacy namespace and binding settings.

Enable it for every systemd VM with the job property, and/or per VM in cloud-config:

```yaml
# deployment manifest (CPI default, applies to systemd VMs only)
properties:
  docker_cpi:
    start_containers_with_systemd: true
    cgroup_v2: true
```

```yaml
# cloud-config vm_types (overrides the CPI default; false opts out)
vm_types:
- name: default
  cloud_properties:
    cgroup_v2: true
```

Requirements, all checked before the container is created unless noted:

- systemd containers (`start_containers_with_systemd` or `force_start_with_systemd`); an explicit
  per-VM `cgroup_v2: true` without systemd is an error, while the CPI-wide default silently skips runit VMs
- the CPI's Docker API version (`docker_cpi.docker.api_version`) must be >= 1.41 for host detection and the
  daemon must support it
- Docker must report cgroup version `1` or `2`; v1 selects the legacy path, while an unknown or
  missing version is an error
- `CgroupnsMode` unset or `private`; no `VolumesFrom`; no `Mounts` or `Tmpfs` that touch `/sys/fs/cgroup`
  (or a parent such as `/sys`). These are validated even if the host turns out to be v1, so a config
  accepted on a v1 host is not rejected after a move to v2. `cloud_properties` `Binds` are ignored
  (the CPI sets the container's binds itself) and a warning is logged
- memory controller delegation and a writable cgroup mount, checked by a preflight inside the container; if it
  fails, VM creation fails with a `cgroup_v2 preflight failed: <check>` error naming the failed check, and
  the container and its ephemeral volume are removed. The CPI also logs a warning when `cgroup_v2` falls back on a v1 host

`docker_cpi.mount_cgroupfs` is ignored when private v2 is selected. On v1 fallback it still controls
the legacy host bind; set it to `false` to omit that bind. Setting it to `false` alone does not
select a private cgroup namespace: legacy systemd containers still use `CgroupnsMode: host`.
Host detection/API failures and v2 preflight failures are errors, not reasons to fall back.
Existing VMs are unaffected until
recreated; disk attach/detach recreates a container with the settings it already has.

## Development

- integration tests: `cd tests && ./run.sh`
- unit tests: `./src/github.com/cppforlife/bosh-docker-cpi/bin/test`

## TODO

- disk migration
- root & ephemeral disk size limits
- persistent disk attach after container is created
- AZ tagging
- efficient stemcell import for swarm
- drain of containers when host is going down
- expose ports
- network name vs cloud_properties
- multiple networks
- [cf] gorouter tcp tuning
  - running_in_container needs to check for docker
- [cf] postgres needs /var/vcap/store
