// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Tests for the JSON-RPC methods added so a client can exercise the full
// system-variable lifecycle and read the CCU identity without a ReGa
// script detour, plus the listMethods metadata a real CCU publishes.

package jsonrpc_test

import (
	"encoding/json"
	"testing"
)

// call posts a method with params and returns the decoded response.
func call(t *testing.T, baseURL, method string, params map[string]any) rpcResp {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "1.1",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return postTo(t, baseURL, string(raw))
}

// TestSysVarLifecycle covers create → read → set → delete, which was
// impossible before: the simulator could set and delete variables but
// never create one.
func TestSysVarLifecycle(t *testing.T) {
	baseURL, stateMgr, _ := newStartedServer(t, false)

	created := call(t, baseURL, "SysVar.createBool", map[string]any{
		"name":     "Anwesenheit",
		"init_val": true,
	})
	if created.Error != nil {
		t.Fatalf("createBool: %v", created.Error)
	}
	rec, ok := created.Result.(map[string]any)
	if !ok {
		t.Fatalf("createBool result = %T, want an object", created.Result)
	}
	for _, key := range []string{"name", "id", "value"} {
		if _, ok := rec[key]; !ok {
			t.Errorf("createBool result missing %q: %v", key, rec)
		}
	}
	if _, found := stateMgr.SystemVariable("Anwesenheit"); !found {
		t.Fatal("variable was not created in the state")
	}

	// Read it back through SysVar.get, which reports the CCU type
	// nomenclature (LOGIC, not BOOL).
	got := call(t, baseURL, "SysVar.get", map[string]any{"id": rec["id"]})
	if got.Error != nil {
		t.Fatalf("SysVar.get: %v", got.Error)
	}
	detail, ok := got.Result.(map[string]any)
	if !ok {
		t.Fatalf("SysVar.get result = %T, want an object", got.Result)
	}
	if detail["type"] != "LOGIC" {
		t.Errorf("type = %v, want LOGIC", detail["type"])
	}
	if detail["valueName0"] != "false" || detail["valueName1"] != "true" {
		t.Errorf("LOGIC variable must carry value names: %v", detail)
	}
	if _, has := detail["minValue"]; has {
		t.Errorf("minValue is NUMBER-only, must not appear on LOGIC: %v", detail)
	}

	// Change it, then delete it.
	if r := call(t, baseURL, "SysVar.setBool", map[string]any{"name": "Anwesenheit", "value": false}); r.Error != nil {
		t.Fatalf("setBool: %v", r.Error)
	}
	if sv, _ := stateMgr.SystemVariable("Anwesenheit"); sv.Value != false {
		t.Errorf("value = %v, want false", sv.Value)
	}
	if r := call(t, baseURL, "SysVar.deleteSysVarByName", map[string]any{"name": "Anwesenheit"}); r.Error != nil {
		t.Fatalf("delete: %v", r.Error)
	}
	if _, found := stateMgr.SystemVariable("Anwesenheit"); found {
		t.Error("variable still present after delete")
	}
}

func TestSysVarCreateFloatReportsBounds(t *testing.T) {
	baseURL, _, _ := newStartedServer(t, false)

	created := call(t, baseURL, "SysVar.createFloat", map[string]any{
		"name":     "Temperatur",
		"minValue": 5.0,
		"maxValue": 30.0,
	})
	if created.Error != nil {
		t.Fatalf("createFloat: %v", created.Error)
	}
	rec := created.Result.(map[string]any)

	got := call(t, baseURL, "SysVar.get", map[string]any{"id": rec["id"]})
	detail := got.Result.(map[string]any)
	if detail["type"] != "NUMBER" {
		t.Errorf("type = %v, want NUMBER", detail["type"])
	}
	if detail["minValue"] != 5.0 || detail["maxValue"] != 30.0 {
		t.Errorf("bounds = %v/%v, want 5/30", detail["minValue"], detail["maxValue"])
	}
	if _, has := detail["valueList"]; has {
		t.Errorf("valueList is LIST-only: %v", detail)
	}
}

