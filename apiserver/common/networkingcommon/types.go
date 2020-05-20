// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package networkingcommon

import (
	"net"

	"github.com/juju/collections/set"
	"github.com/juju/errors"
	"github.com/juju/names/v4"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/core/life"
	corenetwork "github.com/juju/juju/core/network"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/network"
	providercommon "github.com/juju/juju/provider/common"
	"github.com/juju/juju/state"
)

// BackingSubnet defines the methods supported by a Subnet entity
// stored persistently.
//
// TODO(dimitern): Once the state backing is implemented, remove this
// and just use *state.Subnet.
type BackingSubnet interface {
	ID() string
	CIDR() string
	VLANTag() int
	ProviderId() corenetwork.Id
	ProviderNetworkId() corenetwork.Id
	AvailabilityZones() []string
	Status() string
	SpaceName() string
	SpaceID() string
	Life() life.Value
}

// BackingSubnetInfo describes a single subnet to be added in the
// backing store.
//
// TODO(dimitern): Replace state.SubnetInfo with this and remove
// BackingSubnetInfo, once the rest of state backing methods and the
// following pre-reqs are done:
// * Subnets need a reference count to calculate Status.
// * ensure EC2 and MAAS providers accept empty IDs as Subnets() args
//   and return all subnets, including the AvailabilityZones (for EC2;
//   empty for MAAS as zones are orthogonal to networks).
type BackingSubnetInfo struct {
	// ProviderId is a provider-specific network id. This may be empty.
	ProviderId corenetwork.Id

	// ProviderNetworkId is the id of the network containing this
	// subnet from the provider's perspective. It can be empty if the
	// provider doesn't support distinct networks.
	ProviderNetworkId corenetwork.Id

	// CIDR of the network, in 123.45.67.89/24 format.
	CIDR string

	// VLANTag needs to be between 1 and 4094 for VLANs and 0 for normal
	// networks. It's defined by IEEE 802.1Q standard.
	VLANTag int

	// AvailabilityZones describes which availability zone(s) this
	// subnet is in. It can be empty if the provider does not support
	// availability zones.
	AvailabilityZones []string

	// SpaceName holds the juju network space this subnet is
	// associated with. Can be empty if not supported.
	SpaceName string
	SpaceID   string

	// Status holds the status of the subnet. Normally this will be
	// calculated from the reference count and Life of a subnet.
	Status string

	// Live holds the life of the subnet
	Life life.Value
}

// BackingSpace defines the methods supported by a Space entity stored
// persistently.
type BackingSpace interface {
	// ID returns the ID of the space.
	Id() string

	// Name returns the space name.
	Name() string

	// Subnets returns the subnets in the space
	Subnets() ([]BackingSubnet, error)

	// ProviderId returns the network ID of the provider
	ProviderId() corenetwork.Id
}

// NetworkBacking defines the methods needed by the API facade to store and
// retrieve information from the underlying persistency layer (state
// DB).
type NetworkBacking interface {
	environs.EnvironConfigGetter

	// AvailabilityZones returns all cached availability zones (i.e.
	// not from the provider, but in state).
	AvailabilityZones() ([]providercommon.AvailabilityZone, error)

	// SetAvailabilityZones replaces the cached list of availability
	// zones with the given zones.
	SetAvailabilityZones([]providercommon.AvailabilityZone) error

	// AddSpace creates a space
	AddSpace(string, corenetwork.Id, []string, bool) (BackingSpace, error)

	// AllSpaces returns all known Juju network spaces.
	AllSpaces() ([]BackingSpace, error)

	// AddSubnet creates a backing subnet for an existing subnet.
	AddSubnet(BackingSubnetInfo) (BackingSubnet, error)

	// AllSubnets returns all backing subnets.
	AllSubnets() ([]BackingSubnet, error)

	SubnetByCIDR(cidr string) (BackingSubnet, error)

	// ModelTag returns the tag of the model this state is associated to.
	ModelTag() names.ModelTag
}

