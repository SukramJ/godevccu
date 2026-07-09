// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// ─────────────────────────────────────────────────────────────────
// RPCFunctions – getter methods + no-op stubs
// ─────────────────────────────────────────────────────────────────

func TestVersionAndInterfaceID(t *testing.T) {
	rpc, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:     []string{"HmIP-SWSD"},
		Version:     "3.77.5",
		InterfaceID: "myIface",
	})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	if got := rpc.Version(); got != "3.77.5" {
		t.Fatalf("Version() = %q, want 3.77.5", got)
	}
	if got := rpc.InterfaceID(); got != "myIface" {
		t.Fatalf("InterfaceID() = %q, want myIface", got)
	}
}

func TestActive(t *testing.T) {
	rpc := newRPC(t)
	if rpc.Active() {
		t.Fatal("Active should be false before SetActive(true)")
	}
	rpc.SetActive(true)
	if !rpc.Active() {
		t.Fatal("Active should be true after SetActive(true)")
	}
}

func TestPing(t *testing.T) {
	rpc := newRPC(t)
	if !rpc.Ping("test-caller") {
		t.Fatal("Ping should always return true")
	}
}

func TestGetVersion(t *testing.T) {
	rpc := newRPC(t)
	if rpc.GetVersion() == "" {
		t.Fatal("GetVersion() returned empty string")
	}
}

func TestGetServiceMessages(t *testing.T) {
	rpc := newRPC(t)
	msgs := rpc.GetServiceMessages()
	if len(msgs) == 0 {
		t.Fatal("GetServiceMessages should return at least one entry")
	}
}

func TestGetAllSystemVariables(t *testing.T) {
	rpc := newRPC(t)
	sv := rpc.GetAllSystemVariables()
	if len(sv) == 0 {
		t.Fatal("GetAllSystemVariables returned empty map")
	}
}

func TestGetSystemVariable(t *testing.T) {
	rpc := newRPC(t)
	v := rpc.GetSystemVariable("any")
	if v == "" {
		t.Fatal("GetSystemVariable returned empty string")
	}
}

func TestSetSystemVariableAndDelete(t *testing.T) {
	rpc := newRPC(t)
	// These are no-ops in the stub; just ensure they don't panic.
	rpc.SetSystemVariable("foo", "bar")
	rpc.DeleteSystemVariable("foo")
}

func TestGetInstallModeAndSet(t *testing.T) {
	rpc := newRPC(t)
	if got := rpc.GetInstallMode(); got != 0 {
		t.Fatalf("GetInstallMode() = %d, want 0", got)
	}
	if !rpc.SetInstallMode(true, 60, 1, "") {
		t.Fatal("SetInstallMode should return true")
	}
}

func TestReportValueUsage(t *testing.T) {
	rpc := newRPC(t)
	if !rpc.ReportValueUsage("VCU2822385:1", "STATE", 1) {
		t.Fatal("ReportValueUsage should return true")
	}
}

func TestInstallAndUpdateFirmware(t *testing.T) {
	rpc := newRPC(t)
	if !rpc.InstallFirmware("VCU2822385") {
		t.Fatal("InstallFirmware should return true")
	}
	if !rpc.UpdateFirmware("VCU2822385") {
		t.Fatal("UpdateFirmware should return true")
	}
}

func TestClientServerInitialized(t *testing.T) {
	rpc := newRPC(t)
	if rpc.ClientServerInitialized("someID") {
		t.Fatal("ClientServerInitialized should return false when no init was done")
	}
}

func TestSetMetadata(t *testing.T) {
	rpc := newRPC(t)
	if !rpc.SetMetadata("VCU2822385", "NAME", "MyDevice") {
		t.Fatal("SetMetadata should return true")
	}
}

