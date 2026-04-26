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
