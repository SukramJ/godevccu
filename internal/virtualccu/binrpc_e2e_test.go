// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// binrpc_e2e_test.go drives the BIN-RPC transport end to end: register a
// callback over BIN-RPC, make the CCU fire an event, and assert what
// lands on the wire.
//
// The shape of that delivery is the whole point. CUxD wraps every
// callback in `system.multicall`, and a consumer that reads the interface
// id out of params[0] gets a string for a bare call and the sub-call
// array for an envelope. A simulator that pushed bare calls would let
// such a consumer pass while dropping every real CUxD event.

package virtualccu_test

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/binrpc"
	"github.com/SukramJ/godevccu/internal/virtualccu"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// callbackSink is a BIN-RPC listener standing in for a consumer's
// callback server. It records raw requests without interpreting them, so
// the test can assert the envelope rather than a parsed view of it.
type callbackSink struct {
	mu       sync.Mutex
	requests []*binrpc.Request
	addr     string
}

func newCallbackSink(t *testing.T) *callbackSink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &callbackSink{addr: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	return s
}

func (s *callbackSink) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req, err := binrpc.ReadRequest(io.LimitReader(conn, binrpc.MaxMessageSize+8))
	if err != nil {
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	var buf []byte
	_ = binrpc.WriteResponse(sinkWriter(func(p []byte) (int, error) {
		buf = append(buf, p...)
		return len(p), nil
	}), xmlrpc.StringValue(""))
	_, _ = conn.Write(buf)
}

type sinkWriter func([]byte) (int, error)

func (w sinkWriter) Write(p []byte) (int, error) { return w(p) }

func (s *callbackSink) snapshot() []*binrpc.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*binrpc.Request, len(s.requests))
	copy(out, s.requests)
	return out
}

// subCall is one unwrapped member of a system.multicall envelope.
type subCall struct {
	method string
	params xmlrpc.ArrayValue
}

// unwrap flattens every recorded request into its sub-calls. A request
// that is not a multicall envelope is reported under its own method with
// its own params, so a test can tell the two shapes apart rather than
// silently treating them alike.
func (s *callbackSink) unwrap(t *testing.T) []subCall {
	t.Helper()
	var out []subCall
	for _, r := range s.snapshot() {
		if r.Method != "system.multicall" {
			out = append(out, subCall{method: r.Method, params: r.Params})
			continue
		}
		if len(r.Params) != 1 {
			t.Fatalf("system.multicall takes exactly 1 param, got %d", len(r.Params))
		}
		members, ok := xmlrpc.AsArray(r.Params[0])
		if !ok {
			t.Fatalf("params[0] must be the sub-call array, got %v", r.Params[0])
		}
		for _, m := range members {
			st, ok := xmlrpc.AsStruct(m)
			if !ok {
				t.Fatalf("sub-call must be a struct, got %v", m)
			}
			var sc subCall
			for _, member := range st.Members {
				switch member.Name {
				case "methodName":
					sc.method, _ = xmlrpc.AsString(member.Value)
				case "params":
					sc.params, _ = xmlrpc.AsArray(member.Value)
				}
			}
			out = append(out, sc)
		}
	}
	return out
}

// countSubCalls returns how many sub-calls of the given method arrived.
func (s *callbackSink) countSubCalls(t *testing.T, method string) int {
	t.Helper()
	n := 0
	for _, sc := range s.unwrap(t) {
		if sc.method == method {
			n++
		}
	}
	return n
}

