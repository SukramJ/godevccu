// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc_test

// Additional targeted coverage tests to push the remaining gaps above 85%.

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// ─────────────────────────────────────────────────────────────────
// convert.go — As* failure paths (returns false for wrong type)
// ─────────────────────────────────────────────────────────────────

func TestAsStringFail(t *testing.T) {
	t.Parallel()
	if _, ok := xmlrpc.AsString(xmlrpc.IntValue(1)); ok {
		t.Error("AsString(IntValue) should return false")
	}
}

func TestAsIntFail(t *testing.T) {
	t.Parallel()
	if _, ok := xmlrpc.AsInt(xmlrpc.StringValue("x")); ok {
		t.Error("AsInt(StringValue) should return false")
	}
}

func TestAsBoolFail(t *testing.T) {
	t.Parallel()
	if _, ok := xmlrpc.AsBool(xmlrpc.IntValue(1)); ok {
		t.Error("AsBool(IntValue) should return false")
	}
}

func TestAsArrayFail(t *testing.T) {
	t.Parallel()
	if _, ok := xmlrpc.AsArray(xmlrpc.StringValue("x")); ok {
		t.Error("AsArray(StringValue) should return false")
	}
}

func TestAsStructFail(t *testing.T) {
	t.Parallel()
	if _, ok := xmlrpc.AsStruct(xmlrpc.StringValue("x")); ok {
		t.Error("AsStruct(StringValue) should return false")
	}
}

// ─────────────────────────────────────────────────────────────────
// value.go — MarshalXML round-trips for all value types
// ─────────────────────────────────────────────────────────────────

// marshalRoundTrip encodes a single Value as a methodCall param
// and decodes it back, returning the decoded value.
func marshalRoundTrip(t *testing.T, v xmlrpc.Value) xmlrpc.Value {
	t.Helper()
	raw, err := xmlrpc.MarshalCallBytes(&xmlrpc.MethodCall{
		Method: "test",
		Params: []xmlrpc.Value{v},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	call, err := xmlrpc.DecodeCall(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(call.Params) == 0 {
		t.Fatal("no params decoded")
	}
	return call.Params[0]
}

func TestNilValueMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	v := marshalRoundTrip(t, xmlrpc.NilValue{})
	if _, ok := v.(xmlrpc.NilValue); !ok {
		t.Fatalf("expected NilValue, got %T", v)
	}
}

func TestBoolValueMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	v := marshalRoundTrip(t, xmlrpc.BoolValue(true))
	b, ok := xmlrpc.AsBool(v)
	if !ok || !b {
		t.Fatalf("expected true, got %v (%T)", v, v)
	}
	v2 := marshalRoundTrip(t, xmlrpc.BoolValue(false))
	b2, ok2 := xmlrpc.AsBool(v2)
	if !ok2 || b2 {
		t.Fatalf("expected false, got %v", v2)
	}
}

func TestDoubleValueMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	v := marshalRoundTrip(t, xmlrpc.DoubleValue(2.718))
	d, ok := v.(xmlrpc.DoubleValue)
	if !ok {
		t.Fatalf("expected DoubleValue, got %T", v)
	}
	if float64(d) != 2.718 {
		t.Errorf("double = %v, want 2.718", float64(d))
	}
}

func TestBase64ValueMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	payload := []byte("hello world")
	v := marshalRoundTrip(t, xmlrpc.Base64Value(payload))
	b, ok := v.(xmlrpc.Base64Value)
	if !ok {
		t.Fatalf("expected Base64Value, got %T", v)
	}
	if !bytes.Equal([]byte(b), payload) {
		t.Errorf("bytes = %v, want %v", []byte(b), payload)
	}
}

func TestStructValueMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	sv := xmlrpc.StructValue{Members: []xmlrpc.Member{
		{Name: "foo", Value: xmlrpc.StringValue("bar")},
		{Name: "n", Value: xmlrpc.IntValue(99)},
	}}
	v := marshalRoundTrip(t, sv)
	s, ok := xmlrpc.AsStruct(v)
	if !ok {
		t.Fatalf("expected StructValue, got %T", v)
	}
	fooVal, ok := s.Get("foo")
	if !ok {
		t.Fatal("missing 'foo' member")
	}
	if str, ok := xmlrpc.AsString(fooVal); !ok || str != "bar" {
		t.Errorf("foo = %v, want 'bar'", fooVal)
	}
}

func TestArrayValueMarshalRoundTrip(t *testing.T) {
	t.Parallel()
	av := xmlrpc.ArrayValue{xmlrpc.IntValue(1), xmlrpc.IntValue(2), xmlrpc.IntValue(3)}
	v := marshalRoundTrip(t, av)
	arr, ok := xmlrpc.AsArray(v)
	if !ok {
		t.Fatalf("expected ArrayValue, got %T", v)
	}
	if len(arr) != 3 {
		t.Fatalf("len = %d, want 3", len(arr))
	}
}

