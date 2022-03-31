package vm

import (
	dkrnet "github.com/docker/docker/api/types/network"
)

func splitNetworkSettings(netConf *dkrnet.NetworkingConfig) (*dkrnet.NetworkingConfig, map[string]*dkrnet.EndpointSettings) {
	if len(netConf.EndpointsConfig) == 0 {
		return netConf, nil
	}

	for name1, settings1 := range netConf.EndpointsConfig {
		out1 := map[string]*dkrnet.EndpointSettings{name1: settings1}
		out2 := map[string]*dkrnet.EndpointSettings{}

		for name2, settings2 := range netConf.EndpointsConfig {
			if name1 != name2 {
				out2[name2] = settings2
			}
		}

		netConf.EndpointsConfig = out1

		return netConf, out2
	}

	panic("Unreachable")
}
