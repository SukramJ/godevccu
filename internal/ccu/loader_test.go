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
