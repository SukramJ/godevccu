// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package deviceresponses_test

import (
	"testing"

	"github.com/SukramJ/godevccu/internal/deviceresponses"
)

func TestStateWithWorking(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-PSM", "STATE", true, nil)
	if got := out["STATE"]; got != true {
		t.Fatalf("STATE = %v, want true", got)
	}
	if got := out["WORKING"]; got != false {
		t.Fatalf("WORKING = %v, want false", got)
	}
}

func TestPrefixMatch(t *testing.T) {
	// "HmIP-PSM" is matched via the "HmIP-PS" prefix entry.
	out := deviceresponses.ComputeEvents("HmIP-PSMUnknown", "STATE", false, nil)
	if got := out["STATE"]; got != false {
		t.Fatalf("STATE = %v, want false", got)
	}
}

// TestLevelWithActivity pins that a dimmer reports an *idle* activity
// state: the simulator applies LEVEL immediately, so the move is over by
// the time the client sees the event. A moving state next to a final
// LEVEL contradicts itself and leaves a client waiting for an end that
// never comes. The moving phase belongs to the opt-in ramp simulation.
func TestLevelWithActivity(t *testing.T) {
	for _, level := range []any{0.0, 0.5, 1.0} {
		out := deviceresponses.ComputeEvents("HmIP-BDT", "LEVEL", level, nil)
		if got := out["ACTIVITY_STATE"]; got != deviceresponses.ActivityIdle {
			t.Fatalf("ACTIVITY_STATE for level %v = %v, want idle", level, got)
		}
		if out["LEVEL"] != level {
			t.Fatalf("LEVEL = %v, want %v", out["LEVEL"], level)
		}
	}
}

func TestUnknownDeviceEchoes(t *testing.T) {
	out := deviceresponses.ComputeEvents("UNKNOWN", "FOO", "bar", nil)
	if len(out) != 1 || out["FOO"] != "bar" {
		t.Fatalf("default echo failed: %+v", out)
	}
}

func TestLockTargetLevel(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-DLD", "LOCK_TARGET_LEVEL", 0, nil)
	if out["LOCK_STATE"] != 1 {
		t.Fatalf("LOCK_STATE for 0 = %v, want 1", out["LOCK_STATE"])
	}
	out = deviceresponses.ComputeEvents("HmIP-DLD", "LOCK_TARGET_LEVEL", 1, nil)
	if out["LOCK_STATE"] != 2 {
		t.Fatalf("LOCK_STATE for 1 = %v, want 2", out["LOCK_STATE"])
	}
}

// TestMappingCaseInsensitive guards against the exact-match / prefix-match
// byte comparisons silently dropping fixtures whose TYPE attribute is
// spelled differently from the marketing casing used in
// deviceResponseMappings (e.g. "HMIP-PS" in HMIP-PS.json's TYPE field vs.
// the "HmIP-PS" table key).
func TestMappingCaseInsensitive(t *testing.T) {
	r := deviceresponses.Mapping("HMIP-PS", "STATE")
	if r == nil {
		t.Fatal("Mapping(\"HMIP-PS\", \"STATE\") = nil, want the stateWithWorking entry")
	}
	if r.TriggerParam != "STATE" {
		t.Fatalf("TriggerParam = %q, want STATE", r.TriggerParam)
	}

	out := deviceresponses.ComputeEvents("HMIP-PS", "STATE", true, nil)
	if out["STATE"] != true || out["WORKING"] != false {
		t.Fatalf("ComputeEvents(\"HMIP-PS\", ...) = %+v, want STATE=true, WORKING=false", out)
	}
}

// TestBlindLevelActivityState pins the shutter counterpart: a LEVEL
// write reports ACTIVITY_STATE alongside it, and that state is idle
// because LEVEL is already final. Reporting "moving" without ever
// closing it out left a cover client stuck on "moving" for the rest of
// the session.
func TestBlindLevelActivityState(t *testing.T) {
	for _, deviceType := range []string{"HmIP-BROLL", "HmIP-BBL", "HmIP-FBL"} {
		for _, level := range []any{0.0, 0.5, 1.0} {
			out := deviceresponses.ComputeEvents(deviceType, "LEVEL", level, nil)
			if got := out["ACTIVITY_STATE"]; got != deviceresponses.ActivityIdle {
				t.Fatalf("%s ACTIVITY_STATE for LEVEL %v = %v, want idle", deviceType, level, got)
			}
		}
	}
}

// TestActivityForMove covers the direction a ramp reports while it is
// under way.
func TestActivityForMove(t *testing.T) {
	cases := []struct {
		target, current any
		want            int
	}{
		{1.0, 0.0, deviceresponses.ActivityUp},
		{0.0, 1.0, deviceresponses.ActivityDown},
		{0.5, 0.5, deviceresponses.ActivityIdle},
		{0.5, nil, deviceresponses.ActivityIdle},
		{"nonsense", 0.0, deviceresponses.ActivityIdle},
	}
	for _, c := range cases {
		if got := deviceresponses.ActivityForMove(c.target, c.current); got != c.want {
			t.Errorf("ActivityForMove(%v, %v) = %d, want %d", c.target, c.current, got, c.want)
		}
	}
}

