// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package godevccu_test

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/pkg/godevccu"
)

// ─────────────────────────────────────────────────────────────────
// Defaults / DefaultLogicConfig
// ─────────────────────────────────────────────────────────────────

func TestDefaults(t *testing.T) {
	cfg := godevccu.Defaults()
	if cfg.Mode != godevccu.BackendModeOpenCCU {
		t.Errorf("Mode = %v, want BackendModeOpenCCU", cfg.Mode)
	}
	if cfg.XMLRPCPort <= 0 {
		t.Errorf("XMLRPCPort = %d, want >0", cfg.XMLRPCPort)
	}
	if cfg.Host == "" {
		t.Error("Host must not be empty")
	}
}

func TestDefaultLogicConfig(t *testing.T) {
	lc := godevccu.DefaultLogicConfig()
	if lc.StartupDelay != 5*time.Second {
		t.Errorf("StartupDelay = %v, want 5s", lc.StartupDelay)
	}
	if lc.Interval != 60*time.Second {
		t.Errorf("Interval = %v, want 60s", lc.Interval)
	}
}

// ─────────────────────────────────────────────────────────────────
// Re-exported constants
// ─────────────────────────────────────────────────────────────────

func TestReexportedConstants(t *testing.T) {
	if godevccu.Version == "" {
		t.Error("Version must not be empty")
	}
	if godevccu.CCUFirmwareVersion == "" {
		t.Error("CCUFirmwareVersion must not be empty")
	}
	if godevccu.IPLocalhostV4 != "127.0.0.1" {
		t.Errorf("IPLocalhostV4 = %q, want 127.0.0.1", godevccu.IPLocalhostV4)
	}
	if godevccu.IPAnyV4 != "0.0.0.0" {
		t.Errorf("IPAnyV4 = %q, want 0.0.0.0", godevccu.IPAnyV4)
	}
	if godevccu.PortRF != 2001 {
		t.Errorf("PortRF = %d, want 2001", godevccu.PortRF)
	}
}

// ─────────────────────────────────────────────────────────────────
// New — construction only, no Start
// ─────────────────────────────────────────────────────────────────

func TestNewWithEmptyConfig(t *testing.T) {
	// A zero-valued Config should merge defaults without error.
	v, err := godevccu.New(godevccu.Config{})
	if err != nil {
		t.Fatalf("New(Config{}) error: %v", err)
	}
	if v == nil {
		t.Fatal("New returned nil")
	}
	if v.IsRunning() {
		t.Error("IsRunning() = true before Start")
	}
}