// ─────────────────────────────────────────────────────────────────
// decode.go — decodeArray edge case: empty array tag (no data child)
// ─────────────────────────────────────────────────────────────────

func TestDecodeArrayNoDataElement(t *testing.T) {
	t.Parallel()
	// <array></array> — no <data> child — decodes as nil array (empty).
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><array></array></value></param></params></methodCall>`
	call, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodeCall: %v", err)
	}
	if len(call.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(call.Params))
	}
	// Nil array value returns nil from AsArray with ok=false — that's
	// acceptable; what matters is no crash.
	_, _ = xmlrpc.AsArray(call.Params[0])
}

// ─────────────────────────────────────────────────────────────────
// message.go — Fault-only EncodeResponse
// ─────────────────────────────────────────────────────────────────

func TestEncodeResponseFault(t *testing.T) {
	t.Parallel()
	mr := &xmlrpc.MethodResponse{
		Fault: &xmlrpc.Fault{Code: -32601, Message: "method not found"},
	}
	var buf bytes.Buffer
	if err := xmlrpc.EncodeResponse(&buf, mr); err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	got, err := xmlrpc.DecodeResponse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if got.Fault == nil {
		t.Fatal("expected fault, got nil")
	}
	if got.Fault.Code != -32601 {
		t.Errorf("fault code = %d, want -32601", got.Fault.Code)
	}
}

func TestEncodeDecodeResponseParams(t *testing.T) {
	t.Parallel()
	// Multiple params round-trip.
	params := []xmlrpc.Value{
		xmlrpc.StringValue("a"),
		xmlrpc.IntValue(42),
		xmlrpc.BoolValue(true),
	}
	raw, err := xmlrpc.MarshalResponseBytes(&xmlrpc.MethodResponse{Params: params})
	if err != nil {
		t.Fatalf("MarshalResponseBytes: %v", err)
	}
	got, err := xmlrpc.DecodeResponse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if len(got.Params) != 3 {
		t.Fatalf("params = %d, want 3", len(got.Params))
	}
}

// ─────────────────────────────────────────────────────────────────
// message.go — findStart with unexpected end element
// ─────────────────────────────────────────────────────────────────

func TestDecodeCallUnexpectedEndElement(t *testing.T) {
	t.Parallel()
	_, err := xmlrpc.DecodeCall(strings.NewReader(`<?xml version="1.0"?></methodCall>`))
	if err == nil {
		t.Fatal("expected error for unexpected end element")
	}
}

func TestDecodeResponseUnexpectedElement(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodResponse><unexpected/></methodResponse>`
	_, err := xmlrpc.DecodeResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for unexpected element in methodResponse")
	}
}

// ─────────────────────────────────────────────────────────────────
// message.go — decodeFault error cases
// ─────────────────────────────────────────────────────────────────

func TestDecodeFaultMissingValue(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodResponse><fault></fault></methodResponse>`
	_, err := xmlrpc.DecodeResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for fault missing value")
	}
}

func TestDecodeFaultNonStructPayload(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodResponse><fault><value><string>notastruct</string></value></fault></methodResponse>`
	_, err := xmlrpc.DecodeResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for non-struct fault payload")
	}
}

func TestDecodeFaultMissingFaultCode(t *testing.T) {
	t.Parallel()
	// Struct with faultString but missing faultCode.
	body := `<?xml version="1.0"?><methodResponse><fault><value><struct>
		<member><name>faultString</name><value><string>msg</string></value></member>
	</struct></value></fault></methodResponse>`
	_, err := xmlrpc.DecodeResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for missing faultCode")
	}
}

func TestDecodeFaultMissingFaultString(t *testing.T) {
	t.Parallel()
	// Struct with faultCode but missing faultString.
	body := `<?xml version="1.0"?><methodResponse><fault><value><struct>
		<member><name>faultCode</name><value><i4>-1</i4></value></member>
	</struct></value></fault></methodResponse>`
	_, err := xmlrpc.DecodeResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for missing faultString")
	}
}

func TestDecodeFaultCodeNotInt(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodResponse><fault><value><struct>
		<member><name>faultCode</name><value><string>notint</string></value></member>
		<member><name>faultString</name><value><string>msg</string></value></member>
	</struct></value></fault></methodResponse>`
	_, err := xmlrpc.DecodeResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for faultCode not int")
	}
}

func TestDecodeFaultStringNotString(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodResponse><fault><value><struct>
		<member><name>faultCode</name><value><i4>-1</i4></value></member>
		<member><name>faultString</name><value><i4>42</i4></value></member>
	</struct></value></fault></methodResponse>`
	_, err := xmlrpc.DecodeResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for faultString not string")
	}
}

// ─────────────────────────────────────────────────────────────────
// decode.go — struct/member error cases
// ─────────────────────────────────────────────────────────────────

