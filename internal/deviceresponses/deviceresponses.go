// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package deviceresponses ports pydevccu/device_responses.py: it
// describes how individual device types react to value writes by
// emitting one or more follow-up events.
package deviceresponses

import (
	"sort"
	"strings"
)

// Transformer turns a trigger value plus the current paramset into the
// follow-up events that the device emits. Returning nil is equivalent
// to returning a single-entry map echoing the trigger.
type Transformer func(triggerValue any, currentValues map[string]any) map[string]any

// ParameterResponse describes the reaction of a device to one
// parameter write.
type ParameterResponse struct {
	TriggerParam string
	// ResponseParams is reserved for future use (extra parameters to
	// emit independently of the transformer); pydevccu carries the
	// field as well even though most entries leave it empty.
	ResponseParams []string
	// ValueTransformer computes the follow-up event values. May be
	// nil — see [computeEvents] for the default behaviour.
	ValueTransformer Transformer
	// EchoTrigger forces the trigger value to also appear in the
	// emitted event map. Mirrors the same flag in the Python
	// dataclass.
	EchoTrigger bool
}

// deviceResponseMappings groups the parameter responses by device-type
// (or device-type prefix). Order is irrelevant because lookup happens
// via [Mapping]; comments retain the structure of the Python source.
var deviceResponseMappings = map[string]map[string]ParameterResponse{
	// Switches
	"HmIP-PS":   stateWithWorking,
	"HmIP-PSM":  stateWithWorking,
	"HmIP-BSM":  bsmResponses,
	"HmIP-FSM":  stateWithWorking,
	"HmIP-PCBS": stateWithWorking,
	"HM-LC-Sw":  stateWithWorking,

	// Dimmers
	"HmIP-BDT":  levelWithActivity,
	"HmIP-PDT":  levelWithActivity,
	"HmIP-FDT":  levelWithActivity,
	"HM-LC-Dim": levelToLevelReal,

	// Blinds / shutters
	"HmIP-BROLL": blindLevel,
	"HmIP-FROLL": blindLevelLevelOnly,
	"HmIP-BBL":   blindLevel,
	"HmIP-FBL":   blindLevel,
	"HM-LC-Bl1":  levelToLevelReal,

	// Thermostats
	"HmIP-eTRV":    thermostatSetpointWithControlMode,
	"HmIP-HEATING": thermostatSetpointOnly,
	"HmIP-WTH":     thermostatSetpointOnly,
	"HmIP-BWTH":    bwthResponses,
	"HmIP-STH":     thermostatSetpointOnly,
	"HM-CC-RT-DN":  rtdnSetpointAndMode,

	// Sensors with test commands
	"HmIP-SWSD": smokeDetectorTest,

	// Motion / illumination sensors
	"HmIP-SMI": motionSensor,

	// Window/door contacts
	"HmIP-SWDO": windowState,
	"HmIP-SRH":  windowState,

	// Climate sensors (CO2 / VOC concentration)
	"HmIP-SCTH230": concentrationSensor,

	// Lock actuators
	"HmIP-DLD": lockTargetLevel,
}

// Sub-tables. Defined as values rather than via inline literals so the
// rendering reads close to the Python source.
var stateWithWorking = map[string]ParameterResponse{
	"STATE": {
		TriggerParam: "STATE",
		ValueTransformer: func(v any, _ map[string]any) map[string]any {
			return map[string]any{"STATE": v, "WORKING": false}
		},
	},
}

// bsmResponses is HmIP-BSM's own table rather than a reuse of
// stateWithWorking: the energy-metering channel (ENERGIE_METER_TRANSMITTER)
// reports POWER/ENERGY_COUNTER/VOLTAGE/CURRENT/FREQUENCY as read-only
// telemetry (ops=RE) that a real device originates on its own. These
// are single-key echo entries — the simulator has no synthetic energy
// model, it just relays whatever value a test injects (typically via
// ccu.RPCFunctions.SimulateDeviceEvent). Kept as its own map, instead of
// adding these keys to the shared stateWithWorking table, so plain
// switches without an energy-metering channel don't gain phantom
// parameters.
var bsmResponses = map[string]ParameterResponse{
	"STATE":          stateWithWorking["STATE"],
	"POWER":          {TriggerParam: "POWER", EchoTrigger: true},
	"ENERGY_COUNTER": {TriggerParam: "ENERGY_COUNTER", EchoTrigger: true},
	"VOLTAGE":        {TriggerParam: "VOLTAGE", EchoTrigger: true},
	"CURRENT":        {TriggerParam: "CURRENT", EchoTrigger: true},
	"FREQUENCY":      {TriggerParam: "FREQUENCY", EchoTrigger: true},
}

var levelToLevelReal = map[string]ParameterResponse{
	"LEVEL": {
		TriggerParam: "LEVEL",
		ValueTransformer: func(v any, _ map[string]any) map[string]any {
			return map[string]any{"LEVEL": v}
		},
	},
}

