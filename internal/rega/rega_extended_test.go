// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// This file extends the baseline engine_test.go with cases that raise
// coverage for the handlers that are currently at 0 %:
// handleFetchDeviceData, handleGetSysvars, handleGetServiceMessages,
// handleGetInbox, handleSetProgramState, handleBackupStart,
// handleBackupStatus, handleUpdateInfo, handleTriggerUpdate,
// handleGetRooms, handleGetFunctions.

package rega_test

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/rega"
	"github.com/SukramJ/godevccu/internal/state"
)

// newEngine is a convenience helper that creates a fresh state manager
// + engine, letting each test start from a clean slate.
func newEngine(t *testing.T) (*state.Manager, *rega.Engine) {
	t.Helper()
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	e := rega.New(st, nil)
	return st, e
}

// decodeJSON is a small helper that unmarshals JSON output from a
// Result into the target value and fails the test on error.
func decodeJSON(t *testing.T, output string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(output), target); err != nil {
		t.Fatalf("unmarshal %q: %v", output, err)
	}
}

// ─────────────────────────────────────────────────────────────────
// handleGetSysvars
// ─────────────────────────────────────────────────────────────────

func TestGetSysvars(t *testing.T) {
	st, e := newEngine(t)
	st.AddSystemVariable("Presence", "BOOL", false, state.AddSystemVariableOpts{Description: "home"})

	res := e.Execute(`dom.GetObject(ID_SYSTEM_VARIABLES)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "Presence") {
		t.Fatalf("output missing sysvar name: %q", res.Output)
	}
}

func TestGetSysvarsEmpty(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`dom.GetObject(ID_SYSTEM_VARIABLES)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	var arr []any
	decodeJSON(t, res.Output, &arr)
	if len(arr) != 0 {
		t.Errorf("expected empty list, got %d items", len(arr))
	}
}

// ─────────────────────────────────────────────────────────────────
// handleGetServiceMessages
// ─────────────────────────────────────────────────────────────────

func TestGetServiceMessages(t *testing.T) {
	st, e := newEngine(t)
	st.AddServiceMessage("LOW_BAT", "error", "VCU0001234", "Lamp")

	res := e.Execute(`dom.GetObject(ID_SERVICES)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "LOW_BAT") {
		t.Fatalf("output missing service message name: %q", res.Output)
	}
}

func TestGetServiceMessagesEmpty(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`dom.GetObject(ID_SERVICES)`)
	var arr []any
	decodeJSON(t, res.Output, &arr)
	if len(arr) != 0 {
		t.Errorf("expected empty list, got %d items", len(arr))
	}
}

// ─────────────────────────────────────────────────────────────────
// handleGetInbox
// ─────────────────────────────────────────────────────────────────

func TestGetInbox(t *testing.T) {
	st, e := newEngine(t)
	st.AddInboxDevice("VCU9999", "New Switch", "HmIP-PS", "HmIP-RF")

	res := e.Execute(`INBOX`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	// get_inbox_devices.fn writes "id"/"type" (not "deviceId"/
	// "deviceType") and URI-encodes the name.
	var entries []map[string]any
	decodeJSON(t, res.Output, &entries)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1: %q", len(entries), res.Output)
	}
	got := entries[0]
	name, err := url.QueryUnescape(got["name"].(string))
	if err != nil {
		t.Fatalf("name not URL-decodable: %v", err)
	}
	if name != "New Switch" {
		t.Errorf("name = %q, want New Switch", name)
	}
	if got["type"] != "HmIP-PS" {
		t.Errorf("type = %v, want HmIP-PS", got["type"])
	}
	if got["id"] == nil || got["id"] == "" {
		t.Errorf("id missing: %v", got)
	}
	if _, exists := got["deviceType"]; exists {
		t.Error("deviceType must not be emitted — the real script writes type")
	}
}

func TestGetInboxEmpty(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`INBOX`)
	var arr []any
	decodeJSON(t, res.Output, &arr)
	if len(arr) != 0 {
		t.Errorf("expected empty list, got %d items", len(arr))
	}
}

// ─────────────────────────────────────────────────────────────────
// handleSetProgramState
// ─────────────────────────────────────────────────────────────────

func TestSetProgramStateActivate(t *testing.T) {
	st, e := newEngine(t)
	p := st.AddProgram("Morning", "routine", false, 0)

	res := e.Execute(`dom.GetObject(` + itoa(p.ID) + `).Active(true)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	got, ok := st.Program(p.ID)
	if !ok {
		t.Fatal("program not found")
	}
	if !got.Active {
		t.Error("expected Active=true after SetProgramState(true)")
	}
}

func TestSetProgramStateDeactivate(t *testing.T) {
	st, e := newEngine(t)
	p := st.AddProgram("Evening", "routine", true, 0)

	res := e.Execute(`dom.GetObject(` + itoa(p.ID) + `).Active(false)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	got, _ := st.Program(p.ID)
	if got.Active {
		t.Error("expected Active=false after SetProgramState(false)")
	}
}

// set_program_state.fn ends in Write(program.Active()), so the new
// state is echoed back rather than producing empty output.
func TestSetProgramStateWritesNewState(t *testing.T) {
	st, e := newEngine(t)
	p := st.AddProgram("Test", "", true, 0)

	res := e.Execute(`dom.GetObject(` + itoa(p.ID) + `).Active(false)`)
	if res.Output != "false" {
		t.Errorf("output = %q, want false", res.Output)
	}
}

// An unknown program fails the script's "if (program)" guard, so
// nothing is written.
func TestSetProgramStateUnknownProgramWritesNothing(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`dom.GetObject(999999).Active(false)`)
	if res.Output != "" {
		t.Errorf("output = %q, want empty string", res.Output)
	}
}

// ─────────────────────────────────────────────────────────────────
// handleFetchDeviceData
// ─────────────────────────────────────────────────────────────────

func TestFetchDeviceDataByInterfaceAssign(t *testing.T) {
	st, e := newEngine(t)
	st.SetDeviceValue("VCU1234:1", "STATE", true)

	res := e.Execute(`interface = "HmIP-RF"` + "\n" + `foreach (dp, dom.GetObject(ID_DATAPOINTS)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "VCU1234") {
		t.Fatalf("output missing device address: %q", res.Output)
	}
}

func TestFetchDeviceDataByParamHeader(t *testing.T) {
	st, e := newEngine(t)
	st.SetDeviceValue("VCU5678:2", "LEVEL", 0.5)

	// Matches the param: header variant of the pattern.
	res := e.Execute(`!# name: fetch_all_device_data.fn`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	// With an empty iface filter the full cache is returned.
	if !strings.Contains(res.Output, "VCU5678") {
		t.Fatalf("output missing device address: %q", res.Output)
	}
}

func TestFetchDeviceDataEmpty(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`foreach (dp, dom.GetObject(ID_DATAPOINTS)`)
	// The script frames its output with Write('{') … Write('}'), so an
	// empty cache is an empty object — never an empty array.
	var obj map[string]any
	decodeJSON(t, res.Output, &obj)
	if len(obj) != 0 {
		t.Errorf("expected empty object for empty device cache, got %d entries", len(obj))
	}
}

// ─────────────────────────────────────────────────────────────────
// handleBackupStart + handleBackupStatus
// ─────────────────────────────────────────────────────────────────

func TestBackupStartReturnsSuccess(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`CreateBackup()`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	var m map[string]any
	decodeJSON(t, res.Output, &m)
	if m["success"] != true {
		t.Errorf("success field = %v, want true", m["success"])
	}
	if m["pid"] == "" || m["pid"] == nil {
		t.Errorf("pid missing from backup response: %v", m)
	}
}

func TestBackupStatusAfterStart(t *testing.T) {
	st, e := newEngine(t)
	st.StartBackup()

	res := e.Execute(`backup.pid`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	var m map[string]any
	decodeJSON(t, res.Output, &m)
	if m["status"] != "running" {
		t.Errorf("status = %v, want running", m["status"])
	}
}

func TestBackupStatusScriptVariant(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`BACKUP_STATUS`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	// Initial state is "idle".
	if !strings.Contains(res.Output, "idle") {
		t.Fatalf("expected idle status, got %q", res.Output)
	}
}

// ─────────────────────────────────────────────────────────────────
// handleUpdateInfo + handleTriggerUpdate
// ─────────────────────────────────────────────────────────────────

func TestUpdateInfo(t *testing.T) {
	st, e := newEngine(t)
	st.SetUpdateInfo("3.87.0", "3.87.1")

	res := e.Execute(`checkFirmwareUpdate`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	// get_system_update_info.fn writes snake_case keys.
	var m map[string]any
	decodeJSON(t, res.Output, &m)
	if m["update_available"] != true {
		t.Errorf("update_available = %v, want true", m["update_available"])
	}
	if m["available_firmware"] != "3.87.1" {
		t.Errorf("available_firmware = %v, want 3.87.1", m["available_firmware"])
	}
	if m["current_firmware"] != "3.87.0" {
		t.Errorf("current_firmware = %v, want 3.87.0", m["current_firmware"])
	}
}

func TestTriggerUpdate(t *testing.T) {
	st, e := newEngine(t)
	st.SetUpdateInfo("3.87.0", "3.87.1")

	// The TRIGGER_UPDATE keyword is matched by the trigger-update pattern
	// (listed after checkFirmwareUpdate in the engine's pattern slice).
	res := e.Execute(`TRIGGER_UPDATE`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	var m map[string]any
	decodeJSON(t, res.Output, &m)
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
	// After triggering, the firmware version is applied.
	info := st.UpdateInfo()
	if info.UpdateAvailable {
		t.Error("UpdateAvailable should be false after TRIGGER_UPDATE")
	}
}

// ─────────────────────────────────────────────────────────────────
// handleGetRooms
// ─────────────────────────────────────────────────────────────────

func TestGetRooms(t *testing.T) {
	st, e := newEngine(t)
	st.AddRoom("Kitchen", "ground floor", []string{"VCU001:1"}, 0)

	res := e.Execute(`ID_ROOMS`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "Kitchen") {
		t.Fatalf("output missing room name: %q", res.Output)
	}
}

func TestGetRoomsEmpty(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`ID_ROOMS`)
	var arr []any
	decodeJSON(t, res.Output, &arr)
	if len(arr) != 0 {
		t.Errorf("expected empty list, got %d items", len(arr))
	}
}

// ─────────────────────────────────────────────────────────────────
// handleGetFunctions
// ─────────────────────────────────────────────────────────────────

func TestGetFunctions(t *testing.T) {
	st, e := newEngine(t)
	st.AddFunction("Lighting", "all lights", []string{"VCU002:1"}, 0)

	res := e.Execute(`ID_FUNCTIONS`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "Lighting") {
		t.Fatalf("output missing function name: %q", res.Output)
	}
}

func TestGetFunctionsEmpty(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`ID_FUNCTIONS`)
	var arr []any
	decodeJSON(t, res.Output, &arr)
	if len(arr) != 0 {
		t.Errorf("expected empty list, got %d items", len(arr))
	}
}

// ─────────────────────────────────────────────────────────────────
// handleSetSysvar — additional value-type branches
// ─────────────────────────────────────────────────────────────────

func TestSetSysvarFloat(t *testing.T) {
	st, e := newEngine(t)
	st.AddSystemVariable("Temperature", "FLOAT", 0.0, state.AddSystemVariableOpts{})

	res := e.Execute(`dom.GetObject("Temperature").State(21.5)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	sv, _ := st.SystemVariable("Temperature")
	if sv.Value != 21.5 {
		t.Errorf("sysvar value = %v, want 21.5", sv.Value)
	}
}

func TestSetSysvarInteger(t *testing.T) {
	st, e := newEngine(t)
	st.AddSystemVariable("Count", "INTEGER", 0, state.AddSystemVariableOpts{})

	res := e.Execute(`dom.GetObject("Count").State(42)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	sv, _ := st.SystemVariable("Count")
	if sv.Value != 42 {
		t.Errorf("sysvar value = %v, want 42", sv.Value)
	}
}

func TestSetSysvarString(t *testing.T) {
	st, e := newEngine(t)
	st.AddSystemVariable("Tag", "STRING", "", state.AddSystemVariableOpts{})

	res := e.Execute(`dom.GetObject("Tag").State("hello")`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	sv, _ := st.SystemVariable("Tag")
	if sv.Value != "hello" {
		t.Errorf("sysvar value = %v, want hello", sv.Value)
	}
}

func TestSetSysvarFalse(t *testing.T) {
	st, e := newEngine(t)
	st.AddSystemVariable("Flag", "BOOL", true, state.AddSystemVariableOpts{})

	res := e.Execute(`dom.GetObject("Flag").State(false)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	sv, _ := st.SystemVariable("Flag")
	if sv.Value != false {
		t.Errorf("sysvar value = %v, want false", sv.Value)
	}
}

// ─────────────────────────────────────────────────────────────────
// handleBackendInfo — grep variant
// ─────────────────────────────────────────────────────────────────

func TestBackendInfoGrepVariant(t *testing.T) {
	_, e := newEngine(t)
	res := e.Execute(`grep VERSION /etc | grep PRODUCT`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	if !strings.Contains(res.Output, "OpenCCU") {
		t.Fatalf("expected OpenCCU in output, got %q", res.Output)
	}
}

// ─────────────────────────────────────────────────────────────────
// handleGetSerial — system.GetVar variant
// ─────────────────────────────────────────────────────────────────

func TestGetSerialSystemGetVarVariant(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "ABCDEF9876")
	e := rega.New(st, nil)

	res := e.Execute(`system.GetVar("SERIALNO")`)
	if !strings.Contains(res.Output, "ABCDEF9876") {
		t.Fatalf("serial not in output: %q", res.Output)
	}
}

// ─────────────────────────────────────────────────────────────────
// Utility
// ─────────────────────────────────────────────────────────────────

// itoa converts an int to its decimal string representation without
// importing strconv (keeps this file self-contained).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
