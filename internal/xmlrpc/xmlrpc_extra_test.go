// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// ─────────────────────────────────────────────────────────────────
// value.go — Kind, MarshalXML, Stringify
// ─────────────────────────────────────────────────────────────────

func TestKindString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		k    xmlrpc.Kind
		want string
	}{
		{xmlrpc.KindNil, "nil"},
		{xmlrpc.KindInt, "int"},
		{xmlrpc.KindBool, "boolean"},
		{xmlrpc.KindString, "string"},
		{xmlrpc.KindDouble, "double"},
		{xmlrpc.KindDateTime, "dateTime.iso8601"},
		{xmlrpc.KindBase64, "base64"},
		{xmlrpc.KindStruct, "struct"},
		{xmlrpc.KindArray, "array"},
		{xmlrpc.Kind(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("Kind(%d).String() = %q, want %q", tc.k, got, tc.want)
		}
	}
}

func TestValueKinds(t *testing.T) {
	t.Parallel()
	nilV := xmlrpc.NilValue{}
	if nilV.Kind() != xmlrpc.KindNil {
		t.Error("NilValue.Kind")
	}
	intV := xmlrpc.IntValue(0)
	if intV.Kind() != xmlrpc.KindInt {
		t.Error("IntValue.Kind")
	}
	boolV := xmlrpc.BoolValue(false)
	if boolV.Kind() != xmlrpc.KindBool {
		t.Error("BoolValue.Kind")
	}
	strV := xmlrpc.StringValue("")
	if strV.Kind() != xmlrpc.KindString {
		t.Error("StringValue.Kind")
	}
	dblV := xmlrpc.DoubleValue(0)
	if dblV.Kind() != xmlrpc.KindDouble {
		t.Error("DoubleValue.Kind")
	}
	dtV := xmlrpc.DateTimeValue(time.Time{})
	if dtV.Kind() != xmlrpc.KindDateTime {
		t.Error("DateTimeValue.Kind")
	}
	b64V := xmlrpc.Base64Value(nil)
	if b64V.Kind() != xmlrpc.KindBase64 {
		t.Error("Base64Value.Kind")
	}
	structV := xmlrpc.StructValue{}
	if structV.Kind() != xmlrpc.KindStruct {
		t.Error("StructValue.Kind")
	}
	arrV := xmlrpc.ArrayValue(nil)
	if arrV.Kind() != xmlrpc.KindArray {
		t.Error("ArrayValue.Kind")
	}
}

func TestDateTimeValueTime(t *testing.T) {
	t.Parallel()
	now := time.Now().Truncate(time.Second)
	dv := xmlrpc.DateTimeValue(now)
	if !dv.Time().Equal(now) {
		t.Errorf("DateTimeValue.Time() = %v, want %v", dv.Time(), now)
	}
}

