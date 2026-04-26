// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu_test

import (
	"sync/atomic"
	"testing"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/hmconst"
)

func newRPC(t *testing.T) *ccu.RPCFunctions {
	t.Helper()
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-SWSD"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	return rpc
}

func TestListDevicesContainsRoot(t *testing.T) {
	rpc := newRPC(t)
	devs := rpc.ListDevices()
	if len(devs) == 0 {
		t.Fatal("no devices loaded")
	}
	supported := rpc.SupportedDevices()
	addr, ok := supported["HmIP-SWSD"]
	if !ok {
		t.Fatal("HmIP-SWSD missing from supported devices")
	}
	if _, err := rpc.GetDeviceDescription(addr); err != nil {
		t.Fatalf("GetDeviceDescription(%q): %v", addr, err)
	}
}

func TestGetParamsetReturnsDefaults(t *testing.T) {
	rpc := newRPC(t)
	// SMOKE_DETECTOR channel address — VCU2822385:1 in fixture.
	values, err := rpc.GetParamset("VCU2822385:1", hmconst.ParamsetAttrValues)
	if err != nil {
		t.Fatalf("GetParamset: %v", err)
	}
	if len(values) == 0 {
		t.Fatal("expected non-empty default paramset")
	}
}

func TestSetValueFiresEvent(t *testing.T) {
	rpc := newRPC(t)
	var fired atomic.Int32
	var lastValue atomic.Value
	rpc.RegisterParamsetCallback(func(_, _, _ string, value any) {
		fired.Add(1)
		lastValue.Store(value)
	})
	if err := rpc.SetValue("VCU2822385:1", "SMOKE_DETECTOR_COMMAND", 1, true); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if got := fired.Load(); got == 0 {
		t.Fatal("expected at least one event")
	}
}

func TestPutParamsetWritesValue(t *testing.T) {
	rpc := newRPC(t)
	err := rpc.PutParamset("VCU2822385:1", hmconst.ParamsetAttrValues, map[string]any{
		"SMOKE_DETECTOR_COMMAND": 1,
	}, true)
	if err != nil {
		t.Fatalf("PutParamset: %v", err)
	}
	got, err := rpc.GetValue("VCU2822385:1", "SMOKE_DETECTOR_COMMAND")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if got != 1 {
		t.Fatalf("value = %v, want 1", got)
	}
}

func TestUnknownDeviceReturnsError(t *testing.T) {
	rpc := newRPC(t)
	if _, err := rpc.GetDeviceDescription("NONEXISTENT"); err == nil {
		t.Fatal("expected error for unknown device")
	}
}
