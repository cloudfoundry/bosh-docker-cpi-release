# BOSH Docker CPI Architecture

This document provides a technical deep dive into the BOSH Docker CPI implementation, design decisions, and integration points.

## Design Overview

The BOSH Docker CPI implements the standard BOSH Cloud Provider Interface using Docker as the underlying infrastructure. It provides a lightweight alternative to traditional IaaS platforms for development and testing scenarios.

### Core Concepts

- **Containers as VMs**: Each BOSH VM is implemented as a Docker container
- **Volumes as Disks**: Docker volumes serve as persistent disks
- **Images as Stemcells**: Docker images act as BOSH stemcells
- **Networks**: Docker networks provide VM connectivity

### Key Design Principles

1. **Full CPI Compliance**: Implements all required CPI methods
2. **Stateless Operation**: No persistent state between CPI calls
3. **Resource Isolation**: Uses cgroupsv2 for resource management
4. **Agent Compatibility**: Supports standard BOSH agent operations

## Component Architecture

### Directory Structure

```
src/bosh-docker-cpi/
├── main/              # Entry point
├── cpi/               # CPI method implementations
├── vm/                # Container management
├── disk/              # Volume management
├── stemcell/          # Image management
└── config/            # Configuration handling
```

### Entry Points

The CPI uses a command-line interface that accepts JSON-RPC style requests:

```go
// main/main.go
func main() {
    logger := createLogger()
    defer logger.HandlePanic("Main")
    
    cmdRunner := system.NewExecCmdRunner(logger)
    cpi := newCPI(cmdArgs, config, logger)
    
    cli := cpi.NewCLI(cpi, logger)
    err := cli.ServeOnce()
}
```

### Factory Pattern

The CPI extensively uses factory patterns for creating components:

```go
// vm/factory.go
type Factory struct {
    agentEnvService AgentEnvService
    dockerClient    *client.Client
    config         Config
}

func (f *Factory) Create(spec VMSpec) (VM, error) {
    return NewContainer(spec, f.dockerClient, f.agentEnvService)
}
```

## CPI Method Implementations

### VM Management

#### create_vm

Creates a Docker container with specified resources:

```go
// cpi/create_vm.go
func (c *CPI) CreateVM(
    agentID string,
    stemcellCID string,
    cloudProps VMCloudProperties,
    networks Networks,
    diskCIDs []string,
    env Environment,
) (string, error) {
    // 1. Pull stemcell image
    // 2. Create container with resource limits
    // 3. Configure networking
    // 4. Inject agent settings
    // 5. Start container
    // 6. Wait for agent
}
```

Key implementation details:
- Generates unique container names: `vm-${UUID}`
- Sets resource limits via cgroupsv2
- Configures host networking or custom networks
- Mounts agent settings at `/var/vcap/bosh/user_data.json`

#### delete_vm

Removes container and cleans up resources:

```go
func (c *CPI) DeleteVM(vmCID string) error {
    // 1. Stop container
    // 2. Remove container
    // 3. Clean up any attached volumes
}
```

### Disk Management

#### create_disk

Creates a Docker volume:

```go
// cpi/create_disk.go
func (c *CPI) CreateDisk(
    size int,
    cloudProps DiskCloudProperties,
    vmCID *string,
) (string, error) {
    volumeName := fmt.Sprintf("disk-%s", uuid.New())
    // Docker doesn't enforce volume size limits
    return dockerClient.VolumeCreate(volumeName)
}
```

**Limitation**: Docker doesn't enforce volume size limits

#### attach_disk

Mounts volume into container:

```go
func (c *CPI) AttachDisk(vmCID, diskCID string) error {
    // 1. Stop container
    // 2. Update container config with volume mount
    // 3. Restart container
    // 4. Update agent settings
}
```

Mount path: `/var/vcap/store`

### Stemcell Management

#### create_stemcell

Imports a stemcell as Docker image:

```go
// cpi/create_stemcell.go
func (c *CPI) CreateStemcell(
    imagePath string,
    cloudProps StemcellCloudProperties,
) (string, error) {
    // 1. Extract rootfs from stemcell tarball
    // 2. Import as Docker image
    // 3. Tag with unique ID
}
```

Naming convention: `stemcell:${UUID}`

### Network Configuration

The CPI supports single network configuration per VM:

```go
// vm/networks.go
type Network struct {
    Type          string
    IP            string
    Netmask       string
    Gateway       string
    DNS           []string
    Default       []string
    CloudProperties map[string]interface{}
}
```

Network types:
- **manual**: Static IP configuration
- **dynamic**: DHCP (limited support)

## Key Configuration Properties

### CPI Job Properties

```yaml
# jobs/docker_cpi/spec
properties:
  docker_cpi.docker.host:
    description: "Docker daemon endpoint"
    example: "tcp://192.168.50.8:4243"
    default: "unix:///var/run/docker.sock"
    
  docker_cpi.docker.api_version:
    description: "Docker API version"
    default: "1.44"
    
  docker_cpi.docker.tls:
    description: "TLS configuration"
    properties:
      cert: { description: "Client certificate" }
      key: { description: "Client private key" }
      ca: { description: "CA certificate" }
```

### Agent Configuration

```yaml
properties:
  docker_cpi.agent.mbus:
    description: "Message bus URL"
    example: "nats://nats:password@10.254.50.4:4222"
    
  docker_cpi.agent.blobstore:
    description: "Blobstore configuration"
    properties:
      provider: { default: "dav" }
      options:
        endpoint: "http://10.254.50.4:25250"
        user: "agent"
        password: "password"
```

