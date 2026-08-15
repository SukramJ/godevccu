// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Tests for the CCU-shaped JSON payloads. Each pins both halves of the
// opt-in: the CCU form when RealisticSchema is set, and the established
// pydevccu form when it is not.

package jsonrpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/jsonrpc"
	"github.com/SukramJ/godevccu/internal/session"
	"github.com/SukramJ/godevccu/internal/state"
)

// schemaHandlers builds handlers over a seeded state.
func schemaHandlers(t *testing.T, realistic bool) (*jsonrpc.Handlers, *state.Manager) {
	t.Helper()
	stateMgr := state.New(hmconst.BackendModeOpenCCU, "TESTSERIAL1234567890")
	sessMgr := session.New("Admin", "secret", 30*time.Minute, false)
	h := jsonrpc.NewHandlers(stateMgr, sessMgr, nil, nil, 2001)
	h.RealisticSchema = realistic
	return h, stateMgr
}

// invoke calls a handler by name.
func invoke(t *testing.T, h *jsonrpc.Handlers, method string, params map[string]any) any {
	t.Helper()
	handler, ok := h.Methods()[method]
	if !ok {
		t.Fatalf("method %q not registered", method)
	}
	if params == nil {
		params = map[string]any{}
	}
	result, err := handler(context.Background(), params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return result
}

func TestSysVarGetAllCCUNomenclature(t *testing.T) {
	h, stateMgr := schemaHandlers(t, true)
	stateMgr.AddSystemVariable("Anwesenheit", "BOOL", true, state.AddSystemVariableOpts{})
	stateMgr.AddSystemVariable("Temperatur", "FLOAT", 21.5, state.AddSystemVariableOpts{
		MinValue: 5, MaxValue: 30, Unit: "°C",
	})
	stateMgr.AddSystemVariable("Modus", "ENUM", 0, state.AddSystemVariableOpts{
		ValueList: "Aus;Ein",
	})

	entries := invoke(t, h, "SysVar.getAll", nil).([]map[string]any)
	byName := map[string]map[string]any{}
	for _, e := range entries {
		byName[e["name"].(string)] = e
	}

	logic := byName["Anwesenheit"]
	if logic["type"] != "LOGIC" {
		t.Errorf("BOOL reported as %v, want LOGIC", logic["type"])
	}
	// Every value in the CCU payload is a string, whatever its type.
	if _, isString := logic["value"].(string); !isString {
		t.Errorf("value = %T, want a string", logic["value"])
	}
	if logic["valueName0"] != "false" || logic["valueName1"] != "true" {
		t.Errorf("LOGIC needs both state labels: %v", logic)
	}
	if _, has := logic["valueList"]; has {
		t.Errorf("valueList is LIST-only: %v", logic)
	}
	if _, has := logic["description"]; has {
		t.Errorf("the CCU payload has no description: %v", logic)
	}

	number := byName["Temperatur"]
	if number["type"] != "NUMBER" {
		t.Errorf("FLOAT reported as %v, want NUMBER", number["type"])
	}
	if number["minValue"] != "5" || number["maxValue"] != "30" {
		t.Errorf("bounds must be strings: %v / %v", number["minValue"], number["maxValue"])
	}

	list := byName["Modus"]
	if list["type"] != "LIST" {
		t.Errorf("ENUM reported as %v, want LIST", list["type"])
	}
	if list["valueList"] != "Aus;Ein" {
		t.Errorf("valueList = %v", list["valueList"])
	}
	if _, has := list["minValue"]; has {
		t.Errorf("minValue is NUMBER-only: %v", list)
	}
}

func TestSysVarGetAllKeepsPydevccuShapeByDefault(t *testing.T) {
	h, stateMgr := schemaHandlers(t, false)
	stateMgr.AddSystemVariable("Anwesenheit", "BOOL", true, state.AddSystemVariableOpts{
		Description: "Jemand da",
	})

	entries := invoke(t, h, "SysVar.getAll", nil).([]map[string]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry["type"] != "BOOL" {
		t.Errorf("type = %v, want the established BOOL", entry["type"])
	}
	if entry["value"] != true {
		t.Errorf("value = %v, want the native bool", entry["value"])
	}
	if entry["description"] != "Jemand da" {
		t.Errorf("description dropped: %v", entry)
	}
}

func TestProgramLastExecuteTimeFormat(t *testing.T) {
	h, stateMgr := schemaHandlers(t, true)
	p := stateMgr.AddProgram("Heizung", "", true, 0)
	stateMgr.ExecuteProgram(p.ID)

	entries := invoke(t, h, "Program.getAll", nil).([]map[string]any)
	got, isString := entries[0]["lastExecuteTime"].(string)
	if !isString {
		t.Fatalf("lastExecuteTime = %T, want a formatted string", entries[0]["lastExecuteTime"])
	}
	if _, err := time.Parse("2006-01-02 15:04:05", got); err != nil {
		t.Errorf("lastExecuteTime %q is not the CCU format: %v", got, err)
	}
}

func TestProgramLastExecuteTimeStaysFloatByDefault(t *testing.T) {
	h, stateMgr := schemaHandlers(t, false)
	p := stateMgr.AddProgram("Heizung", "", true, 0)
	stateMgr.ExecuteProgram(p.ID)

	entries := invoke(t, h, "Program.getAll", nil).([]map[string]any)
	if _, isFloat := entries[0]["lastExecuteTime"].(float64); !isFloat {
		t.Fatalf("lastExecuteTime = %T, want the established float", entries[0]["lastExecuteTime"])
	}
}

// A never-executed program reports an empty timestamp rather than the
// epoch.
func TestNeverExecutedProgramReportsEmptyTime(t *testing.T) {
	h, stateMgr := schemaHandlers(t, true)
	stateMgr.AddProgram("Nie", "", true, 0)
	entries := invoke(t, h, "Program.getAll", nil).([]map[string]any)
	if entries[0]["lastExecuteTime"] != "" {
		t.Errorf("lastExecuteTime = %v, want empty", entries[0]["lastExecuteTime"])
	}
}

func TestRoomListAllReturnsIDsOnly(t *testing.T) {
	h, stateMgr := schemaHandlers(t, true)
	stateMgr.AddRoom("Wohnzimmer", "", nil, 0)
	stateMgr.AddRoom("Küche", "", nil, 0)

	ids, ok := invoke(t, h, "Room.listAll", nil).([]string)
	if !ok {
		t.Fatalf("Room.listAll returned %T, want a list of ids", invoke(t, h, "Room.listAll", nil))
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want 2", ids)
	}
}

func TestRoomListAllAliasesGetAllByDefault(t *testing.T) {
	h, stateMgr := schemaHandlers(t, false)
	stateMgr.AddRoom("Wohnzimmer", "", nil, 0)

	rooms, ok := invoke(t, h, "Room.listAll", nil).([]map[string]any)
	if !ok {
		t.Fatal("Room.listAll must keep returning full room objects by default")
	}
	if rooms[0]["name"] != "Wohnzimmer" {
		t.Errorf("room = %v", rooms[0])
	}
}

// The privilege table has to cover every registered method, or
// listMethods reports a fallback level for some of them.
func TestEveryMethodHasAPrivilegeLevel(t *testing.T) {
	h, _ := schemaHandlers(t, false)
	entries := invoke(t, h, "system.listMethods", nil).([]map[string]any)
	for _, entry := range entries {
		if entry["info"] == "" {
			t.Errorf("method %v has no description — missing from the level table", entry["name"])
		}
	}
}
