// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package virtualccu_test

import (
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/virtualccu"
)

// startEphemeral spins up a VirtualCCU using ephemeral ports.
func startEphemeral(t *testing.T) *virtualccu.VirtualCCU {
	t.Helper()
	v, err := virtualccu.New(virtualccu.Config{
		Mode:        hmconst.BackendModeOpenCCU,
		Host:        "127.0.0.1",
		XMLRPCPort:  virtualccu.EphemeralPort,
		JSONRPCPort: virtualccu.EphemeralPort,
		Username:    "Admin",
		Password:    "test",
		AuthEnabled: true,
		Devices:     []string{"HmIP-SWSD"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v.XMLRPCAddr() != nil && v.JSONRPCAddr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return v
}

func TestState(t *testing.T) {
	v := startEphemeral(t)
	if v.State() == nil {
		t.Fatal("State() returned nil")
	}
}

func TestSession(t *testing.T) {
	v := startEphemeral(t)
	if v.Session() == nil {
		t.Fatal("Session() returned nil")
	}
}

func TestRPC(t *testing.T) {
	v := startEphemeral(t)
	if v.RPC() == nil {
		t.Fatal("RPC() returned nil after Start")
	}
}

// TestSimulateDeviceEvent proves the VirtualCCU-level convenience
// wrapper reaches RPCFunctions.SimulateDeviceEvent and the resulting
// event is observable through the same callback path a chip-tool
// RECEIVE test would subscribe on.
func TestSimulateDeviceEvent(t *testing.T) {
	v := startEphemeral(t)

	var (
		gotKey   string
		gotValue any
	)
	v.RPC().RegisterParamsetCallback(func(_, _, valueKey string, value any) {
		gotKey, gotValue = valueKey, value
	})

	if err := v.SimulateDeviceEvent("VCU2822385:1", "SMOKE_DETECTOR_ALARM_STATUS", 1); err != nil {
		t.Fatalf("SimulateDeviceEvent: %v", err)
	}
	if gotKey != "SMOKE_DETECTOR_ALARM_STATUS" || gotValue != 1 {
		t.Fatalf("callback got (%q, %v), want (SMOKE_DETECTOR_ALARM_STATUS, 1)", gotKey, gotValue)
	}
}

// TestSimulateDeviceEventBeforeStart guards the not-running error path.
func TestSimulateDeviceEventBeforeStart(t *testing.T) {
	v, err := virtualccu.New(virtualccu.Config{
		Mode: hmconst.BackendModeOpenCCU,
		Host: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.SimulateDeviceEvent("VCU2822385:1", "SMOKE_DETECTOR_ALARM_STATUS", 1); err == nil {
		t.Fatal("expected error before Start")
	}
}

func TestMode(t *testing.T) {
	v, err := virtualccu.New(virtualccu.Config{
		Mode: hmconst.BackendModeCCU,
		Host: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := v.Mode(); got != hmconst.BackendModeCCU {
		t.Fatalf("Mode() = %v, want CCU", got)
	}
}

func TestIsRunning(t *testing.T) {
	v, err := virtualccu.New(virtualccu.Config{
		Mode:        hmconst.BackendModeOpenCCU,
		Host:        "127.0.0.1",
		XMLRPCPort:  virtualccu.EphemeralPort,
		JSONRPCPort: virtualccu.EphemeralPort,
		Devices:     []string{"HmIP-SWSD"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v.IsRunning() {
		t.Fatal("IsRunning should be false before Start")
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !v.IsRunning() {
		t.Fatal("IsRunning should be true after Start")
	}
	_ = v.Stop()
	if v.IsRunning() {
		t.Fatal("IsRunning should be false after Stop")
	}
}

func TestXMLRPCAddrNilBeforeStart(t *testing.T) {
	v, err := virtualccu.New(virtualccu.Config{
		Mode:    hmconst.BackendModeOpenCCU,
		Host:    "127.0.0.1",
		Devices: []string{"HmIP-SWSD"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v.XMLRPCAddr() != nil {
		t.Fatal("XMLRPCAddr() should be nil before Start")
	}
	if v.JSONRPCAddr() != nil {
		t.Fatal("JSONRPCAddr() should be nil before Start")
	}
}

func TestRPCNilBeforeStart(t *testing.T) {
	v, err := virtualccu.New(virtualccu.Config{
		Mode: hmconst.BackendModeOpenCCU,
		Host: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v.RPC() != nil {
		t.Fatal("RPC() should return nil before Start")
	}
}

func TestDoubleStart(t *testing.T) {
	v := startEphemeral(t)
	if err := v.Start(); err == nil {
		t.Fatal("double Start should return error")
	}
}

func TestStopIdempotent(t *testing.T) {
	v := startEphemeral(t)
	if err := v.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := v.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestHomegearMode(t *testing.T) {
	// Homegear mode: no JSON-RPC server.
	v, err := virtualccu.New(virtualccu.Config{
		Mode:        hmconst.BackendModeHomegear,
		Host:        "127.0.0.1",
		XMLRPCPort:  virtualccu.EphemeralPort,
		AuthEnabled: false,
		Devices:     []string{"HmIP-SWSD"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start Homegear: %v", err)
	}
	defer v.Stop() //nolint:errcheck

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && v.XMLRPCAddr() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	if v.XMLRPCAddr() == nil {
		t.Fatal("XMLRPCAddr nil after Start in Homegear mode")
	}
	// JSON-RPC is not started in Homegear mode.
	if v.JSONRPCAddr() != nil {
		t.Fatal("JSONRPCAddr should be nil in Homegear mode")
	}
}

func TestSetupDefaultsOption(t *testing.T) {
	v, err := virtualccu.New(virtualccu.Config{
		Mode:          hmconst.BackendModeOpenCCU,
		Host:          "127.0.0.1",
		XMLRPCPort:    virtualccu.EphemeralPort,
		JSONRPCPort:   virtualccu.EphemeralPort,
		SetupDefaults: true,
		Devices:       []string{"HmIP-SWSD"},
	})
	if err != nil {
		t.Fatalf("New with SetupDefaults: %v", err)
	}
	// Programs etc. should be pre-populated.
	if len(v.State().Programs()) == 0 {
		t.Fatal("SetupDefaults did not seed programs")
	}
}

func TestDefaultsFunction(t *testing.T) {
	cfg := virtualccu.Defaults()
	if cfg.Mode == 0 {
		t.Fatal("Defaults() returned zero mode")
	}
	if cfg.Host == "" {
		t.Fatal("Defaults() returned empty host")
	}
	if cfg.XMLRPCPort <= 0 {
		t.Fatal("Defaults() returned non-positive XMLRPCPort")
	}
}
