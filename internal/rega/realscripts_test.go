// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// This file runs the engine against the *unmodified* ReGa scripts a real
// client sends (testdata/scripts, copied from aiohomematic). The former
// tests used hand-written fragments, which is why six scripts could be
// routed to the wrong handler without a single test going red:
// set_program_state.fn and set_system_variable.fn were answered with
// listings and changed nothing, accept_device_in_inbox.fn and
// acknowledge_message.fn returned arrays instead of {"success":…},
// create_backup_status.fn was answered with backend info, and
// get_program_descriptions.fn came back in the wrong shape.
//
// Each case asserts the way the client actually consumes the response —
// the keys it indexes and the container type it iterates.

package rega_test

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/rega"
	"github.com/SukramJ/godevccu/internal/state"
)

// loadScript returns the real script with its ##placeholders##
// substituted the same way a client does before posting it.
func loadScript(t *testing.T, name string, params map[string]string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "scripts", name))
	if err != nil {
		t.Fatalf("read script %s: %v", name, err)
	}
	script := string(raw)
	for k, v := range params {
		script = strings.ReplaceAll(script, "##"+k+"##", v)
	}
	if strings.Contains(script, "##") {
		t.Fatalf("script %s still has unsubstituted placeholders", name)
	}
	return script
}

func seededEngine(t *testing.T) (*state.Manager, *rega.Engine) {
	t.Helper()
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	return st, rega.New(st, nil)
}

// TestRealScriptsAreRoutedByName is the regression that would have
// caught the whole finding class: every shipped script must reach a
// handler that produces its own wire shape, not a generic listing.
func TestRealScriptsAreRoutedByName(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "scripts"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	var scripts []string
	for _, en := range entries {
		if strings.HasSuffix(en.Name(), ".fn") {
			scripts = append(scripts, en.Name())
		}
	}
	if len(scripts) == 0 {
		t.Fatal("no scripts in testdata/scripts")
	}

	// Every script must answer with valid JSON — an empty string means
	// no handler claimed it. The write scripts need their target to
	// exist, otherwise they legitimately write nothing.
	for _, name := range scripts {
		t.Run(name, func(t *testing.T) {
			st, e := seededEngine(t)
			st.AddProgram("Programm", "", true, 1234)
			st.AddSystemVariable("Var", "STRING", "", state.AddSystemVariableOpts{})
			script := loadScript(t, name, map[string]string{
				"interface":      "HmIP-RF",
				"id":             "1234",
				"state":          "1",
				"name":           "Var",
				"value":          "text",
				"device_address": "VCU0000001",
				"message_id":     "1",
			})
			res := e.Execute(script)
			if !res.Success {
				t.Fatalf("execute failed: %s", res.Error)
			}
			if res.Output == "" {
				t.Fatalf("no handler claimed %s — it fell through to the empty default", name)
			}
			var decoded any
			if err := json.Unmarshal([]byte(res.Output), &decoded); err != nil {
				t.Fatalf("output of %s is not JSON: %v — %q", name, err, res.Output)
			}
		})
	}
}

// TestRealSetProgramStateChangesState pins the finding that hurt most:
// the script reported success while the program state never moved.
func TestRealSetProgramStateChangesState(t *testing.T) {
	st, e := seededEngine(t)
	p := st.AddProgram("Heizung Morgens", "Startet um 6:00", true, 0)

	res := e.Execute(loadScript(t, "set_program_state.fn", map[string]string{
		"id":    itoa(p.ID),
		"state": "0",
	}))
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	got, _ := st.Program(p.ID)
	if got.Active {
		t.Fatal("program still active — the write was swallowed")
	}
	if res.Output != "false" {
		t.Errorf("output = %q, want false (the script writes program.Active())", res.Output)
	}
}

// TestRealSetSystemVariableChangesValue covers the second silent no-op.
func TestRealSetSystemVariableChangesValue(t *testing.T) {
	st, e := seededEngine(t)
	st.AddSystemVariable("Statustext", "STRING", "alt", state.AddSystemVariableOpts{})

	res := e.Execute(loadScript(t, "set_system_variable.fn", map[string]string{
		"name":  "Statustext",
		"value": "neu",
	}))
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	sv, _ := st.SystemVariable("Statustext")
	if sv.Value != "neu" {
		t.Fatalf("sysvar = %v, want neu — the write was swallowed", sv.Value)
	}
}

