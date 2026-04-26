// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package tests holds black-box integration tests that consume only the
// public pkg/godevccu API surface. Use this directory for assertions
// that should still pass after internal refactors.
package tests_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
	"github.com/SukramJ/godevccu/pkg/godevccu"
)

func TestPublicAPISmoke(t *testing.T) {
	cfg := godevccu.Defaults()
	cfg.XMLRPCPort = freePort(t)
	cfg.JSONRPCPort = freePort(t)
	cfg.Devices = []string{"HmIP-SWSD"}
	cfg.SetupDefaults = true
	cfg.Password = "test"

	v, err := godevccu.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })

	if !v.IsRunning() {
		t.Fatal("IsRunning returned false after Start")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (v.XMLRPCAddr() == nil || v.JSONRPCAddr() == nil) {
		time.Sleep(10 * time.Millisecond)
	}

	url := "http://" + v.XMLRPCAddr().String() + "/"
	c := xmlrpc.NewClient(url)
	res, err := c.Call(context.Background(), "listDevices", nil)
	if err != nil {
		t.Fatalf("listDevices: %v", err)
	}
	if arr, ok := res.(xmlrpc.ArrayValue); !ok || len(arr) == 0 {
		t.Fatal("listDevices returned no devices")
	}

	// State manager is part of the public surface.
	st := v.State()
	if got := len(st.Programs()); got != 4 {
		t.Fatalf("default programs = %d, want 4", got)
	}
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
