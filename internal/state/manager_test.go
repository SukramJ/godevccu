// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package state_test

import (
	"sync/atomic"
	"testing"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/state"
)

func TestPrograms(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")

	p := m.AddProgram("Heating", "morning", true, 0)
	if p.ID != 1000 {
		t.Fatalf("first program id = %d, want 1000", p.ID)
	}

	if !m.ExecuteProgram(p.ID) {
		t.Fatal("ExecuteProgram returned false on active program")
	}
	got, ok := m.Program(p.ID)
	if !ok {
		t.Fatal("Program not found after add")
	}
	if got.LastExecuteTime <= 0 {
		t.Fatalf("LastExecuteTime not updated: %f", got.LastExecuteTime)
	}

	if !m.SetProgramActive(p.ID, false) {
		t.Fatal("SetProgramActive failed")
	}
	if m.ExecuteProgram(p.ID) {
		t.Fatal("ExecuteProgram should fail when program is inactive")
	}

	if !m.DeleteProgram(p.ID) {
		t.Fatal("DeleteProgram failed")
	}
	if _, ok := m.Program(p.ID); ok {
		t.Fatal("program still present after delete")
	}
}

func TestSysVars(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")

	sv := m.AddSystemVariable("Presence", "BOOL", true, state.AddSystemVariableOpts{Description: "x"})
	if sv.ID != 2000 {
		t.Fatalf("first sysvar id = %d, want 2000", sv.ID)
	}

	if !m.SetSystemVariable("Presence", false) {
		t.Fatal("set returned false")
	}
	got, ok := m.SystemVariable("Presence")
	if !ok || got.Value != false {
		t.Fatalf("sysvar value = %v, want false", got.Value)
	}

	if !m.DeleteSystemVariable("Presence") {
		t.Fatal("delete returned false")
	}
	if _, ok := m.SystemVariable("Presence"); ok {
		t.Fatal("sysvar still present after delete")
	}
}

func TestSysVarCallback(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	m.AddSystemVariable("Presence", "BOOL", false, state.AddSystemVariableOpts{})

	var fired atomic.Int32
	m.RegisterSysVarCallback(func(_ string, _ any) { fired.Add(1) })

	m.SetSystemVariable("Presence", true)
	m.SetSystemVariable("Presence", false)

	if got := fired.Load(); got != 2 {
		t.Fatalf("callback fired %d times, want 2", got)
	}
}

func TestRoomsAndFunctions(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")

	r := m.AddRoom("Living", "", []string{"VCU1:1"}, 0)
	if r.ID != 3000 {
		t.Fatalf("room id = %d, want 3000", r.ID)
	}
	if !m.AddChannelToRoom(r.ID, "VCU2:2") {
		t.Fatal("AddChannelToRoom failed")
	}
	got, _ := m.Room(r.ID)
	if len(got.ChannelIDs) != 2 {
		t.Fatalf("room channel count = %d, want 2", len(got.ChannelIDs))
	}

	f := m.AddFunction("Lights", "", nil, 0)
	if f.ID != 4000 {
		t.Fatalf("function id = %d, want 4000", f.ID)
	}
	if !m.AddChannelToFunction(f.ID, "VCU3:3") {
		t.Fatal("AddChannelToFunction failed")
	}
}

func TestServiceMessages(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	a := m.AddServiceMessage("Battery", "LOWBAT", "VCU0001:1", "Sensor")
	m.AddServiceMessage("Unreach", "UNREACH", "VCU0002:1", "Switch")
	if got := len(m.ServiceMessages()); got != 2 {
		t.Fatalf("len(messages) = %d, want 2", got)
	}
	if !m.ClearServiceMessage(a.ID) {
		t.Fatal("clear failed")
	}
	if got := len(m.ServiceMessages()); got != 1 {
		t.Fatalf("after clear: %d, want 1", got)
	}
	if n := m.ClearAllServiceMessages(); n != 1 {
		t.Fatalf("clear all returned %d, want 1", n)
	}
}

func TestBackup(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	pid := m.StartBackup()
	if pid == "" {
		t.Fatal("empty PID from StartBackup")
	}
	if got := m.BackupStatus().Status; got != "running" {
		t.Fatalf("status = %q, want running", got)
	}
	m.CompleteBackup([]byte("hello"), "backup.tar")
	st := m.BackupStatus()
	if st.Status != "completed" || st.Size != 5 {
		t.Fatalf("after complete: %+v", st)
	}
	m.ResetBackup()
	if got := m.BackupStatus().Status; got != "idle" {
		t.Fatalf("after reset: %q", got)
	}
}

func TestUpdateInfo(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	m.SetUpdateInfo("3.0", "3.1")
	info := m.UpdateInfo()
	if !info.UpdateAvailable {
		t.Fatal("expected UpdateAvailable=true after differing versions")
	}
	if !m.TriggerUpdate() {
		t.Fatal("TriggerUpdate returned false on pending update")
	}
	info = m.UpdateInfo()
	if info.UpdateAvailable {
		t.Fatal("UpdateAvailable should be false after trigger")
	}
}

func TestSerial(t *testing.T) {
	m := state.New(hmconst.BackendModeCCU, "0123456789ABCDEF")
	got := m.Serial()
	if got != "6789ABCDEF" {
		t.Fatalf("Serial() = %q, want %q", got, "6789ABCDEF")
	}
}