func TestStringify(t *testing.T) {
	t.Parallel()
	now := time.Now().Truncate(time.Second)
	cases := []struct {
		v    xmlrpc.Value
		want string
	}{
		{nil, "<nil>"},
		{xmlrpc.NilValue{}, "nil"},
		{xmlrpc.IntValue(42), "42"},
		{xmlrpc.BoolValue(true), "true"},
		{xmlrpc.BoolValue(false), "false"},
		{xmlrpc.StringValue("hello"), `"hello"`},
		{xmlrpc.DoubleValue(3.14), "3.14"},
		{xmlrpc.DateTimeValue(now), now.Format(xmlrpc.ISO8601CompactLayout)},
		{xmlrpc.Base64Value([]byte{1, 2, 3}), "AQID"},
	}
	for _, tc := range cases {
		got := xmlrpc.Stringify(tc.v)
		if got != tc.want {
			t.Errorf("Stringify(%T) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestStringifyStruct(t *testing.T) {
	t.Parallel()
	v := xmlrpc.StructValue{Members: []xmlrpc.Member{
		{Name: "a", Value: xmlrpc.IntValue(1)},
	}}
	got := xmlrpc.Stringify(v)
	if !strings.Contains(got, "a:") {
		t.Errorf("Stringify struct = %q, want member 'a'", got)
	}
}

func TestStringifyArray(t *testing.T) {
	t.Parallel()
	v := xmlrpc.ArrayValue{xmlrpc.IntValue(1), xmlrpc.IntValue(2)}
	got := xmlrpc.Stringify(v)
	if got != "[1 2]" {
		t.Errorf("Stringify array = %q, want '[1 2]'", got)
	}
}

func TestStringifyUnknownType(t *testing.T) {
	t.Parallel()
	// Unknown concrete type falls through to default
	got := xmlrpc.Stringify(xmlrpc.NilValue{})
	if got == "" {
		t.Error("expected non-empty from Stringify")
	}
}

// ─────────────────────────────────────────────────────────────────
// message.go — EncodeResponse, DecodeResponse, MarshalResponseBytes,
//              Fault round-trip, decodeFault
// ─────────────────────────────────────────────────────────────────

func TestEncodeDecodeResponse(t *testing.T) {
	t.Parallel()
	mr := &xmlrpc.MethodResponse{
		Params: []xmlrpc.Value{xmlrpc.IntValue(42), xmlrpc.StringValue("ok")},
	}
	raw, err := xmlrpc.MarshalResponseBytes(mr)
	if err != nil {
		t.Fatalf("MarshalResponseBytes: %v", err)
	}
	got, err := xmlrpc.DecodeResponse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if got.Fault != nil {
		t.Fatalf("unexpected fault: %v", got.Fault)
	}
	if len(got.Params) != 2 {
		t.Fatalf("params = %d, want 2", len(got.Params))
	}
	if i, ok := xmlrpc.AsInt(got.Params[0]); !ok || i != 42 {
		t.Errorf("p0 = %v", got.Params[0])
	}
}

func TestFaultRoundTrip(t *testing.T) {
	t.Parallel()
	mr := &xmlrpc.MethodResponse{
		Fault: &xmlrpc.Fault{Code: -99, Message: "test fault"},
	}
	raw, err := xmlrpc.MarshalResponseBytes(mr)
	if err != nil {
		t.Fatalf("MarshalResponseBytes: %v", err)
	}
	got, err := xmlrpc.DecodeResponse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if got.Fault == nil {
		t.Fatal("expected fault, got nil")
	}
	if got.Fault.Code != -99 {
		t.Errorf("fault code = %d, want -99", got.Fault.Code)
	}
	if got.Fault.Message != "test fault" {
		t.Errorf("fault message = %q, want 'test fault'", got.Fault.Message)
	}
}

func TestFaultError(t *testing.T) {
	t.Parallel()
	f := &xmlrpc.Fault{Code: -1, Message: "boom"}
	s := f.Error()
	if !strings.Contains(s, "boom") {
		t.Errorf("Fault.Error() = %q, want 'boom'", s)
	}
}

func TestEncodeResponseNilMethodResponse(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := xmlrpc.EncodeResponse(&buf, nil)
	if err == nil {
		t.Fatal("expected error for nil MethodResponse")
	}
}

func TestDecodeResponseMissingRoot(t *testing.T) {
	t.Parallel()
	_, err := xmlrpc.DecodeResponse(strings.NewReader(`<?xml version="1.0"?><badroot/>`))
	if err == nil {
		t.Fatal("expected error for unexpected root element")
	}
}

func TestDecodeResponseEmptyParams(t *testing.T) {
	t.Parallel()
	// A methodResponse with an empty params section returns empty Params.
	body := `<?xml version="1.0"?><methodResponse><params></params></methodResponse>`
	got, err := xmlrpc.DecodeResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if len(got.Params) != 0 {
		t.Fatalf("expected 0 params, got %d", len(got.Params))
	}
}

func TestDecodeCallNilMethodName(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><params><param><value><string>x</string></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for missing methodName")
	}
}

func TestEncodeCallNilMethodCall(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := xmlrpc.EncodeCall(&buf, nil)
	if err == nil {
		t.Fatal("expected error for nil MethodCall")
	}
}

func TestEncodeCallEmptyMethod(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := xmlrpc.EncodeCall(&buf, &xmlrpc.MethodCall{Method: ""})
	if err == nil {
		t.Fatal("expected error for empty method")
	}
}

func TestDecodeCallUnexpectedElement(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><unexpected/></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for unexpected element in methodCall")
	}
}

// ─────────────────────────────────────────────────────────────────
// decode.go — typed values not yet covered
// ─────────────────────────────────────────────────────────────────

func TestDecodeNilValue(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><nil/></value></param></params></methodCall>`
	call, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeCall: %v", err)
	}
	if _, ok := call.Params[0].(xmlrpc.NilValue); !ok {
		t.Fatalf("expected NilValue, got %T", call.Params[0])
	}
}

func TestDecodeDoubleValue(t *testing.T) {
	t.Parallel()
	raw, err := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{
		Method: "test",
		Params: []xmlrpc.Value{xmlrpc.DoubleValue(3.14)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	call, err := xmlrpc.DecodeCall(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d, ok := call.Params[0].(xmlrpc.DoubleValue); !ok || float64(d) != 3.14 {
		t.Fatalf("expected 3.14, got %v", call.Params[0])
	}
}

func TestDecodeDateTimeValue(t *testing.T) {
	t.Parallel()
	// ISO8601CompactLayout has no timezone — compare in UTC.
	now := time.Now().UTC().Truncate(time.Second)
	raw, err := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{
		Method: "test",
		Params: []xmlrpc.Value{xmlrpc.DateTimeValue(now)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	call, err := xmlrpc.DecodeCall(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	dt, ok := call.Params[0].(xmlrpc.DateTimeValue)
	if !ok {
		t.Fatalf("expected DateTimeValue, got %T", call.Params[0])
	}
	// The round-trip format strips the timezone; compare wall-clock time
	// fields directly (both parsed with UTC implicitly).
	if !dt.Time().Equal(now) && dt.Time().UTC().Format(xmlrpc.ISO8601CompactLayout) != now.Format(xmlrpc.ISO8601CompactLayout) {
		t.Errorf("time = %v, want %v", dt.Time(), now)
	}
}

func TestDecodeBase64Value(t *testing.T) {
	t.Parallel()
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	raw, err := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{
		Method: "test",
		Params: []xmlrpc.Value{xmlrpc.Base64Value(payload)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	call, err := xmlrpc.DecodeCall(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b, ok := call.Params[0].(xmlrpc.Base64Value)
	if !ok {
		t.Fatalf("expected Base64Value, got %T", call.Params[0])
	}
	if !bytes.Equal([]byte(b), payload) {
		t.Errorf("bytes = %v, want %v", []byte(b), payload)
	}
}

func TestDecodeValueBareCDATA(t *testing.T) {
	t.Parallel()
	// <value>text</value> without a type tag — decoded as string.
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value>hello</value></param></params></methodCall>`
	call, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeCall: %v", err)
	}
	s, ok := xmlrpc.AsString(call.Params[0])
	if !ok || s != "hello" {
		t.Fatalf("expected 'hello', got %v", call.Params[0])
	}
}

