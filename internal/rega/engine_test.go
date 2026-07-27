// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

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

func TestExecuteBackendInfo(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	e := rega.New(st, nil)
	res := e.Execute(`!# name: get_backend_info.fn
system.Exec("cat /VERSION");`)
	if !strings.Contains(res.Output, "OpenCCU") {
		t.Fatalf("output missing product: %q", res.Output)
	}
}

func TestExecuteSerial(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "0123456789")
	e := rega.New(st, nil)
	res := e.Execute(`!# name: get_serial.fn`)
	if !strings.Contains(res.Output, "0123456789") {
		t.Fatalf("expected serial in output, got %q", res.Output)
	}
}

func TestExecutePrograms(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	st.AddProgram("Foo", "bar", true, 0)
	e := rega.New(st, nil)
	res := e.Execute(`dom.GetObject(ID_PROGRAMS)`)
	if !strings.Contains(res.Output, "Foo") {
		t.Fatalf("output missing program name: %q", res.Output)
	}
}

func TestSetSysVarFromScript(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	st.AddSystemVariable("Presence", "BOOL", false, state.AddSystemVariableOpts{})
	e := rega.New(st, nil)
	res := e.Execute(`dom.GetObject("Presence").State(true)`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	sv, _ := st.SystemVariable("Presence")
	if sv.Value != true {
		t.Fatalf("sysvar = %v, want true", sv.Value)
	}
}

func TestExecuteSysvarDescriptions(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	st.AddSystemVariable("Presence", "BOOL", false, state.AddSystemVariableOpts{
		ID:          950,
		Description: "HAHM",
	})
	st.AddSystemVariable("svEnergyCounter_14884_000858A994D482:7", "FLOAT", 0.0, state.AddSystemVariableOpts{
		ID:             951,
		ChannelAddress: "000858A994D482:7",
	})
	e := rega.New(st, nil)

	// The description-script family walks ID_SYSTEM_VARIABLES AND calls
	// .DPInfo() per variable — the dedicated handler must win over the
	// generic sysvar handler and frame ids as strings.
	res := e.Execute(`foreach (sVarId, dom.GetObject(ID_SYSTEM_VARIABLES).EnumIDs()) { object oVar = dom.GetObject(sVarId); Write(oVar.DPInfo().UriEncode()); }`)
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	var entries []struct {
		ID             string `json:"id"`
		Description    string `json:"description"`
		ChannelAddress string `json:"channel_address"`
	}
	if err := json.Unmarshal([]byte(res.Output), &entries); err != nil {
		t.Fatalf("output is not the description wire shape (string ids): %v — %s", err, res.Output)
	}
	byID := map[string]struct {
		ID             string `json:"id"`
		Description    string `json:"description"`
		ChannelAddress string `json:"channel_address"`
	}{}
	for _, en := range entries {
		byID[en.ID] = en
	}
	if got := byID["950"]; got.Description != "HAHM" || got.ChannelAddress != "" {
		t.Fatalf("var 950 = %+v, want description HAHM and empty channel_address", got)
	}
	got := byID["951"]
	decoded, err := url.QueryUnescape(got.ChannelAddress)
	if err != nil {
		t.Fatalf("channel_address not URL-decodable: %v", err)
	}
	if decoded != "000858A994D482:7" {
		t.Fatalf("var 951 channel_address = %q, want 000858A994D482:7", decoded)
	}
}

func TestExecuteSysvarsGenericStillAnswersWithoutDPInfo(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	st.AddSystemVariable("Presence", "BOOL", false, state.AddSystemVariableOpts{})
	e := rega.New(st, nil)
	res := e.Execute(`dom.GetObject(ID_SYSTEM_VARIABLES)`)
	if !strings.Contains(res.Output, "Presence") {
		t.Fatalf("generic sysvar handler output missing variable name: %q", res.Output)
	}
}

func TestExecuteWriteEcho(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	e := rega.New(st, nil)
	res := e.Execute(`Write("hello")`)
	if res.Output != "hello" {
		t.Fatalf("output = %q, want hello", res.Output)
	}
}

func TestUnknownScriptReturnsEmpty(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	e := rega.New(st, nil)
	res := e.Execute(`mystery script`)
	if !res.Success || res.Output != "" {
		t.Fatalf("expected empty success, got %+v", res)
	}
}

// TestExecuteBOMPrefixedScriptReturnsEmpty mirrors the real-CCU
// behaviour verified against an OpenCCU on 2026-04-28: scripts that
// start with a UTF-8 BOM (0xEF 0xBB 0xBF) are silently dropped and
// the runScript JSON-RPC method returns an empty result. Without this
// guardrail in the simulator, accidental BOM injection on the
// gohomematic side would only surface in production.
func TestExecuteBOMPrefixedScriptReturnsEmpty(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	e := rega.New(st, nil)
	// Without BOM the same script returns "hello".
	if got := e.Execute(`Write("hello")`).Output; got != "hello" {
		t.Fatalf("baseline (no BOM) output = %q, want hello", got)
	}
	// With BOM the engine must return empty.
	res := e.Execute("\xef\xbb\xbf" + `Write("hello")`)
	if !res.Success {
		t.Fatalf("BOM script must succeed (empty result), got error %q", res.Error)
	}
	if res.Output != "" {
		t.Fatalf("BOM-prefixed script output = %q, want empty (real CCU drops BOM scripts)", res.Output)
	}
}

func TestExecuteAlarmMessages(t *testing.T) {
	st := state.New(hmconst.BackendModeOpenCCU, "TEST0001")
	e := rega.New(st, nil)
	// The real aiohomematic script iterates ID_SYSTEM_VARIABLES and calls
	// DPInfo(); the dedicated pattern must win over the sysvar handlers and
	// return the empty active-alarm list.
	res := e.Execute(`!# name: get_alarm_messages.fn
object oSysvars = dom.GetObject(ID_SYSTEM_VARIABLES);
string sDPInfo = oVar.DPInfo();`)
	if res.Output != "[]" {
		t.Fatalf("expected empty alarm list, got %q", res.Output)
	}
}
