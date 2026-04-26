// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

func TestRoundTripCall(t *testing.T) {
	call := &xmlrpc.MethodCall{
		Method: "test",
		Params: []xmlrpc.Value{
			xmlrpc.StringValue("hello"),
			xmlrpc.IntValue(42),
			xmlrpc.BoolValue(true),
			xmlrpc.ArrayValue{xmlrpc.StringValue("a"), xmlrpc.StringValue("b")},
			xmlrpc.StructValue{Members: []xmlrpc.Member{
				{Name: "name", Value: xmlrpc.StringValue("foo")},
				{Name: "n", Value: xmlrpc.IntValue(7)},
			}},
		},
	}
	raw, err := xmlrpc.MarshalCallBytes(call)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	got, err := xmlrpc.DecodeCall(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Method != "test" {
		t.Fatalf("method = %q", got.Method)
	}
	if len(got.Params) != 5 {
		t.Fatalf("params = %d", len(got.Params))
	}
	if s, ok := xmlrpc.AsString(got.Params[0]); !ok || s != "hello" {
		t.Fatalf("p0 = %v", got.Params[0])
	}
	if i, ok := xmlrpc.AsInt(got.Params[1]); !ok || i != 42 {
		t.Fatalf("p1 = %v", got.Params[1])
	}
	if b, ok := xmlrpc.AsBool(got.Params[2]); !ok || !b {
		t.Fatalf("p2 = %v", got.Params[2])
	}
	if arr, ok := xmlrpc.AsArray(got.Params[3]); !ok || len(arr) != 2 {
		t.Fatalf("p3 = %v", got.Params[3])
	}
	if s, ok := xmlrpc.AsStruct(got.Params[4]); !ok {
		t.Fatalf("p4 = %v", got.Params[4])
	} else if v, ok := s.Get("name"); !ok || string(v.(xmlrpc.StringValue)) != "foo" {
		t.Fatalf("struct.name = %v", v)
	}
}

func TestFromAnyConvertsMaps(t *testing.T) {
	in := map[string]any{
		"name": "foo",
		"n":    7,
	}
	v := xmlrpc.FromAny(in)
	s, ok := v.(xmlrpc.StructValue)
	if !ok {
		t.Fatalf("want struct, got %T", v)
	}
	if len(s.Members) != 2 {
		t.Fatalf("members = %d", len(s.Members))
	}
}

func TestToAnyRecursive(t *testing.T) {
	v := xmlrpc.StructValue{Members: []xmlrpc.Member{
		{Name: "list", Value: xmlrpc.ArrayValue{xmlrpc.IntValue(1), xmlrpc.IntValue(2)}},
		{Name: "txt", Value: xmlrpc.StringValue("ok")},
	}}
	out := xmlrpc.ToAny(v).(map[string]any)
	if got := out["txt"]; got != "ok" {
		t.Fatalf("txt = %v", got)
	}
	list, ok := out["list"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("list = %v", list)
	}
}