## Implementation Details

### Container Lifecycle

1. **Creation**:
   - Pull stemcell image
   - Create container with `--init` flag
   - Configure cgroups v2 limits
   - Set up networking
   - Mount agent config

2. **Starting**:
   - For Noble: Use systemd mode
   - For others: Direct process execution
   - Wait for agent readiness

3. **Deletion**:
   - Graceful stop (SIGTERM)
   - Force removal after timeout
   - Clean up volumes

### Agent Environment Management

The agent environment is injected via bind mount:

```go
// vm/fs_agent_env_service.go
type FSAgentEnvService struct {
    rootPath string
}

func (s *FSAgentEnvService) Update(settings AgentEnv) error {
    // Write to /var/vcap/bosh/user_data.json
    // Inside container bind mount
}
```

### Resource Management

Uses cgroupsv2 for resource limits:

```go
// vm/resource_validator.go
func ValidateCgroupsV2() error {
    // Check /sys/fs/cgroup/cgroup.controllers
    // Verify memory, cpu, io, pids controllers
}

// Container creation
Resources: container.Resources{
    Memory:     int64(cloudProps.Memory) * 1024 * 1024,
    NanoCPUs:   int64(cloudProps.CPUs * 1000000000),
    PidsLimit:  &pidsLimit,
}
```

### Stemcell Detection

Automatically detects stemcell OS version:

```go
// vm/stemcell_detector.go
func DetectStemcellOS(imageID string) (string, error) {
    // Inspect image labels
    // Check /etc/os-release
    // Determine Noble/Jammy/etc
}
```

## Integration Points

### BOSH Director Communication

1. **CPI Protocol**: JSON-RPC over stdin/stdout
2. **Agent Protocol**: NATS messaging
3. **Blobstore**: HTTP DAV for artifacts

### Docker API Usage

```go
// Client initialization
client, err := client.NewClientWithOpts(
    client.WithHost(config.Host),
    client.WithVersion(config.APIVersion),
    client.WithHTTPClient(httpClient),
)
```

Key Docker APIs used:
- Container: Create, Start, Stop, Remove, Inspect
- Volume: Create, Remove, List
- Network: Create, Connect, Disconnect
- Image: Pull, Import, Tag, Remove

### File Service

Provides file operations inside containers:

```go
// vm/file_service.go
type FileService interface {
    Upload(containerID, srcPath, dstPath string) error
    Download(containerID, srcPath string) (io.Reader, error)
}
```

Used for:
- Agent configuration updates
- Log retrieval
- Debugging

## Limitations and Trade-offs

### Design Limitations

1. **Single Network**: Only one network per VM
2. **No AZ Support**: No availability zone concept
3. **Disk Size**: Docker doesn't enforce volume sizes
4. **Persistence**: Limited persistent disk support in deployments

### Performance Considerations

1. **Container Overhead**: Lower than VMs but higher than native processes
2. **Network Performance**: Bridge networking adds latency
3. **Disk I/O**: Volume driver dependent
4. **Memory**: No overcommit, hard limits via cgroups

### Security Implications

1. **Privileged Containers**: Required for Docker socket access
2. **Shared Kernel**: Less isolation than VMs
3. **Network Isolation**: Depends on Docker network driver
4. **Resource Limits**: Enforced by cgroupsv2

## Advanced Topics

### Systemd Mode (Noble)

For Ubuntu Noble stemcells:

```go
if stemcellOS == "ubuntu-noble" {
    createConfig.Cmd = []string{"/sbin/init"}
    hostConfig.Init = false  // systemd is init
}
```

### Docker Desktop Compatibility

Special handling for Docker Desktop:

```go
// vm/docker_desktop_helper.go
func isDockerDesktop() bool {
    // Check for Docker Desktop specific paths
    // Adjust socket permissions
}
```

### BPM Integration

Supports BOSH Process Manager:

```yaml
# manifests/ops-docker-bpm-compatibility.yml
- type: replace
  path: /instance_groups/name=bosh/jobs/name=bpm?
  value:
    name: bpm
    release: bpm
```

## Testing Considerations

### Unit Tests

```bash
cd src/bosh-docker-cpi
./bin/test
```

Uses Ginkgo/Gomega for BDD-style testing.

### Integration Tests

```bash
cd tests
./run.sh
```

Tests full lifecycle:
1. Director creation
2. Stemcell upload
3. Deployment (Zookeeper)
4. VM operations
5. Persistent disk handling

### Resource Validation

```bash
./test_cgroupsv2_validation.sh
./test_resource_enforcement.sh
```

Verifies:
- cgroupsv2 availability
- Resource limit enforcement
- Container isolation

## Future Enhancements

Potential improvements:

1. **Multi-Network Support**: Allow VMs with multiple NICs
2. **Volume Plugins**: Support for advanced storage drivers
3. **Registry Integration**: Direct stemcell pulls from registries
4. **Rootless Mode**: Full rootless Docker support
5. **Kubernetes Backend**: Use K8s as container orchestrator

## Conclusion

The Docker CPI provides a fully functional BOSH CPI implementation using Docker containers. While designed for development and testing, its architecture demonstrates how BOSH's abstraction layer enables diverse infrastructure backends. Understanding its implementation helps when:

- Debugging CPI issues
- Contributing improvements
- Building custom CPIs
- Optimizing BOSH deployments

For practical usage, see the [Getting Started Guide](getting_started.md). For troubleshooting, consult the [Troubleshooting Guide](troubleshooting.md).