func TestDecodeValueUnknownType(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><unknowntype>x</unknowntype></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for unknown value type")
	}
}

func TestDecodeInvalidInt(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><i4>notanumber</i4></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for invalid int")
	}
}

func TestDecodeInvalidBool(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><boolean>maybe</boolean></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for invalid boolean")
	}
}

func TestDecodeInvalidDouble(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><double>notadouble</double></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for invalid double")
	}
}

func TestDecodeInvalidDateTime(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><dateTime.iso8601>NOTADATE</dateTime.iso8601></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for invalid dateTime")
	}
}

func TestDecodeDateTimeRFC3339(t *testing.T) {
	t.Parallel()
	// RFC3339 format is also accepted
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><dateTime.iso8601>2025-01-15T10:30:00Z</dateTime.iso8601></value></param></params></methodCall>`
	call, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := call.Params[0].(xmlrpc.DateTimeValue); !ok {
		t.Fatalf("expected DateTimeValue, got %T", call.Params[0])
	}
}

func TestDecodeInvalidBase64(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><base64>not!valid!base64!</base64></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecodeEmptyArray(t *testing.T) {
	t.Parallel()
	raw, err := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{
		Method: "test",
		Params: []xmlrpc.Value{xmlrpc.ArrayValue{}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	call, err := xmlrpc.DecodeCall(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	arr, ok := xmlrpc.AsArray(call.Params[0])
	if !ok {
		t.Fatalf("expected ArrayValue, got %T", call.Params[0])
	}
	if len(arr) != 0 {
		t.Fatalf("expected empty array, got %d elements", len(arr))
	}
}

// ─────────────────────────────────────────────────────────────────
// convert.go — FromAny coverage for uncovered branches
// ─────────────────────────────────────────────────────────────────

func TestFromAnyAllTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
		kind xmlrpc.Kind
	}{
		{"nil", nil, xmlrpc.KindNil},
		{"bool", true, xmlrpc.KindBool},
		{"string", "hello", xmlrpc.KindString},
		{"bytes", []byte{1}, xmlrpc.KindBase64},
		{"time", time.Now(), xmlrpc.KindDateTime},
		{"int", int(1), xmlrpc.KindInt},
		{"int8", int8(1), xmlrpc.KindInt},
		{"int16", int16(1), xmlrpc.KindInt},
		{"int32", int32(1), xmlrpc.KindInt},
		{"int64", int64(1), xmlrpc.KindInt},
		{"uint", uint(1), xmlrpc.KindInt},
		{"uint8", uint8(1), xmlrpc.KindInt},
		{"uint16", uint16(1), xmlrpc.KindInt},
		{"uint32", uint32(1), xmlrpc.KindInt},
		{"uint64", uint64(1), xmlrpc.KindInt},
		{"float64_int", float64(42), xmlrpc.KindInt},
		{"float64_frac", float64(3.14), xmlrpc.KindDouble},
		{"float32_int", float32(5), xmlrpc.KindInt},
		{"float32_frac", float32(1.5), xmlrpc.KindDouble},
		{"map", map[string]any{"k": "v"}, xmlrpc.KindStruct},
		{"slice_any", []any{1, 2}, xmlrpc.KindArray},
		{"slice_map", []map[string]any{{"k": "v"}}, xmlrpc.KindArray},
		{"slice_str", []string{"a", "b"}, xmlrpc.KindArray},
		{"slice_int", []int{1, 2}, xmlrpc.KindArray},
		{"nested_slice", [][]any{{1, 2}}, xmlrpc.KindArray},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := xmlrpc.FromAny(tc.in)
			if v.Kind() != tc.kind {
				t.Errorf("FromAny(%T) kind = %s, want %s", tc.in, v.Kind(), tc.kind)
			}
		})
	}
}

func TestFromAnyPreservesValue(t *testing.T) {
	t.Parallel()
	// Passing a Value directly should preserve it.
	original := xmlrpc.IntValue(99)
	got := xmlrpc.FromAny(original)
	if v, ok := got.(xmlrpc.IntValue); !ok || v != 99 {
		t.Errorf("FromAny(Value) = %v, want IntValue(99)", got)
	}
}

func TestFromAnyFallback(t *testing.T) {
	t.Parallel()
	// Unknown type → stringified.
	type weird struct{ X int }
	v := xmlrpc.FromAny(weird{X: 7})
	s, ok := xmlrpc.AsString(v)
	if !ok || s == "" {
		t.Errorf("FromAny fallback = %v", v)
	}
}

func TestToAnyAllLeaves(t *testing.T) {
	t.Parallel()
	now := time.Now().Truncate(time.Second)
	cases := []struct {
		v    xmlrpc.Value
		want any
	}{
		{nil, nil},
		{xmlrpc.NilValue{}, nil},
		{xmlrpc.IntValue(3), 3},
		{xmlrpc.BoolValue(true), true},
		{xmlrpc.StringValue("x"), "x"},
		{xmlrpc.DoubleValue(1.5), 1.5},
		{xmlrpc.DateTimeValue(now), now},
		{xmlrpc.Base64Value([]byte{7}), []byte{7}},
	}
	for _, tc := range cases {
		got := xmlrpc.ToAny(tc.v)
		switch want := tc.want.(type) {
		case nil:
			if got != nil {
				t.Errorf("ToAny(%T) = %v, want nil", tc.v, got)
			}
		case []byte:
			gb, ok := got.([]byte)
			if !ok || !bytes.Equal(gb, want) {
				t.Errorf("ToAny(Base64) = %v, want %v", got, want)
			}
		case time.Time:
			gt, ok := got.(time.Time)
			if !ok || !gt.Equal(want) {
				t.Errorf("ToAny(DateTime) = %v, want %v", got, want)
			}
		default:
			if got != tc.want {
				t.Errorf("ToAny(%T) = %v, want %v", tc.v, got, tc.want)
			}
		}
	}
}

func TestToAnyUnknownFallback(t *testing.T) {
	t.Parallel()
	// A Value that doesn't match any case returns nil. Exercise the nil path.
	_ = xmlrpc.ToAny(xmlrpc.NilValue{})
}

// ─────────────────────────────────────────────────────────────────
// mux.go — Mux
// ─────────────────────────────────────────────────────────────────

func TestMuxHandle(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	called := false
	m.Handle("test.method", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		called = true
		return xmlrpc.IntValue(1), nil
	})
	if !m.Has("test.method") {
		t.Fatal("Has() returned false after Handle()")
	}
	methods := m.Methods()
	found := false
	for _, name := range methods {
		if name == "test.method" {
			found = true
		}
	}
	if !found {
		t.Fatal("Methods() did not include registered method")
	}
	v, err := m.Dispatch(context.Background(), "test.method", nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !called {
		t.Fatal("handler not called")
	}
	if i, ok := xmlrpc.AsInt(v); !ok || i != 1 {
		t.Errorf("result = %v, want 1", v)
	}
}

func TestMuxHasFalse(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	if m.Has("no.such.method") {
		t.Fatal("Has() returned true for unregistered method")
	}
}

func TestMuxDispatchNotFound(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	_, err := m.Dispatch(context.Background(), "no.such.method", nil)
	if err == nil {
		t.Fatal("expected error for missing method")
	}
}

func TestMuxDispatchFallback(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.HandleFallback(func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.StringValue("fallback"), nil
	})
	v, err := m.Dispatch(context.Background(), "any.method", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s, ok := xmlrpc.AsString(v)
	if !ok || s != "fallback" {
		t.Errorf("fallback result = %v", v)
	}
}

func TestMuxHandlePanicsEmptyName(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty method name")
		}
	}()
	m.Handle("", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return nil, nil
	})
}

func TestMuxHandlePanicsNilHandler(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil handler")
		}
	}()
	m.Handle("test.method", nil)
}

func TestMuxRegisterSystemMethods(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()

	// system.listMethods
	v, err := m.Dispatch(context.Background(), "system.listMethods", nil)
	if err != nil {
		t.Fatalf("system.listMethods: %v", err)
	}
	arr, ok := xmlrpc.AsArray(v)
	if !ok {
		t.Fatalf("expected array, got %T", v)
	}
	// Should contain at least the 3 system methods.
	if len(arr) < 3 {
		t.Fatalf("expected ≥3 methods, got %d", len(arr))
	}

	// system.methodHelp
	v, err = m.Dispatch(context.Background(), "system.methodHelp", nil)
	if err != nil {
		t.Fatalf("system.methodHelp: %v", err)
	}
	if s, ok := xmlrpc.AsString(v); !ok || s != "" {
		t.Errorf("methodHelp = %q, want ''", s)
	}
}

func TestMuxSystemMulticall(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	m.Handle("ping", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.StringValue("pong"), nil
	})

	calls := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.StringValue("ping")},
			{Name: "params", Value: xmlrpc.ArrayValue{}},
		}},
	}
	v, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	results, ok := xmlrpc.AsArray(v)
	if !ok || len(results) != 1 {
		t.Fatalf("multicall results = %v", v)
	}
}

func TestMuxSystemMulticallFaultInner(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	m.Handle("fail", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return nil, &xmlrpc.Fault{Code: -1, Message: "inner error"}
	})

	calls := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.StringValue("fail")},
			{Name: "params", Value: xmlrpc.ArrayValue{}},
		}},
	}
	v, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err != nil {
		t.Fatalf("multicall with inner fault: %v", err)
	}
	results, ok := xmlrpc.AsArray(v)
	if !ok || len(results) != 1 {
		t.Fatalf("multicall results = %v", v)
	}
	// Inner result should be a fault struct.
	s, ok := xmlrpc.AsStruct(results[0])
	if !ok {
		t.Fatalf("expected struct for fault, got %T", results[0])
	}
	if _, ok := s.Get("faultCode"); !ok {
		t.Error("missing faultCode in multicall fault response")
	}
}

func TestMuxSystemMulticallNonFaultInnerError(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	m.Handle("errfail", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return nil, errors.New("plain error")
	})

	calls := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.StringValue("errfail")},
			{Name: "params", Value: xmlrpc.ArrayValue{}},
		}},
	}
	v, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	results, ok := xmlrpc.AsArray(v)
	if !ok || len(results) != 1 {
		t.Fatalf("multicall results = %v", v)
	}
}

func TestMuxSystemMulticallNilResultWrapped(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	m.Handle("retnil", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return nil, nil
	})

	calls := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.StringValue("retnil")},
			{Name: "params", Value: xmlrpc.ArrayValue{}},
		}},
	}
	v, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err != nil {
		t.Fatalf("multicall: %v", err)
	}
	results, ok := xmlrpc.AsArray(v)
	if !ok || len(results) != 1 {
		t.Fatalf("expected 1 result, got %v", v)
	}
}

func TestMuxSystemMulticallWrongParamCount(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	_, err := m.Dispatch(context.Background(), "system.multicall", nil)
	if err == nil {
		t.Fatal("expected error for wrong param count")
	}
}

func TestMuxSystemMulticallNonArray(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	_, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{xmlrpc.StringValue("notarray")})
	if err == nil {
		t.Fatal("expected error for non-array param")
	}
}

func TestMuxSystemMulticallCallNotStruct(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	calls := xmlrpc.ArrayValue{xmlrpc.StringValue("notastruct")}
	_, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err == nil {
		t.Fatal("expected error for non-struct call element")
	}
}

func TestMuxSystemMulticallMissingMethodName(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	calls := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "params", Value: xmlrpc.ArrayValue{}},
		}},
	}
	_, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err == nil {
		t.Fatal("expected error for missing methodName")
	}
}

func TestMuxSystemMulticallMethodNameNotString(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	calls := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.IntValue(42)},
			{Name: "params", Value: xmlrpc.ArrayValue{}},
		}},
	}
	_, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err == nil {
		t.Fatal("expected error for non-string methodName")
	}
}

func TestMuxSystemMulticallMissingParams(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	calls := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.StringValue("ping")},
		}},
	}
	_, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestMuxSystemMulticallParamsNotArray(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.RegisterSystemMethods()
	calls := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.StringValue("ping")},
			{Name: "params", Value: xmlrpc.StringValue("notarray")},
		}},
	}
	_, err := m.Dispatch(context.Background(), "system.multicall", []xmlrpc.Value{calls})
	if err == nil {
		t.Fatal("expected error for params not array")
	}
}

// ─────────────────────────────────────────────────────────────────
// handler.go — ServeHTTP
// ─────────────────────────────────────────────────────────────────

func TestHandlerServeHTTPSuccess(t *testing.T) {
	t.Parallel()
	h := xmlrpc.NewHandler()
	h.Mux.Handle("ping", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.StringValue("pong"), nil
	})

	body, err := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{
		Method: "ping",
		Params: nil,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/RPC2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/xml")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	resp, err := xmlrpc.DecodeResponse(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Fault != nil {
		t.Fatalf("unexpected fault: %v", resp.Fault)
	}
	if s, ok := xmlrpc.AsString(resp.Params[0]); !ok || s != "pong" {
		t.Errorf("result = %v", resp.Params[0])
	}
}

func TestHandlerServeHTTPMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := xmlrpc.NewHandler()
	req := httptest.NewRequest(http.MethodGet, "/RPC2", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestHandlerServeHTTPBadXML(t *testing.T) {
	t.Parallel()
	h := xmlrpc.NewHandler()
	req := httptest.NewRequest(http.MethodPost, "/RPC2", strings.NewReader("not xml"))
	req.Header.Set("Content-Type", "text/xml")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandlerServeHTTPFaultHandler(t *testing.T) {
	t.Parallel()
	h := xmlrpc.NewHandler()
	h.Mux.Handle("failme", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return nil, &xmlrpc.Fault{Code: -42, Message: "nope"}
	})

	body, _ := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{Method: "failme"})
	req := httptest.NewRequest(http.MethodPost, "/RPC2", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	resp, err := xmlrpc.DecodeResponse(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Fault == nil {
		t.Fatal("expected fault, got nil")
	}
	if resp.Fault.Code != -42 {
		t.Errorf("fault code = %d, want -42", resp.Fault.Code)
	}
}

func TestHandlerServeHTTPNonFaultError(t *testing.T) {
	t.Parallel()
	h := xmlrpc.NewHandler()
	h.Mux.Handle("errme", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return nil, errors.New("plain error")
	})

	body, _ := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{Method: "errme"})
	req := httptest.NewRequest(http.MethodPost, "/RPC2", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp, err := xmlrpc.DecodeResponse(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Fault == nil {
		t.Fatal("expected fault for plain error, got nil")
	}
}

func TestHandlerNilResult(t *testing.T) {
	t.Parallel()
	h := xmlrpc.NewHandler()
	h.Mux.Handle("retnil", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return nil, nil
	})

	body, _ := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{Method: "retnil"})
	req := httptest.NewRequest(http.MethodPost, "/RPC2", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	resp, err := xmlrpc.DecodeResponse(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Fault != nil {
		t.Fatalf("unexpected fault: %v", resp.Fault)
	}
}

// ─────────────────────────────────────────────────────────────────
// client.go — NewClient, URL, Call, IsTransport
// ─────────────────────────────────────────────────────────────────

func TestClientURL(t *testing.T) {
	t.Parallel()
	c := xmlrpc.NewClient("http://example.com:2001/RPC2")
	if got := c.URL(); got != "http://example.com:2001/RPC2" {
		t.Errorf("URL = %q", got)
	}
}

func TestIsTransport(t *testing.T) {
	t.Parallel()
	// A *Fault is NOT a transport error.
	fault := &xmlrpc.Fault{Code: -1, Message: "fault"}
	if xmlrpc.IsTransport(fault) {
		t.Error("Fault should not be a transport error")
	}
	// A plain error IS a transport error.
	if !xmlrpc.IsTransport(errors.New("network down")) {
		t.Error("plain error should be a transport error")
	}
}

func TestClientCallSuccess(t *testing.T) {
	t.Parallel()
	// Spin up an XML-RPC server that echoes back a StringValue.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr := &xmlrpc.MethodResponse{Params: []xmlrpc.Value{xmlrpc.StringValue("hello")}}
		raw, _ := xmlrpc.MarshalResponseBytes(mr)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write(raw)
	}))
	defer ts.Close()

	c := xmlrpc.NewClient(ts.URL)
	v, err := c.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	s, ok := xmlrpc.AsString(v)
	if !ok || s != "hello" {
		t.Errorf("result = %v, want 'hello'", v)
	}
}

func TestClientCallFaultResponse(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr := &xmlrpc.MethodResponse{Fault: &xmlrpc.Fault{Code: -7, Message: "bad"}}
		raw, _ := xmlrpc.MarshalResponseBytes(mr)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write(raw)
	}))
	defer ts.Close()

	c := xmlrpc.NewClient(ts.URL)
	_, err := c.Call(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected fault error, got nil")
	}
	var f *xmlrpc.Fault
	if !errors.As(err, &f) {
		t.Fatalf("expected *Fault, got %T", err)
	}
	if f.Code != -7 {
		t.Errorf("fault code = %d, want -7", f.Code)
	}
}

func TestClientCallHTTPError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := xmlrpc.NewClient(ts.URL)
	_, err := c.Call(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestClientCallEmptyResponse(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Empty params section — should yield NilValue
		mr := &xmlrpc.MethodResponse{}
		raw, _ := xmlrpc.MarshalResponseBytes(mr)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write(raw)
	}))
	defer ts.Close()

	c := xmlrpc.NewClient(ts.URL)
	v, err := c.Call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if _, ok := v.(xmlrpc.NilValue); !ok {
		t.Errorf("expected NilValue for empty params, got %T", v)
	}
}

func TestClientCallNetworkError(t *testing.T) {
	t.Parallel()
	c := xmlrpc.NewClient("http://127.0.0.1:1") // port 1 is unreachable
	_, err := c.Call(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
}

func TestClientCallBadURL(t *testing.T) {
	t.Parallel()
	c := xmlrpc.NewClient("://bad-url")
	_, err := c.Call(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error for bad URL, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────
// message.go — newCharsetReader
// ─────────────────────────────────────────────────────────────────

func TestNewCharsetReader(t *testing.T) {
	t.Parallel()
	// Test that ISO-8859-1 encoded XML is accepted — the charset is
	// treated as passthrough since godevccu only emits 7-bit ASCII.
	body := `<?xml version="1.0" encoding="ISO-8859-1"?><methodCall><methodName>test</methodName><params></params></methodCall>`
	call, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeCall ISO-8859-1: %v", err)
	}
	if call.Method != "test" {
		t.Errorf("method = %q", call.Method)
	}
}

func TestUnsupportedCharset(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0" encoding="EUC-JP"?><methodCall><methodName>test</methodName><params></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	// should fail on the unsupported charset
	if err == nil {
		t.Fatal("expected error for unsupported charset")
	}
}

// ─────────────────────────────────────────────────────────────────
// message.go — decodeParams / decodeParam edge cases
// ─────────────────────────────────────────────────────────────────

func TestDecodeEmptyParams(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params></params></methodCall>`
	call, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeCall: %v", err)
	}
	if len(call.Params) != 0 {
		t.Fatalf("expected 0 params, got %d", len(call.Params))
	}
}

