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
		`conflicts with network (.+) \(.+\): networks have overlapping IPv4`)

	// network with name foo3 already exists
	alreadyExistsCheck = "already exists"
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

func (n Networks) Enable() (NetProps, apiv1.Network, error) {
	if len(n.networks) == 0 {
		return NetProps{}, nil, bosherr.Error("Expected exactly one network; received zero")
	}

	for _, net := range n.networks {
		net.SetPreconfigured()
	}

	netProps := NetProps{Driver: "bridge"}
	network := n.networks.Default()

	err := network.CloudProps().As(&netProps)
	if err != nil {
		return NetProps{}, nil, bosherr.WrapError(err, "Unmarshaling network properties")
	}

	if len(network.Netmask()) == 0 {
		netProps.Name, err = n.createDynamicNetwork(netProps)
		if err != nil {
			return NetProps{}, nil, bosherr.WrapError(err, "Creating dynamic network")
		}
	} else {
		netProps.Name, err = n.createManualNetwork(netProps, network)
		if err != nil {
			return NetProps{}, nil, bosherr.WrapError(err, "Creating manual network")
		}
	}

	return netProps, network, nil
}

func (n Networks) createDynamicNetwork(netProps NetProps) (string, error) {
	if len(netProps.Name) == 0 {
		// todo pick up network name?
		return "", bosherr.Error("Expected network to specify 'name'")
	}

	createOpts := dkrtypes.NetworkCreate{
		Driver: netProps.Driver,

		CheckDuplicate: true,
		EnableIPv6:     false, // todo ipv6 support
		Internal:       false,
		Attachable:     false,
	}

	_, err := n.dkrClient.NetworkCreate(context.TODO(), netProps.Name, createOpts)
	if err != nil {
		if !strings.Contains(err.Error(), alreadyExistsCheck) {
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
		EnableIPv6:     false, // todo ipv6 support
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
