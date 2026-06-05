// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu_test

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// ─────────────────────────────────────────────────────────────────────────────
// ListBidcosInterfaces
// ─────────────────────────────────────────────────────────────────────────────

// TestListBidcosInterfacesReturnsEmptySlice verifies that ListBidcosInterfaces
// returns a non-nil, empty slice (no physical gateways are modelled).
func TestListBidcosInterfacesReturnsEmptySlice(t *testing.T) {
	rpc := newRPC(t)
	result := rpc.ListBidcosInterfaces()
	if result == nil {
		t.Fatal("ListBidcosInterfaces returned nil, want non-nil empty slice")
	}
	if len(result) != 0 {
		t.Fatalf("ListBidcosInterfaces returned %d entries, want 0", len(result))
	}
}

// TestListBidcosInterfacesViaXMLRPC exercises the listBidcosInterfaces handler
// end-to-end through the HTTP server, confirming an empty array and no fault.
func TestListBidcosInterfacesViaXMLRPC(t *testing.T) {
	srv := newTestServer(t)
	url := "http://" + srv.LocalAddr().String() + "/"
	client := xmlrpc.NewClient(url)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Call(ctx, "listBidcosInterfaces", nil)
	if err != nil {
		t.Fatalf("listBidcosInterfaces via XML-RPC: %v", err)
	}
	arr, ok := result.(xmlrpc.ArrayValue)
	if !ok {
		t.Fatalf("result type = %T, want xmlrpc.ArrayValue", result)
	}
	if len(arr) != 0 {
		t.Fatalf("result length = %d, want 0", len(arr))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReplaceDevice push
// ─────────────────────────────────────────────────────────────────────────────

// TestReplaceDevicePushesCallback verifies that ReplaceDevice sends a
// replaceDevice XML-RPC call to every registered callback receiver with the
// correct three parameters: interfaceID, oldAddress, newAddress.
func TestReplaceDevicePushesCallback(t *testing.T) {
	rpc := newRPC(t)

	type captured struct {
		method string
		params []string
	}
	// Buffered so the handler never blocks, even for the listDevices/newDevices
	// calls the simulator sends during Init's askDevices phase.
	ch := make(chan captured, 16)

	callbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		method, vals := parseCallbackBody(body)
		ch <- captured{method: method, params: vals}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0"?><methodResponse><params><param>`+
			`<value><i4>0</i4></value></param></params></methodResponse>`)
	}))
	defer callbackSrv.Close()

	rpc.Init(callbackSrv.URL, "test-interface")
	time.Sleep(150 * time.Millisecond)

	rpc.ReplaceDevice(context.Background(), "OLDADDR:1", "NEWADDR:1")

	// Drain until we find the replaceDevice call (or time out).
	deadline := time.After(2 * time.Second)
	var got *captured
	for got == nil {
		select {
		case c := <-ch:
			if c.method == "replaceDevice" {
				got = &c
			}
		case <-deadline:
			t.Fatal("timeout waiting for replaceDevice callback")
		}
	}

	if len(got.params) < 3 {
		t.Fatalf("got %d params, want at least 3: %v", len(got.params), got.params)
	}
	if got.params[0] != "test-interface" {
		t.Fatalf("params[0] (interfaceID) = %q, want %q", got.params[0], "test-interface")
	}
	if got.params[1] != "OLDADDR:1" {
		t.Fatalf("params[1] (oldAddress) = %q, want %q", got.params[1], "OLDADDR:1")
	}
	if got.params[2] != "NEWADDR:1" {
		t.Fatalf("params[2] (newAddress) = %q, want %q", got.params[2], "NEWADDR:1")
	}
}

// TestReplaceDeviceNoRemotesIsNoOp confirms that ReplaceDevice with no
// registered remotes completes without error or panic.
func TestReplaceDeviceNoRemotesIsNoOp(t *testing.T) {
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-SWSD"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	rpc.ReplaceDevice(context.Background(), "OLD:1", "NEW:1")
}

// ─────────────────────────────────────────────────────────────────────────────
// ReaddedDevice push
// ─────────────────────────────────────────────────────────────────────────────

// parseReaddedCallbackBody extracts the method name, the string value of the
// first parameter, and the string array from the second parameter of an
// XML-RPC methodCall body.
func parseReaddedCallbackBody(data []byte) (method, ifID string, addresses []string) {
	type valueNode struct {
		String string `xml:"string"`
		Array  struct {
			Data struct {
				Values []struct {
					String string `xml:"string"`
				} `xml:"value"`
			} `xml:"data"`
		} `xml:"array"`
	}
	type callNode struct {
		MethodName string `xml:"methodName"`
		Params     []struct {
			Value valueNode `xml:"value"`
		} `xml:"params>param"`
	}
	var c callNode
	if err := xml.Unmarshal(data, &c); err != nil {
		return "", "", nil
	}
	method = c.MethodName
	if len(c.Params) > 0 {
		ifID = c.Params[0].Value.String
	}
	if len(c.Params) > 1 {
		for _, v := range c.Params[1].Value.Array.Data.Values {
			if v.String != "" {
				addresses = append(addresses, v.String)
			}
		}
	}
	return method, ifID, addresses
}

// TestReaddedDevicePushesCallback verifies that ReaddedDevice sends a
// readdedDevice XML-RPC call to every registered callback receiver with the
// correct two parameters: interfaceID and addresses array.
func TestReaddedDevicePushesCallback(t *testing.T) {
	rpc := newRPC(t)

	type captured struct {
		method    string
		ifID      string
		addresses []string
	}
	// Buffered so the handler never blocks during the Init askDevices phase.
	ch := make(chan captured, 16)

	callbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		method, ifID, addrs := parseReaddedCallbackBody(body)
		ch <- captured{method: method, ifID: ifID, addresses: addrs}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0"?><methodResponse><params><param>`+
			`<value><i4>0</i4></value></param></params></methodResponse>`)
	}))
	defer callbackSrv.Close()

	rpc.Init(callbackSrv.URL, "test-interface")
	time.Sleep(150 * time.Millisecond)

	rpc.ReaddedDevice(context.Background(), []string{"ADDR1", "ADDR2"})

	// Drain until we find the readdedDevice call (or time out).
	deadline := time.After(2 * time.Second)
	var got *captured
	for got == nil {
		select {
		case c := <-ch:
			if c.method == "readdedDevice" {
				got = &c
			}
		case <-deadline:
			t.Fatal("timeout waiting for readdedDevice callback")
		}
	}

	if got.ifID != "test-interface" {
		t.Fatalf("interfaceID = %q, want %q", got.ifID, "test-interface")
	}
	wantAddrs := []string{"ADDR1", "ADDR2"}
	if len(got.addresses) != len(wantAddrs) {
		t.Fatalf("addresses = %v, want %v", got.addresses, wantAddrs)
	}
	for i, a := range wantAddrs {
		if got.addresses[i] != a {
			t.Fatalf("addresses[%d] = %q, want %q", i, got.addresses[i], a)
		}
	}
}

// TestReaddedDeviceNoRemotesIsNoOp confirms that ReaddedDevice with no
// registered remotes completes without error or panic.
func TestReaddedDeviceNoRemotesIsNoOp(t *testing.T) {
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-SWSD"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	rpc.ReaddedDevice(context.Background(), []string{"ADDR1", "ADDR2"})
}
