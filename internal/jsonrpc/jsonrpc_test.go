// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package jsonrpc_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/jsonrpc"
	"github.com/SukramJ/godevccu/internal/rega"
	"github.com/SukramJ/godevccu/internal/session"
	"github.com/SukramJ/godevccu/internal/state"
)

// ─────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────

type rpcResp struct {
	JSONRPC string         `json:"jsonrpc"`
	Result  any            `json:"result"`
	Error   map[string]any `json:"error"`
	ID      any            `json:"id"`
}

// ─────────────────────────────────────────────────────────────────
// Real server approach — use Start() / Stop() and httptest transport
// ─────────────────────────────────────────────────────────────────

func newStartedServer(t *testing.T, authEnabled bool) (baseURL string, stateMgr *state.Manager, sessMgr *session.Manager) {
	t.Helper()
	stateMgr = state.New(hmconst.BackendModeCCU, "TESTSERIAL1234567890")
	sessMgr = session.New("Admin", "secret", 30*time.Minute, authEnabled)
	handlers := jsonrpc.NewHandlers(stateMgr, sessMgr, nil, nil, 2001)
	srv := jsonrpc.NewServer(jsonrpc.Config{
		Address:  "127.0.0.1:0",
		Handlers: handlers,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	baseURL = "http://" + srv.LocalAddr().String()
	return baseURL, stateMgr, sessMgr
}

func postTo(t *testing.T, url, body string) rpcResp {
	t.Helper()
	resp, err := http.Post(url+"/api/homematic.cgi", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out rpcResp
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return out
}

func callTo(t *testing.T, baseURL, method string, params map[string]any, id any) rpcResp {
	t.Helper()
	payload := map[string]any{
		"jsonrpc": "1.1",
		"method":  method,
		"id":      id,
	}
	if params != nil {
		payload["params"] = params
	}
	raw, _ := json.Marshal(payload)
	return postTo(t, baseURL, string(raw))
}

func callWithSid(t *testing.T, baseURL, method string, params map[string]any, id any, sid string) rpcResp {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_session_id_"] = sid
	return callTo(t, baseURL, method, params, id)
}

// ─────────────────────────────────────────────────────────────────
// Server lifecycle
// ─────────────────────────────────────────────────────────────────

func TestServerStartStop(t *testing.T) {
	t.Parallel()
	stateMgr := state.New(hmconst.BackendModeCCU, "SN")
	sessMgr := session.New("Admin", "secret", 30*time.Minute, false)
	handlers := jsonrpc.NewHandlers(stateMgr, sessMgr, nil, nil, 2001)
	srv := jsonrpc.NewServer(jsonrpc.Config{Address: "127.0.0.1:0", Handlers: handlers})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	addr := srv.LocalAddr()
	if addr == nil {
		t.Fatal("LocalAddr is nil after Start")
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stop again should be a no-op.
	if err := srv.Stop(); err != nil {
		t.Fatalf("double Stop: %v", err)
	}
}

func TestServerDoubleStart(t *testing.T) {
	t.Parallel()
	stateMgr := state.New(hmconst.BackendModeCCU, "SN")
	sessMgr := session.New("Admin", "secret", 30*time.Minute, false)
	handlers := jsonrpc.NewHandlers(stateMgr, sessMgr, nil, nil, 2001)
	srv := jsonrpc.NewServer(jsonrpc.Config{Address: "127.0.0.1:0", Handlers: handlers})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop() //nolint:errcheck
	if err := srv.Start(); err == nil {
		t.Fatal("expected error on double Start, got nil")
	}
}

func TestLocalAddrBeforeStart(t *testing.T) {
	t.Parallel()
	stateMgr := state.New(hmconst.BackendModeCCU, "SN")
	sessMgr := session.New("Admin", "secret", 30*time.Minute, false)
	handlers := jsonrpc.NewHandlers(stateMgr, sessMgr, nil, nil, 2001)
	srv := jsonrpc.NewServer(jsonrpc.Config{Address: "127.0.0.1:0", Handlers: handlers})
	if srv.LocalAddr() != nil {
		t.Fatal("expected nil LocalAddr before Start")
	}
}

// ─────────────────────────────────────────────────────────────────
// CCU meta methods
// ─────────────────────────────────────────────────────────────────

func TestGetAuthEnabled(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "CCU.getAuthEnabled", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if r.Result != false {
		t.Fatalf("expected false, got %v", r.Result)
	}
}

func TestGetHTTPSRedirectEnabled(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "CCU.getHttpsRedirectEnabled", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if r.Result != false {
		t.Fatalf("expected false, got %v", r.Result)
	}
}

func TestListMethods(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "system.listMethods", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("expected non-empty array, got %v", r.Result)
	}
}

// ─────────────────────────────────────────────────────────────────
// Session methods
// ─────────────────────────────────────────────────────────────────

func TestSessionLogin(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	r := callTo(t, base, "Session.login", map[string]any{
		"username": "Admin",
		"password": "secret",
	}, 1)
	if r.Error != nil {
		t.Fatalf("login error: %v", r.Error)
	}
	sid, ok := r.Result.(string)
	if !ok || sid == "" {
		t.Fatalf("expected non-empty session id, got %v", r.Result)
	}
}

func TestSessionLoginWrongPassword(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	r := callTo(t, base, "Session.login", map[string]any{
		"username": "Admin",
		"password": "wrong",
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	// Empty string means login failed — no error is returned, just ""
	sid, _ := r.Result.(string)
	if sid != "" {
		t.Fatalf("expected empty session id, got %q", sid)
	}
}

func TestSessionLogout(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	// Login first
	r := callTo(t, base, "Session.login", map[string]any{
		"username": "Admin",
		"password": "secret",
	}, 1)
	sid, _ := r.Result.(string)

	// Logout
	r2 := callTo(t, base, "Session.logout", map[string]any{"_session_id_": sid}, 2)
	if r2.Error != nil {
		t.Fatalf("logout error: %v", r2.Error)
	}
}

func TestSessionRenew(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	r := callTo(t, base, "Session.login", map[string]any{
		"username": "Admin",
		"password": "secret",
	}, 1)
	sid, _ := r.Result.(string)

	r2 := callTo(t, base, "Session.renew", map[string]any{"_session_id_": sid}, 2)
	if r2.Error != nil {
		t.Fatalf("renew error: %v", r2.Error)
	}
}

func TestSessionRenewInvalid(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	r := callTo(t, base, "Session.renew", map[string]any{"_session_id_": "nosuchsid"}, 1)
	if r.Error == nil {
		t.Fatal("expected error for invalid session renew, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// Authentication gate
// ─────────────────────────────────────────────────────────────────

func TestAuthRequiredForProtectedMethod(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	r := callTo(t, base, "Interface.ping", nil, 1)
	if r.Error == nil {
		t.Fatal("expected session error, got nil")
	}
}

func TestAuthenticatedCallSucceeds(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	r := callTo(t, base, "Session.login", map[string]any{
		"username": "Admin",
		"password": "secret",
	}, 1)
	sid, _ := r.Result.(string)

	r2 := callWithSid(t, base, "Interface.ping", nil, 2, sid)
	if r2.Error != nil {
		t.Fatalf("ping error: %v", r2.Error)
	}
}

// ─────────────────────────────────────────────────────────────────
// Interface methods
// ─────────────────────────────────────────────────────────────────

func TestInterfaceListInterfaces(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.listInterfaces", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("expected interface list, got %v", r.Result)
	}
}

func TestInterfacePing(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.ping", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if r.Result != true {
		t.Fatalf("expected true, got %v", r.Result)
	}
}

func TestInterfaceGetInstallMode(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getInstallMode", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceSetInstallMode(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.setInstallMode", map[string]any{"on": true}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceSetInstallModeHMIP(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.setInstallModeHMIP", map[string]any{"on": true}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceGetMasterValue(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getMasterValue", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceInit(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.init", map[string]any{
		"url":         "http://localhost:8120",
		"interfaceId": "test-id",
	}, 1)
	// RPC is nil, so returns "" with no error
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceListDevices(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.listDevices", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceGetDeviceDescriptionMissingAddress(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getDeviceDescription", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing address, got nil")
	}
}

func TestInterfaceGetDeviceDescriptionNoRPC(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getDeviceDescription", map[string]any{"address": "ABC123"}, 1)
	// RPC is nil → ErrObject
	if r.Error == nil {
		t.Fatal("expected error when RPC is nil, got nil")
	}
}

func TestInterfaceGetParamsetMissingAddress(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getParamset", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing address, got nil")
	}
}

func TestInterfaceGetParamsetNoRPC(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getParamset", map[string]any{"address": "ABC123:0"}, 1)
	// RPC is nil → empty map, no error
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceGetParamsetDescription(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getParamsetDescription", map[string]any{"address": "ABC123:0"}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceGetValueMissingParams(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getValue", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing address/valueKey, got nil")
	}
}

func TestInterfaceGetValueNoRPC(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.getValue", map[string]any{
		"address":  "ABC123:1",
		"valueKey": "STATE",
	}, 1)
	// RPC is nil → nil, nil
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceSetValueMissingParams(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.setValue", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing address/valueKey, got nil")
	}
}

func TestInterfaceSetValueNoRPC(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.setValue", map[string]any{
		"address":  "ABC123:1",
		"valueKey": "STATE",
		"value":    true,
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfacePutParamsetMissingAddress(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.putParamset", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing address, got nil")
	}
}

