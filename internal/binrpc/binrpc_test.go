// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// binrpc_test.go covers the wire codec and the request/response cycle.
//
// The multicall guard is the load-bearing one: CUxD wraps every callback
// in `system.multicall`, and a simulator that pushed bare calls instead
// would be useless for the defect class it exists to catch.

package binrpc

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// roundTrip encodes params as a request and decodes them back.
func roundTrip(t *testing.T, method string, params []xmlrpc.Value) *Request {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteRequest(&buf, method, params); err != nil {
		t.Fatalf("WriteRequest: %v", err)
	}
	req, err := ReadRequest(&buf)
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}
	return req
}

func TestRequestRoundTripPreservesEveryValueKind(t *testing.T) {
	t.Parallel()

	params := []xmlrpc.Value{
		xmlrpc.StringValue("CUX2801001:1"),
		xmlrpc.IntValue(-42),
		xmlrpc.BoolValue(true),
		xmlrpc.DoubleValue(21.5),
		xmlrpc.ArrayValue{xmlrpc.StringValue("a"), xmlrpc.IntValue(7)},
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "ADDRESS", Value: xmlrpc.StringValue("CUX2801001")},
			{Name: "VERSION", Value: xmlrpc.IntValue(1)},
		}},
	}
	req := roundTrip(t, "setValue", params)

	if req.Method != "setValue" {
		t.Errorf("method = %q, want setValue", req.Method)
	}
	if len(req.Params) != len(params) {
		t.Fatalf("param count = %d, want %d", len(req.Params), len(params))
	}
	if s, _ := xmlrpc.AsString(req.Params[0]); s != "CUX2801001:1" {
		t.Errorf("string param = %q", s)
	}
	if n, _ := xmlrpc.AsInt(req.Params[1]); n != -42 {
		t.Errorf("int param = %d, want -42", n)
	}
	if b, _ := xmlrpc.AsBool(req.Params[2]); !b {
		t.Error("bool param = false, want true")
	}
	d, ok := req.Params[3].(xmlrpc.DoubleValue)
	if !ok || math.Abs(float64(d)-21.5) > 1e-6 {
		t.Errorf("double param = %v, want ~21.5", req.Params[3])
	}
	arr, ok := xmlrpc.AsArray(req.Params[4])
	if !ok || len(arr) != 2 {
		t.Fatalf("array param = %v", req.Params[4])
	}
	st, ok := xmlrpc.AsStruct(req.Params[5])
	if !ok || len(st.Members) != 2 || st.Members[0].Name != "ADDRESS" {
		t.Errorf("struct param = %v", req.Params[5])
	}
}

func TestStringRoundTripUsesLatin1(t *testing.T) {
	t.Parallel()

	// Umlauts and the degree sign are ordinary in CCU device and channel
	// names and all sit inside Latin-1, so they must survive intact.
	const name = "Büro Süd 21°C"
	req := roundTrip(t, "setName", []xmlrpc.Value{xmlrpc.StringValue(name)})
	if s, _ := xmlrpc.AsString(req.Params[0]); s != name {
		t.Errorf("Latin-1 characters must survive the round trip: got %q, want %q", s, name)
	}

	// A rune above U+00FF has no Latin-1 representation. Refusing it is
	// the deliberate choice: a mangled device name is harder to trace back
	// than a refused encode.
	for _, unrepresentable := range []string{"emoji 🙂", "em dash —"} {
		if err := WriteRequest(&bytes.Buffer{}, "setName",
			[]xmlrpc.Value{xmlrpc.StringValue(unrepresentable)}); err == nil {
			t.Errorf("%q must be refused, not silently mangled", unrepresentable)
		}
	}
}

func TestResponseAndFaultRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteResponse(&buf, xmlrpc.StringValue("pong")); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	resp, err := ReadResponse(&buf)
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	if s, _ := xmlrpc.AsString(resp.Value); s != "pong" {
		t.Errorf("response = %v, want pong", resp.Value)
	}

	buf.Reset()
	if err := WriteFault(&buf, &xmlrpc.Fault{Code: -1, Message: "unknown.method"}); err != nil {
		t.Fatalf("WriteFault: %v", err)
	}
	resp, err = ReadResponse(&buf)
	if err != nil {
		t.Fatalf("ReadResponse(fault): %v", err)
	}
	if resp.Fault == nil || resp.Fault.Code != -1 || resp.Fault.Message != "unknown.method" {
		t.Errorf("fault = %+v, want code -1 / unknown.method", resp.Fault)
	}
}

