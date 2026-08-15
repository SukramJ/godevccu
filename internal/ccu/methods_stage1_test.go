// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Tests for the XML-RPC methods real clients call but the simulator did
// not answer: determineParameter (aiohomematic calls it on every
// unreliable datapoint), getParamsetId, activateLinkParamset,
// getLinkInfo/setLinkInfo, getAllMetadata — plus the metadata store that
// makes a setMetadata → getMetadata round trip work at all.

package ccu_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

func TestMetadataRoundTrip(t *testing.T) {
	rpc := newRPC(t)
	root := rootAddress(t, rpc)

	if !rpc.SetMetadata(root, "operateGroupOnly", "false") {
		t.Fatal("SetMetadata returned false")
	}
	got, err := rpc.GetMetadata(root, "operateGroupOnly")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if got != "false" {
		t.Fatalf("metadata = %v, want the value that was written", got)
	}

	all := rpc.GetAllMetadata(root)
	if all["operateGroupOnly"] != "false" {
		t.Fatalf("GetAllMetadata = %v, want the stored entry", all)
	}
}

// A key that was never written still falls back to the device
// description, so the established NAME behaviour is unaffected.
func TestMetadataFallsBackToDescription(t *testing.T) {
	rpc := newRPC(t)
	root := rootAddress(t, rpc)

	name, err := rpc.GetMetadata(root, "NAME")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if s, _ := name.(string); s == "" {
		t.Fatal("NAME fallback lost")
	}
}

func TestDetermineParameterFiresEvent(t *testing.T) {
	rpc := newRPC(t)
	channel := rootAddress(t, rpc) + ":1"
	// Pick a parameter the loaded device actually has, so the test
	// cannot pass by taking an early exit.
	param := anyValuesParameter(t, rpc, channel)

	fired := make(chan string, 1)
	rpc.RegisterParamsetCallback(func(_, address, valueKey string, _ any) {
		if strings.EqualFold(address, channel) && valueKey == param {
			select {
			case fired <- valueKey:
			default:
			}
		}
	})

	ok, err := rpc.DetermineParameter(channel, param, "")
	if err != nil {
		t.Fatalf("DetermineParameter(%s, %s): %v", channel, param, err)
	}
	if !ok {
		t.Fatal("DetermineParameter returned false")
	}
	select {
	case got := <-fired:
		if got != param {
			t.Fatalf("event parameter = %q, want %q", got, param)
		}
	case <-time.After(time.Second):
		t.Fatal("no event fired — a real CCU reports the re-read value")
	}
}

// anyValuesParameter returns one parameter from the channel's VALUES
// paramset.
func anyValuesParameter(t *testing.T, rpc *ccu.RPCFunctions, channel string) string {
	t.Helper()
	desc, err := rpc.GetParamsetDescription(channel, "VALUES")
	if err != nil {
		t.Fatalf("GetParamsetDescription(%s): %v", channel, err)
	}
	for name := range desc {
		return name
	}
	t.Fatalf("channel %s has no VALUES parameters", channel)
	return ""
}

func TestDetermineParameterUnknownParameterFaults(t *testing.T) {
	rpc := newRPC(t)
	channel := rootAddress(t, rpc) + ":1"

	if _, err := rpc.DetermineParameter(channel, "NO_SUCH_PARAM", ""); err == nil {
		t.Fatal("expected an error for an unknown parameter")
	}
}

func TestGetParamsetIdIsStablePerType(t *testing.T) {
	rpc := newRPC(t)
	root := rootAddress(t, rpc)

	id, err := rpc.GetParamsetID(root, "MASTER")
	if err != nil {
		t.Fatalf("GetParamsetId: %v", err)
	}
	if id == "" {
		t.Fatal("empty paramset id")
	}
	again, _ := rpc.GetParamsetID(root, "MASTER")
	if id != again {
		t.Fatalf("paramset id not stable: %q vs %q", id, again)
	}
	values, _ := rpc.GetParamsetID(root, "VALUES")
	if values == id {
		t.Fatal("MASTER and VALUES must not share an id")
	}
	if _, err := rpc.GetParamsetID("VCU0000404", "MASTER"); err == nil {
		t.Fatal("expected an error for an unknown device")
	}
}