// BackingSubnetToParamsSubnetV2 converts a network backing subnet to the new
// version of the subnet API parameter.
func BackingSubnetToParamsSubnetV2(subnet BackingSubnet) params.SubnetV2 {
	return params.SubnetV2{
		ID:     subnet.ID(),
		Subnet: BackingSubnetToParamsSubnet(subnet),
	}
}

func BackingSubnetToParamsSubnet(subnet BackingSubnet) params.Subnet {
	return params.Subnet{
		CIDR:              subnet.CIDR(),
		VLANTag:           subnet.VLANTag(),
		ProviderId:        subnet.ProviderId().String(),
		ProviderNetworkId: subnet.ProviderNetworkId().String(),
		Zones:             subnet.AvailabilityZones(),
		Status:            subnet.Status(),
		SpaceTag:          names.NewSpaceTag(subnet.SpaceName()).String(),
		Life:              subnet.Life(),
	}
}

// NetworkInterfacesToStateArgs splits the given interface list into a slice of
// state.LinkLayerDeviceArgs and a slice of state.LinkLayerDeviceAddress.
func NetworkInterfacesToStateArgs(ifaces corenetwork.InterfaceInfos) (
	[]state.LinkLayerDeviceArgs,
	[]state.LinkLayerDeviceAddress,
) {
	var devicesArgs []state.LinkLayerDeviceArgs
	var devicesAddrs []state.LinkLayerDeviceAddress

	logger.Tracef("transforming network interface list to state args: %+v", ifaces)
	seenDeviceNames := set.NewStrings()
	for _, iface := range ifaces {
		logger.Tracef("transforming device %q", iface.InterfaceName)
		if !seenDeviceNames.Contains(iface.InterfaceName) {
			// First time we see this, add it to devicesArgs.
			seenDeviceNames.Add(iface.InterfaceName)
			var mtu uint
			if iface.MTU >= 0 {
				mtu = uint(iface.MTU)
			}
			args := state.LinkLayerDeviceArgs{
				Name:        iface.InterfaceName,
				MTU:         mtu,
				ProviderID:  iface.ProviderId,
				Type:        corenetwork.LinkLayerDeviceType(iface.InterfaceType),
				MACAddress:  iface.MACAddress,
				IsAutoStart: !iface.NoAutoStart,
				IsUp:        !iface.Disabled,
				ParentName:  iface.ParentInterfaceName,
			}
			logger.Tracef("state device args for device: %+v", args)
			devicesArgs = append(devicesArgs, args)
		}

		cidrAddress, err := iface.CIDRAddress()
		if err != nil {
			logger.Warningf("ignoring address for device %q: %v", iface.InterfaceName, err)
			continue
		}

		var derivedConfigMethod corenetwork.AddressConfigMethod
		switch method := corenetwork.AddressConfigMethod(iface.ConfigType); method {
		case corenetwork.StaticAddress, corenetwork.DynamicAddress,
			corenetwork.LoopbackAddress, corenetwork.ManualAddress:
			derivedConfigMethod = method
		case "dhcp": // awkward special case
			derivedConfigMethod = corenetwork.DynamicAddress
		default:
			derivedConfigMethod = corenetwork.StaticAddress
		}

		addr := state.LinkLayerDeviceAddress{
			DeviceName:        iface.InterfaceName,
			ProviderID:        iface.ProviderAddressId,
			ProviderNetworkID: iface.ProviderNetworkId,
			ProviderSubnetID:  iface.ProviderSubnetId,
			ConfigMethod:      derivedConfigMethod,
			CIDRAddress:       cidrAddress,
			DNSServers:        iface.DNSServers.ToIPAddresses(),
			DNSSearchDomains:  iface.DNSSearchDomains,
			GatewayAddress:    iface.GatewayAddress.Value,
			IsDefaultGateway:  iface.IsDefaultGateway,
		}
		logger.Tracef("state address args for device: %+v", addr)
		devicesAddrs = append(devicesAddrs, addr)
	}
	logger.Tracef("seen devices: %+v", seenDeviceNames.SortedValues())
	logger.Tracef("network interface list transformed to state args:\n%+v\n%+v", devicesArgs, devicesAddrs)
	return devicesArgs, devicesAddrs
}

