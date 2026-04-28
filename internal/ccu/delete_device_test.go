// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu_test

import (
	"context"
	"encoding/xml"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// ─────────────────────────────────────────────────────────────────────────────
// RPCFunctions-level tests (no HTTP round-trip)
// ─────────────────────────────────────────────────────────────────────────────

// TestDeleteDeviceRemovesFromState verifies that after DeleteDevice the root
// device and all its channels are no longer reachable via GetDeviceDescription.
func TestDeleteDeviceRemovesFromState(t *testing.T) {
	rpc := newRPC(t) // loads HmIP-SWSD

	supported := rpc.SupportedDevices()
	rootAddr, ok := supported["HmIP-SWSD"]
	if !ok {
		t.Fatal("HmIP-SWSD missing from supported devices")
	}

	// Device must exist before the call.
	if _, err := rpc.GetDeviceDescription(rootAddr); err != nil {
		t.Fatalf("GetDeviceDescription before delete: %v", err)
	}

	rpc.DeleteDevice(context.Background(), rootAddr, 0)

	// Root must be gone.
	if _, err := rpc.GetDeviceDescription(rootAddr); err == nil {
		t.Fatal("root device still present after DeleteDevice, expected error")
	}

	// Channels must also be gone (channel address = root + ":N").
	chanAddr := rootAddr + ":1"
	if _, err := rpc.GetDeviceDescription(chanAddr); err == nil {
		t.Fatalf("channel %q still present after DeleteDevice", chanAddr)
	}

	// ListDevices must not contain the root address.
	for _, d := range rpc.ListDevices() {
		addr, _ := d["ADDRESS"].(string)
		if strings.EqualFold(addr, rootAddr) {
			t.Fatalf("ListDevices still contains root %q after DeleteDevice", rootAddr)
		}
	}
}

// TestDeleteDeviceUnknownAddressIsNoError verifies that DeleteDevice on an
// address that does not exist completes silently — idempotent, matching
// pydevccu behaviour.
func TestDeleteDeviceUnknownAddressIsNoError(t *testing.T) {
	rpc := newRPC(t)
	// Must not panic and should not error.
	rpc.DeleteDevice(context.Background(), "NONEXISTENT_ADDR_XYZ", 0)
	// Repeated calls are also fine.
	rpc.DeleteDevice(context.Background(), "NONEXISTENT_ADDR_XYZ", 1)
}

// TestDeleteDevicePushesDeleteDevicesCallback verifies that after a successful
// delete the simulator pushes a deleteDevices callback to all registered
// remotes containing the removed addresses.
func TestDeleteDevicePushesDeleteDevicesCallback(t *testing.T) {
	rpc := newRPC(t)

	supported := rpc.SupportedDevices()
	rootAddr, ok := supported["HmIP-SWSD"]
	if !ok {
		t.Fatal("HmIP-SWSD missing from supported devices")
	}

	var mu sync.Mutex
	var receivedMethod string
	var receivedAddrs []string

	// Minimal XML-RPC callback receiver that records the method name and
	// the string values in the array parameter.
	callbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		method, addrs := parseCallbackBody(body)
		mu.Lock()
		receivedMethod = method
		receivedAddrs = append(receivedAddrs, addrs...)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0"?><methodResponse><params><param>`+
			`<value><i4>0</i4></value></param></params></methodResponse>`)
	}))
	defer callbackSrv.Close()

	// Register the callback receiver via Init; the simulator calls
	// askDevices + pushDevices asynchronously so give it a moment.
	rpc.Init(callbackSrv.URL, "test-interface")
	time.Sleep(150 * time.Millisecond)

	rpc.DeleteDevice(context.Background(), rootAddr, 0)
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	method := receivedMethod
	addrs := receivedAddrs
	mu.Unlock()

	if method != "deleteDevices" {
		t.Fatalf("callback method = %q, want deleteDevices", method)
	}
	found := false
	for _, a := range addrs {
		if strings.EqualFold(a, rootAddr) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("deleteDevices callback did not include root address %q; got %v", rootAddr, addrs)
	}
}

