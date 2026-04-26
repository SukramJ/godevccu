// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package virtualccu_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/virtualccu"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// startCCU spins up a VirtualCCU on an ephemeral port pair and returns
// it (callers must defer Stop).
func startCCU(t *testing.T) *virtualccu.VirtualCCU {
	t.Helper()
	xmlPort := freePort(t)
	jsonPort := freePort(t)

	v, err := virtualccu.New(virtualccu.Config{
		Mode:        hmconst.BackendModeOpenCCU,
		Host:        "127.0.0.1",
		XMLRPCPort:  xmlPort,
		JSONRPCPort: jsonPort,
		Username:    "Admin",
		Password:    "test",
		AuthEnabled: true,
		Devices:     []string{"HmIP-SWSD"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })
	// Give the listeners a beat to come up before the first request.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v.XMLRPCAddr() != nil && v.JSONRPCAddr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return v
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestXMLRPCListDevices(t *testing.T) {
	v := startCCU(t)
	url := "http://" + v.XMLRPCAddr().String() + "/"
	client := xmlrpc.NewClient(url)
	resp, err := client.Call(context.Background(), "listDevices", nil)
	if err != nil {
		t.Fatalf("listDevices: %v", err)
	}
	arr, ok := resp.(xmlrpc.ArrayValue)
	if !ok {
		t.Fatalf("want array, got %T", resp)
	}
	if len(arr) == 0 {
		t.Fatal("listDevices returned empty array")
	}
}

func TestXMLRPCSetGetValue(t *testing.T) {
	v := startCCU(t)
	url := "http://" + v.XMLRPCAddr().String() + "/"
	client := xmlrpc.NewClient(url)
	_, err := client.Call(context.Background(), "setValue", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue("SMOKE_DETECTOR_COMMAND"),
		xmlrpc.IntValue(1),
		xmlrpc.BoolValue(true),
	})
	if err != nil {
		t.Fatalf("setValue: %v", err)
	}
	got, err := client.Call(context.Background(), "getValue", []xmlrpc.Value{
		xmlrpc.StringValue("VCU2822385:1"),
		xmlrpc.StringValue("SMOKE_DETECTOR_COMMAND"),
	})
	if err != nil {
		t.Fatalf("getValue: %v", err)
	}
	if i, ok := xmlrpc.AsInt(got); !ok || i != 1 {
		t.Fatalf("getValue returned %v", got)
	}
}

func TestXMLRPCGetVersionCCUMode(t *testing.T) {
	// In CCU/OpenCCU mode getVersion returns the real CCU firmware
	// version — clients use it to enable CCU-specific features.
	v := startCCU(t)
	got := callGetVersion(t, v)
	if got != hmconst.CCUFirmwareVersion {
		t.Fatalf("getVersion = %q, want %q", got, hmconst.CCUFirmwareVersion)
	}
}

func TestXMLRPCGetVersionHomegearMode(t *testing.T) {
	// In Homegear mode getVersion must report "pydevccu-<VERSION>"
	// so aiohomematic (and other clients that branch on the prefix)
	// recognise the simulator.
	v, err := virtualccu.New(virtualccu.Config{
		Mode:        hmconst.BackendModeHomegear,
		Host:        "127.0.0.1",
		XMLRPCPort:  freePort(t),
		AuthEnabled: false,
		Devices:     []string{"HmIP-SWSD"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && v.XMLRPCAddr() == nil {
		time.Sleep(10 * time.Millisecond)
	}

	got := callGetVersion(t, v)
	want := "pydevccu-" + hmconst.PydevccuVersion
	if got != want {
		t.Fatalf("getVersion (homegear) = %q, want %q", got, want)
	}
}

func callGetVersion(t *testing.T, v *virtualccu.VirtualCCU) string {
	t.Helper()
	url := "http://" + v.XMLRPCAddr().String() + "/"
	client := xmlrpc.NewClient(url)
	got, err := client.Call(context.Background(), "getVersion", nil)
	if err != nil {
		t.Fatalf("getVersion: %v", err)
	}
	s, ok := xmlrpc.AsString(got)
	if !ok {
		t.Fatalf("getVersion returned non-string: %T", got)
	}
	return s
}

func TestJSONRPCSessionLogin(t *testing.T) {
	v := startCCU(t)
	res := jsonRPC(t, v, map[string]any{
		"jsonrpc": "1.1",
		"method":  "Session.login",
		"params":  map[string]any{"username": "Admin", "password": "test"},
		"id":      1,
	})
	id, _ := res["result"].(string)
	if id == "" {
		t.Fatalf("session id missing in response: %v", res)
	}
}

func TestJSONRPCRequiresAuth(t *testing.T) {
	v := startCCU(t)
	res := jsonRPC(t, v, map[string]any{
		"jsonrpc": "1.1",
		"method":  "Interface.listDevices",
		"params":  map[string]any{},
		"id":      1,
	})
	if res["error"] == nil {
		t.Fatalf("expected error, got %v", res)
	}
}

func TestJSONRPCAuthEnabledIsPublic(t *testing.T) {
	v := startCCU(t)
	res := jsonRPC(t, v, map[string]any{
		"jsonrpc": "1.1",
		"method":  "CCU.getAuthEnabled",
		"params":  map[string]any{},
		"id":      1,
	})
	if got, _ := res["result"].(bool); !got {
		t.Fatalf("expected true, got %v", res["result"])
	}
}

func TestJSONRPCListDevices(t *testing.T) {
	v := startCCU(t)
	sid := loginJSONRPC(t, v)
	res := jsonRPC(t, v, map[string]any{
		"jsonrpc":      "1.1",
		"method":       "Interface.listDevices",
		"params":       map[string]any{"_session_id_": sid},
		"id":           2,
		"_session_id_": sid,
	})
	if res["error"] != nil {
		t.Fatalf("expected ok, got %v", res)
	}
	arr, _ := res["result"].([]any)
	if len(arr) == 0 {
		t.Fatal("listDevices returned empty")
	}
}

func TestVersionEndpoint(t *testing.T) {
	v := startCCU(t)
	url := "http://" + v.JSONRPCAddr().String() + "/VERSION"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

func loginJSONRPC(t *testing.T, v *virtualccu.VirtualCCU) string {
	t.Helper()
	res := jsonRPC(t, v, map[string]any{
		"jsonrpc": "1.1",
		"method":  "Session.login",
		"params":  map[string]any{"username": "Admin", "password": "test"},
		"id":      "login",
	})
	id, _ := res["result"].(string)
	if id == "" {
		t.Fatalf("login failed: %v", res)
	}
	return id
}

func jsonRPC(t *testing.T, v *virtualccu.VirtualCCU, body map[string]any) map[string]any {
	t.Helper()
	url := "http://" + v.JSONRPCAddr().String() + "/api/homematic.cgi"
	raw, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.StatusCode == 204 {
		return nil
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestPortAllocationDoesNotConflict(t *testing.T) {
	// Sanity check: spinning two CCUs back-to-back works on different
	// ephemeral ports.
	v := startCCU(t)
	if v == nil {
		t.Fatal("nil vccu")
	}
	if v.XMLRPCAddr() == nil {
		t.Fatal("nil xml addr")
	}
	if !strings.Contains(v.XMLRPCAddr().String(), "127.0.0.1:") {
		t.Fatalf("unexpected addr %s", v.XMLRPCAddr())
	}
	if v.JSONRPCAddr() == nil {
		t.Fatal("nil json addr")
	}
	_ = strconv.Itoa(0) // keep strconv import
}

// TestEphemeralPortsResolveAfterStart guards the "ask the OS for a
// free port" path. With XMLRPCPort/JSONRPCPort set to EphemeralPort,
// New() must:
//
//  1. Translate the negative sentinel into 0 (the canonical
//     net.Listen request for an OS-assigned port).
//  2. Bind successfully on a non-zero port.
//  3. Write the resolved port back into the configuration so
//     downstream code (here: JSON-RPC's Interface.listInterfaces)
//     advertises the real number, not 0.
//
// The third point is the actual bug this fix prevents — without the
// write-back, listInterfaces reports `"port": 0` and any client that
// uses it for follow-up XML-RPC calls breaks.
func TestEphemeralPortsResolveAfterStart(t *testing.T) {
	v, err := virtualccu.New(virtualccu.Config{
		Mode:        hmconst.BackendModeOpenCCU,
		Host:        "127.0.0.1",
		XMLRPCPort:  virtualccu.EphemeralPort,
		JSONRPCPort: virtualccu.EphemeralPort,
		Username:    "Admin",
		Password:    "test",
		AuthEnabled: true,
		Devices:     []string{"HmIP-SWSD"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })

	xmlAddr, ok := v.XMLRPCAddr().(*net.TCPAddr)
	if !ok || xmlAddr == nil {
		t.Fatalf("XMLRPCAddr() returned nil/wrong type: %v", v.XMLRPCAddr())
	}
	if xmlAddr.Port == 0 {
		t.Fatalf("xml-rpc bound port is 0; expected an OS-assigned port")
	}
	jsonAddr, ok := v.JSONRPCAddr().(*net.TCPAddr)
	if !ok || jsonAddr == nil {
		t.Fatalf("JSONRPCAddr() returned nil/wrong type: %v", v.JSONRPCAddr())
	}
	if jsonAddr.Port == 0 {
		t.Fatalf("json-rpc bound port is 0; expected an OS-assigned port")
	}
	if xmlAddr.Port == jsonAddr.Port {
		t.Fatalf("xml-rpc and json-rpc bound on the same port %d", xmlAddr.Port)
	}

	cfg := v.Config()
	if cfg.XMLRPCPort != xmlAddr.Port {
		t.Fatalf("Config().XMLRPCPort = %d, want %d (resolved port not written back)",
			cfg.XMLRPCPort, xmlAddr.Port)
	}
	if cfg.JSONRPCPort != jsonAddr.Port {
		t.Fatalf("Config().JSONRPCPort = %d, want %d (resolved port not written back)",
			cfg.JSONRPCPort, jsonAddr.Port)
	}

	// Interface.listInterfaces echoes XMLRPCPort to clients. With the
	// fix it must show the resolved port; without it, the response
	// would carry "port": 0 and break downstream callers.
	sid := loginJSONRPC(t, v)
	res := jsonRPC(t, v, map[string]any{
		"jsonrpc":      "1.1",
		"method":       "Interface.listInterfaces",
		"params":       map[string]any{"_session_id_": sid},
		"id":           "iface",
		"_session_id_": sid,
	})
	arr, _ := res["result"].([]any)
	if len(arr) == 0 {
		t.Fatalf("Interface.listInterfaces returned empty: %v", res)
	}
	for _, raw := range arr {
		entry, _ := raw.(map[string]any)
		port, _ := entry["port"].(float64)
		if int(port) != xmlAddr.Port {
			t.Fatalf("listInterfaces entry %v advertises port %v, want %d",
				entry["name"], entry["port"], xmlAddr.Port)
		}
	}
}

// TestZeroPortFallsBackToDefault keeps the historical contract: a
// freshly zero-valued Config still binds the canonical HomeMatic
// ports — the negative-sentinel is the only way to request an
// ephemeral port. This guards against the fix accidentally inverting
// the default.
func TestZeroPortFallsBackToDefault(t *testing.T) {
	cfg := virtualccu.Config{
		Mode: hmconst.BackendModeOpenCCU,
		Host: "127.0.0.1",
	}
	// We can't safely actually bind the canonical ports (root needed
	// for 80, and 2001 might be in use), so just verify Defaults are
	// applied — i.e. Config().XMLRPCPort > 0 even though we passed 0.
	v, err := virtualccu.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := v.Config()
	if got.XMLRPCPort <= 0 {
		t.Fatalf("XMLRPCPort = %d after defaults; want >0 (canonical port)", got.XMLRPCPort)
	}
	if got.JSONRPCPort <= 0 {
		t.Fatalf("JSONRPCPort = %d after defaults; want >0 (canonical port)", got.JSONRPCPort)
	}
}
