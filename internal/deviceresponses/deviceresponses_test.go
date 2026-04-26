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

func TestLevelWithActivity(t *testing.T) {
	out := deviceresponses.ComputeEvents("HmIP-BDT", "LEVEL", 0.0, nil)
	if got := out["ACTIVITY_STATE"]; got != 0 {
		t.Fatalf("ACTIVITY_STATE for level 0 = %v, want 0", got)
	}
	out = deviceresponses.ComputeEvents("HmIP-BDT", "LEVEL", 0.5, nil)
	if got := out["ACTIVITY_STATE"]; got != 2 {
		t.Fatalf("ACTIVITY_STATE for non-zero = %v, want 2", got)
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
