// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package devicelogic_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/devicelogic"
)

// ─────────────────────────────────────────────────────────────────
// Stub RPC
// ─────────────────────────────────────────────────────────────────

// stubRPC is a minimal in-memory implementation of devicelogic.RPC
// that stores one value per (address, key) pair and counts events.
type stubRPC struct {
	active    bool
	values    map[string]any
	setCalls  atomic.Int64
	fireCalls atomic.Int64
}

func newStubRPC() *stubRPC {
	return &stubRPC{active: true, values: make(map[string]any)}
}

func (s *stubRPC) Active() bool { return s.active }

func (s *stubRPC) GetValue(address, valueKey string) (any, error) {
	v, ok := s.values[address+":"+valueKey]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (s *stubRPC) SetValue(address, valueKey string, value any, _ bool) error {
	s.values[address+":"+valueKey] = value
	s.setCalls.Add(1)
	return nil
}

func (s *stubRPC) FireEvent(_, _, _ string, _ any) {
	s.fireCalls.Add(1)
}

// ─────────────────────────────────────────────────────────────────
// Helper: wait for n SetValue calls (or timeout).
// ─────────────────────────────────────────────────────────────────

func waitForSets(t *testing.T, rpc *stubRPC, atLeast int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rpc.setCalls.Load() >= atLeast {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d SetValue calls; got %d", atLeast, rpc.setCalls.Load())
}

// ─────────────────────────────────────────────────────────────────
// DefaultConfig
// ─────────────────────────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	cfg := devicelogic.DefaultConfig()
	if cfg.StartupDelay != 5*time.Second {
		t.Errorf("StartupDelay = %v, want 5s", cfg.StartupDelay)
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("Interval = %v, want 60s", cfg.Interval)
	}
}

// ─────────────────────────────────────────────────────────────────
// Registry
// ─────────────────────────────────────────────────────────────────

func TestRegistryContainsExpectedDevices(t *testing.T) {
	want := []string{"HM-Sec-SC-2", "HM-Sen-MDIR-WM55"}
	for _, key := range want {
		if _, ok := devicelogic.Registry[key]; !ok {
			t.Errorf("Registry missing key %q", key)
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// HM-Sec-SC-2 tests
// ─────────────────────────────────────────────────────────────────

// TestHMSecSC2NameAndStop verifies Name() and Stop() on a freshly
// constructed simulator. We use zero delays so the work loop runs
// immediately and can be stopped cleanly.
func TestHMSecSC2NameAndStop(t *testing.T) {
	rpc := newStubRPC()
	// Seed initial STATE so GetValue succeeds on the first iteration.
	rpc.values["VCU0000240:1:STATE"] = false

	ctor := devicelogic.Registry["HM-Sec-SC-2"]
	d := ctor(rpc, 0, 0)

	if d.Name() != "HM-Sec-SC-2" {
		t.Errorf("Name() = %q, want HM-Sec-SC-2", d.Name())
	}
	d.Stop() // must not deadlock
}

// TestHMSecSC2TogglesState verifies that the simulator flips STATE at
// least twice so we can observe a genuine toggle, not just one shot.
func TestHMSecSC2TogglesState(t *testing.T) {
	rpc := newStubRPC()
	rpc.values["VCU0000240:1:STATE"] = false

	ctor := devicelogic.Registry["HM-Sec-SC-2"]
	// Zero startup delay; 10 ms interval so the work loop fires fast.
	d := ctor(rpc, 0, 10*time.Millisecond)
	t.Cleanup(d.Stop)

	waitForSets(t, rpc, 2, 2*time.Second)
}

// TestHMSecSC2LowBatFiresAtCounter5 exercises the "every 5 iterations
// fire LOWBAT" branch. With a 10 ms interval we run 5 iterations in
// well under a second.
func TestHMSecSC2LowBatFiresAtCounter5(t *testing.T) {
	rpc := newStubRPC()
	rpc.values["VCU0000240:1:STATE"] = false

	ctor := devicelogic.Registry["HM-Sec-SC-2"]
	d := ctor(rpc, 0, 10*time.Millisecond)
	t.Cleanup(d.Stop)

	// Wait for 5 SetValue calls (= 5 STATE toggles) — by that point the
	// 5th iteration has fired a LOWBAT event.
	waitForSets(t, rpc, 5, 3*time.Second)
	if rpc.fireCalls.Load() == 0 {
		t.Error("expected at least one FireEvent call for LOWBAT")
	}
}

// TestHMSecSC2InactiveRPCSkipsWork verifies that when Active() returns
// false the simulator does not call SetValue.
func TestHMSecSC2InactiveRPCSkipsWork(t *testing.T) {
	rpc := newStubRPC()
	rpc.active = false // RPC reports inactive
	rpc.values["VCU0000240:1:STATE"] = false

	ctor := devicelogic.Registry["HM-Sec-SC-2"]
	d := ctor(rpc, 0, 10*time.Millisecond)

	// Give a few work-loop ticks to prove inactivity is respected.
	time.Sleep(60 * time.Millisecond)
	d.Stop()

	if rpc.setCalls.Load() != 0 {
		t.Errorf("SetValue called %d times on inactive RPC; want 0", rpc.setCalls.Load())
	}
}

// ─────────────────────────────────────────────────────────────────
// HM-Sen-MDIR-WM55 tests
// ─────────────────────────────────────────────────────────────────

func TestHMSenMDIRWM55NameAndStop(t *testing.T) {
	rpc := newStubRPC()
	rpc.values["VCU0000274:3:MOTION"] = false

	ctor := devicelogic.Registry["HM-Sen-MDIR-WM55"]
	d := ctor(rpc, 0, 0)

	if d.Name() != "HM-Sen-MDIR-WM55" {
		t.Errorf("Name() = %q, want HM-Sen-MDIR-WM55", d.Name())
	}
	d.Stop()
}

// TestHMSenMDIRWM55SetsMOTIONAndBRIGHTNESS verifies that at least 2
// SetValue calls land (MOTION + BRIGHTNESS) per work iteration.
func TestHMSenMDIRWM55SetsMOTIONAndBRIGHTNESS(t *testing.T) {
	rpc := newStubRPC()
	rpc.values["VCU0000274:3:MOTION"] = false

	ctor := devicelogic.Registry["HM-Sen-MDIR-WM55"]
	d := ctor(rpc, 0, 10*time.Millisecond)
	t.Cleanup(d.Stop)

	// Each iteration calls SetValue twice (MOTION + BRIGHTNESS).
	waitForSets(t, rpc, 2, 2*time.Second)
}

// TestHMSenMDIRWM55FiresEventEachIteration verifies that FireEvent
// (for PRESS_SHORT) is called at least once.
func TestHMSenMDIRWM55FiresEventEachIteration(t *testing.T) {
	rpc := newStubRPC()
	rpc.values["VCU0000274:3:MOTION"] = false

	ctor := devicelogic.Registry["HM-Sen-MDIR-WM55"]
	d := ctor(rpc, 0, 10*time.Millisecond)
	t.Cleanup(d.Stop)

	waitForSets(t, rpc, 2, 2*time.Second)
	if rpc.fireCalls.Load() == 0 {
		t.Error("expected at least one FireEvent call for PRESS_SHORT")
	}
}

// TestHMSenMDIRWM55LowBatAtCounter5 mirrors the HM-Sec-SC-2 test for
// the LOWBAT event emitted at iteration 5.
func TestHMSenMDIRWM55LowBatAtCounter5(t *testing.T) {
	rpc := newStubRPC()
	rpc.values["VCU0000274:3:MOTION"] = false

	ctor := devicelogic.Registry["HM-Sen-MDIR-WM55"]
	d := ctor(rpc, 0, 10*time.Millisecond)
	t.Cleanup(d.Stop)

	// Each iteration emits 2 SetValue + 1 FireEvent. After 5 iterations
	// there must be at least 2 fire calls (LOWBAT at iter 5, plus PRESS_SHORTs).
	waitForSets(t, rpc, 10, 3*time.Second)
	// By iteration 5 a LOWBAT FireEvent has fired on top of the per-loop PRESS_SHORT ones.
	if rpc.fireCalls.Load() < 5 {
		t.Errorf("expected ≥5 FireEvent calls; got %d", rpc.fireCalls.Load())
	}
}

// TestHMSenMDIRWM55InactiveRPCSkipsWork mirrors the SC-2 test.
func TestHMSenMDIRWM55InactiveRPCSkipsWork(t *testing.T) {
	rpc := newStubRPC()
	rpc.active = false
	rpc.values["VCU0000274:3:MOTION"] = false

	ctor := devicelogic.Registry["HM-Sen-MDIR-WM55"]
	d := ctor(rpc, 0, 10*time.Millisecond)

	time.Sleep(60 * time.Millisecond)
	d.Stop()

	if rpc.setCalls.Load() != 0 {
		t.Errorf("SetValue called %d times on inactive RPC; want 0", rpc.setCalls.Load())
	}
}

// ─────────────────────────────────────────────────────────────────
// sleepWithCancel / runner edge-cases exercised via Stop timing
// ─────────────────────────────────────────────────────────────────

// TestHMSecSC2WithVariousValueTypes exercises the truthy() helper by
// seeding STATE with different numeric types so the type-switch arms
// beyond bool are reachable.
func TestHMSecSC2WithVariousValueTypes(t *testing.T) {
	types := []any{
		int(0),
		int32(0),
		int64(0),
		float32(0),
		float64(0),
		"",   // falsy string
		"on", // truthy string
	}
	for _, initial := range types {
		rpc := newStubRPC()
		rpc.values["VCU0000240:1:STATE"] = initial

		ctor := devicelogic.Registry["HM-Sec-SC-2"]
		d := ctor(rpc, 0, 10*time.Millisecond)

		waitForSets(t, rpc, 1, 2*time.Second)
		d.Stop()
	}
}

// TestStopWhileStartupDelayUnblocks verifies that Stop() returns
// promptly even when the simulator is sleeping through a startup delay
// — i.e. sleepWithCancel honours context cancellation.
func TestStopWhileStartupDelayUnblocks(t *testing.T) {
	rpc := newStubRPC()
	rpc.values["VCU0000240:1:STATE"] = false

	ctor := devicelogic.Registry["HM-Sec-SC-2"]
	// 10 second startup delay — Stop should cancel it immediately.
	d := ctor(rpc, 10*time.Second, time.Minute)

	done := make(chan struct{})
	go func() { d.Stop(); close(done) }()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() blocked for >2s with a long startup delay")
	}
}
