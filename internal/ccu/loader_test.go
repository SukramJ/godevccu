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