// waitForSubCall polls until a sub-call with the given method arrives.
// Matching on the sub-call rather than the request method matters: the
// registration handshake also travels as a multicall, so a test that
// matched the envelope would assert against the wrong callback.
func (s *callbackSink) waitForSubCall(t *testing.T, method string) subCall {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, sc := range s.unwrap(t) {
			if sc.method == method {
				return sc
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	var got []string
	for _, sc := range s.unwrap(t) {
		got = append(got, sc.method)
	}
	t.Fatalf("no %q callback arrived within 5s; observed sub-calls: %v", method, got)
	return subCall{}
}

// requireEveryCallbackWasWrapped asserts the simulator never pushed a
// bare call. This is the CUxD behaviour the transport exists to model —
// a single unwrapped delivery would let a consumer that cannot parse the
// envelope pass anyway.
func (s *callbackSink) requireEveryCallbackWasWrapped(t *testing.T) {
	t.Helper()
	for _, r := range s.snapshot() {
		if r.Method != "system.multicall" {
			t.Errorf("callback %q arrived unwrapped; CUxD wraps every callback in system.multicall", r.Method)
		}
	}
}

// startWithBINRPC boots a simulator with the CUxD transport enabled.
func startWithBINRPC(t *testing.T) *virtualccu.VirtualCCU {
	t.Helper()
	v, err := virtualccu.New(virtualccu.Config{
		Host:        "127.0.0.1",
		XMLRPCPort:  virtualccu.EphemeralPort,
		JSONRPCPort: virtualccu.EphemeralPort,
		BINRPCPort:  virtualccu.EphemeralPort,
		Devices:     []string{"HM-LC-Bl1-FM"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := v.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = v.Stop() })
	return v
}

func TestBINRPCListenerServesTheSameMethodSet(t *testing.T) {
	v := startWithBINRPC(t)

	addr := v.BINRPCAddr()
	if addr == nil {
		t.Fatal("BINRPCPort was set, so a listener must be bound")
	}
	if port := v.Config().BINRPCPort; port == 0 || port != addr.(*net.TCPAddr).Port {
		t.Errorf("Config().BINRPCPort = %d must reflect the bound port %v", port, addr)
	}

	client := binrpc.NewClient(binrpc.URLScheme + addr.String())
	res, err := client.Call(context.Background(), "listDevices", nil)
	if err != nil {
		t.Fatalf("listDevices over BIN-RPC: %v", err)
	}
	arr, ok := xmlrpc.AsArray(res)
	if !ok || len(arr) == 0 {
		t.Fatalf("listDevices must answer with the device list over BIN-RPC too, got %v", res)
	}
}

// TestBINRPCCallbackArrivesAsMulticall is the guard the transport exists
// for. Taking the envelope away — pushing a bare `event` — is exactly the
// simulator behaviour that let a real consumer defect stay hidden.
func TestBINRPCCallbackArrivesAsMulticall(t *testing.T) {
	v := startWithBINRPC(t)
	sink := newCallbackSink(t)

	const interfaceID = "consumer-CUxD"
	client := binrpc.NewClient(binrpc.URLScheme + v.BINRPCAddr().String())
	if _, err := client.Call(context.Background(), "init", []xmlrpc.Value{
		xmlrpc.StringValue(binrpc.URLScheme + sink.addr),
		xmlrpc.StringValue(interfaceID),
	}); err != nil {
		t.Fatalf("init over BIN-RPC: %v", err)
	}

	if err := v.SimulateDeviceEvent("VCU0000045:1", "LEVEL", 0.5); err != nil {
		t.Fatalf("SimulateDeviceEvent: %v", err)
	}

	got := sink.waitForSubCall(t, "event")
	sink.requireEveryCallbackWasWrapped(t)

	if len(got.params) != 4 {
		t.Fatalf("event carries (interface_id, address, parameter, value), got %d params", len(got.params))
	}
	if id, _ := xmlrpc.AsString(got.params[0]); id != interfaceID {
		t.Errorf("interface id = %q, want %q — it lives inside the sub-call, "+
			"not in the envelope's params[0]", id, interfaceID)
	}
	if param, _ := xmlrpc.AsString(got.params[2]); param != "LEVEL" {
		t.Errorf("parameter = %q, want LEVEL", param)
	}
}

// TestBINRPCDeregistrationStopsCallbacks pins that init with the URL
// alone removes the registration, the only form the real CCU honours.
func TestBINRPCDeregistrationStopsCallbacks(t *testing.T) {
	v := startWithBINRPC(t)
	sink := newCallbackSink(t)

	callbackURL := binrpc.URLScheme + sink.addr
	client := binrpc.NewClient(binrpc.URLScheme + v.BINRPCAddr().String())
	if _, err := client.Call(context.Background(), "init", []xmlrpc.Value{
		xmlrpc.StringValue(callbackURL), xmlrpc.StringValue("consumer-CUxD"),
	}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := v.SimulateDeviceEvent("VCU0000045:1", "LEVEL", 0.5); err != nil {
		t.Fatalf("SimulateDeviceEvent: %v", err)
	}
	sink.waitForSubCall(t, "event")

	// Deregister: URL only, second parameter omitted.
	if _, err := client.Call(context.Background(), "init",
		[]xmlrpc.Value{xmlrpc.StringValue(callbackURL)}); err != nil {
		t.Fatalf("deinit: %v", err)
	}
	before := sink.countSubCalls(t, "event")
	if err := v.SimulateDeviceEvent("VCU0000045:1", "LEVEL", 0.25); err != nil {
		t.Fatalf("SimulateDeviceEvent: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if after := sink.countSubCalls(t, "event"); after != before {
		t.Errorf("a deregistered callback must receive nothing further: %d → %d events", before, after)
	}
}
