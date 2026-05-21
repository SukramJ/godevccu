// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package state_test

import (
	"testing"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/state"
)

// ─────────────────────────────────────────────────────────────────
// Defaults helpers
// ─────────────────────────────────────────────────────────────────

func TestSetupDefaults(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	state.SetupDefaults(m)

	if len(m.Programs()) == 0 {
		t.Fatal("SetupDefaultPrograms: no programs")
	}
	if len(m.SystemVariables()) == 0 {
		t.Fatal("SetupDefaultSysvars: no sysvars")
	}
	if len(m.Rooms()) == 0 {
		t.Fatal("SetupDefaultRooms: no rooms")
	}
	if len(m.Functions()) == 0 {
		t.Fatal("SetupDefaultFunctions: no functions")
	}
}

func TestSetupDefaultProgramsIndividually(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "X")
	state.SetupDefaultPrograms(m)
	if len(m.Programs()) == 0 {
		t.Fatal("expected programs")
	}
}

func TestSetupDefaultSyvarsIndividually(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "X")
	state.SetupDefaultSysvars(m)
	if len(m.SystemVariables()) == 0 {
		t.Fatal("expected sysvars")
	}
}

func TestSetupDefaultRoomsIndividually(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "X")
	state.SetupDefaultRooms(m)
	if len(m.Rooms()) == 0 {
		t.Fatal("expected rooms")
	}
}

func TestSetupDefaultFunctionsIndividually(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "X")
	state.SetupDefaultFunctions(m)
	if len(m.Functions()) == 0 {
		t.Fatal("expected functions")
	}
}

// ─────────────────────────────────────────────────────────────────
// Manager accessors not yet covered
// ─────────────────────────────────────────────────────────────────

func TestMode(t *testing.T) {
	m := state.New(hmconst.BackendModeCCU, "S")
	if m.Mode() != hmconst.BackendModeCCU {
		t.Fatalf("Mode() = %v, want CCU", m.Mode())
	}
}

func TestBackendInfo(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	info := m.BackendInfo()
	_ = info // just check it doesn't panic
	info.Hostname = "testhost"
	m.SetBackendInfo(info)
	if m.BackendInfo().Hostname != "testhost" {
		t.Fatal("SetBackendInfo not persisted")
	}
}

func TestSerialShort(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "SHORT")
	if got := m.Serial(); got != "SHORT" {
		t.Fatalf("Serial() = %q, want SHORT", got)
	}
}

func TestProgramByName(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddProgram("Alpha", "", true, 0)
	p, ok := m.ProgramByName("Alpha")
	if !ok || p.Name != "Alpha" {
		t.Fatal("ProgramByName failed")
	}
	_, ok = m.ProgramByName("Missing")
	if ok {
		t.Fatal("ProgramByName should return false for missing")
	}
}

func TestProgramsList(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddProgram("P1", "", true, 0)
	m.AddProgram("P2", "", false, 0)
	if got := len(m.Programs()); got != 2 {
		t.Fatalf("Programs() len = %d, want 2", got)
	}
}

func TestSystemVariables(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddSystemVariable("V1", "BOOL", true, state.AddSystemVariableOpts{})
	m.AddSystemVariable("V2", "FLOAT", 1.5, state.AddSystemVariableOpts{})
	if got := len(m.SystemVariables()); got != 2 {
		t.Fatalf("SystemVariables() len = %d, want 2", got)
	}
}

func TestSystemVariableByID(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	sv := m.AddSystemVariable("V", "BOOL", false, state.AddSystemVariableOpts{})
	got, ok := m.SystemVariableByID(sv.ID)
	if !ok || got.Name != "V" {
		t.Fatalf("SystemVariableByID failed: ok=%v", ok)
	}
	_, ok = m.SystemVariableByID(99999)
	if ok {
		t.Fatal("expected false for missing ID")
	}
}

func TestRooms(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddRoom("R1", "", nil, 0)
	m.AddRoom("R2", "", nil, 0)
	if got := len(m.Rooms()); got != 2 {
		t.Fatalf("Rooms() len = %d, want 2", got)
	}
}

func TestRemoveChannelFromRoom(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	r := m.AddRoom("Living", "", []string{"VCU1:1", "VCU2:2"}, 0)
	ok := m.RemoveChannelFromRoom(r.ID, "VCU1:1")
	if !ok {
		t.Fatal("RemoveChannelFromRoom returned false")
	}
	got, _ := m.Room(r.ID)
	if len(got.ChannelIDs) != 1 {
		t.Fatalf("expected 1 channel after remove, got %d", len(got.ChannelIDs))
	}
	// remove from nonexistent room
	if m.RemoveChannelFromRoom(99999, "VCU1:1") {
		t.Fatal("expected false for nonexistent room")
	}
}

func TestFunctions(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddFunction("F1", "", nil, 0)
	m.AddFunction("F2", "", nil, 0)
	if got := len(m.Functions()); got != 2 {
		t.Fatalf("Functions() len = %d, want 2", got)
	}
}

func TestFunction(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	f := m.AddFunction("Lights", "", nil, 0)
	got, ok := m.Function(f.ID)
	if !ok || got.Name != "Lights" {
		t.Fatalf("Function() failed: ok=%v", ok)
	}
	_, ok = m.Function(99999)
	if ok {
		t.Fatal("Function() should return false for missing ID")
	}
}