func TestSysVarCreateEnumAndSetEnum(t *testing.T) {
	baseURL, _, _ := newStartedServer(t, false)

	created := call(t, baseURL, "SysVar.createEnum", map[string]any{
		"name":    "Modus",
		"valList": "Aus;Ein;Automatik",
	})
	if created.Error != nil {
		t.Fatalf("createEnum: %v", created.Error)
	}
	rec := created.Result.(map[string]any)

	got := call(t, baseURL, "SysVar.get", map[string]any{"id": rec["id"]})
	detail := got.Result.(map[string]any)
	if detail["type"] != "LIST" {
		t.Errorf("type = %v, want LIST", detail["type"])
	}
	if detail["valueList"] != "Aus;Ein;Automatik" {
		t.Errorf("valueList = %v, want the created list", detail["valueList"])
	}

	// setEnum echoes the new list back.
	set := call(t, baseURL, "SysVar.setEnum", map[string]any{
		"name":      "Modus",
		"valueList": "Aus;Ein",
	})
	if set.Result != "Aus;Ein" {
		t.Errorf("setEnum = %v, want the echoed list", set.Result)
	}
}

func TestSysVarCreateRejectsDuplicate(t *testing.T) {
	baseURL, _, _ := newStartedServer(t, false)
	params := map[string]any{"name": "Doppelt", "init_val": true}
	if r := call(t, baseURL, "SysVar.createBool", params); r.Error != nil {
		t.Fatalf("first create: %v", r.Error)
	}
	if r := call(t, baseURL, "SysVar.createBool", params); r.Error == nil {
		t.Error("expected an error when creating the same variable twice")
	}
}

func TestCCUIdentityMethods(t *testing.T) {
	baseURL, _, _ := newStartedServer(t, false)

	serial := call(t, baseURL, "CCU.getSerial", nil)
	if serial.Error != nil {
		t.Fatalf("getSerial: %v", serial.Error)
	}
	// A CCU reports the last 10 characters of its serial.
	if serial.Result != "1234567890" {
		t.Errorf("serial = %v, want the last 10 chars of the configured serial", serial.Result)
	}

	version := call(t, baseURL, "CCU.getVersion", nil)
	if version.Error != nil {
		t.Fatalf("getVersion: %v", version.Error)
	}
	if s, _ := version.Result.(string); s == "" {
		t.Error("empty version")
	}
}

// TestListMethodsCarriesLevelAndInfo pins the CCU shape: name, privilege
// level and description, sorted by name.
func TestListMethodsCarriesLevelAndInfo(t *testing.T) {
	baseURL, _, _ := newStartedServer(t, false)

	res := call(t, baseURL, "system.listMethods", nil)
	if res.Error != nil {
		t.Fatalf("listMethods: %v", res.Error)
	}
	entries, ok := res.Result.([]any)
	if !ok {
		t.Fatalf("result = %T, want an array", res.Result)
	}
	if len(entries) == 0 {
		t.Fatal("no methods reported")
	}

	prev := ""
	seen := map[string]map[string]any{}
	for _, raw := range entries {
		m, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("entry = %T, want an object", raw)
		}
		name, _ := m["name"].(string)
		if name == "" {
			t.Fatalf("entry without a name: %v", m)
		}
		if _, ok := m["level"]; !ok {
			t.Errorf("method %q has no level", name)
		}
		if _, ok := m["info"]; !ok {
			t.Errorf("method %q has no info", name)
		}
		if prev != "" && name < prev {
			t.Errorf("methods not sorted: %q after %q", name, prev)
		}
		prev = name
		seen[name] = m
	}

	// Spot-check a level taken from the CCU's own methods.conf.
	if login, ok := seen["Session.login"]; ok && login["level"] != "NONE" {
		t.Errorf("Session.login level = %v, want NONE", login["level"])
	}
	if run, ok := seen["ReGa.runScript"]; ok && run["level"] != "ADMIN" {
		t.Errorf("ReGa.runScript level = %v, want ADMIN", run["level"])
	}
}

func TestMethodHelpReturnsDescription(t *testing.T) {
	baseURL, _, _ := newStartedServer(t, false)

	res := call(t, baseURL, "system.methodHelp", map[string]any{"name": "SysVar.getAll"})
	if res.Error != nil {
		t.Fatalf("methodHelp: %v", res.Error)
	}
	if s, _ := res.Result.(string); s == "" {
		t.Error("empty description for a known method")
	}

	unknown := call(t, baseURL, "system.methodHelp", map[string]any{"name": "Nope.nope"})
	if unknown.Error == nil {
		t.Error("expected an error for an unknown method")
	}
}