func TestDecodeStructUnexpectedElement(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><struct><unexpected/></struct></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for unexpected element inside struct")
	}
}

func TestDecodeMemberMissingName(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><struct>
		<member><value><string>val</string></value></member>
	</struct></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for member missing name")
	}
}

func TestDecodeMemberMissingValue(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><struct>
		<member><name>k</name></member>
	</struct></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for member missing value")
	}
}

func TestDecodeMemberUnexpectedElement(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><struct>
		<member><name>k</name><unexpected/></member>
	</struct></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for unexpected element inside member")
	}
}

// ─────────────────────────────────────────────────────────────────
// decode.go — consumeCloseOrSelfClose with stray start element
// ─────────────────────────────────────────────────────────────────

func TestNilValueWithChildElement(t *testing.T) {
	t.Parallel()
	// <nil><stray/></nil> should fail
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><nil><stray/></nil></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for nil with child element")
	}
}

// ─────────────────────────────────────────────────────────────────
// decode.go — expectEnd stray start element
// ─────────────────────────────────────────────────────────────────

func TestExpectEndWithStrayElement(t *testing.T) {
	t.Parallel()
	// <i4>1<stray/>…</i4> — after chardata, unexpected start
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><param><value><i4>1<stray/>2</i4></value></param></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for stray element inside typed value")
	}
}

// ─────────────────────────────────────────────────────────────────
// message.go — decodeParams unexpected element
// ─────────────────────────────────────────────────────────────────

func TestDecodeParamsUnexpectedElement(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0"?><methodCall><methodName>test</methodName><params><unexpected/></params></methodCall>`
	_, err := xmlrpc.DecodeCall(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for unexpected element inside params")
	}
}

// ─────────────────────────────────────────────────────────────────
// Mux — concurrent safety
// ─────────────────────────────────────────────────────────────────

func TestMuxConcurrentDispatch(t *testing.T) {
	t.Parallel()
	m := xmlrpc.NewMux()
	m.Handle("echo", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) > 0 {
			return params[0], nil
		}
		return xmlrpc.NilValue{}, nil
	})

	const goroutines = 20
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			_, _ = m.Dispatch(context.Background(), "echo", []xmlrpc.Value{xmlrpc.IntValue(int32(n))}) //nolint:gosec
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// ─────────────────────────────────────────────────────────────────
// client.go — call with params (encode path)
// ─────────────────────────────────────────────────────────────────

func TestClientCallWithParams(t *testing.T) {
	t.Parallel()
	h := xmlrpc.NewHandler()
	h.Mux.Handle("echo", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) == 0 {
			return xmlrpc.NilValue{}, nil
		}
		return params[0], nil
	})
	ts := newXMLRPCServer(t, h)

	c := xmlrpc.NewClient(ts.URL)
	v, err := c.Call(context.Background(), "echo", []xmlrpc.Value{xmlrpc.StringValue("world")})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	s, ok := xmlrpc.AsString(v)
	if !ok || s != "world" {
		t.Errorf("result = %v, want 'world'", v)
	}
}

// ─────────────────────────────────────────────────────────────────
// MarshalCallBytes with multiple params (encodeParams path)
// ─────────────────────────────────────────────────────────────────

func TestMarshalCallBytesMultipleParams(t *testing.T) {
	t.Parallel()
	mc := &xmlrpc.MethodCall{
		Method: "multi",
		Params: []xmlrpc.Value{
			xmlrpc.IntValue(1),
			xmlrpc.StringValue("two"),
			xmlrpc.BoolValue(true),
			xmlrpc.DoubleValue(4.0),
		},
	}
	raw, err := xmlrpc.MarshalCallBytes(mc)
	if err != nil {
		t.Fatalf("MarshalCallBytes: %v", err)
	}
	call, err := xmlrpc.DecodeCall(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("DecodeCall: %v", err)
	}
	if len(call.Params) != 4 {
		t.Fatalf("expected 4 params, got %d", len(call.Params))
	}
}

// ─────────────────────────────────────────────────────────────────
// Stringify with unknown type (not Value interface)
// ─────────────────────────────────────────────────────────────────

// There is no way to trigger the default branch of Stringify from user code
// since the function only accepts xmlrpc.Value. The nil branch is a test
// for explicit nil passthrough.
func TestStringifyNil(t *testing.T) {
	t.Parallel()
	got := xmlrpc.Stringify(nil)
	if got != "<nil>" {
		t.Errorf("Stringify(nil) = %q, want '<nil>'", got)
	}
}

// ─────────────────────────────────────────────────────────────────
// Helper — newXMLRPCServer starts an httptest.Server with the given handler.
// ─────────────────────────────────────────────────────────────────

type serverHelper struct{ URL string }

func newXMLRPCServer(t *testing.T, h *xmlrpc.Handler) *serverHelper {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &serverHelper{URL: ts.URL}
}