func TestNewWithExplicitConfig(t *testing.T) {
	v, err := godevccu.New(godevccu.Config{
		Mode:        godevccu.BackendModeOpenCCU,
		Host:        "127.0.0.1",
		XMLRPCPort:  godevccu.EphemeralPort,
		JSONRPCPort: godevccu.EphemeralPort,
		Username:    "Admin",
		AuthEnabled: false,
		Serial:      "TESTSERIAL01",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v == nil {
		t.Fatal("New returned nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// Start → query → Stop lifecycle round-trip
// ─────────────────────────────────────────────────────────────────

// startFacade spins up a VirtualCCU on ephemeral ports via the public
// facade and registers a Cleanup stop. It waits up to 2s for both
// listeners to become ready.
func startFacade(t *testing.T, mode godevccu.BackendMode) *godevccu.VirtualCCU {
	t.Helper()
	v, err := godevccu.New(godevccu.Config{
		Mode:        mode,
		Host:        "127.0.0.1",
		XMLRPCPort:  godevccu.EphemeralPort,
		JSONRPCPort: godevccu.EphemeralPort,
		AuthEnabled: false,
		Serial:      "TESTFACADE01",
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
		if v.XMLRPCAddr() != nil {
			if mode == godevccu.BackendModeHomegear || v.JSONRPCAddr() != nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return v
}

func TestLifecycleOpenCCU(t *testing.T) {
	v := startFacade(t, godevccu.BackendModeOpenCCU)

	if !v.IsRunning() {
		t.Error("IsRunning() = false after Start")
	}

	xmlAddr := v.XMLRPCAddr()
	if xmlAddr == nil {
		t.Fatal("XMLRPCAddr() = nil after Start")
	}
	tcpAddr, ok := xmlAddr.(*net.TCPAddr)
	if !ok || tcpAddr.Port == 0 {
		t.Fatalf("XMLRPCAddr port = 0 after Start; addr = %v", xmlAddr)
	}

	jsonAddr := v.JSONRPCAddr()
	if jsonAddr == nil {
		t.Fatal("JSONRPCAddr() = nil after Start (OpenCCU mode)")
	}
	jsonTCP, ok := jsonAddr.(*net.TCPAddr)
	if !ok || jsonTCP.Port == 0 {
		t.Fatalf("JSONRPCAddr port = 0 after Start; addr = %v", jsonAddr)
	}

	// Config must reflect the resolved ephemeral ports.
	cfg := v.Config()
	if cfg.XMLRPCPort != tcpAddr.Port {
		t.Errorf("Config().XMLRPCPort = %d, want %d", cfg.XMLRPCPort, tcpAddr.Port)
	}
	if cfg.JSONRPCPort != jsonTCP.Port {
		t.Errorf("Config().JSONRPCPort = %d, want %d", cfg.JSONRPCPort, jsonTCP.Port)
	}
}

func TestLifecycleHomegear(t *testing.T) {
	// Homegear mode starts XML-RPC only; JSONRPCAddr returns nil.
	v := startFacade(t, godevccu.BackendModeHomegear)

	if v.XMLRPCAddr() == nil {
		t.Fatal("XMLRPCAddr() = nil in Homegear mode")
	}
	if v.Mode() != godevccu.BackendModeHomegear {
		t.Errorf("Mode() = %v, want Homegear", v.Mode())
	}
}

func TestDoubleStartReturnsError(t *testing.T) {
	v := startFacade(t, godevccu.BackendModeOpenCCU)

	err := v.Start()
	if err == nil {
		t.Error("expected error on double Start, got nil")
	}
}

func TestStopIdempotent(t *testing.T) {
	v, err := godevccu.New(godevccu.Config{
		Mode:        godevccu.BackendModeOpenCCU,
		Host:        "127.0.0.1",
		XMLRPCPort:  godevccu.EphemeralPort,
		JSONRPCPort: godevccu.EphemeralPort,
		AuthEnabled: false,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := v.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	// Second Stop must be a no-op.
	if err := v.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

// TestStateAccessorAfterNew checks that the State() and Session()
// accessors return non-nil values without requiring Start.
func TestStateAccessorAfterNew(t *testing.T) {
	v, err := godevccu.New(godevccu.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v.State() == nil {
		t.Error("State() = nil")
	}
	if v.Session() == nil {
		t.Error("Session() = nil")
	}
}

// TestBackendModeConstants verifies re-exported mode constants
// so consumers don't need to reach into internal/hmconst.
func TestBackendModeConstants(t *testing.T) {
	if godevccu.BackendModeHomegear == 0 {
		t.Error("BackendModeHomegear must be non-zero")
	}
	if godevccu.BackendModeCCU == 0 {
		t.Error("BackendModeCCU must be non-zero")
	}
	if godevccu.BackendModeOpenCCU == 0 {
		t.Error("BackendModeOpenCCU must be non-zero")
	}
}

// TestRealismDefaultsAreOff pins the contract: a zero-valued Config
// enables none of the CCU-realism behaviours, so an existing consumer
// sees exactly what it saw before.
func TestRealismDefaultsAreOff(t *testing.T) {
	var cfg godevccu.Config
	if cfg.Realism.Any() {
		t.Fatalf("zero Config already opts into realism: %+v", cfg.Realism)
	}
	if cfg.InterfacePorts != nil {
		t.Error("zero Config must not configure interface listeners")
	}
	if cfg.TLS.Enabled {
		t.Error("zero Config must not enable TLS")
	}
}

// TestRealismCCUEnablesEverything guards against a field being added to
// Realism without being wired into the preset.
func TestRealismCCUEnablesEverything(t *testing.T) {
	full := godevccu.RealismCCU()
	value := reflect.ValueOf(full)
	for i := range value.NumField() {
		field := value.Field(i)
		if field.Kind() == reflect.Bool && !field.Bool() {
			t.Errorf("RealismCCU leaves %s off", value.Type().Field(i).Name)
		}
	}
}

// TestDefaultInterfacePortsMatchTheCCU pins the canonical ports and that
// callers get their own copy to mutate.
func TestDefaultInterfacePortsMatchTheCCU(t *testing.T) {
	ports := godevccu.DefaultInterfacePorts()
	expected := map[string]int{
		godevccu.InterfaceBidCosRF:       2001,
		godevccu.InterfaceHmIPRF:         2010,
		godevccu.InterfaceBidCosWired:    2000,
		godevccu.InterfaceVirtualDevices: 9292,
	}
	for name, want := range expected {
		if got := ports[name]; got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	ports[godevccu.InterfaceBidCosRF] = 1
	if godevccu.DefaultInterfacePorts()[godevccu.InterfaceBidCosRF] != 2001 {
		t.Error("callers share the defaults map — mutating one run affects the next")
	}
}