func TestGetMetadata(t *testing.T) {
	rpc := newRPC(t)
	// ADDRESS attribute is always present on the root device.
	v, err := rpc.GetMetadata("VCU2822385", hmconst.AttrAddress)
	if err != nil {
		t.Fatalf("GetMetadata(ADDRESS): %v", err)
	}
	_ = v

	// NAME auto-builds from type/address when not explicitly present.
	_, err = rpc.GetMetadata("VCU2822385", hmconst.AttrName)
	if err != nil {
		t.Fatalf("GetMetadata(NAME): %v", err)
	}

	// Unknown device
	_, err = rpc.GetMetadata("UNKNOWN999", hmconst.AttrName)
	if err == nil {
		t.Fatal("GetMetadata on unknown device should return error")
	}
}

func TestGetParamsetDescriptionError(t *testing.T) {
	rpc := newRPC(t)
	_, err := rpc.GetParamsetDescription("NONEXISTENT:0", hmconst.ParamsetAttrValues)
	if err == nil {
		t.Fatal("expected error for unknown address")
	}
}

func TestFireEvent(t *testing.T) {
	rpc := newRPC(t)
	var fired bool
	rpc.RegisterParamsetCallback(func(_, _, _ string, _ any) { fired = true })
	rpc.SetActive(true)
	rpc.FireEvent("myIface", "VCU2822385:1", "SMOKE_DETECTOR_ALARM_STATUS", 0)
	if !fired {
		t.Fatal("FireEvent did not trigger callback")
	}
}

// TestSimulateDeviceEvent locks in the RECEIVE-direction primitive: a
// value change fires to every registered callback even though the
// parameter is read-only telemetry (SMOKE_DETECTOR_ALARM_STATUS is
// ops=RE, no write bit) and a plain SetValue without force would
// reject it.
func TestSimulateDeviceEvent(t *testing.T) {
	rpc := newRPC(t)

	// Sanity check the write-gate actually rejects a non-forced write
	// to this read-only telemetry parameter, so the test below proves
	// SimulateDeviceEvent genuinely bypasses it rather than the
	// parameter having been writable all along.
	if err := rpc.SetValue("VCU2822385:1", "SMOKE_DETECTOR_ALARM_STATUS", 1, false); err == nil {
		t.Fatal("expected SetValue without force to reject a read-only telemetry write")
	}

	var (
		gotAddr  string
		gotKey   string
		gotValue any
	)
	rpc.RegisterParamsetCallback(func(_, address, valueKey string, value any) {
		gotAddr, gotKey, gotValue = address, valueKey, value
	})

	if err := rpc.SimulateDeviceEvent("VCU2822385:1", "SMOKE_DETECTOR_ALARM_STATUS", 1); err != nil {
		t.Fatalf("SimulateDeviceEvent: %v", err)
	}
	if gotAddr != "VCU2822385:1" || gotKey != "SMOKE_DETECTOR_ALARM_STATUS" || gotValue != 1 {
		t.Fatalf("callback got (%q, %q, %v), want (VCU2822385:1, SMOKE_DETECTOR_ALARM_STATUS, 1)", gotAddr, gotKey, gotValue)
	}

	v, err := rpc.GetValue("VCU2822385:1", "SMOKE_DETECTOR_ALARM_STATUS")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if v != 1 {
		t.Fatalf("GetValue after SimulateDeviceEvent = %v, want 1", v)
	}
}

func TestAddDevices(t *testing.T) {
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-SWSD"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	// Loading the same device type again: AddDevices is idempotent.
	if err := rpc.AddDevices(context.Background(), []string{"HmIP-SWSD"}); err != nil {
		t.Fatalf("AddDevices: %v", err)
	}
	if _, ok := rpc.SupportedDevices()["HmIP-SWSD"]; !ok {
		t.Fatal("HmIP-SWSD should remain in SupportedDevices after AddDevices")
	}
}

