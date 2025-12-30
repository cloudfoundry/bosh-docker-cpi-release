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
