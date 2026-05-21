// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package deviceresponses_test

import (
	"testing"

	"github.com/SukramJ/godevccu/internal/deviceresponses"
)

// ─────────────────────────────────────────────────────────────────
// Mapping
// ─────────────────────────────────────────────────────────────────

func TestMappingExactMatch(t *testing.T) {
	r := deviceresponses.Mapping("HmIP-PS", "STATE")
	if r == nil {
		t.Fatal("Mapping(HmIP-PS, STATE) returned nil")
	}
}

func TestMappingPrefixFallback(t *testing.T) {
	// HmIP-PSM is not a key; should fall back to "HmIP-PS".
	r := deviceresponses.Mapping("HmIP-PSM", "STATE")
	if r == nil {
		t.Fatal("Mapping prefix fallback returned nil")
	}
}

func TestMappingNil(t *testing.T) {
	if r := deviceresponses.Mapping("UNKNOWN", "FOO"); r != nil {
		t.Fatalf("expected nil for unknown device, got %+v", r)
	}
}

// ─────────────────────────────────────────────────────────────────
// ComputeEvents – all device tables
// ─────────────────────────────────────────────────────────────────

func TestBlindLevel(t *testing.T) {
	// HmIP-BROLL, LEVEL — should echo LEVEL_2 from current.
	out := deviceresponses.ComputeEvents("HmIP-BROLL", "LEVEL", 0.5,
		map[string]any{"LEVEL_2": 0.3})
	if got := out["LEVEL"]; got != 0.5 {
		t.Fatalf("LEVEL = %v, want 0.5", got)
	}
	if got := out["LEVEL_2"]; got != 0.3 {
		t.Fatalf("LEVEL_2 = %v, want 0.3", got)
	}
}

func TestBlindLevelNoLevel2(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-BROLL", "LEVEL", 0.5, nil)
	if _, ok := out["LEVEL_2"]; ok {
		t.Fatal("LEVEL_2 should not appear when not in current")
	}
}

func TestBlindLevelLevel2Trigger(t *testing.T) {
	// LEVEL_2 trigger with EchoTrigger=true.
	out := deviceresponses.ComputeEvents("HmIP-BROLL", "LEVEL_2", 0.7, nil)
	if got := out["LEVEL_2"]; got != 0.7 {
		t.Fatalf("LEVEL_2 echo = %v, want 0.7", got)
	}
}

func TestBlindLevelLevelOnly(t *testing.T) {
	// HmIP-FROLL uses blindLevelLevelOnly (no LEVEL_2 trigger entry).
	out := deviceresponses.ComputeEvents("HmIP-FROLL", "LEVEL", 1.0,
		map[string]any{"LEVEL_2": 0.5})
	if out["LEVEL"] != 1.0 {
		t.Fatalf("LEVEL = %v", out["LEVEL"])
	}
	if out["LEVEL_2"] != 0.5 {
		t.Fatalf("LEVEL_2 = %v", out["LEVEL_2"])
	}
}

func TestLevelToLevelReal(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-BDT", "LEVEL", 0.8, nil)
	if out["LEVEL"] != 0.8 {
		t.Fatalf("LEVEL = %v", out["LEVEL"])
	}
	if _, ok := out["ACTIVITY_STATE"]; !ok {
		t.Fatal("ACTIVITY_STATE missing from dimmer response")
	}
}

func TestHMDimmerLevelToLevelReal(t *testing.T) {
	// HM-LC-Dim uses levelToLevelReal table.
	out := deviceresponses.ComputeEvents("HM-LC-Dim1", "LEVEL", 0.3, nil)
	if out["LEVEL"] != 0.3 {
		t.Fatalf("LEVEL = %v, want 0.3", out["LEVEL"])
	}
}

func TestThermostatSetpointOnly(t *testing.T) {
	// Without CONTROL_MODE in current → default 1.
	out := deviceresponses.ComputeEvents("HmIP-WTH-1", "SET_POINT_TEMPERATURE", 21.0, nil)
	if out["SET_POINT_TEMPERATURE"] != 21.0 {
		t.Fatalf("SET_POINT_TEMPERATURE = %v", out["SET_POINT_TEMPERATURE"])
	}
	if out["CONTROL_MODE"] != 1 {
		t.Fatalf("CONTROL_MODE = %v, want 1", out["CONTROL_MODE"])
	}
}

func TestThermostatSetpointOnlyWithMode(t *testing.T) {
	// With CONTROL_MODE in current → echoed.
	out := deviceresponses.ComputeEvents("HmIP-WTH", "SET_POINT_TEMPERATURE", 22.0,
		map[string]any{"CONTROL_MODE": 3})
	if out["CONTROL_MODE"] != 3 {
		t.Fatalf("CONTROL_MODE = %v, want 3", out["CONTROL_MODE"])
	}
}

func TestThermostatSetpointWithControlMode(t *testing.T) {
	// HmIP-eTRV — SET_POINT_TEMPERATURE.
	out := deviceresponses.ComputeEvents("HmIP-eTRV", "SET_POINT_TEMPERATURE", 20.0, nil)
	if out["SET_POINT_TEMPERATURE"] != 20.0 {
		t.Fatalf("SET_POINT_TEMPERATURE = %v", out["SET_POINT_TEMPERATURE"])
	}
}

