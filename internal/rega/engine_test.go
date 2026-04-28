// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package rega_test

import (
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
