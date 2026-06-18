// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package virtualccu_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/virtualccu"
)

// startCCUNotReady spins up a VirtualCCU that boots in its "still warming up"
// state (JSON-RPC 503, checkrega != OK) on an ephemeral port pair.
func startCCUNotReady(t *testing.T) *virtualccu.VirtualCCU {
	t.Helper()
	v, err := virtualccu.New(virtualccu.Config{
		Mode:          hmconst.BackendModeOpenCCU,
		Host:          "127.0.0.1",
		XMLRPCPort:    freePort(t),
		JSONRPCPort:   freePort(t),
		Username:      "Admin",
		Password:      "test",
		AuthEnabled:   true,
		Devices:       []string{"HmIP-SWSD"},
		StartNotReady: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v.JSONRPCAddr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return v
}

func checkRega(t *testing.T, v *virtualccu.VirtualCCU) (status int, body string) {
	t.Helper()
	resp, err := http.Get("http://" + v.JSONRPCAddr().String() + "/ise/checkrega.cgi")
	if err != nil {
		t.Fatalf("checkrega GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func jsonRPCStatus(t *testing.T, v *virtualccu.VirtualCCU) int {
	t.Helper()
	resp, err := http.Post("http://"+v.JSONRPCAddr().String()+"/api/homematic.cgi",
		"application/json", strings.NewReader(`{"method":"system.listMethods","params":{}}`))
	if err != nil {
		t.Fatalf("jsonrpc POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestReadinessGate verifies the simulated boot window: while not ready the
// CCU answers checkrega with a non-OK body and the JSON-RPC web API with 503;
// after SetReady(true) checkrega returns the literal "OK" and JSON-RPC serves.
func TestReadinessGate(t *testing.T) {
	t.Parallel()
	v := startCCUNotReady(t)

	if v.Ready() {
		t.Fatal("Ready() = true, want false for a StartNotReady CCU")
	}
	if st, body := checkRega(t, v); st != http.StatusOK || body == "OK" {
		t.Fatalf("checkrega while booting = (%d, %q), want 200 with body != OK", st, body)
	}
	if st := jsonRPCStatus(t, v); st != http.StatusServiceUnavailable {
		t.Fatalf("JSON-RPC while booting = %d, want 503", st)
	}

	v.SetReady(true)

	if !v.Ready() {
		t.Fatal("Ready() = false after SetReady(true)")
	}
	if st, body := checkRega(t, v); st != http.StatusOK || body != "OK" {
		t.Fatalf("checkrega when ready = (%d, %q), want 200 + body OK", st, body)
	}
	if st := jsonRPCStatus(t, v); st == http.StatusServiceUnavailable {
		t.Fatal("JSON-RPC still 503 after SetReady(true)")
	}
}

// TestReadyByDefault confirms an ordinary CCU is immediately ready, so
// existing fixtures are unaffected by the gate.
func TestReadyByDefault(t *testing.T) {
	t.Parallel()
	v := startCCU(t)
	if !v.Ready() {
		t.Fatal("Ready() = false for a default CCU, want true")
	}
	if st, body := checkRega(t, v); st != http.StatusOK || body != "OK" {
		t.Fatalf("checkrega = (%d, %q), want 200 + body OK", st, body)
	}
}
