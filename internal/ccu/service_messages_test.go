// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// TestGetServiceMessagesUnit verifies that RPCFunctions.GetServiceMessages
// returns a non-empty [][]any with the expected structure.
func TestGetServiceMessagesUnit(t *testing.T) {
	rpc := newRPC(t)
	msgs := rpc.GetServiceMessages()
	if len(msgs) == 0 {
		t.Fatal("GetServiceMessages: expected at least one service message, got none")
	}
	// Each row must have at least 3 elements: address, parameter, status.
	for i, row := range msgs {
		if len(row) < 3 {
			t.Errorf("GetServiceMessages: row %d has %d elements, want ≥ 3", i, len(row))
		}
	}
}

// TestGetServiceMessagesViaXMLRPC makes a real XML-RPC call to the server and
// asserts that the response is a properly nested ArrayValue — NOT a StringValue
// produced by the fmt.Sprintf fallback.
func TestGetServiceMessagesViaXMLRPC(t *testing.T) {
	srv := newTestServer(t)
	url := "http://" + srv.LocalAddr().String() + "/"
	client := xmlrpc.NewClient(url)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	v, err := client.Call(ctx, "getServiceMessages", nil)
	if err != nil {
		t.Fatalf("getServiceMessages: %v", err)
	}
	if v == nil {
		t.Fatal("getServiceMessages: nil response")
	}

	// Top-level value must be an ArrayValue, not a StringValue.
	outer, ok := xmlrpc.AsArray(v)
	if !ok {
		t.Fatalf("getServiceMessages: expected ArrayValue, got %T (%s)", v, xmlrpc.Stringify(v))
	}
	if len(outer) == 0 {
		t.Fatal("getServiceMessages: outer array is empty, expected at least one row")
	}

	// Each element of the outer array must itself be an ArrayValue.
	for i, elem := range outer {
		if _, ok := xmlrpc.AsArray(elem); !ok {
			t.Errorf("getServiceMessages: row %d is %T (%s), want ArrayValue",
				i, elem, xmlrpc.Stringify(elem))
		}
	}
	t.Logf("getServiceMessages: %d row(s), first row: %s", len(outer), xmlrpc.Stringify(outer[0]))
}