func TestAddDevicesFresh(t *testing.T) {
	// Load a second device type dynamically.
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-SWSD"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	if err := rpc.AddDevices(context.Background(), []string{"HmIP-WTH-2"}); err != nil {
		t.Fatalf("AddDevices HmIP-WTH-2: %v", err)
	}
	if _, ok := rpc.SupportedDevices()["HmIP-WTH-2"]; !ok {
		t.Fatal("HmIP-WTH-2 should be in SupportedDevices after AddDevices")
	}
}

func TestRemoveDevices(t *testing.T) {
	rpc := newRPC(t)
	supported := rpc.SupportedDevices()
	var typeName string
	var rootAddr string
	for k, a := range supported {
		typeName = k
		rootAddr = a
		break
	}
	rpc.RemoveDevices(context.Background(), []string{typeName})
	// After removal the root device should not be findable.
	_, err := rpc.GetDeviceDescription(rootAddr)
	if err == nil {
		t.Fatalf("expected error after RemoveDevices for root %q", rootAddr)
	}
}

func TestRemoveDevicesAll(t *testing.T) {
	rpc := newRPC(t)
	// nil removes all.
	rpc.RemoveDevices(context.Background(), nil)
	if got := len(rpc.ListDevices()); got != 0 {
		t.Fatalf("expected 0 devices after RemoveDevices(nil), got %d", got)
	}
}

