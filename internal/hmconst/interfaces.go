// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package hmconst

import "strings"

// Interface names.
//
// A CCU runs one process per protocol family, each with its own port,
// its own callback registry and its own set of devices. Clients address
// them by these names — they are the values of Interface.listInterfaces
// and the "interface" field of Device.listAllDetail, and a client that
// does not recognise a name falls back to its own default.
const (
	// InterfaceBidCosRF is the classic HomeMatic RF interface (rfd).
	InterfaceBidCosRF = "BidCos-RF"
	// InterfaceHmIPRF is the HomeMatic IP interface (HMIPServer).
	InterfaceHmIPRF = "HmIP-RF"
	// InterfaceBidCosWired is the wired RS485 interface (hs485d).
	InterfaceBidCosWired = "BidCos-Wired"
	// InterfaceVirtualDevices is the group/heating-group interface the
	// HMServer serves under the /groups path.
	InterfaceVirtualDevices = "VirtualDevices"
)

// InterfaceDescriptions are the human-readable labels a CCU reports
// alongside each interface name.
var InterfaceDescriptions = map[string]string{
	InterfaceBidCosRF:       "BidCos-RF",
	InterfaceHmIPRF:         "HmIP-RF",
	InterfaceBidCosWired:    "BidCos-Wired",
	InterfaceVirtualDevices: "Virtual Devices",
}

// DefaultInterfacePorts are the ports a CCU exposes each interface on.
// The interface processes themselves listen on 32000-range ports and a
// reverse proxy maps these public ones onto them; only the public ones
// are observable through the API, so those are what the simulator
// binds.
var DefaultInterfacePorts = map[string]int{
	InterfaceBidCosRF:       PortRF,
	InterfaceHmIPRF:         PortIP,
	InterfaceBidCosWired:    PortWired,
	InterfaceVirtualDevices: PortGroups,
}

// InterfaceOrder is the sequence a CCU lists its interfaces in.
var InterfaceOrder = []string{
	InterfaceBidCosRF,
	InterfaceVirtualDevices,
	InterfaceHmIPRF,
	InterfaceBidCosWired,
}

// InterfaceForType maps a device type to the interface that serves it.
// The classification follows the type prefix, which is how a CCU
// separates its device universe:
//
//   - HmIP-*, HmIPW-*, ALPHA-IP-* and ELV-* belong to HomeMatic IP
//   - HMW-* are the wired devices
//   - HM-CC-VG-* (heating groups) and INT-* live on the group interface
//   - everything else is classic BidCos-RF
func InterfaceForType(deviceType string) string {
	switch {
	case hasAnyPrefix(deviceType, "HmIPW-"):
		return InterfaceHmIPRF
	case hasAnyPrefix(deviceType, "HmIP-", "ALPHA-IP-", "ELV-"):
		return InterfaceHmIPRF
	case hasAnyPrefix(deviceType, "HMW-", "HMW"):
		return InterfaceBidCosWired
	case hasAnyPrefix(deviceType, "HM-CC-VG-", "INT-"):
		return InterfaceVirtualDevices
	default:
		return InterfaceBidCosRF
	}
}

// hasAnyPrefix reports whether s starts with any of the prefixes,
// case-insensitively.
func hasAnyPrefix(s string, prefixes ...string) bool {
	upper := strings.ToUpper(s)
	for _, p := range prefixes {
		if strings.HasPrefix(upper, strings.ToUpper(p)) {
			return true
		}
	}
	return false
}