func TestThermostatControlModeEcho(t *testing.T) {
	// HmIP-eTRV — CONTROL_MODE has EchoTrigger=true.
	out := deviceresponses.ComputeEvents("HmIP-eTRV", "CONTROL_MODE", 2, nil)
	if out["CONTROL_MODE"] != 2 {
		t.Fatalf("CONTROL_MODE = %v, want 2", out["CONTROL_MODE"])
	}
}

func TestRtdnSetpointAndMode(t *testing.T) {
	out := deviceresponses.ComputeEvents("HM-CC-RT-DN", "SET_TEMPERATURE", 19.0, nil)
	if out["SET_TEMPERATURE"] != 19.0 {
		t.Fatalf("SET_TEMPERATURE = %v", out["SET_TEMPERATURE"])
	}
}

func TestSmokeDetectorTest(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-SWSD", "SMOKE_DETECTOR_COMMAND", 1, nil)
	if _, ok := out["SMOKE_DETECTOR_ALARM_STATUS"]; !ok {
		t.Fatal("SMOKE_DETECTOR_ALARM_STATUS missing")
	}
	if _, ok := out["SMOKE_DETECTOR_TEST_RESULT"]; !ok {
		t.Fatal("SMOKE_DETECTOR_TEST_RESULT missing")
	}
}

func TestWindowState(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-SWDO", "STATE", 1, nil)
	if out["STATE"] != 1 {
		t.Fatalf("STATE = %v, want 1", out["STATE"])
	}
}

func TestLookupOrDefault(t *testing.T) {
	// thermostatSetpointOnly exercises lookupOrDefault with a nil map.
	out := deviceresponses.ComputeEvents("HmIP-BWTH", "SET_POINT_TEMPERATURE", 21.5, nil)
	if out["CONTROL_MODE"] != 1 {
		t.Fatalf("lookupOrDefault(nil) = %v, want 1", out["CONTROL_MODE"])
	}
}

func TestIsZeroVariousTypes(t *testing.T) {
	// isZero is exercised through levelWithActivity and lockTargetLevel.
	// nil → activity 0 equivalent (via bool false which isZero catches).
	out := deviceresponses.ComputeEvents("HmIP-BDT", "LEVEL", false, nil)
	if out["ACTIVITY_STATE"] != 0 {
		t.Fatalf("ACTIVITY_STATE for bool-false = %v, want 0", out["ACTIVITY_STATE"])
	}
	// int 0.
	out = deviceresponses.ComputeEvents("HmIP-DLD", "LOCK_TARGET_LEVEL", int64(0), nil)
	if out["LOCK_STATE"] != 1 {
		t.Fatalf("LOCK_STATE for int64(0) = %v", out["LOCK_STATE"])
	}
	// float32 0.
	out = deviceresponses.ComputeEvents("HmIP-DLD", "LOCK_TARGET_LEVEL", float32(0), nil)
	if out["LOCK_STATE"] != 1 {
		t.Fatalf("LOCK_STATE for float32(0) = %v", out["LOCK_STATE"])
	}
	// empty string.
	out = deviceresponses.ComputeEvents("HmIP-DLD", "LOCK_TARGET_LEVEL", "", nil)
	if out["LOCK_STATE"] != 1 {
		t.Fatalf("LOCK_STATE for empty string = %v", out["LOCK_STATE"])
	}
	// nil.
	out = deviceresponses.ComputeEvents("HmIP-DLD", "LOCK_TARGET_LEVEL", nil, nil)
	if out["LOCK_STATE"] != 1 {
		t.Fatalf("LOCK_STATE for nil = %v", out["LOCK_STATE"])
	}
}

func TestWindowStateHmIPSRH(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-SRH", "STATE", 2, nil)
	if out["STATE"] != 2 {
		t.Fatalf("STATE = %v, want 2", out["STATE"])
	}
}

func TestComputeEventsEchoTriggerNotDuplicated(t *testing.T) {
	// LEVEL_2 on HmIP-BROLL: EchoTrigger=true, nil transformer.
	// ComputeEvents must echo the trigger value exactly once.
	out := deviceresponses.ComputeEvents("HmIP-BROLL", "LEVEL_2", 0.9, nil)
	if out["LEVEL_2"] != 0.9 {
		t.Fatalf("LEVEL_2 = %v, want 0.9", out["LEVEL_2"])
	}
	if len(out) != 1 {
		t.Fatalf("expected exactly 1 key, got %d: %v", len(out), out)
	}
}

func TestComputeEventsNilTransformerDefaultEcho(t *testing.T) {
	// When transformer is nil and EchoTrigger is false, events falls back
	// to echoing the trigger (the events==nil branch).
	// Find such a case: levelToLevelReal has a transformer so it won't hit nil.
	// Use an unregistered device type with unknown param — hits the r==nil path (echo verbatim).
	out := deviceresponses.ComputeEvents("TOTALLY_UNKNOWN", "MY_PARAM", "val", nil)
	if out["MY_PARAM"] != "val" {
		t.Fatalf("echo = %v", out)
	}
}