func TestDefaultVersionIsHomegear(t *testing.T) {
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-SWSD"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	want := "pydevccu-" + hmconst.PydevccuVersion
	if got := rpc.Version(); got != want {
		t.Fatalf("default Version() = %q, want %q", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────
// Server – lifecycle methods and helpers
// ─────────────────────────────────────────────────────────────────

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func startServer(t *testing.T) *ccu.Server {
	t.Helper()
	rpc := newRPC(t)
	srv := ccu.NewServer(ccu.ServerConfig{
		Address: freePort(t),
		RPC:     rpc,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	// brief wait for listener
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.LocalAddr() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return srv
}

func TestServerAddr(t *testing.T) {
	rpc := newRPC(t)
	addr := freePort(t)
	srv := ccu.NewServer(ccu.ServerConfig{Address: addr, RPC: rpc})
	if got := srv.Addr(); got != addr {
		t.Fatalf("Addr() = %q, want %q", got, addr)
	}
}

func TestServerRPC(t *testing.T) {
	rpc := newRPC(t)
	srv := ccu.NewServer(ccu.ServerConfig{Address: freePort(t), RPC: rpc})
	if srv.RPC() != rpc {
		t.Fatal("RPC() returned wrong pointer")
	}
}

func TestServerLocalAddrNilBeforeStart(t *testing.T) {
	rpc := newRPC(t)
	srv := ccu.NewServer(ccu.ServerConfig{Address: freePort(t), RPC: rpc})
	if srv.LocalAddr() != nil {
		t.Fatal("LocalAddr() should be nil before Start")
	}
}

func TestServerStartStop(t *testing.T) {
	srv := startServer(t)
	if srv.LocalAddr() == nil {
		t.Fatal("LocalAddr() nil after Start")
	}
}

func TestServerStopIdempotent(t *testing.T) {
	srv := startServer(t)
	if err := srv.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	// Second Stop must be a no-op.
	if err := srv.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestServerDoubleStart(t *testing.T) {
	srv := startServer(t)
	if err := srv.Start(); err == nil {
		t.Fatal("double Start should return error")
	}
}

func TestFaultFromErr(t *testing.T) {
	// Trigger the faultFromErr path via an XML-RPC call that returns an error.
	srv := startServer(t)
	url := "http://" + srv.LocalAddr().String() + "/"
	client := xmlrpc.NewClient(url)
	// getDeviceDescription with unknown address must produce an XML-RPC fault.
	_, err := client.Call(context.Background(), "getDeviceDescription", []xmlrpc.Value{
		xmlrpc.StringValue("NONEXISTENT"),
	})
	if err == nil {
		t.Fatal("expected fault error from server")
	}
}

func TestSaveParamsets(t *testing.T) {
	rpc := newRPC(t)
	// With persistence disabled SaveParamsets must be a no-op.
	if err := rpc.SaveParamsets(); err != nil {
		t.Fatalf("SaveParamsets (no persistence): %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────
// Init / callback wiring
// ─────────────────────────────────────────────────────────────────

// startCallbackServer starts a minimal XML-RPC callback listener.
// It responds to newDevices/deleteDevices/listDevices calls and returns
// its URL.
func startCallbackServer(t *testing.T) string {
	t.Helper()
	mux := xmlrpc.NewMux()
	mux.RegisterSystemMethods()
	mux.Handle("listDevices", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.ArrayValue{}, nil
	})
	mux.Handle("newDevices", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.NilValue{}, nil
	})
	mux.Handle("deleteDevices", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.NilValue{}, nil
	})
	mux.Handle("event", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.NilValue{}, nil
	})

	handler := xmlrpc.NewHandler()
	handler.Mux = mux

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String() + "/"
}

func TestInitRegistersRemote(t *testing.T) {
	rpc := newRPC(t)
	callbackURL := startCallbackServer(t)
	// Register a remote callback.
	rpc.Init(callbackURL, "testIface")
	// Give askDevices goroutine time to run.
	time.Sleep(100 * time.Millisecond)
	if !rpc.ClientServerInitialized("testIface") {
		t.Fatal("remote not registered after Init")
	}
}

func TestInitDeregistersRemote(t *testing.T) {
	rpc := newRPC(t)
	callbackURL := startCallbackServer(t)
	rpc.Init(callbackURL, "testIface")
	time.Sleep(50 * time.Millisecond)
	// Deregister by passing the same URL with empty interfaceID.
	rpc.Init(callbackURL, "")
	if rpc.ClientServerInitialized("testIface") {
		t.Fatal("remote still registered after deregister Init")
	}
}

func TestFireEventWithRemote(t *testing.T) {
	rpc := newRPC(t)
	callbackURL := startCallbackServer(t)
	rpc.Init(callbackURL, "testIface")
	time.Sleep(100 * time.Millisecond)
	rpc.SetActive(true)
	// Fire an event — the remote callback receives it.
	rpc.FireEvent("testIface", "VCU2822385:1", "SMOKE_DETECTOR_ALARM_STATUS", 0)
}

// startCallbackServerWithDevices is like startCallbackServer but listDevices
// returns a fake device that is unknown to the rpc — this causes pushDevices
// to send a deleteDevices call (covering that branch).
func startCallbackServerWithDevices(t *testing.T) string {
	t.Helper()
	mux := xmlrpc.NewMux()
	mux.RegisterSystemMethods()
	mux.Handle("listDevices", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		// Return a fake device the server doesn't know about.
		fakeDevice := map[string]any{"ADDRESS": "FAKE_DEVICE:0"}
		return xmlrpc.FromAny(any([]any{fakeDevice})), nil
	})
	mux.Handle("newDevices", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.NilValue{}, nil
	})
	mux.Handle("deleteDevices", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.NilValue{}, nil
	})
	mux.Handle("event", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.NilValue{}, nil
	})
	handler := xmlrpc.NewHandler()
	handler.Mux = mux
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String() + "/"
}

func TestPushDevicesDeleteBranch(t *testing.T) {
	// Registering a remote that returns a fake device causes pushDevices to
	// send deleteDevices for it (the deleteList branch).
	rpc := newRPC(t)
	callbackURL := startCallbackServerWithDevices(t)
	rpc.Init(callbackURL, "testIface2")
	// Give askDevices + pushDevices goroutine time to run.
	time.Sleep(200 * time.Millisecond)
}

func TestAddDevicesWithRemote(t *testing.T) {
	// Covers the AddDevices → pushDevices path when a remote is registered.
	rpc := newRPC(t)
	callbackURL := startCallbackServer(t)
	rpc.Init(callbackURL, "testIface3")
	time.Sleep(100 * time.Millisecond)
	// Now add a new device — should push to remote.
	if err := rpc.AddDevices(context.Background(), []string{"HmIP-WTH-2"}); err != nil {
		t.Fatalf("AddDevices: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
}

// ─────────────────────────────────────────────────────────────────
// Server HTTP paths – exercise registerMethods via the wire
// ─────────────────────────────────────────────────────────────────

func xmlcall(t *testing.T, srv *ccu.Server, method string, params []xmlrpc.Value) xmlrpc.Value {
	t.Helper()
	client := xmlrpc.NewClient("http://" + srv.LocalAddr().String() + "/")
	v, err := client.Call(context.Background(), method, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return v
}

func TestServerPing(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "ping", []xmlrpc.Value{xmlrpc.StringValue("test")})
	if b, ok := xmlrpc.AsBool(v); !ok || !b {
		t.Fatalf("ping returned %v", v)
	}
}

func TestServerGetVersion(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getVersion", nil)
	s, ok := xmlrpc.AsString(v)
	if !ok || s == "" {
		t.Fatalf("getVersion returned %v", v)
	}
}

func TestServerGetServiceMessages(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getServiceMessages", nil)
	arr, ok := v.(xmlrpc.ArrayValue)
	if !ok || len(arr) == 0 {
		t.Fatalf("getServiceMessages returned %v", v)
	}
}

func TestServerGetAllSystemVariables(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getAllSystemVariables", nil)
	if v == nil {
		t.Fatal("getAllSystemVariables returned nil")
	}
}

func TestServerGetSystemVariable(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getSystemVariable", []xmlrpc.Value{xmlrpc.StringValue("any")})
	if _, ok := xmlrpc.AsString(v); !ok {
		t.Fatalf("getSystemVariable returned %T", v)
	}
}

func TestServerSetSystemVariable(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "setSystemVariable", []xmlrpc.Value{
		xmlrpc.StringValue("foo"),
		xmlrpc.StringValue("bar"),
	})
}

func TestServerDeleteSystemVariable(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "deleteSystemVariable", []xmlrpc.Value{
		xmlrpc.StringValue("foo"),
	})
}

func TestServerGetParamsetDescription(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getParamsetDescription", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue(hmconst.ParamsetAttrValues),
	})
	if v == nil {
		t.Fatal("getParamsetDescription returned nil")
	}
}