// NetworkingEnvironFromModelConfig constructs and returns
// environs.NetworkingEnviron using the given configGetter. Returns an error
// satisfying errors.IsNotSupported() if the model config does not support
// networking features.
func NetworkingEnvironFromModelConfig(configGetter environs.EnvironConfigGetter) (environs.NetworkingEnviron, error) {
	modelConfig, err := configGetter.ModelConfig()
	if err != nil {
		return nil, errors.Annotate(err, "failed to get model config")
	}
	cloudSpec, err := configGetter.CloudSpec()
	if err != nil {
		return nil, errors.Annotate(err, "failed to get cloudspec")
	}
	if cloudSpec.Type == cloud.CloudTypeCAAS {
		return nil, errors.NotSupportedf("CAAS model %q networking", modelConfig.Name())
	}

	env, err := environs.GetEnviron(configGetter, environs.New)
	if err != nil {
		return nil, errors.Annotate(err, "failed to construct a model from config")
	}
	netEnviron, supported := environs.SupportsNetworking(env)
	if !supported {
		// " not supported" will be appended to the message below.
		return nil, errors.NotSupportedf("model %q networking", modelConfig.Name())
	}
	return netEnviron, nil
}

// NetworkConfigSource defines the necessary calls to obtain the network
// configuration of a machine.
type NetworkConfigSource interface {
	// SysClassNetPath returns the Linux kernel userspace SYSFS path used by
	// this source. DefaultNetworkConfigSource() uses network.SysClassNetPath.
	SysClassNetPath() string

	// Interfaces returns information about all network interfaces on the
	// machine as []net.Interface.
	Interfaces() ([]net.Interface, error)

	// InterfaceAddresses returns information about all addresses assigned to
	// the network interface with the given name.
	InterfaceAddresses(name string) ([]net.Addr, error)
}

func networkToParamsNetworkInfo(info network.NetworkInfo) params.NetworkInfo {
	addresses := make([]params.InterfaceAddress, len(info.Addresses))
	for i, addr := range info.Addresses {
		addresses[i] = params.InterfaceAddress{
			Address: addr.Address,
			CIDR:    addr.CIDR,
		}
	}
	return params.NetworkInfo{
		MACAddress:    info.MACAddress,
		InterfaceName: info.InterfaceName,
		Addresses:     addresses,
	}
}

func MachineNetworkInfoResultToNetworkInfoResult(inResult state.MachineNetworkInfoResult) params.NetworkInfoResult {
	if inResult.Error != nil {
		return params.NetworkInfoResult{Error: common.ServerError(inResult.Error)}
	}
	infos := make([]params.NetworkInfo, len(inResult.NetworkInfos))
	for i, info := range inResult.NetworkInfos {
		infos[i] = networkToParamsNetworkInfo(info)
	}
	return params.NetworkInfoResult{
		Info: infos,
	}
}

func FanConfigToFanConfigResult(config network.FanConfig) params.FanConfigResult {
	result := params.FanConfigResult{make([]params.FanConfigEntry, len(config))}
	for i, entry := range config {
		result.Fans[i] = params.FanConfigEntry{entry.Underlay.String(), entry.Overlay.String()}
	}
	return result
}

func FanConfigResultToFanConfig(config params.FanConfigResult) (network.FanConfig, error) {
	rv := make(network.FanConfig, len(config.Fans))
	for i, entry := range config.Fans {
		_, ipnet, err := net.ParseCIDR(entry.Underlay)
		if err != nil {
			return nil, err
		}
		rv[i].Underlay = ipnet
		_, ipnet, err = net.ParseCIDR(entry.Overlay)
		if err != nil {
			return nil, err
		}
		rv[i].Overlay = ipnet
	}
	return rv, nil
}
