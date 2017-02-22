# Random snippets

```
$ docker network create -d overlay --subnet=10.10.0.2/16 net2
$ docker network create -d overlay --subnet=172.16.0.0/16 net2
```

```
$ gunzip -d < ~/Downloads/bosh-stemcell-3147-warden-boshlite-ubuntu-trusty-go_agent/image | docker import - bosh-stemcell2
```

```
$ docker run -dit -v vol1:/tmp/lol -e constraint:node==e8edf9ce-7819-427f-bf84-af1684689938 bosh-stemcell:18ac6548-e613-456b-5c27-b1063a009d0a bash
```

```
func (f Factory) findNodeWithDisk(diskCID string) {
  info, err := f.dkrClient.Info()
  if err != nil {
    return Container{}, bosherr.WrapError(err, "Fetching info")
  }

  nodes := []string{}

  // [2]string{" 4f9b707f-625c-4ae1-ae9c-0654e61ac6c1", "10.244.1.4:4243"}
  // [2]string{"  └ Status", "Healthy"}
  for i, part := range info.SystemStatus {
    if strings.Contains(part[0], "Status") {
      nodes = append(nodes, strings.TrimSpace(info.SystemStatus[i-1][0]))
    }
  }

  f.logger.Debug(f.logTag, "Found nodes: %#v", nodes)

  return node
}
```

```
Attaching disk 'd5b88b2e-fab6-427e-71ef-61007a2dd317' to VM 'd15c95fc-d120-4460-7a1c-a4223fc0ad5c': Restarting by recreating: Disposing of container before disk attachment: Killing container: Error response from daemon: Container d15c95fc-d120-4460-7a1c-a4223fc0ad5c running on unhealthy node 4f9b707f-625c-4ae1-ae9c-0654e61ac6c1

Attaching disk 'd5b88b2e-fab6-427e-71ef-61007a2dd317' to VM '7198e9d7-120e-4e86-6693-55dd5162f027': Restarting by recreating: Disposing of container before disk attachment: Error response from daemon: Cannot connect to the docker engine endpoint

Creating VM with agent ID '44875d58-8705-4fe7-bc31-68b13f8f2994': Creating container: Error response from daemon: Container created but refresh didn't report it back
```