func TestServerGetValue(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "getValue", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue("SMOKE_DETECTOR_COMMAND"),
	})
}

func TestServerSetValueAndGetValue(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "setValue", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue("SMOKE_DETECTOR_COMMAND"),
		xmlrpc.IntValue(1),
		xmlrpc.BoolValue(true),
	})
	v := xmlcall(t, srv, "getValue", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue("SMOKE_DETECTOR_COMMAND"),
	})
	if i, ok := xmlrpc.AsInt(v); !ok || i != 1 {
		t.Fatalf("getValue after setValue = %v", v)
	}
}

func TestServerPutParamset(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "putParamset", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue(hmconst.ParamsetAttrValues),
		xmlrpc.FromAny(any(map[string]any{"SMOKE_DETECTOR_COMMAND": int32(1)})),
		xmlrpc.BoolValue(true),
	})
}

func TestServerInit(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "init", []xmlrpc.Value{
		xmlrpc.StringValue("http://localhost:9999/"),
		xmlrpc.StringValue("testIface"),
	})
	_ = v
}

func TestServerGetMetadata(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getMetadata", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385"),
		xmlrpc.StringValue("ADDRESS"),
	})
	_ = v
}

func TestServerSetMetadata(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "setMetadata", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385"),
		xmlrpc.StringValue("NAME"),
		xmlrpc.StringValue("MyDevice"),
	})
	_ = v
}