// The script only writes variables whose ValueTypeStr() is "String".
func TestRealSetSystemVariableIgnoresNonStringVariable(t *testing.T) {
	st, e := seededEngine(t)
	st.AddSystemVariable("Anwesenheit", "BOOL", false, state.AddSystemVariableOpts{})

	res := e.Execute(loadScript(t, "set_system_variable.fn", map[string]string{
		"name":  "Anwesenheit",
		"value": "neu",
	}))
	if res.Output != "" {
		t.Errorf("output = %q, want empty (non-String variables are skipped)", res.Output)
	}
	sv, _ := st.SystemVariable("Anwesenheit")
	if sv.Value != false {
		t.Errorf("sysvar = %v, want unchanged false", sv.Value)
	}
}

// TestRealFetchAllDeviceDataIsAMapping asserts the container type and
// the exact key layout the client indexes:
// "<interface>.<channel_address>.<parameter>".
func TestRealFetchAllDeviceDataIsAMapping(t *testing.T) {
	st, e := seededEngine(t)
	st.SetDeviceValue("VCU0000001:1", "STATE", true)
	st.SetDeviceValue("VCU0000001:1", "LEVEL", 0.5)

	res := e.Execute(loadScript(t, "fetch_all_device_data.fn", map[string]string{
		"interface": "HmIP-RF",
	}))

	// The client calls .items() on this — a list raises AttributeError.
	var data map[string]any
	if err := json.Unmarshal([]byte(res.Output), &data); err != nil {
		t.Fatalf("output is not a JSON object: %v — %q", err, res.Output)
	}
	want := "HmIP-RF.VCU0000001:1.STATE"
	if _, ok := data[url.QueryEscape(want)]; !ok {
		if _, plain := data[want]; !plain {
			t.Fatalf("missing key %q, got %v", want, keysOf(data))
		}
	}
	for k, v := range data {
		decoded, err := url.QueryUnescape(k)
		if err != nil {
			t.Fatalf("key %q not URL-decodable: %v", k, err)
		}
		if !strings.HasPrefix(decoded, "HmIP-RF.") {
			t.Errorf("key %q lacks the interface prefix", decoded)
		}
		if decoded == want && v != true {
			t.Errorf("STATE = %v, want true", v)
		}
	}
}

