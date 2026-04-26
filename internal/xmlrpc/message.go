// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
)

// Fault is the data carried by an XML-RPC <fault> response. It also
// implements error so handlers can return it directly.
type Fault struct {
	Code    int
	Message string
}

func (f *Fault) Error() string {
	return fmt.Sprintf("xmlrpc fault %d: %s", f.Code, f.Message)
}

// MethodCall is a deserialised <methodCall>.
type MethodCall struct {
	Method string
	Params []Value
}

// MethodResponse is a deserialised <methodResponse>. Exactly one of
// Params / Fault is populated.
type MethodResponse struct {
	Params []Value
	Fault  *Fault
}

const xmlPreamble = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// EncodeCall writes mc to w using UTF-8 (sufficient for pydevccu
// clients; the original CCU emits ISO-8859-1 but accepts UTF-8 too).
func EncodeCall(w io.Writer, mc *MethodCall) error {
	if mc == nil {
		return errors.New("xmlrpc: EncodeCall: nil MethodCall")
	}
	if mc.Method == "" {
		return errors.New("xmlrpc: EncodeCall: Method is empty")
	}
	if _, err := io.WriteString(w, xmlPreamble); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	root := xml.StartElement{Name: xml.Name{Local: "methodCall"}}
	if err := enc.EncodeToken(root); err != nil {
		return err
	}
	if err := writeBareElement(enc, "methodName", mc.Method); err != nil {
		return err
	}
	if err := encodeParams(enc, mc.Params); err != nil {
		return err
	}
	if err := enc.EncodeToken(root.End()); err != nil {
		return err
	}
	return enc.Flush()
}

// EncodeResponse writes mr to w. If Fault is set, Params is ignored.
func EncodeResponse(w io.Writer, mr *MethodResponse) error {
	if mr == nil {
		return errors.New("xmlrpc: EncodeResponse: nil MethodResponse")
	}
	if _, err := io.WriteString(w, xmlPreamble); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	root := xml.StartElement{Name: xml.Name{Local: "methodResponse"}}
	if err := enc.EncodeToken(root); err != nil {
		return err
	}
	if mr.Fault != nil {
		if err := encodeFault(enc, mr.Fault); err != nil {
			return err
		}
	} else if err := encodeParams(enc, mr.Params); err != nil {
		return err
	}
	if err := enc.EncodeToken(root.End()); err != nil {
		return err
	}
	return enc.Flush()
}

// DecodeCall reads a <methodCall> from r.
func DecodeCall(r io.Reader) (*MethodCall, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = newCharsetReader
	start, err := findStart(dec, "methodCall")
	if err != nil {
		return nil, err
	}

	var out MethodCall
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("xmlrpc: decode methodCall: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "methodName":
				s, cerr := readChardata(dec, t)
				if cerr != nil {
					return nil, cerr
				}
				out.Method = s
			case "params":
				params, perr := decodeParams(dec, t)
				if perr != nil {
					return nil, perr
				}
				out.Params = params
			default:
				return nil, fmt.Errorf("xmlrpc: methodCall: unexpected <%s>", t.Name.Local)
			}
		case xml.EndElement:
			if t.Name.Local != start.Name.Local {
				return nil, fmt.Errorf("xmlrpc: methodCall: unexpected </%s>", t.Name.Local)
			}
			if out.Method == "" {
				return nil, errors.New("xmlrpc: methodCall missing <methodName>")
			}
			return &out, nil
		}
	}
}

// DecodeResponse reads a <methodResponse> from r.
func DecodeResponse(r io.Reader) (*MethodResponse, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = newCharsetReader
	start, err := findStart(dec, "methodResponse")
	if err != nil {
		return nil, err
	}

	var out MethodResponse
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("xmlrpc: decode methodResponse: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "params":
				params, perr := decodeParams(dec, t)
				if perr != nil {
					return nil, perr
				}
				out.Params = params
			case "fault":
				fault, ferr := decodeFault(dec, t)
				if ferr != nil {
					return nil, ferr
				}
				out.Fault = fault
			default:
				return nil, fmt.Errorf("xmlrpc: methodResponse: unexpected <%s>", t.Name.Local)
			}
		case xml.EndElement:
			if t.Name.Local != start.Name.Local {
				return nil, fmt.Errorf("xmlrpc: methodResponse: unexpected </%s>", t.Name.Local)
			}
			return &out, nil
		}
	}
}

func findStart(d *xml.Decoder, expected string) (xml.StartElement, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return xml.StartElement{}, fmt.Errorf("xmlrpc: find <%s>: %w", expected, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != expected {
				return xml.StartElement{}, fmt.Errorf("xmlrpc: expected <%s>, got <%s>", expected, t.Name.Local)
			}
			return t, nil
		case xml.CharData, xml.Comment, xml.ProcInst, xml.Directive:
			// ignore
		case xml.EndElement:
			return xml.StartElement{}, fmt.Errorf("xmlrpc: unexpected </%s> while looking for <%s>", t.Name.Local, expected)
		}
	}
}