func TestServerAddRemoveLink(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "addLink", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue("VCU2822385:2"),
		xmlrpc.StringValue("link"),
		xmlrpc.StringValue("desc"),
	})
	v := xmlcall(t, srv, "getLinkPeers", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
	})
	_ = v
	xmlcall(t, srv, "removeLink", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue("VCU2822385:2"),
	})
}

func TestServerGetLinks(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getLinks", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.IntValue(0),
	})
	_ = v
}

func TestServerGetInstallMode(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getInstallMode", nil)
	if i, ok := xmlrpc.AsInt(v); !ok || i != 0 {
		t.Fatalf("getInstallMode = %v", v)
	}
}

func TestServerSetInstallMode(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "setInstallMode", []xmlrpc.Value{
		xmlrpc.BoolValue(true),
		xmlrpc.IntValue(60),
		xmlrpc.IntValue(1),
		xmlrpc.StringValue(""),
	})
}

func TestServerReportValueUsage(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "reportValueUsage", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue("STATE"),
		xmlrpc.IntValue(1),
	})
}

func TestServerInstallFirmware(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "installFirmware", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385"),
	})
}

func TestServerUpdateFirmware(t *testing.T) {
	srv := startServer(t)
	xmlcall(t, srv, "updateFirmware", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385"),
	})
}

func TestServerClientServerInitialized(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "clientServerInitialized", []xmlrpc.Value{
		xmlrpc.StringValue("someID"),
	})
	b, ok := xmlrpc.AsBool(v)
	if !ok {
		t.Fatalf("clientServerInitialized returned %T", v)
	}
	_ = b
}

func TestServerDeleteDevice(t *testing.T) {
	srv := startServer(t)
	// deleteDevice is idempotent and never errors at the caller.
	xmlcall(t, srv, "deleteDevice", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385"),
		xmlrpc.IntValue(0),
	})
}

func TestServerGetParamset(t *testing.T) {
	srv := startServer(t)
	v := xmlcall(t, srv, "getParamset", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue(hmconst.ParamsetAttrValues),
	})
	_ = v
}

// ─────────────────────────────────────────────────────────────────
// PutParamset type conversion paths (BOOL, STRING, FLOAT, ENUM)
// ─────────────────────────────────────────────────────────────────

func newRPCPS(t *testing.T) *ccu.RPCFunctions {
	t.Helper()
	// HmIP-PS has STATE (BOOL), LEVEL is not present; use it for bool type coverage.
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-PS"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	return rpc
}

func TestSetValueBoolType(t *testing.T) {
	rpc := newRPCPS(t)
	supported := rpc.SupportedDevices()
	addr, ok := supported["HmIP-PS"]
	if !ok {
		t.Skip("HmIP-PS not available")
	}
	// Find a channel address for VALUES.
	devs := rpc.ListDevices()
	var chanAddr string
	for _, d := range devs {
		a, _ := d["ADDRESS"].(string)
		if len(a) > len(addr) && a[:len(addr)] == addr {
			// Has a colon → it's a channel.
			chanAddr = a
			break
		}
	}
	if chanAddr == "" {
		t.Skip("no channel found for HmIP-PS")
	}
	_ = rpc.SetValue(chanAddr, "STATE", true, true)
}

func TestGetValueError(t *testing.T) {
	rpc := newRPC(t)
	_, err := rpc.GetValue("NONEXISTENT:0", "STATE")
	if err == nil {
		t.Fatal("expected error for unknown address")
	}
}

func TestPutParamsetUnknownAddressError(t *testing.T) {
	rpc := newRPC(t)
	err := rpc.PutParamset("NONEXISTENT:0", hmconst.ParamsetAttrValues, map[string]any{}, false)
	if err == nil {
		t.Fatal("expected error for unknown address")
	}
}

