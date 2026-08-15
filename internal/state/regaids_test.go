// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package state_test

import (
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/state"
)

func TestRegaIDsAreStableAndUnique(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")

	first := m.RegisterAddress("VCU0000001")
	second := m.RegisterAddress("VCU0000001:1")
	if first == 0 || second == 0 {
		t.Fatal("ids must be non-zero, clients treat 0 as absent")
	}
	if first == second {
		t.Fatal("device and channel must not share an id")
	}
	if again := m.RegisterAddress("VCU0000001"); again != first {
		t.Fatalf("id changed on re-registration: %d vs %d", again, first)
	}
	// Addresses are matched case-insensitively, like everywhere else.
	if lower := m.RegisterAddress("vcu0000001"); lower != first {
		t.Fatalf("case variant got a different id: %d vs %d", lower, first)
	}
}

func TestRegaIDsDoNotCollideWithOtherObjectKinds(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	program := m.AddProgram("P", "", true, 0)
	sysvar := m.AddSystemVariable("V", "BOOL", false, state.AddSystemVariableOpts{})
	room := m.AddRoom("R", "", nil, 0)
	device := m.RegisterAddress("VCU0000001")

	seen := map[int]string{
		program.ID: "program",
		sysvar.ID:  "sysvar",
		room.ID:    "room",
	}
	if kind, clash := seen[device]; clash {
		t.Fatalf("device id %d collides with a %s id", device, kind)
	}
}

func TestRegaLookupsRoundTrip(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	id := m.RegisterAddress("VCU0000001:2")

	if got := m.RegaID("VCU0000001:2"); got != id {
		t.Errorf("RegaID = %d, want %d", got, id)
	}
	if got := m.RegaAddress(id); got != "VCU0000001:2" {
		t.Errorf("RegaAddress = %q, want the registered address", got)
	}
	if got := m.RegaID("VCU0000404"); got != 0 {
		t.Errorf("unregistered address reports id %d, want 0", got)
	}
	if got := m.RegaAddress(999999); got != "" {
		t.Errorf("unknown id reports %q, want empty", got)
	}
}

// ChannelIDsForAddresses is what turns a room's stored addresses into
// the ids a client cross-references.
func TestChannelIDsForAddressesSkipsUnregistered(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	known := m.RegisterAddress("VCU0000001:1")

	ids := m.ChannelIDsForAddresses([]string{"VCU0000001:1", "VCU0000404:1"})
	if len(ids) != 1 || ids[0] != known {
		t.Fatalf("ids = %v, want just the registered channel %d", ids, known)
	}
}

func TestRegisterAddressesKeepsOrder(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	m.RegisterAddresses([]string{"A", "B", "C"})
	if m.RegaID("A") >= m.RegaID("B") || m.RegaID("B") >= m.RegaID("C") {
		t.Fatal("ids must follow registration order")
	}
}

func TestEmptyAddressGetsNoID(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	if id := m.RegisterAddress(""); id != 0 {
		t.Fatalf("empty address got id %d, want 0", id)
	}
}

// ─────────────────────────────────────────────────────────────────
// Backup lifecycle
// ─────────────────────────────────────────────────────────────────

func TestBackupCompletesAfterDelay(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	m.SetBackupCompletionDelay(50 * time.Millisecond)

	m.StartBackup()
	if got := m.BackupStatus().Status; got != "running" {
		t.Fatalf("status = %q, want running", got)
	}

	time.Sleep(120 * time.Millisecond)
	status := m.BackupStatus()
	if status.Status != "completed" {
		t.Fatalf("status = %q, want completed", status.Status)
	}
	if status.Size == 0 || len(m.BackupData()) == 0 {
		t.Error("completed backup carries no payload")
	}
	if status.Filepath == "" || status.Filename == "" {
		t.Errorf("completed backup lacks file details: %+v", status)
	}
}

func TestBackupStaysRunningWithoutDelay(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	m.StartBackup()
	time.Sleep(80 * time.Millisecond)
	if got := m.BackupStatus().Status; got != "running" {
		t.Fatalf("status = %q, want running (automation off by default)", got)
	}
}

// ─────────────────────────────────────────────────────────────────
// Built-in system variables
// ─────────────────────────────────────────────────────────────────

func TestSetupBuiltinSysvarsUsesRealIDs(t *testing.T) {
	m := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	state.SetupBuiltinSysvars(m)

	// Clients key their special handling on these ids.
	for _, id := range []int{40, 41} {
		sv, ok := m.SystemVariableByID(id)
		if !ok {
			t.Fatalf("built-in variable %d missing", id)
		}
		if !sv.Internal {
			t.Errorf("variable %d must be marked internal", id)
		}
	}
	presence, ok := m.SystemVariable("${sysVarPresence}")
	if !ok {
		t.Fatal("${sysVarPresence} missing — the rename path is untestable without it")
	}
	if presence.ValueName0 == "" || presence.ValueName1 == "" {
		t.Error("a LOGIC variable needs both state labels")
	}
}