var levelWithActivity = map[string]ParameterResponse{
	"LEVEL": {
		TriggerParam: "LEVEL",
		ValueTransformer: func(v any, _ map[string]any) map[string]any {
			activity := 2
			if isZero(v) {
				activity = 0
			}
			return map[string]any{"LEVEL": v, "ACTIVITY_STATE": activity}
		},
	},
}

var blindLevel = map[string]ParameterResponse{
	"LEVEL": {
		TriggerParam: "LEVEL",
		ValueTransformer: func(v any, current map[string]any) map[string]any {
			// Same shape as levelWithActivity: HmIP-BROLL/HmIP-BBL/
			// HmIP-FBL report ACTIVITY_STATE alongside LEVEL too, so a
			// LEVEL write must synthesize it here as well. The ENUM
			// index differs from the dimmer mapping (1 = moving, not
			// levelWithActivity's 2) — a blind's ACTIVITY_STATE only
			// distinguishes moving vs. idle from a LEVEL write, it
			// does not know the direction (UP=1/DOWN=2 in the wire
			// enum) without a target LEVEL to compare against.
			activity := 1
			if isZero(v) {
				activity = 0
			}
			out := map[string]any{"LEVEL": v, "ACTIVITY_STATE": activity}
			if l2, ok := current["LEVEL_2"]; ok {
				out["LEVEL_2"] = l2
			}
			return out
		},
	},
	"LEVEL_2": {TriggerParam: "LEVEL_2", EchoTrigger: true},
}

var blindLevelLevelOnly = map[string]ParameterResponse{
	"LEVEL": {
		TriggerParam: "LEVEL",
		ValueTransformer: func(v any, current map[string]any) map[string]any {
			out := map[string]any{"LEVEL": v}
			if l2, ok := current["LEVEL_2"]; ok {
				out["LEVEL_2"] = l2
			}
			return out
		},
	},
}

var thermostatSetpointOnly = map[string]ParameterResponse{
	"SET_POINT_TEMPERATURE": {
		TriggerParam: "SET_POINT_TEMPERATURE",
		ValueTransformer: func(v any, current map[string]any) map[string]any {
			mode := lookupOrDefault(current, "CONTROL_MODE", 1)
			return map[string]any{
				"SET_POINT_TEMPERATURE": v,
				"CONTROL_MODE":          mode,
			}
		},
	},
}

// bwthResponses is HmIP-BWTH's own table rather than a reuse of
// thermostatSetpointOnly: the wall-mount thermostat channel also
// reports ACTUAL_TEMPERATURE and HUMIDITY as read-only telemetry
// (ops=RE) that a real device originates on its own. Single-key echo
// entries document that these are simulator-settable via
// ccu.RPCFunctions.SimulateDeviceEvent rather than relying on the
// untested default echo fallback.
var bwthResponses = map[string]ParameterResponse{
	"SET_POINT_TEMPERATURE": thermostatSetpointOnly["SET_POINT_TEMPERATURE"],
	"ACTUAL_TEMPERATURE":    {TriggerParam: "ACTUAL_TEMPERATURE", EchoTrigger: true},
	"HUMIDITY":              {TriggerParam: "HUMIDITY", EchoTrigger: true},
}

var thermostatSetpointWithControlMode = map[string]ParameterResponse{
	"SET_POINT_TEMPERATURE": {
		TriggerParam: "SET_POINT_TEMPERATURE",
		ValueTransformer: func(v any, current map[string]any) map[string]any {
			mode := lookupOrDefault(current, "CONTROL_MODE", 1)
			return map[string]any{
				"SET_POINT_TEMPERATURE": v,
				"CONTROL_MODE":          mode,
			}
		},
	},
	"CONTROL_MODE": {TriggerParam: "CONTROL_MODE", EchoTrigger: true},
}

var rtdnSetpointAndMode = map[string]ParameterResponse{
	"SET_TEMPERATURE": {TriggerParam: "SET_TEMPERATURE", EchoTrigger: true},
	"CONTROL_MODE":    {TriggerParam: "CONTROL_MODE", EchoTrigger: true},
}

var smokeDetectorTest = map[string]ParameterResponse{
	"SMOKE_DETECTOR_COMMAND": {
		TriggerParam: "SMOKE_DETECTOR_COMMAND",
		ValueTransformer: func(_ any, _ map[string]any) map[string]any {
			return map[string]any{
				"SMOKE_DETECTOR_ALARM_STATUS": 0,
				"SMOKE_DETECTOR_TEST_RESULT":  0,
			}
		},
	},
	// SMOKE_DETECTOR_ALARM_STATUS is also read-only telemetry (ops=RE)
	// in its own right — a real detector originates an alarm state
	// change on its own, independently of SMOKE_DETECTOR_COMMAND's
	// test-trigger reset above. Single-key echo, same as the default
	// fallback, kept explicit so the contract is documented and tested.
	"SMOKE_DETECTOR_ALARM_STATUS": {TriggerParam: "SMOKE_DETECTOR_ALARM_STATUS", EchoTrigger: true},
	// LOW_BAT lives on the MAINTENANCE channel (ch0) of every
	// battery-powered HmIP device; PARENT_TYPE routes ch0 writes
	// through the owning device type's table (see
	// RPCFunctions.deviceTypeForAddressLocked), so it is registered
	// here rather than under a synthetic "MAINTENANCE" entry.
	"LOW_BAT": {TriggerParam: "LOW_BAT", EchoTrigger: true},
}