func TestDecodeRejectsBadMarkerAndOversizedFrame(t *testing.T) {
	t.Parallel()

	if _, err := ReadRequest(bytes.NewReader([]byte("XXX\x00\x00\x00\x00\x00"))); err == nil {
		t.Error("a frame without the Bin marker must be rejected")
	}
	oversized := []byte{'B', 'i', 'n', 0x00, 0xFF, 0xFF, 0xFF, 0xFF}
	if _, err := ReadRequest(bytes.NewReader(oversized)); err == nil {
		t.Error("a declared payload beyond MaxMessageSize must be rejected before allocating")
	}
}

// echoDispatcher returns the method name so a test can prove which call
// reached the server.
type echoDispatcher struct{ calls chan *Request }

func (d echoDispatcher) Dispatch(_ context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error) {
	d.calls <- &Request{Method: method, Params: params}
	if method == "boom" {
		return nil, &xmlrpc.Fault{Code: -7, Message: "boom"}
	}
	return xmlrpc.StringValue(method), nil
}

func newTestServer(t *testing.T) (*Client, chan *Request) {
	t.Helper()
	calls := make(chan *Request, 8)
	srv, err := NewServer("127.0.0.1:0", echoDispatcher{calls: calls}, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close(); <-done })
	return NewClient(URLScheme + srv.Addr().String()), calls
}

func TestServerServesRequestAndFault(t *testing.T) {
	t.Parallel()

	client, calls := newTestServer(t)

	res, err := client.Call(context.Background(), "ping", []xmlrpc.Value{xmlrpc.StringValue("tok")})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if s, _ := xmlrpc.AsString(res); s != "ping" {
		t.Errorf("result = %v, want ping", res)
	}
	got := <-calls
	if got.Method != "ping" || len(got.Params) != 1 {
		t.Errorf("dispatched %q with %d params", got.Method, len(got.Params))
	}

	_, err = client.Call(context.Background(), "boom", nil)
	var fault *xmlrpc.Fault
	if !errors.As(err, &fault) || fault.Code != -7 {
		t.Errorf("a dispatcher fault must reach the caller as a fault, got %v", err)
	}
	<-calls
}

// TestCallMulticallWrapsLikeCUxD is the guard that gives the simulator
// its reason to exist: a callback must arrive as a `system.multicall`
// envelope whose sub-call carries the real method and parameters. A
// consumer parsing params[0] as the interface id sees an array here and
// a string for a bare call — the difference this models.
func TestCallMulticallWrapsLikeCUxD(t *testing.T) {
	t.Parallel()

	client, calls := newTestServer(t)

	params := []xmlrpc.Value{
		xmlrpc.StringValue("iface-1"),
		xmlrpc.StringValue("CUX2801001:1"),
		xmlrpc.StringValue("STATE"),
		xmlrpc.BoolValue(true),
	}
	if _, err := client.CallMulticall(context.Background(), "event", params); err != nil {
		t.Fatalf("CallMulticall: %v", err)
	}

	got := <-calls
	if got.Method != "system.multicall" {
		t.Fatalf("method on the wire = %q, want system.multicall — "+
			"a bare call would not reproduce CUxD's envelope", got.Method)
	}
	if len(got.Params) != 1 {
		t.Fatalf("multicall takes exactly 1 param (the sub-call array), got %d", len(got.Params))
	}
	subCalls, ok := xmlrpc.AsArray(got.Params[0])
	if !ok || len(subCalls) != 1 {
		t.Fatalf("params[0] must be a 1-element sub-call array, got %v", got.Params[0])
	}
	st, ok := xmlrpc.AsStruct(subCalls[0])
	if !ok {
		t.Fatalf("sub-call must be a struct, got %v", subCalls[0])
	}
	var name string
	var inner xmlrpc.ArrayValue
	for _, m := range st.Members {
		switch m.Name {
		case "methodName":
			name, _ = xmlrpc.AsString(m.Value)
		case "params":
			inner, _ = xmlrpc.AsArray(m.Value)
		}
	}
	if name != "event" {
		t.Errorf("sub-call methodName = %q, want event", name)
	}
	if len(inner) != len(params) {
		t.Fatalf("sub-call params = %d, want %d", len(inner), len(params))
	}
	if s, _ := xmlrpc.AsString(inner[0]); s != "iface-1" {
		t.Errorf("interface id must be the sub-call's first param, got %q", s)
	}
}

func TestURLHelpers(t *testing.T) {
	t.Parallel()

	if !IsBINRPCURL("xmlrpc_bin://10.0.0.5:8701") {
		t.Error("xmlrpc_bin:// must be recognised")
	}
	if IsBINRPCURL("http://10.0.0.5:2001/RPC2") {
		t.Error("an HTTP callback URL must not be taken for BIN-RPC")
	}
	if got := AddrFromURL("xmlrpc_bin://10.0.0.5:8701"); got != "10.0.0.5:8701" {
		t.Errorf("AddrFromURL = %q, want 10.0.0.5:8701", got)
	}
}