// TestDeleteDeviceMultipleParamsHandled verifies that flags values 0 and 1
// are both accepted without error.
func TestDeleteDeviceMultipleParamsHandled(t *testing.T) {
	rpc0 := newRPC(t)
	sup0 := rpc0.SupportedDevices()
	root0, ok := sup0["HmIP-SWSD"]
	if !ok {
		t.Fatal("HmIP-SWSD missing (flags=0 instance)")
	}
	rpc0.DeleteDevice(context.Background(), root0, 0)
	if _, err := rpc0.GetDeviceDescription(root0); err == nil {
		t.Fatal("device still present after DeleteDevice(flags=0)")
	}

	rpc1 := newRPC(t)
	sup1 := rpc1.SupportedDevices()
	root1, ok := sup1["HmIP-SWSD"]
	if !ok {
		t.Fatal("HmIP-SWSD missing (flags=1 instance)")
	}
	rpc1.DeleteDevice(context.Background(), root1, 1)
	if _, err := rpc1.GetDeviceDescription(root1); err == nil {
		t.Fatal("device still present after DeleteDevice(flags=1)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Full HTTP round-trip tests via the XML-RPC mux
// ─────────────────────────────────────────────────────────────────────────────

// TestDeleteDeviceViaXMLRPC exercises the deleteDevice handler end-to-end
// through the HTTP server, confirming the device is removed from state.
func TestDeleteDeviceViaXMLRPC(t *testing.T) {
	srv := newTestServer(t)
	supported := srv.RPC().SupportedDevices()
	rootAddr, ok := supported["HmIP-SWSD"]
	if !ok {
		t.Fatal("HmIP-SWSD not in supported devices")
	}
	url := "http://" + srv.LocalAddr().String() + "/"
	client := xmlrpc.NewClient(url)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Call(ctx, "deleteDevice", []xmlrpc.Value{xmlrpc.StringValue(rootAddr), xmlrpc.IntValue(0)})
	if err != nil {
		t.Fatalf("deleteDevice via XML-RPC: %v", err)
	}

	if _, err := srv.RPC().GetDeviceDescription(rootAddr); err == nil {
		t.Fatal("device still present after deleteDevice XML-RPC call")
	}
}

// TestDeleteDeviceUnknownViaXMLRPC confirms that calling deleteDevice with an
// unknown address does not return a fault.
func TestDeleteDeviceUnknownViaXMLRPC(t *testing.T) {
	srv := newTestServer(t)
	url := "http://" + srv.LocalAddr().String() + "/"
	client := xmlrpc.NewClient(url)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Call(ctx, "deleteDevice", []xmlrpc.Value{xmlrpc.StringValue("NO_SUCH_DEVICE"), xmlrpc.IntValue(0)})
	if err != nil {
		t.Fatalf("deleteDevice unknown address returned error: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// newTestServer starts a ccu.Server on an OS-assigned port and registers
// a Cleanup to stop it.
func newTestServer(t *testing.T) *ccu.Server {
	t.Helper()
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: []string{"HmIP-SWSD"}})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	s := ccu.NewServer(ccu.ServerConfig{Address: "127.0.0.1:0", RPC: rpc})
	if err := s.Start(); err != nil {
		t.Fatalf("Server.Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	// Poll until LocalAddr resolves.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.LocalAddr() != nil {
			if _, ok := s.LocalAddr().(*net.TCPAddr); ok {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s
}

// parseCallbackBody extracts the method name and all string values (including
// array elements) from an XML-RPC methodCall body sent by the simulator to
// its callback receivers.
func parseCallbackBody(data []byte) (method string, stringVals []string) {
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
		return "", nil
	}
	method = c.MethodName
	for _, p := range c.Params {
		if p.Value.String != "" {
			stringVals = append(stringVals, p.Value.String)
		}
		for _, v := range p.Value.Array.Data.Values {
			if v.String != "" {
				stringVals = append(stringVals, v.String)
			}
		}
	}
	return method, stringVals
}