// motionSensor documents that HmIP-SMI's MOTION and CURRENT_ILLUMINATION
// are read-only telemetry (ops=RE) a real sensor originates on its own.
var motionSensor = map[string]ParameterResponse{
	"MOTION":               {TriggerParam: "MOTION", EchoTrigger: true},
	"CURRENT_ILLUMINATION": {TriggerParam: "CURRENT_ILLUMINATION", EchoTrigger: true},
	"LOW_BAT":              {TriggerParam: "LOW_BAT", EchoTrigger: true},
}

// concentrationSensor documents that HmIP-SCTH230's CO2/VOC CONCENTRATION
// reading is read-only telemetry (ops=RE) a real sensor originates on
// its own.
var concentrationSensor = map[string]ParameterResponse{
	"CONCENTRATION": {TriggerParam: "CONCENTRATION", EchoTrigger: true},
}

var windowState = map[string]ParameterResponse{
	"STATE": {
		TriggerParam: "STATE",
		ValueTransformer: func(v any, _ map[string]any) map[string]any {
			return map[string]any{"STATE": v}
		},
	},
	// LOW_BAT lives on the MAINTENANCE channel (ch0); see the comment
	// on smokeDetectorTest's LOW_BAT entry above for why it belongs
	// in the owning device type's table.
	"LOW_BAT": {TriggerParam: "LOW_BAT", EchoTrigger: true},
}

var lockTargetLevel = map[string]ParameterResponse{
	"LOCK_TARGET_LEVEL": {
		TriggerParam:   "LOCK_TARGET_LEVEL",
		ResponseParams: []string{"LOCK_STATE"},
		ValueTransformer: func(v any, _ map[string]any) map[string]any {
			state := 2 // unlocked
			if isZero(v) {
				state = 1 // locked
			}
			return map[string]any{"LOCK_STATE": state}
		},
	},
}

// Mapping returns the response definition for (deviceType, param), or
// nil when nothing is registered.
//
// Lookup uses an exact match first and then falls back to the longest
// registered prefix that deviceType starts with — so "HmIP-PSM" still
// resolves through the "HmIP-PS" entry even though the longer form is
// not listed explicitly. Both the exact and the prefix match are
// case-insensitive: the embedded device-description fixtures spell the
// TYPE attribute inconsistently (e.g. "HMIP-PS" in HMIP-PS.json vs.
// "HmIP-BSM" in HmIP-BSM.json), while this table always uses the
// marketing spelling, so a byte-exact comparison silently drops entries
// like Mapping("HMIP-PS", "STATE").
func Mapping(deviceType, param string) *ParameterResponse {
	for k, m := range deviceResponseMappings {
		if strings.EqualFold(k, deviceType) {
			if r, ok := m[param]; ok {
				return &r
			}
			break
		}
	}
	keys := make([]string, 0, len(deviceResponseMappings))
	for k := range deviceResponseMappings {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, prefix := range keys {
		if startsWith(deviceType, prefix) {
			if r, ok := deviceResponseMappings[prefix][param]; ok {
				return &r
			}
		}
	}
	return nil
}

// ComputeEvents returns the follow-up events emitted when param is
// written on a device of deviceType. When no mapping is registered the
// trigger event is echoed verbatim — same default as pydevccu.
func ComputeEvents(deviceType, param string, value any, current map[string]any) map[string]any {
	r := Mapping(deviceType, param)
	if r == nil {
		return map[string]any{param: value}
	}
	var events map[string]any
	if r.ValueTransformer != nil {
		events = r.ValueTransformer(value, current)
	}
	if events == nil {
		events = map[string]any{param: value}
	}
	if r.EchoTrigger {
		if _, ok := events[param]; !ok {
			events[param] = value
		}
	}
	return events
}

// ─────────────────────────────────────────────────────────────────
// Internals
// ─────────────────────────────────────────────────────────────────

func startsWith(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

func isZero(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case int:
		return x == 0
	case int32:
		return x == 0
	case int64:
		return x == 0
	case float32:
		return x == 0
	case float64:
		return x == 0
	case bool:
		return !x
	case string:
		return x == ""
	}
	return false
}

func lookupOrDefault(current map[string]any, key string, def any) any {
	if current == nil {
		return def
	}
	if v, ok := current[key]; ok {
		return v
	}
	return def
}