func TestInterfacePutParamsetNoRPC(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.putParamset", map[string]any{
		"address": "ABC123:0",
		"set":     map[string]any{"KEY": "val"},
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfacePutParamsetAlternateParamsetKey(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.putParamset", map[string]any{
		"address":  "ABC123:0",
		"paramset": map[string]any{"KEY": "val"},
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceIsPresent(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Interface.isPresent", map[string]any{"address": "ABC123"}, 1)
	// RPC is nil → false, no error
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if r.Result != false {
		t.Fatalf("expected false, got %v", r.Result)
	}
}

// ─────────────────────────────────────────────────────────────────
// Device / Channel
// ─────────────────────────────────────────────────────────────────

func TestDeviceListAllDetail(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Device.listAllDetail", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestDeviceGetMissingAddress(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Device.get", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing address, got nil")
	}
}

func TestDeviceGetNoRPC(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Device.get", map[string]any{"address": "ABC123"}, 1)
	if r.Error == nil {
		t.Fatal("expected error when RPC is nil, got nil")
	}
}

func TestDeviceGetByID(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Device.get", map[string]any{"id": "ABC123"}, 1)
	// RPC is nil → error
	if r.Error == nil {
		t.Fatal("expected error when RPC is nil")
	}
}

func TestSetName(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	r := callTo(t, base, "Device.setName", map[string]any{
		"address": "ABC123",
		"name":    "My Device",
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if name, ok := stateMgr.DeviceName("ABC123"); !ok || name != "My Device" {
		t.Fatalf("device name not set, got %q", name)
	}
}

func TestChannelSetName(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	r := callTo(t, base, "Channel.setName", map[string]any{
		"id":   "ABC123:0",
		"name": "Channel 0",
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if name, ok := stateMgr.DeviceName("ABC123:0"); !ok || name != "Channel 0" {
		t.Fatalf("channel name not set, got %q", name)
	}
}

func TestSetNameMissingAddress(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Device.setName", map[string]any{"name": "foo"}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing address, got nil")
	}
}

func TestChannelHasProgramIDs(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Channel.hasProgramIds", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", r.Result)
	}
	if len(arr) != 0 {
		t.Fatalf("expected empty array, got %v", arr)
	}
}

// ─────────────────────────────────────────────────────────────────
// Programs
// ─────────────────────────────────────────────────────────────────

func TestProgramGetAll(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddProgram("TestProg", "desc", true, 0)
	r := callTo(t, base, "Program.getAll", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("expected program list, got %v", r.Result)
	}
}

func TestProgramExecute(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	p := stateMgr.AddProgram("TestProg", "desc", true, 0)

	r := callTo(t, base, "Program.execute", map[string]any{"id": float64(p.ID)}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestProgramExecuteMissingID(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Program.execute", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestProgramExecuteStringID(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	p := stateMgr.AddProgram("TestProg", "desc", true, 0)
	r := callTo(t, base, "Program.execute", map[string]any{"id": "1000", "programId": p.ID}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestProgramSetActive(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	p := stateMgr.AddProgram("TestProg", "desc", true, 0)

	r := callTo(t, base, "Program.setActive", map[string]any{
		"id":     float64(p.ID),
		"active": false,
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestProgramSetActiveIsActive(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	p := stateMgr.AddProgram("TestProg", "desc", false, 0)

	r := callTo(t, base, "Program.setActive", map[string]any{
		"id":       float64(p.ID),
		"isActive": true,
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestProgramSetActiveMissingID(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Program.setActive", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// SysVar
// ─────────────────────────────────────────────────────────────────

func TestSysVarGetAll(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddSystemVariable("MyVar", "FLOAT", 3.14, state.AddSystemVariableOpts{})
	r := callTo(t, base, "SysVar.getAll", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("expected sysvar list, got %v", r.Result)
	}
}

func TestSysVarGetValueByName(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddSystemVariable("MyVar", "FLOAT", 42.0, state.AddSystemVariableOpts{})

	r := callTo(t, base, "SysVar.getValueByName", map[string]any{"name": "MyVar"}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestSysVarGetValueByNameNotFound(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "SysVar.getValueByName", map[string]any{"name": "NoSuchVar"}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing sysvar, got nil")
	}
}

func TestSysVarGetValueByNameMissingName(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "SysVar.getValueByName", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestSysVarSetBool(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddSystemVariable("BoolVar", "BOOL", false, state.AddSystemVariableOpts{})

	r := callTo(t, base, "SysVar.setBool", map[string]any{
		"name":  "BoolVar",
		"value": true,
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestSysVarSetFloat(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddSystemVariable("FloatVar", "FLOAT", 0.0, state.AddSystemVariableOpts{})

	r := callTo(t, base, "SysVar.setFloat", map[string]any{
		"name":  "FloatVar",
		"value": 3.14,
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestSysVarSetString(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddSystemVariable("StrVar", "STRING", "", state.AddSystemVariableOpts{})

	r := callTo(t, base, "SysVar.setString", map[string]any{
		"name":  "StrVar",
		"value": "hello",
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestSysVarSetMissingName(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "SysVar.setBool", map[string]any{"value": true}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestSysVarDelete(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddSystemVariable("TempVar", "FLOAT", 0.0, state.AddSystemVariableOpts{})

	r := callTo(t, base, "SysVar.deleteSysVarByName", map[string]any{"name": "TempVar"}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestSysVarDeleteMissingName(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "SysVar.deleteSysVarByName", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// Rooms / Subsections
// ─────────────────────────────────────────────────────────────────

func TestRoomGetAll(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddRoom("Living Room", "main room", []string{"ABC123:1"}, 0)

	r := callTo(t, base, "Room.getAll", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("expected room list, got %v", r.Result)
	}
}

func TestRoomListAll(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddRoom("Bedroom", "sleep", nil, 0)

	r := callTo(t, base, "Room.listAll", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestSubsectionGetAll(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.AddFunction("Lighting", "lights", []string{"ABC123:1"}, 0)

	r := callTo(t, base, "Subsection.getAll", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	arr, ok := r.Result.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("expected subsection list, got %v", r.Result)
	}
}

// ─────────────────────────────────────────────────────────────────
// ReGa
// ─────────────────────────────────────────────────────────────────

func TestRegaRunScriptNoReGa(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "ReGa.runScript", map[string]any{"script": "var x = 1;"}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestRegaRunScriptMissingScript(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "ReGa.runScript", map[string]any{}, 1)
	if r.Error == nil {
		t.Fatal("expected error for missing script, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// Protocol edge cases
// ─────────────────────────────────────────────────────────────────

func TestMethodNotFound(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := callTo(t, base, "Unknown.method", nil, 1)
	if r.Error == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
}

func TestInvalidJSONRPCVersion(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := postTo(t, base, `{"jsonrpc":"0.9","method":"Interface.ping","id":1}`)
	if r.Error == nil {
		t.Fatal("expected error for invalid version, got nil")
	}
}

func TestInvalidJSON(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	resp, err := http.Post(base+"/api/homematic.cgi", "application/json", strings.NewReader("not-json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out rpcResp
	_ = json.Unmarshal(raw, &out)
	if out.Error == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestEmptyMethod(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	r := postTo(t, base, `{"jsonrpc":"1.1","method":"","id":1}`)
	if r.Error == nil {
		t.Fatal("expected error for empty method, got nil")
	}
}

func TestGETMethodNotAllowed(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	resp, err := http.Get(base + "/api/homematic.cgi")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	// GET returns 405 and JSON body.
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestBatchRequest(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	body := `[{"jsonrpc":"1.1","method":"Interface.ping","id":1},{"jsonrpc":"1.1","method":"CCU.getAuthEnabled","id":2}]`
	resp, err := http.Post(base+"/api/homematic.cgi", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out []rpcResp
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal batch: %v / body=%s", err, raw)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(out))
	}
}

func TestBatchRequestEmpty(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	resp, err := http.Post(base+"/api/homematic.cgi", "application/json", strings.NewReader(`[]`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out rpcResp
	_ = json.Unmarshal(raw, &out)
	if out.Error == nil {
		t.Fatal("expected error for empty batch, got nil")
	}
}

func TestNotification(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	// Notification: id is null
	resp, err := http.Post(base+"/api/homematic.cgi", "application/json",
		strings.NewReader(`{"jsonrpc":"1.1","method":"Interface.ping","id":null}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	// Notification returns 204
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 for notification, got %d", resp.StatusCode)
	}
}

func TestArrayParams(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	// Array params are wrapped in {"args": [...]}
	r := postTo(t, base, `{"jsonrpc":"1.1","method":"Interface.ping","params":[],"id":1}`)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

// ─────────────────────────────────────────────────────────────────
// VERSION + Backup + Maintenance endpoints
// ─────────────────────────────────────────────────────────────────

func TestVersionEndpoint(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	resp, err := http.Get(base + "/VERSION")
	if err != nil {
		t.Fatalf("GET /VERSION: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "VERSION=") {
		t.Fatalf("expected VERSION= in body, got %q", body)
	}
}

func TestBackupDownloadNotAvailable(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	resp, err := http.Get(base + "/config/cp_security.cgi")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for no backup, got %d", resp.StatusCode)
	}
}

func TestBackupDownloadAvailable(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.CompleteBackup([]byte("backup-data"), "backup.tar.gz")

	resp, err := http.Get(base + "/config/cp_security.cgi")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "backup-data" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestBackupDownloadAuthRequired(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, true)
	stateMgr.CompleteBackup([]byte("secret"), "backup.tar.gz")

	resp, err := http.Get(base + "/config/cp_security.cgi")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", resp.StatusCode)
	}
}

func TestMaintenanceCheckUpdate(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.SetUpdateInfo("3.55.5", "3.57.0")

	body := `{"action":"checkUpdate"}`
	resp, err := http.Post(base+"/config/cp_maintenance.cgi", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out["updateAvailable"] != true {
		t.Fatalf("expected updateAvailable=true, got %v", out["updateAvailable"])
	}
}

func TestMaintenanceTriggerUpdate(t *testing.T) {
	t.Parallel()
	base, stateMgr, _ := newStartedServer(t, false)
	stateMgr.SetUpdateInfo("3.55.5", "3.57.0")

	body := `{"action":"triggerUpdate"}`
	resp, err := http.Post(base+"/config/cp_maintenance.cgi", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out["success"] != true {
		t.Fatalf("expected success=true, got %v", out["success"])
	}
}

func TestMaintenanceUnknownAction(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	body := `{"action":"unknown"}`
	resp, err := http.Post(base+"/config/cp_maintenance.cgi", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestMaintenanceAuthRequired(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	body := `{"action":"checkUpdate"}`
	resp, err := http.Post(base+"/config/cp_maintenance.cgi", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMaintenanceNoBody(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, false)
	resp, err := http.Post(base+"/config/cp_maintenance.cgi", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for no action, got %d", resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────
// Session ID extraction edge cases
// ─────────────────────────────────────────────────────────────────

func TestSessionIDFromTopLevel(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	// Login
	r := callTo(t, base, "Session.login", map[string]any{"username": "Admin", "password": "secret"}, 1)
	sid, _ := r.Result.(string)

	// Top-level _session_id_ in JSON
	body, _ := json.Marshal(map[string]any{
		"jsonrpc":      "1.1",
		"method":       "Interface.ping",
		"id":           2,
		"_session_id_": sid,
	})
	r2 := postTo(t, base, string(body))
	if r2.Error != nil {
		t.Fatalf("unexpected error with top-level session id: %v", r2.Error)
	}
}

func TestSessionIDPythonStyle(t *testing.T) {
	t.Parallel()
	base, _, _ := newStartedServer(t, true)
	r := callTo(t, base, "Session.login", map[string]any{"username": "Admin", "password": "secret"}, 1)
	sid, _ := r.Result.(string)

	// Python-style: {"_session_id_": "abc"} as a JSON string value
	pythonStr := `{'_session_id_': '` + sid + `'}`
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "1.1",
		"method":  "Interface.ping",
		"id":      3,
		"params":  map[string]any{"_session_id_": pythonStr},
	})
	r2 := postTo(t, base, string(body))
	if r2.Error != nil {
		t.Fatalf("python-style session extraction failed: %v", r2.Error)
	}
}

// ─────────────────────────────────────────────────────────────────
// Errors package
// ─────────────────────────────────────────────────────────────────

func TestErrorCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      *jsonrpc.Error
		wantCode int
	}{
		{"parse", jsonrpc.ErrParse("msg"), jsonrpc.ErrParseError},
		{"invalid", jsonrpc.ErrInvalid("msg"), jsonrpc.ErrInvalidRequest},
		{"method", jsonrpc.ErrMethod("foo"), jsonrpc.ErrMethodNotFound},
		{"params", jsonrpc.ErrParams("msg"), jsonrpc.ErrInvalidParams},
		{"internal", jsonrpc.ErrInternal("msg"), jsonrpc.ErrInternalError},
		{"auth", jsonrpc.ErrAuth("msg"), jsonrpc.ErrAuthRequired},
		{"session", jsonrpc.ErrSession("msg"), jsonrpc.ErrSessionExpired},
		{"permission", jsonrpc.ErrPermission("msg"), jsonrpc.ErrPermissionDenied},
		{"object", jsonrpc.ErrObject("Device", "ABC"), jsonrpc.ErrObjectNotFound},
		{"operation", jsonrpc.ErrOperation("msg"), jsonrpc.ErrInvalidOperation},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.err.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d", tc.err.Code, tc.wantCode)
			}
			if tc.err.Error() == "" {
				t.Fatal("Error() returned empty string")
			}
			d := tc.err.MarshalDict()
			if d["code"] != tc.err.Code {
				t.Fatalf("MarshalDict code = %v, want %d", d["code"], tc.err.Code)
			}
		})
	}
}

func TestErrorWithData(t *testing.T) {
	t.Parallel()
	e := &jsonrpc.Error{Code: jsonrpc.ErrInternalError, Message: "oops", Data: "extra"}
	d := e.MarshalDict()
	if d["data"] != "extra" {
		t.Fatalf("MarshalDict data = %v, want 'extra'", d["data"])
	}
}

// ─────────────────────────────────────────────────────────────────
// Handlers: NewHandlers + Methods
// ─────────────────────────────────────────────────────────────────

func TestNewHandlers(t *testing.T) {
	t.Parallel()
	stateMgr := state.New(hmconst.BackendModeCCU, "SN")
	sessMgr := session.New("Admin", "secret", 30*time.Minute, false)
	h := jsonrpc.NewHandlers(stateMgr, sessMgr, nil, nil, 2001)
	if h == nil {
		t.Fatal("NewHandlers returned nil")
	}
	methods := h.Methods()
	if len(methods) == 0 {
		t.Fatal("Methods() returned empty map")
	}
	// Spot check required methods exist.
	required := []string{"Session.login", "Interface.ping", "SysVar.getAll", "Room.getAll", "Program.getAll"}
	for _, m := range required {
		if _, ok := methods[m]; !ok {
			t.Errorf("missing method %q", m)
		}
	}
}

func TestPublicMethods(t *testing.T) {
	t.Parallel()
	expected := []string{"Session.login", "CCU.getAuthEnabled", "CCU.getHttpsRedirectEnabled", "system.listMethods"}
	for _, m := range expected {
		if _, ok := jsonrpc.PublicMethods[m]; !ok {
			t.Errorf("expected %q to be in PublicMethods", m)
		}
	}
}

// ─────────────────────────────────────────────────────────────────
// RPC functions wired (via ccu.RPCFunctions)
// ─────────────────────────────────────────────────────────────────

// newServerWithRPC creates a server backed by a real ccu.RPCFunctions
// loaded from the embedded device data (no loader required for empty state).
func newServerWithRPC(t *testing.T) (baseURL string, stateMgr *state.Manager) {
	t.Helper()
	stateMgr = state.New(hmconst.BackendModeCCU, "TESTSERIAL1234567890")
	sessMgr := session.New("Admin", "secret", 30*time.Minute, false)
	rpcFn, err := ccu.NewRPCFunctions(ccu.Options{})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	regaEng := rega.New(stateMgr, rpcFn)
	handlers := jsonrpc.NewHandlers(stateMgr, sessMgr, rpcFn, regaEng, 2001)
	srv := jsonrpc.NewServer(jsonrpc.Config{
		Address:  "127.0.0.1:0",
		Handlers: handlers,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return "http://" + srv.LocalAddr().String(), stateMgr
}

func TestInterfaceListDevicesWithRPC(t *testing.T) {
	t.Parallel()
	base, _ := newServerWithRPC(t)
	r := callTo(t, base, "Interface.listDevices", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestDeviceListAllDetailWithRPC(t *testing.T) {
	t.Parallel()
	base, _ := newServerWithRPC(t)
	r := callTo(t, base, "Device.listAllDetail", nil, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceInitWithRPC(t *testing.T) {
	t.Parallel()
	base, _ := newServerWithRPC(t)
	r := callTo(t, base, "Interface.init", map[string]any{
		"url":          "http://localhost:8120",
		"interface_id": "HmIP-RF",
	}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestReGaRunScriptWithEngine(t *testing.T) {
	t.Parallel()
	base, _ := newServerWithRPC(t)
	r := callTo(t, base, "ReGa.runScript", map[string]any{"script": "var x = 1;"}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
}

func TestInterfaceIsPresentWithRPC(t *testing.T) {
	t.Parallel()
	base, _ := newServerWithRPC(t)
	r := callTo(t, base, "Interface.isPresent", map[string]any{"address": "NOTEXIST"}, 1)
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if r.Result != false {
		t.Fatalf("expected false for unknown address, got %v", r.Result)
	}
}