// TestRealInboxScripts covers the listing shape plus the accept script,
// which the "INBOX" catch-all used to answer with the listing itself.
func TestRealInboxScripts(t *testing.T) {
	st, e := seededEngine(t)
	st.AddInboxDevice("VCU0000009", "Neuer Schalter", "HmIP-PS", "HmIP-RF")

	listing := e.Execute(loadScript(t, "get_inbox_devices.fn", nil))
	var devices []map[string]any
	if err := json.Unmarshal([]byte(listing.Output), &devices); err != nil {
		t.Fatalf("inbox listing is not a JSON array: %v — %q", err, listing.Output)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	// The client indexes "id" and "type".
	for _, key := range []string{"id", "address", "name", "type", "interface"} {
		if _, ok := devices[0][key]; !ok {
			t.Errorf("inbox entry missing key %q: %v", key, devices[0])
		}
	}

	accept := e.Execute(loadScript(t, "accept_device_in_inbox.fn", map[string]string{
		"device_address": "VCU0000009",
	}))
	// The client calls .get("success") — an array raises AttributeError.
	var result map[string]any
	if err := json.Unmarshal([]byte(accept.Output), &result); err != nil {
		t.Fatalf("accept result is not a JSON object: %v — %q", err, accept.Output)
	}
	if result["success"] != true {
		t.Errorf("success = %v, want true", result["success"])
	}
	if len(st.InboxDevices()) != 0 {
		t.Error("device still in inbox — the accept was swallowed")
	}
}

func TestRealAcceptUnknownDeviceReportsError(t *testing.T) {
	_, e := seededEngine(t)
	res := e.Execute(loadScript(t, "accept_device_in_inbox.fn", map[string]string{
		"device_address": "VCU0000404",
	}))
	var result map[string]any
	if err := json.Unmarshal([]byte(res.Output), &result); err != nil {
		t.Fatalf("not a JSON object: %v — %q", err, res.Output)
	}
	if result["success"] != false {
		t.Errorf("success = %v, want false", result["success"])
	}
	if result["error"] != "Device not found" {
		t.Errorf("error = %v, want \"Device not found\"", result["error"])
	}
}

// TestRealAcknowledgeMessage covers the second {"success":…} script.
func TestRealAcknowledgeMessage(t *testing.T) {
	st, e := seededEngine(t)
	msg := st.AddServiceMessage("UNREACH", "UNREACH", "VCU0000001:0", "Schalter")

	res := e.Execute(loadScript(t, "acknowledge_message.fn", map[string]string{
		"message_id": itoa(msg.ID),
	}))
	var result map[string]any
	if err := json.Unmarshal([]byte(res.Output), &result); err != nil {
		t.Fatalf("not a JSON object: %v — %q", err, res.Output)
	}
	if result["success"] != true {
		t.Errorf("success = %v, want true", result["success"])
	}
	if len(st.ServiceMessages()) != 0 {
		t.Error("message still present — the receipt was swallowed")
	}
}

// TestRealBackupStatusShape pins create_backup_status.fn, which the
// "/VERSION" pattern used to answer with backend info.
func TestRealBackupStatusShape(t *testing.T) {
	_, e := seededEngine(t)
	res := e.Execute(loadScript(t, "create_backup_status.fn", nil))
	var status map[string]any
	if err := json.Unmarshal([]byte(res.Output), &status); err != nil {
		t.Fatalf("not a JSON object: %v — %q", err, res.Output)
	}
	if status["status"] != "idle" {
		t.Fatalf("status = %v, want idle (got backend info?)", status["status"])
	}
	// An unfinished backup carries nothing but the status.
	if len(status) != 1 {
		t.Errorf("unfinished backup must report status only, got %v", status)
	}
}

// TestRealBackendInfoKeys pins the is_ha_app spelling.
func TestRealBackendInfoKeys(t *testing.T) {
	_, e := seededEngine(t)
	res := e.Execute(loadScript(t, "get_backend_info.fn", nil))
	var info map[string]any
	if err := json.Unmarshal([]byte(res.Output), &info); err != nil {
		t.Fatalf("not a JSON object: %v — %q", err, res.Output)
	}
	if _, ok := info["is_ha_app"]; !ok {
		t.Errorf("missing key is_ha_app: %v", info)
	}
	if info["product"] != "OpenCCU" {
		t.Errorf("product = %v, want OpenCCU", info["product"])
	}
}

// TestRealProgramDescriptionsShape pins the {id: string, description}
// shape against the full Program.getAll payload it used to return.
func TestRealProgramDescriptionsShape(t *testing.T) {
	st, e := seededEngine(t)
	st.AddProgram("Heizung", "Beschreibung mit Leerzeichen", true, 0)

	res := e.Execute(loadScript(t, "get_program_descriptions.fn", nil))
	var entries []map[string]any
	if err := json.Unmarshal([]byte(res.Output), &entries); err != nil {
		t.Fatalf("not a JSON array: %v — %q", err, res.Output)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if _, ok := entries[0]["id"].(string); !ok {
		t.Errorf("id = %v, want a string", entries[0]["id"])
	}
	desc, _ := entries[0]["description"].(string)
	decoded, err := url.QueryUnescape(desc)
	if err != nil {
		t.Fatalf("description not URL-decodable: %v", err)
	}
	if decoded != "Beschreibung mit Leerzeichen" {
		t.Errorf("description = %q, want the decoded original", decoded)
	}
}

// TestRealUpdateScriptsUseSnakeCase pins both firmware scripts.
func TestRealUpdateScriptsUseSnakeCase(t *testing.T) {
	st, e := seededEngine(t)
	st.SetUpdateInfo("3.87.0", "3.87.1")

	info := e.Execute(loadScript(t, "get_system_update_info.fn", nil))
	var m map[string]any
	if err := json.Unmarshal([]byte(info.Output), &m); err != nil {
		t.Fatalf("not a JSON object: %v — %q", err, info.Output)
	}
	for _, key := range []string{"current_firmware", "available_firmware", "update_available", "check_script_available"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q: %v", key, m)
		}
	}

	trigger := e.Execute(loadScript(t, "trigger_firmware_update.fn", nil))
	var tm map[string]any
	if err := json.Unmarshal([]byte(trigger.Output), &tm); err != nil {
		t.Fatalf("not a JSON object: %v — %q", err, trigger.Output)
	}
	for _, key := range []string{"success", "script_available", "message"} {
		if _, ok := tm[key]; !ok {
			t.Errorf("missing key %q: %v", key, tm)
		}
	}
}

// uriEncode must percent-encode spaces: clients decode with unquote(),
// not unquote_plus(), so a "+" would survive into the decoded name.
func TestNamesEncodeSpacesAsPercent20(t *testing.T) {
	st, e := seededEngine(t)
	st.AddProgram("Heizung Morgens", "", true, 0)

	res := e.Execute(`dom.GetObject(ID_PROGRAMS)`)
	if strings.Contains(res.Output, "Heizung+Morgens") {
		t.Fatalf("space encoded as '+', clients decode with unquote(): %q", res.Output)
	}
	if !strings.Contains(res.Output, "Heizung%20Morgens") {
		t.Fatalf("expected %%20-encoded name, got %q", res.Output)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
