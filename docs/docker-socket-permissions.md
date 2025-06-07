# Docker Socket Permissions

## Issue

When the BOSH director container tries to access the Docker daemon, it may encounter permission denied errors:

```
permission denied while trying to connect to the Docker daemon socket at unix:///docker.sock
```

This happens because the Docker socket (`/var/run/docker.sock`) is mounted into the BOSH director container at `/docker.sock`, but the container process doesn't have the necessary permissions to access it.

## Root Cause

The Docker socket typically has permissions `660` (rw-rw----) and is owned by `root:docker`. When bind-mounted into a container:

1. The numeric GID is preserved, not the group name
2. The container process may not have that GID
3. Even running as root in the container may not help due to user namespace remapping

## Solutions

### Option 1: Run Container as Root with Privileged Mode (Default)

The default configuration uses these ops files:
- `ops-docker-socket-permissions.yml` - Enables privileged mode
- `ops-docker-socket-stable-path.yml` - Mounts socket to `/docker.sock`

This works in most cases but may fail in some Docker configurations.

### Option 2: World-Writable Socket Permissions (Less Secure)

If the default doesn't work, you can use world-writable permissions:

```bash
DOCKER_SOCKET_WORLD_WRITABLE=true make test
```

This applies `ops-docker-socket-world-writable.yml` which:
- Runs the container as user ID 0 (root)
- Enables privileged mode
- Disables AppArmor and seccomp restrictions

**WARNING**: This is less secure and should only be used in development environments.

### Option 3: Fix Host Docker Socket Permissions

Ensure your user is in the docker group:

```bash
sudo usermod -aG docker $USER
# Log out and back in for changes to take effect
```

### Option 4: Use Rootless Docker

Rootless Docker runs the daemon as your user, avoiding permission issues:
https://docs.docker.com/engine/security/rootless/

## Detection

The test suite now automatically detects Docker socket permission issues:

1. `make prepare` - Checks if user is in docker group
2. `tests/run.sh` - Validates socket permissions before deployment

## Environment Variables

- `DOCKER_SOCKET_WORLD_WRITABLE=true` - Use less secure but more compatible permissions
- `SKIP_DOCKER_SOCKET_CHECK=true` - Skip Docker socket validation (not recommended)

## Troubleshooting

1. Check socket permissions:
   ```bash
   ls -la /var/run/docker.sock
   stat -c "%a %U:%G" /var/run/docker.sock
   ```

2. Test Docker access:
   ```bash
   docker version
   ```

3. Check if you're in docker group:
   ```bash
   groups | grep docker
   ```

4. Inside the BOSH director container, check mounted socket:
   ```bash
   docker exec -it <container-id> ls -la /docker.sock
   ```