func TestLinkInfoRoundTrip(t *testing.T) {
	rpc := newRPC(t)
	root := rootAddress(t, rpc)
	sender, receiver := root+":1", root+":2"

	// Without a link there is nothing to describe.
	if _, err := rpc.GetLinkInfo(sender, receiver); err == nil {
		t.Fatal("expected an error before the link exists")
	}

	rpc.AddLink(sender, receiver, "", "")
	if ok, err := rpc.SetLinkInfo(sender, receiver, "Flurlicht", "Taster schaltet Licht"); err != nil || !ok {
		t.Fatalf("SetLinkInfo: %v (ok=%v)", err, ok)
	}
	info, err := rpc.GetLinkInfo(sender, receiver)
	if err != nil {
		t.Fatalf("GetLinkInfo: %v", err)
	}
	if info["NAME"] != "Flurlicht" {
		t.Errorf("NAME = %v, want Flurlicht", info["NAME"])
	}
	if info["DESCRIPTION"] != "Taster schaltet Licht" {
		t.Errorf("DESCRIPTION = %v, want the stored description", info["DESCRIPTION"])
	}
}

func TestActivateLinkParamset(t *testing.T) {
	rpc := newRPC(t)
	root := rootAddress(t, rpc)
	sender, receiver := root+":1", root+":2"

	if _, err := rpc.ActivateLinkParamset(sender, receiver, false); err == nil {
		t.Fatal("expected an error without a link")
	}
	rpc.AddLink(sender, receiver, "", "")
	if ok, err := rpc.ActivateLinkParamset(sender, receiver, false); err != nil || !ok {
		t.Fatalf("ActivateLinkParamset: %v (ok=%v)", err, ok)
	}
	// Activatable from either end.
	if ok, err := rpc.ActivateLinkParamset(receiver, sender, true); err != nil || !ok {
		t.Fatalf("ActivateLinkParamset (reverse): %v (ok=%v)", err, ok)
	}
}

func TestRssiInfoShape(t *testing.T) {
	rpc := newRPC(t)
	info := rpc.RssiInfo()
	if len(info) == 0 {
		t.Fatal("no RSSI entries")
	}
	for device, raw := range info {
		if strings.Contains(device, ":") {
			t.Errorf("channel %q must not carry RSSI", device)
		}
		partners, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("entry for %q = %T, want a partner map", device, raw)
		}
		values, ok := partners["CENTRAL"].([]any)
		if !ok || len(values) != 2 {
			t.Fatalf("partner entry = %v, want a two-element rssi pair", partners["CENTRAL"])
		}
	}
}

// TestStage1MethodsAreReachableViaXMLRPC confirms the new methods are
// registered on the mux — clients discover them through
// system.listMethods and skip anything missing.
func TestStage1MethodsAreReachableViaXMLRPC(t *testing.T) {
	srv := newTestServer(t)
	client := xmlrpc.NewClient("http://" + srv.LocalAddr().String() + "/")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Call(ctx, "system.listMethods", nil)
	if err != nil {
		t.Fatalf("system.listMethods: %v", err)
	}
	arr, ok := result.(xmlrpc.ArrayValue)
	if !ok {
		t.Fatalf("result type = %T, want ArrayValue", result)
	}
	have := make(map[string]bool, len(arr))
	for _, v := range arr {
		if s, ok := xmlrpc.AsString(v); ok {
			have[s] = true
		}
	}
	for _, name := range []string{
		"determineParameter", "getParamsetId", "activateLinkParamset",
		"getLinkInfo", "setLinkInfo", "getAllMetadata", "rssiInfo",
	} {
		if !have[name] {
			t.Errorf("method %q not registered", name)
		}
	}
}

// rootAddress returns the first loaded device's root address.
func rootAddress(t *testing.T, rpc *ccu.RPCFunctions) string {
	t.Helper()
	for _, addr := range rpc.SupportedDevices() {
		return addr
	}
	t.Fatal("no devices loaded")
	return ""
}
