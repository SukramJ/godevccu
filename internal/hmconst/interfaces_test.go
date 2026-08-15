// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package hmconst_test

import (
	"testing"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// TestInterfaceForType pins the classification the multi-interface
// model partitions the device catalogue by. Getting a family wrong
// means a client connecting to one interface sees devices that a real
// CCU serves on a different port.
func TestInterfaceForType(t *testing.T) {
	cases := map[string]string{
		// HomeMatic IP, including the wired and licensed variants.
		"HmIP-SWSD":    hmconst.InterfaceHmIPRF,
		"HmIP-BROLL":   hmconst.InterfaceHmIPRF,
		"HmIPW-DRD3":   hmconst.InterfaceHmIPRF,
		"ALPHA-IP-RBG": hmconst.InterfaceHmIPRF,
		"ELV-SH-BS2":   hmconst.InterfaceHmIPRF,
		// Wired BidCos.
		"HMW-LC-Sw2-DR":  hmconst.InterfaceBidCosWired,
		"HMW-IO-12-Sw14": hmconst.InterfaceBidCosWired,
		// Groups and internal devices.
		"HM-CC-VG-1": hmconst.InterfaceVirtualDevices,
		"INT-CCU":    hmconst.InterfaceVirtualDevices,
		// Everything else is classic RF.
		"HM-LC-Sw1-Pl": hmconst.InterfaceBidCosRF,
		"HM-Sec-SC-2":  hmconst.InterfaceBidCosRF,
		"":             hmconst.InterfaceBidCosRF,
	}
	for deviceType, want := range cases {
		if got := hmconst.InterfaceForType(deviceType); got != want {
			t.Errorf("InterfaceForType(%q) = %q, want %q", deviceType, got, want)
		}
	}
}

// The classification must not depend on the caller's casing, since
// addresses and types are handled case-insensitively throughout.
func TestInterfaceForTypeIsCaseInsensitive(t *testing.T) {
	if got := hmconst.InterfaceForType("hmip-swsd"); got != hmconst.InterfaceHmIPRF {
		t.Errorf("lower-case type classified as %q", got)
	}
	if got := hmconst.InterfaceForType("hmw-lc-sw2-dr"); got != hmconst.InterfaceBidCosWired {
		t.Errorf("lower-case wired type classified as %q", got)
	}
}

// Every interface must have a default port and a description, or the
// inventory reports blanks.
func TestInterfaceMetadataIsComplete(t *testing.T) {
	for _, name := range hmconst.InterfaceOrder {
		if port, ok := hmconst.DefaultInterfacePorts[name]; !ok || port == 0 {
			t.Errorf("interface %q has no default port", name)
		}
		if hmconst.InterfaceDescriptions[name] == "" {
			t.Errorf("interface %q has no description", name)
		}
	}
	if len(hmconst.InterfaceOrder) != len(hmconst.DefaultInterfacePorts) {
		t.Errorf("listing order covers %d interfaces, ports cover %d",
			len(hmconst.InterfaceOrder), len(hmconst.DefaultInterfacePorts))
	}
}

// The ports must be distinct — two interfaces on one port is exactly
// the defect the model exists to fix.
func TestDefaultInterfacePortsAreDistinct(t *testing.T) {
	seen := map[int]string{}
	for name, port := range hmconst.DefaultInterfacePorts {
		if other, clash := seen[port]; clash {
			t.Errorf("interfaces %q and %q share port %d", name, other, port)
		}
		seen[port] = name
	}
}