func TestAddChannelToFunctionDuplicate(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	f := m.AddFunction("Lights", "", nil, 0)
	m.AddChannelToFunction(f.ID, "VCU1:1")
	m.AddChannelToFunction(f.ID, "VCU1:1") // duplicate — must not grow
	got, _ := m.Function(f.ID)
	if len(got.ChannelIDs) != 1 {
		t.Fatalf("expected 1 channel (no dup), got %d", len(got.ChannelIDs))
	}
}

func TestAddChannelToFunctionMissing(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	if m.AddChannelToFunction(99999, "VCU1:1") {
		t.Fatal("expected false for nonexistent function")
	}
}

func TestInboxDevices(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddInboxDevice("VCU9:0", "Switch", "HmIP-PS", "HmIP-RF")
	m.AddInboxDevice("VCU8:0", "Sensor", "HmIP-STH", "HmIP-RF")
	devs := m.InboxDevices()
	if len(devs) != 2 {
		t.Fatalf("InboxDevices() len = %d, want 2", len(devs))
	}
}

func TestAcceptInboxDevice(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddInboxDevice("VCU9:0", "Switch", "HmIP-PS", "HmIP-RF")
	if !m.AcceptInboxDevice("VCU9:0") {
		t.Fatal("AcceptInboxDevice returned false")
	}
	if len(m.InboxDevices()) != 0 {
		t.Fatal("device still in inbox after accept")
	}
	if m.AcceptInboxDevice("VCU9:0") {
		t.Fatal("AcceptInboxDevice should return false for missing")
	}
}

func TestRejectInboxDevice(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddInboxDevice("VCU7:0", "Switch", "HmIP-PS", "HmIP-RF")
	if !m.RejectInboxDevice("VCU7:0") {
		t.Fatal("RejectInboxDevice returned false")
	}
}

func TestFailBackup(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.StartBackup()
	m.FailBackup("err")
	if got := m.BackupStatus().Status; got != "failed" {
		t.Fatalf("BackupStatus after FailBackup = %q, want failed", got)
	}
}

func TestBackupData(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.CompleteBackup([]byte("data"), "backup.tar")
	if string(m.BackupData()) != "data" {
		t.Fatal("BackupData mismatch")
	}
}

func TestDeviceValues(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.SetDeviceValue("VCU1", "STATE", true)
	v, ok := m.DeviceValue("VCU1", "STATE")
	if !ok || v != true {
		t.Fatalf("DeviceValue() = %v, %v", v, ok)
	}
	_, ok = m.DeviceValue("VCU1", "MISSING")
	if ok {
		t.Fatal("DeviceValue should return false for missing key")
	}
	all := m.AllDeviceValues("")
	if len(all) != 1 {
		t.Fatalf("AllDeviceValues len = %d, want 1", len(all))
	}
	m.ClearDeviceValues()
	if len(m.AllDeviceValues("")) != 0 {
		t.Fatal("AllDeviceValues should be empty after clear")
	}
}

func TestDeviceNames(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.SetDeviceName("VCU1:0", "Kitchen Switch")
	n, ok := m.DeviceName("vcu1:0") // lowercase → should match
	if !ok || n != "Kitchen Switch" {
		t.Fatalf("DeviceName() = %q, %v", n, ok)
	}
	_, ok = m.DeviceName("VCU99:0")
	if ok {
		t.Fatal("DeviceName should return false for missing")
	}
	all := m.AllDeviceNames()
	if len(all) != 1 {
		t.Fatalf("AllDeviceNames len = %d, want 1", len(all))
	}
}

func TestRegisterProgramCallback(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	m.AddProgram("P", "", true, 0)
	p, _ := m.ProgramByName("P")
	var count int
	m.RegisterProgramCallback(func(_ int, _ bool) { count++ })
	m.ExecuteProgram(p.ID)
	if count != 1 {
		t.Fatalf("program callback fired %d times, want 1", count)
	}
}

func TestClearAll(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	state.SetupDefaults(m)
	m.SetDeviceValue("VCU1", "STATE", true)
	m.ClearAll()
	if len(m.Programs()) != 0 {
		t.Fatal("Programs not cleared")
	}
	if len(m.SystemVariables()) != 0 {
		t.Fatal("SystemVariables not cleared")
	}
	if len(m.Rooms()) != 0 {
		t.Fatal("Rooms not cleared")
	}
	if len(m.Functions()) != 0 {
		t.Fatal("Functions not cleared")
	}
	if len(m.AllDeviceValues("")) != 0 {
		t.Fatal("DeviceValues not cleared")
	}
}

func TestAddChannelToRoomDuplicate(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	r := m.AddRoom("Kitchen", "", []string{"VCU1:1"}, 0)
	m.AddChannelToRoom(r.ID, "VCU1:1") // duplicate
	got, _ := m.Room(r.ID)
	if len(got.ChannelIDs) != 1 {
		t.Fatalf("expected 1 channel (no dup), got %d", len(got.ChannelIDs))
	}
}

func TestAddChannelToRoomMissing(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "S")
	if m.AddChannelToRoom(99999, "VCU1:1") {
		t.Fatal("expected false for nonexistent room")
	}
}