func encodeParams(e *xml.Encoder, params []Value) error {
	paramsStart := xml.StartElement{Name: xml.Name{Local: "params"}}
	if err := e.EncodeToken(paramsStart); err != nil {
		return err
	}
	for i, p := range params {
		paramStart := xml.StartElement{Name: xml.Name{Local: "param"}}
		if err := e.EncodeToken(paramStart); err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("xmlrpc: param %d is nil", i)
		}
		if err := p.MarshalXML(e, valueEnvelope); err != nil {
			return err
		}
		if err := e.EncodeToken(paramStart.End()); err != nil {
			return err
		}
	}
	return e.EncodeToken(paramsStart.End())
}

func decodeParams(d *xml.Decoder, start xml.StartElement) ([]Value, error) {
	var out []Value
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("xmlrpc: decode params: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "param" {
				return nil, fmt.Errorf("xmlrpc: params: unexpected <%s>", t.Name.Local)
			}
			v, err := decodeParam(d)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		case xml.EndElement:
			if t.Name.Local != start.Name.Local {
				return nil, fmt.Errorf("xmlrpc: params: unexpected </%s>", t.Name.Local)
			}
			return out, nil
		}
	}
}

func decodeParam(d *xml.Decoder) (Value, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("xmlrpc: decode param: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "value" {
				return nil, fmt.Errorf("xmlrpc: param: unexpected <%s>", t.Name.Local)
			}
			v, err := DecodeValue(d, t)
			if err != nil {
				return nil, err
			}
			if err := expectEnd(d, "param"); err != nil {
				return nil, err
			}
			return v, nil
		case xml.EndElement:
			if t.Name.Local == "param" {
				return NilValue{}, nil
			}
		}
	}
}

func encodeFault(e *xml.Encoder, f *Fault) error {
	faultStart := xml.StartElement{Name: xml.Name{Local: "fault"}}
	if err := e.EncodeToken(faultStart); err != nil {
		return err
	}
	payload := StructValue{Members: []Member{
		{Name: "faultCode", Value: IntValue(int32(f.Code))}, //nolint:gosec
		{Name: "faultString", Value: StringValue(f.Message)},
	}}
	if err := payload.MarshalXML(e, valueEnvelope); err != nil {
		return err
	}
	return e.EncodeToken(faultStart.End())
}

func decodeFault(d *xml.Decoder, start xml.StartElement) (*Fault, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("xmlrpc: decode fault: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "value" {
				return nil, fmt.Errorf("xmlrpc: fault: unexpected <%s>", t.Name.Local)
			}
			v, err := DecodeValue(d, t)
			if err != nil {
				return nil, err
			}
			s, ok := v.(StructValue)
			if !ok {
				return nil, fmt.Errorf("xmlrpc: fault payload must be struct, got %s", v.Kind())
			}
			codeVal, ok := s.Get("faultCode")
			if !ok {
				return nil, errors.New("xmlrpc: fault missing faultCode")
			}
			msgVal, ok := s.Get("faultString")
			if !ok {
				return nil, errors.New("xmlrpc: fault missing faultString")
			}
			codeInt, ok := codeVal.(IntValue)
			if !ok {
				return nil, fmt.Errorf("xmlrpc: faultCode must be int, got %s", codeVal.Kind())
			}
			msgStr, ok := msgVal.(StringValue)
			if !ok {
				return nil, fmt.Errorf("xmlrpc: faultString must be string, got %s", msgVal.Kind())
			}
			if err := expectEnd(d, "fault"); err != nil {
				return nil, err
			}
			return &Fault{Code: int(codeInt), Message: string(msgStr)}, nil
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return nil, errors.New("xmlrpc: <fault> missing <value>")
			}
		}
	}
}

// MarshalCallBytes is a convenience around EncodeCall.
func MarshalCallBytes(mc *MethodCall) ([]byte, error) {
	var buf bytes.Buffer
	if err := EncodeCall(&buf, mc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalResponseBytes is a convenience around EncodeResponse.
func MarshalResponseBytes(mr *MethodResponse) ([]byte, error) {
	var buf bytes.Buffer
	if err := EncodeResponse(&buf, mr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// newCharsetReader maps the legacy ISO-8859-1 declaration the CCU uses
// to a passthrough — the bytes are 7-bit clean for everything godevccu
// emits.
func newCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch charset {
	case "utf-8", "UTF-8", "utf8", "":
		return input, nil
	case "iso-8859-1", "ISO-8859-1":
		return input, nil
	}
	return nil, fmt.Errorf("xmlrpc: unsupported charset %q", charset)
}