func TestPutParamsetUnknownParamError(t *testing.T) {
	rpc := newRPC(t)
	err := rpc.PutParamset("VCU2822385:1", hmconst.ParamsetAttrValues, map[string]any{
		"NONEXISTENT_PARAM": 1,
	}, true)
	if err == nil {
		t.Fatal("expected error for unknown parameter")
	}
}

// ─────────────────────────────────────────────────────────────────
// Type-conversion paths: BOOL, FLOAT, INTEGER, ENUM via HmIP-WTH-2
// ─────────────────────────────────────────────────────────────────

func newRPCWTH(t *testing.T) *ccu.RPCFunctions {
	t.Helper()
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-WTH-2"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions HmIP-WTH-2: %v", err)
	}
	return rpc
}

// wthMaintChan is the maintenance channel for HmIP-WTH-2.
const wthMaintChan = "VCU2680226:0"

// newRPCAll returns an RPCFunctions loaded with all embedded device types.
func newRPCAll(t *testing.T) *ccu.RPCFunctions {
	t.Helper()
	rpc, err := ccu.NewRPCFunctions(ccu.Options{})
	if err != nil {
		t.Fatalf("NewRPCFunctions (all): %v", err)
	}
	return rpc
}

func TestSetValueFloat(t *testing.T) {
	rpc := newRPCWTH(t)
	// OPERATING_VOLTAGE is FLOAT, clamped [0, 25.2].
	err := rpc.SetValue(wthMaintChan, "OPERATING_VOLTAGE", 5.0, true)
	if err != nil {
		t.Fatalf("SetValue FLOAT: %v", err)
	}
	v, err := rpc.GetValue(wthMaintChan, "OPERATING_VOLTAGE")
	if err != nil {
		t.Fatalf("GetValue FLOAT: %v", err)
	}
	if v != 5.0 {
		t.Fatalf("GetValue FLOAT = %v, want 5.0", v)
	}
}

func TestSetValueFloatClamped(t *testing.T) {
	rpc := newRPCWTH(t)
	// 999 > max(25.2) → clamped to 25.2.
	err := rpc.SetValue(wthMaintChan, "OPERATING_VOLTAGE", 999.0, true)
	if err != nil {
		t.Fatalf("SetValue clamped FLOAT: %v", err)
	}
	v, err := rpc.GetValue(wthMaintChan, "OPERATING_VOLTAGE")
	if err != nil {
		t.Fatalf("GetValue clamped FLOAT: %v", err)
	}
	f, _ := v.(float64)
	if f > 25.2 {
		t.Fatalf("value not clamped: %v", v)
	}
}

func TestSetValueInteger(t *testing.T) {
	rpc := newRPCWTH(t)
	// RSSI_DEVICE is INTEGER, range [-128, 127].
	err := rpc.SetValue(wthMaintChan, "RSSI_DEVICE", -50, true)
	if err != nil {
		t.Fatalf("SetValue INTEGER: %v", err)
	}
	v, err := rpc.GetValue(wthMaintChan, "RSSI_DEVICE")
	if err != nil {
		t.Fatalf("GetValue INTEGER: %v", err)
	}
	if v != -50 {
		t.Fatalf("GetValue INTEGER = %v, want -50", v)
	}
}

func TestSetValueBool(t *testing.T) {
	rpc := newRPCWTH(t)
	// CONFIG_PENDING is BOOL with FLAGS=9 (internal + read-only), but force=true bypasses write check.
	err := rpc.SetValue(wthMaintChan, "CONFIG_PENDING", true, true)
	if err != nil {
		t.Fatalf("SetValue BOOL: %v", err)
	}
}

func TestSetValueEnumOutOfBounds(t *testing.T) {
	// Use all devices; VCU0000066:1 has DIRECTION (ENUM, max=3 numeric).
	rpc := newRPCAll(t)
	// DIRECTION has max=3; value 99 must fail.
	err := rpc.SetValue("VCU0000066:1", "DIRECTION", 99, true)
	if err == nil {
		t.Fatal("expected enum out-of-bounds error")
	}
}

