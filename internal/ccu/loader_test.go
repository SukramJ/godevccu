// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import "testing"

// TestLoadAllDevicesRestrictIsCaseInsensitive guards against a
// regression where the restrict list was compared byte-exact against
// the filename-derived device-type key: the embedded fixture set spells
// device types inconsistently ("HMIP-PS.json" vs "HmIP-PSMCO.json"),
// so a caller-supplied marketing spelling like "HmIP-PS" must still
// resolve against the all-caps fixture.
func TestLoadAllDevicesRestrictIsCaseInsensitive(t *testing.T) {
	sets, err := loadAllDevices([]string{"HmIP-PS"}, false)
	if err != nil {
		t.Fatalf("loadAllDevices(HmIP-PS): %v", err)
	}
	if len(sets) == 0 {
		t.Fatal("loadAllDevices(HmIP-PS) returned no device sets")
	}

	sets, err = loadAllDevices([]string{"HmIP-SWDO"}, false)
	if err != nil {
		t.Fatalf("loadAllDevices(HmIP-SWDO): %v", err)
	}
	if len(sets) == 0 {
		t.Fatal("loadAllDevices(HmIP-SWDO) returned no device sets")
	}
}

// TestLoadAllDevicesRestrictExactCaseStillWorks locks in that the fix
// does not regress the already-working exact-case lookups.
func TestLoadAllDevicesRestrictExactCaseStillWorks(t *testing.T) {
	sets, err := loadAllDevices([]string{"HmIP-BSM"}, false)
	if err != nil {
		t.Fatalf("loadAllDevices(HmIP-BSM): %v", err)
	}
	if len(sets) == 0 {
		t.Fatal("loadAllDevices(HmIP-BSM) returned no device sets")
	}
}

// TestLoadAllDevicesVariantFileKeepsReportedType locks in that a
// firmware variant stored under its own filename loads next to the
// base variant: the 2-channel HB-LC-Bl1-Velux keeps the TYPE the device
// reports, and both actuator channels are BLIND channels.
func TestLoadAllDevicesVariantFileKeepsReportedType(t *testing.T) {
	sets, err := loadAllDevices([]string{"HB-LC-Bl1-Velux", "HB-LC-Bl1-Velux-2ch"}, false)
	if err != nil {
		t.Fatalf("loadAllDevices(HB-LC-Bl1-Velux*): %v", err)
	}
	blindChannels := map[string]int{}
	for _, set := range sets {
		for _, dev := range set.devices {
			addr, _ := dev["ADDRESS"].(string)
			if addr == set.rootDeviceAddr && dev["TYPE"] != "HB-LC-Bl1-Velux" {
				t.Errorf("%s: root TYPE = %v, want HB-LC-Bl1-Velux", set.deviceTypeKey, dev["TYPE"])
			}
			if dev["TYPE"] == "BLIND" {
				blindChannels[set.deviceTypeKey]++
			}
		}
	}
	want := map[string]int{"HB-LC-Bl1-Velux": 1, "HB-LC-Bl1-Velux-2ch": 2}
	for key, count := range want {
		if blindChannels[key] != count {
			t.Errorf("%s: %d BLIND channels, want %d", key, blindChannels[key], count)
		}
	}
}

// TestLoadAllDevicesHmIPBDTI locks in the HmIP-BDT-I catalogue entry.
// It is not the HmIP-BDT with a suffix: channel 3 is an extra
// MULTI_MODE_INPUT_TRANSMITTER, so the dimmer channels move up by one
// and there are 8 channels instead of 7.
func TestLoadAllDevicesHmIPBDTI(t *testing.T) {
	sets, err := loadAllDevices([]string{"HmIP-BDT-I"}, false)
	if err != nil {
		t.Fatalf("loadAllDevices(HmIP-BDT-I): %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("loadAllDevices(HmIP-BDT-I) returned %d device sets, want 1", len(sets))
	}
	set := sets[0]
	channelTypes := map[string]any{}
	for _, dev := range set.devices {
		addr, _ := dev["ADDRESS"].(string)
		if addr == set.rootDeviceAddr {
			if dev["TYPE"] != "HmIP-BDT-I" {
				t.Errorf("root TYPE = %v, want HmIP-BDT-I", dev["TYPE"])
			}
			continue
		}
		channelTypes[addr[len(set.rootDeviceAddr):]] = dev["TYPE"]
	}
	want := map[string]string{
		":0": "MAINTENANCE",
		":1": "KEY_TRANSCEIVER",
		":2": "KEY_TRANSCEIVER",
		":3": "MULTI_MODE_INPUT_TRANSMITTER",
		":4": "DIMMER_TRANSMITTER",
		":5": "DIMMER_VIRTUAL_RECEIVER",
		":6": "DIMMER_VIRTUAL_RECEIVER",
		":7": "DIMMER_VIRTUAL_RECEIVER",
		":8": "DIMMER_WEEK_PROFILE",
	}
	if len(channelTypes) != len(want) {
		t.Errorf("%d channels, want %d", len(channelTypes), len(want))
	}
	for suffix, typ := range want {
		if channelTypes[suffix] != typ {
			t.Errorf("channel %s TYPE = %v, want %s", suffix, channelTypes[suffix], typ)
		}
	}
}