// TestBWTHTelemetry covers G4: ACTUAL_TEMPERATURE and HUMIDITY are
// read-only telemetry on HmIP-BWTH, settable via the simulator's
// SimulateDeviceEvent primitive rather than a plain (unforced) write.
func TestBWTHTelemetry(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-BWTH", "ACTUAL_TEMPERATURE", 21.5, nil)
	if out["ACTUAL_TEMPERATURE"] != 21.5 {
		t.Fatalf("ACTUAL_TEMPERATURE = %v, want 21.5", out["ACTUAL_TEMPERATURE"])
	}
	out = deviceresponses.ComputeEvents("HmIP-BWTH", "HUMIDITY", 55, nil)
	if out["HUMIDITY"] != 55 {
		t.Fatalf("HUMIDITY = %v, want 55", out["HUMIDITY"])
	}
}

// TestBSMTelemetry covers G4: the HmIP-BSM energy-metering channel's
// POWER/ENERGY_COUNTER/VOLTAGE/CURRENT/FREQUENCY parameters are
// read-only telemetry, simulator-settable via echo entries.
func TestBSMTelemetry(t *testing.T) {
	cases := map[string]any{
		"POWER":          12.3,
		"ENERGY_COUNTER": 456.7,
		"VOLTAGE":        230.0,
		"CURRENT":        0.05,
		"FREQUENCY":      50.0,
	}
	for param, value := range cases {
		out := deviceresponses.ComputeEvents("HmIP-BSM", param, value, nil)
		if out[param] != value {
			t.Fatalf("%s = %v, want %v", param, out[param], value)
		}
	}
}

// TestSWSDAlarmStatusTelemetry covers G4: SMOKE_DETECTOR_ALARM_STATUS
// is read-only telemetry independent of the SMOKE_DETECTOR_COMMAND
// test-trigger entry.
func TestSWSDAlarmStatusTelemetry(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-SWSD", "SMOKE_DETECTOR_ALARM_STATUS", 1, nil)
	if out["SMOKE_DETECTOR_ALARM_STATUS"] != 1 {
		t.Fatalf("SMOKE_DETECTOR_ALARM_STATUS = %v, want 1", out["SMOKE_DETECTOR_ALARM_STATUS"])
	}
}

// TestSMITelemetry covers G4: HmIP-SMI's MOTION and CURRENT_ILLUMINATION
// are read-only telemetry.
func TestSMITelemetry(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-SMI", "MOTION", true, nil)
	if out["MOTION"] != true {
		t.Fatalf("MOTION = %v, want true", out["MOTION"])
	}
	out = deviceresponses.ComputeEvents("HmIP-SMI", "CURRENT_ILLUMINATION", 123.4, nil)
	if out["CURRENT_ILLUMINATION"] != 123.4 {
		t.Fatalf("CURRENT_ILLUMINATION = %v, want 123.4", out["CURRENT_ILLUMINATION"])
	}
}

// TestSWDOStateTelemetry covers G4's SWDO.STATE combo, including the
// all-caps device-type spelling that HMIP-SWDO.json's TYPE attribute
// actually carries (exercising the G2 case-fold fix end-to-end).
func TestSWDOStateTelemetry(t *testing.T) {
	out := deviceresponses.ComputeEvents("HMIP-SWDO", "STATE", 1, nil)
	if out["STATE"] != 1 {
		t.Fatalf("STATE = %v, want 1", out["STATE"])
	}
}

// TestSCTH230ConcentrationTelemetry covers G4: HmIP-SCTH230's CO2/VOC
// CONCENTRATION reading is read-only telemetry.
func TestSCTH230ConcentrationTelemetry(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-SCTH230", "CONCENTRATION", 450.0, nil)
	if out["CONCENTRATION"] != 450.0 {
		t.Fatalf("CONCENTRATION = %v, want 450.0", out["CONCENTRATION"])
	}
}

// TestLowBatteryTelemetry covers G4: LOW_BAT lives on the ch0
// MAINTENANCE channel and is simulator-settable on every device type
// this change wired it up for.
func TestLowBatteryTelemetry(t *testing.T) {
	for _, deviceType := range []string{"HmIP-SWSD", "HMIP-SWDO", "HmIP-SRH", "HmIP-SMI"} {
		out := deviceresponses.ComputeEvents(deviceType, "LOW_BAT", true, nil)
		if out["LOW_BAT"] != true {
			t.Fatalf("%s LOW_BAT = %v, want true", deviceType, out["LOW_BAT"])
		}
	}
}