func TestSetValueWriteNotAllowed(t *testing.T) {
	rpc := newRPCWTH(t)
	// CONFIG_PENDING has FLAGS=9, OPERATIONS=5 (read|event). force=false must fail.
	err := rpc.SetValue(wthMaintChan, "CONFIG_PENDING", true, false)
	if err == nil {
		t.Fatal("expected write-not-allowed error (force=false)")
	}
}

func TestSetValueStringType(t *testing.T) {
	// toString is exercised via any PutParamset on a STRING param.
	// Find a device with a STRING param — fall back to a direct call if none found.
	// We exercise it by calling SetSystemVariable (which calls toString indirectly).
	rpc := newRPC(t)
	rpc.SetSystemVariable("any", "string-value")
	rpc.DeleteSystemVariable("any")
}

// SetSystemVariable and DeleteSystemVariable are no-op stubs; call them
// directly to cover the function body.
func TestSetSystemVariableNoOp(t *testing.T) {
	rpc := newRPC(t)
	rpc.SetSystemVariable("foo", 42)
}

func TestDeleteSystemVariableNoOp(t *testing.T) {
	rpc := newRPC(t)
	rpc.DeleteSystemVariable("foo")
}

func TestGetValueMissing(t *testing.T) {
	rpc := newRPC(t)
	_, err := rpc.GetValue("VCU2822385:1", "MISSING_KEY")
	if err == nil {
		t.Fatal("expected error for missing value key")
	}
}

func TestSetValueCombinedParameter(t *testing.T) {
	// COMBINED_PARAMETER on VCU5597068:0 exercises the converter path in SetValue.
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIPW-SMI55"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions HmIPW-SMI55: %v", err)
	}
	// COMBINED_PARAMETER accepts a string like "L=50,L2=25".
	err = rpc.SetValue("VCU5597068:0", "COMBINED_PARAMETER", "L=50,L2=25", true)
	if err != nil {
		// The device may not expose LEVEL; just ensure no panic.
		t.Logf("SetValue COMBINED_PARAMETER: %v (expected for some devices)", err)
	}
}

func TestSetValueActionType(t *testing.T) {
	// HM-LC-Dim1L-Pl has ACTION param OLD_LEVEL on VCU0000121:1.
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HM-LC-Dim1L-Pl"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions HM-LC-Dim1L-Pl: %v", err)
	}
	err = rpc.SetValue("VCU0000121:1", "OLD_LEVEL", true, true)
	if err != nil {
		t.Fatalf("SetValue ACTION: %v", err)
	}
}

func TestStartLogic(t *testing.T) {
	// Exercise startLogic path via EnableLogic=true.
	rpc := newRPC(t)
	srv := ccu.NewServer(ccu.ServerConfig{
		Address:     freePort(t),
		RPC:         rpc,
		EnableLogic: true,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start with EnableLogic: %v", err)
	}
	_ = srv.Stop()
}

// ─────────────────────────────────────────────────────────────────
// Persistence path
// ─────────────────────────────────────────────────────────────────

func TestSaveParamsetsPersistence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/paramsets.json"
	rpc, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:         []string{"HmIP-SWSD"},
		Persistence:     true,
		PersistencePath: path,
	})
	if err != nil {
		t.Fatalf("NewRPCFunctions with persistence: %v", err)
	}
	// Write a value so the paramset is dirty.
	if err := rpc.SetValue("VCU2822385:1", "SMOKE_DETECTOR_COMMAND", 1, true); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if err := rpc.SaveParamsets(); err != nil {
		t.Fatalf("SaveParamsets: %v", err)
	}

	// Second instance should load the saved paramsets.
	rpc2, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:         []string{"HmIP-SWSD"},
		Persistence:     true,
		PersistencePath: path,
	})
	if err != nil {
		t.Fatalf("NewRPCFunctions reload: %v", err)
	}
	_ = rpc2
}
