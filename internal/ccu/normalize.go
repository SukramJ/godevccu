// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"strings"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// Load-time data normalisation.
//
// The embedded catalogue is imported verbatim from pydevccu, and
// CLAUDE.md forbids editing it in place — script/copy_data.sh would
// overwrite any correction on the next import. So the gaps are closed
// while loading instead, which leaves the fixtures untouched and keeps
// the correction reviewable in one place.
//
// What is off, measured across the embedded set:
//
//   - 25850 parameter descriptions carry no ID, although a CCU reports
//     the parameter name there
//   - 18393 carry UNIT: null, which serialises as <nil/> — legal
//     XML-RPC, but a strict non-Python client rejects it
//   - 1626 BOOL parameters carry a numeric DEFAULT instead of a boolean
//   - 1387 device descriptions carry an empty FIRMWARE, 268 root
//     devices have no AVAILABLE_FIRMWARE at all, and 270 report
//     UPDATABLE as an integer
//
// None of this is observable as an error; it just means a client reads
// blanks where a real CCU reports values.

// fallbackFirmware is the version reported for a device whose
// description carries none. A plausible current firmware — the point is
// that the field is populated and parseable, not which build it names.
const fallbackFirmware = "1.0.0"

// normalizeDeviceSet fills the gaps of one loaded device type.
func normalizeDeviceSet(set *loadedDeviceSet) {
	for _, device := range set.devices {
		normalizeDeviceDescription(device)
	}
	for _, paramsets := range set.paramsetByAddr {
		for _, raw := range paramsets {
			params, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for name, entry := range params {
				if description, ok := entry.(map[string]any); ok {
					normalizeParameter(name, description)
				}
			}
		}
	}
}

// normalizeDeviceDescription completes the firmware fields a client
// reads off a device.
func normalizeDeviceDescription(device map[string]any) {
	address, _ := device[hmconst.AttrAddress].(string)
	isRoot := address != "" && !strings.Contains(address, ":")

	if firmware, ok := device["FIRMWARE"].(string); !ok || firmware == "" {
		device["FIRMWARE"] = fallbackFirmware
	}
	if isRoot {
		if available, ok := device["AVAILABLE_FIRMWARE"].(string); !ok || available == "" {
			// No update pending: a CCU reports the installed version.
			device["AVAILABLE_FIRMWARE"] = device["FIRMWARE"]
		}
	}
	// UPDATABLE is a boolean on the wire; the older catalogue entries
	// carry 0/1.
	if updatable, present := device["UPDATABLE"]; present {
		if _, isBool := updatable.(bool); !isBool {
			device["UPDATABLE"] = asBoolValue(updatable)
		}
	}
}

// normalizeParameter completes one parameter description.
func normalizeParameter(name string, description map[string]any) {
	// A CCU reports the parameter's own name as its ID.
	if id, ok := description["ID"].(string); !ok || id == "" {
		description["ID"] = name
	}
	// UNIT: null serialises as <nil/>; an absent unit is an empty
	// string on a CCU.
	if unit, present := description["UNIT"]; present && unit == nil {
		description["UNIT"] = ""
	}
	if typeName, _ := description["TYPE"].(string); typeName == hmconst.ParamsetTypeBool {
		for _, field := range []string{"DEFAULT", "MIN", "MAX"} {
			if value, present := description[field]; present {
				if _, isBool := value.(bool); !isBool {
					description[field] = asBoolValue(value)
				}
			}
		}
	}
}

// asBoolValue coerces a catalogue value to a bool.
func asBoolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		return x == "1" || strings.EqualFold(x, "true")
	default:
		return false
	}
}