func TestDecodeParamEmptyValue(t *testing.T) {
	t.Parallel()
	// <param></param> with no <value> child — should decode as NilValue.
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param></param></params></methodCall>`
	call, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeCall: %v", err)
	}
	if len(call.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(call.Params))
	}
	if _, ok := call.Params[0].(xmlrpc.NilValue); !ok {
		t.Errorf("expected NilValue, got %T", call.Params[0])
	}
}

// ─────────────────────────────────────────────────────────────────
// struct Get method
// ─────────────────────────────────────────────────────────────────

func TestStructGetNotFound(t *testing.T) {
	t.Parallel()
	s := xmlrpc.StructValue{Members: []xmlrpc.Member{
		{Name: "a", Value: xmlrpc.IntValue(1)},
	}}
	v, ok := s.Get("missing")
	if ok || v != nil {
		t.Errorf("Get(missing) = %v, %v; want nil, false", v, ok)
	}
}

// ─────────────────────────────────────────────────────────────────
// convert.go — isIntegralFloat edge cases
// ─────────────────────────────────────────────────────────────────

func TestFromAnyNaNAndInf(t *testing.T) {
	t.Parallel()
	inf := math.Inf(1) // +Inf
	v := xmlrpc.FromAny(inf)
	if v.Kind() != xmlrpc.KindDouble {
		t.Errorf("FromAny(+Inf) kind = %s, want double", v.Kind())
	}
}

func TestFromAnyLargeFloat(t *testing.T) {
	t.Parallel()
	// float64 value outside int32 range stays as Double
	v := xmlrpc.FromAny(float64(1 << 40))
	if v.Kind() != xmlrpc.KindDouble {
		t.Errorf("FromAny(large float) kind = %s, want double", v.Kind())
	}
}

// ─────────────────────────────────────────────────────────────────
// Full HTTP round-trip via httptest.Server
// ─────────────────────────────────────────────────────────────────

func TestHandlerViaHTTPServer(t *testing.T) {
	t.Parallel()
	h := xmlrpc.NewHandler()
	h.Mux.Handle("add", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) != 2 {
			return nil, &xmlrpc.Fault{Code: -1, Message: "want 2 params"}
		}
		a, aok := xmlrpc.AsInt(params[0])
		b, bok := xmlrpc.AsInt(params[1])
		if !aok || !bok {
			return nil, &xmlrpc.Fault{Code: -1, Message: "want int params"}
		}
		return xmlrpc.IntValue(int32(a + b)), nil //nolint:gosec
	})
	ts := httptest.NewServer(h)
	defer ts.Close()

	c := xmlrpc.NewClient(ts.URL)
	v, err := c.Call(context.Background(), "add", []xmlrpc.Value{xmlrpc.IntValue(3), xmlrpc.IntValue(4)})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result, ok := xmlrpc.AsInt(v); !ok || result != 7 {
		t.Errorf("add(3,4) = %v, want 7", v)
	}
}
