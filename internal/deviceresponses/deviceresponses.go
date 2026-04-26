// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package deviceresponses ports pydevccu/device_responses.py: it
// describes how individual device types react to value writes by
// emitting one or more follow-up events.
package deviceresponses

import "sort"

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
	"HmIP-BSM":  stateWithWorking,
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
	"HmIP-BWTH":    thermostatSetpointOnly,
	"HmIP-STH":     thermostatSetpointOnly,
	"HM-CC-RT-DN":  rtdnSetpointAndMode,

	// Sensors with test commands
	"HmIP-SWSD": smokeDetectorTest,

	// Window/door contacts
	"HmIP-SWDO": windowState,
	"HmIP-SRH":  windowState,

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
			out := map[string]any{"LEVEL": v}
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
}

var windowState = map[string]ParameterResponse{
	"STATE": {
		TriggerParam: "STATE",
		ValueTransformer: func(v any, _ map[string]any) map[string]any {
			return map[string]any{"STATE": v}
		},
	},
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
// not listed explicitly.
func Mapping(deviceType, param string) *ParameterResponse {
	if m, ok := deviceResponseMappings[deviceType]; ok {
		if r, ok := m[param]; ok {
			return &r
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
	return s[:len(prefix)] == prefix
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
