package vm

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"
	"github.com/cppforlife/bosh-cpi-go/apiv1"
	dkrclient "github.com/docker/engine-api/client"
	dkrtypes "github.com/docker/engine-api/types"
	dkrnet "github.com/docker/engine-api/types/network"
)

var (
	// cannot create network e93d788114a1985a... (br-e93d788114a1):
	// conflicts with network 6255ad39454ee358... (br-6255ad39454e):
	// networks have overlapping IPv4
	conflictingNetMatch = regexp.MustCompile(
		`conflicts with network (.+) \(.+\): networks have overlapping IPv[46]`)

	// network with name foo3 already exists
	alreadyExistsCheck = "already exists"

	// operation is not permitted on predefined bridge network
	predifinedNetworkCheck = "not permitted on predefined"
)

type Networks struct {
	dkrClient *dkrclient.Client
	uuidGen   boshuuid.Generator
	networks  apiv1.Networks
}

func NewNetworks(
	dkrClient *dkrclient.Client,
	uuidGen boshuuid.Generator,
	networks apiv1.Networks,
) Networks {
	return Networks{dkrClient, uuidGen, networks}
}

func (n Networks) Enable() (string, *dkrnet.NetworkingConfig, error) {
	if len(n.networks) == 0 {
		return "", nil, bosherr.Error("Expected exactly one network; received zero")
	}

	var netConfigPairs []netConfigPair
	onlyHasIPv6Networks := true

	for name, net := range n.networks {
		net.SetPreconfigured()

		netProps, err := n.enableSingleNetwork(net)
		if err != nil {
			return "", nil, bosherr.WrapErrorf(err, "Enabling network '%s'", name)
		}

		netConfigPairs = append(netConfigPairs, netConfigPair{net, netProps})

		if !newIPAddr(net.IP()).IsV6() {
			onlyHasIPv6Networks = false
		}
	}

	networkInitBashCmd := " : " // bash noop

	if onlyHasIPv6Networks {
		// Docker seems to add IPv4 address to the container
		// regardless if one was requested on IPv6 interfaces; remove them.
		// Ignore fe80:: addresses — they appear on ALL interfaces.
		// Similarly we ignore the loopback (::1) interfaces.
		networkInitBashCmd = strings.Join([]string{
			// make sure there are no tabs
			`for DEV in $(grep -E -v "^fe80|^0{31}1" /proc/net/if_inet6 | awk '{print $6}'); do`,
			`ip addr del $(ip -4 addr show $DEV | grep "inet" | awk '{print $2}') dev $DEV`,
			`done`,
		}, "\n")
	}

	return networkInitBashCmd, n.networkingConfig(netConfigPairs), nil
}

type netConfigPair struct {
	Network apiv1.Network
	Props   NetProps
}

func (n Networks) networkingConfig(netConfigPairs []netConfigPair) *dkrnet.NetworkingConfig {
	netConfig := &dkrnet.NetworkingConfig{
		EndpointsConfig: map[string]*dkrnet.EndpointSettings{},
	}

	for _, pair := range netConfigPairs {
		endPtConfig := &dkrnet.EndpointSettings{
			IPAMConfig: &dkrnet.EndpointIPAMConfig{},
		}

		if !pair.Network.IsDynamic() {
			if newIPAddr(pair.Network.IP()).IsV6() {
				endPtConfig.IPAMConfig.IPv6Address = pair.Network.IP()
			} else {
				endPtConfig.IPAMConfig.IPv4Address = pair.Network.IP()
			}
		}

		netConfig.EndpointsConfig[pair.Props.Name] = endPtConfig
	}

	return netConfig
}

func (n Networks) enableSingleNetwork(network apiv1.Network) (NetProps, error) {
	netProps := NetProps{Driver: "bridge"}

	err := network.CloudProps().As(&netProps)
	if err != nil {
		return NetProps{}, bosherr.WrapError(err, "Unmarshaling network properties")
	}

	if network.IsDynamic() {
		netProps.Name, err = n.createDynamicNetwork(netProps)
		if err != nil {
			return NetProps{}, bosherr.WrapError(err, "Creating dynamic network")
		}
	} else {
		netProps.Name, err = n.createManualNetwork(netProps, network)
		if err != nil {
			return NetProps{}, bosherr.WrapError(err, "Creating manual network")
		}
	}

	return netProps, nil
}

func (n Networks) createDynamicNetwork(netProps NetProps) (string, error) {
	if len(netProps.Name) == 0 {
		// todo pick up network name?
		return "", bosherr.Error("Expected network to specify 'name'")
	}

	createOpts := dkrtypes.NetworkCreate{
		Driver: netProps.Driver,

		CheckDuplicate: true,
		EnableIPv6:     netProps.EnableIPv6,
		Internal:       false,
		Attachable:     false,
	}

	_, err := n.dkrClient.NetworkCreate(context.TODO(), netProps.Name, createOpts)
	if err != nil {
		if !(strings.Contains(err.Error(), alreadyExistsCheck) ||
			strings.Contains(err.Error(), predifinedNetworkCheck)) {
			return "", err
		}
	}

	return netProps.Name, nil
}

func (n Networks) createManualNetwork(netProps NetProps, network apiv1.Network) (string, error) {
	name := netProps.Name

	if len(name) == 0 {
		name = network.IPWithSubnetMask() // todo better name?
	}

	createOpts := dkrtypes.NetworkCreate{
		Driver: netProps.Driver,

		CheckDuplicate: true,
		EnableIPv6:     netProps.EnableIPv6 || newIPAddr(network.IP()).IsV6(),
		Internal:       false,
		Attachable:     false,

		IPAM: &dkrnet.IPAM{
			Driver: "default",
			Config: []dkrnet.IPAMConfig{
				{Subnet: network.IPWithSubnetMask()},
			},
		},
	}

	_, err := n.dkrClient.NetworkCreate(context.TODO(), name, createOpts)
	if err != nil {
		// Network with same name was just created
		// todo assume equivalent to our configuration
		if strings.Contains(err.Error(), alreadyExistsCheck) {
			return name, nil
		}

		matches := conflictingNetMatch.FindStringSubmatch(err.Error())
		if len(matches) > 0 {
			if len(matches) != 2 {
				panic(fmt.Sprintf("Internal inconsistency: Expected len(%s matches) == 2:", conflictingNetMatch))
			}

			if len(netProps.Name) > 0 {
				return "", bosherr.WrapErrorf(err,
					"Expected network '%s' to not have subnet '%s' "+
						"while trying to create network '%s' with the same subnet",
					matches[1], network.IPWithSubnetMask(), netProps.Name)
			}

			return matches[1], nil
		}

		return "", err
	}

	return name, nil
}
