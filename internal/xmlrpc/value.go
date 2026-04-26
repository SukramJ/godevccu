// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package xmlrpc implements just enough of the XML-RPC protocol to act
// as a HomeMatic CCU server. It is a tightened, server-side port of the
// gohomematic transport package.
//
// All public surfaces are exported as Value types plus encode/decode
// helpers. Higher levels (the ccu package) translate Value trees into
// the loosely-typed map[string]any values that mirror the Python source.
package xmlrpc

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Kind identifies the concrete XML-RPC value carried by [Value].
type Kind int

// Supported value kinds. Order is stable.
const (
	KindNil Kind = iota
	KindInt
	KindBool
	KindString
	KindDouble
	KindDateTime
	KindBase64
	KindStruct
	KindArray
)

// String returns the XML token representing the kind.
func (k Kind) String() string {
	switch k {
	case KindNil:
		return "nil"
	case KindInt:
		return "int"
	case KindBool:
		return "boolean"
	case KindString:
		return "string"
	case KindDouble:
		return "double"
	case KindDateTime:
		return "dateTime.iso8601"
	case KindBase64:
		return "base64"
	case KindStruct:
		return "struct"
	case KindArray:
		return "array"
	default:
		return "unknown"
	}
}

// Value is the sum type covering every XML-RPC value. Implementations
// serialise themselves including the wrapping <value>…</value>.
type Value interface {
	Kind() Kind
	MarshalXML(e *xml.Encoder, start xml.StartElement) error
}

var valueEnvelope = xml.StartElement{Name: xml.Name{Local: "value"}}

// NilValue serialises as <value><nil/></value>.
type NilValue struct{}

func (NilValue) Kind() Kind { return KindNil }

func (NilValue) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	return writeTagged(e, "nil", "")
}

// IntValue maps to <i4>.
type IntValue int32

func (IntValue) Kind() Kind { return KindInt }

func (v IntValue) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	return writeTagged(e, "i4", strconv.FormatInt(int64(v), 10))
}

// BoolValue maps to <boolean>.
type BoolValue bool

func (BoolValue) Kind() Kind { return KindBool }

func (v BoolValue) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	s := "0"
	if v {
		s = "1"
	}
	return writeTagged(e, "boolean", s)
}

// StringValue maps to <string>.
type StringValue string

func (StringValue) Kind() Kind { return KindString }

func (v StringValue) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	return writeTagged(e, "string", string(v))
}

// DoubleValue maps to <double>.
type DoubleValue float64

func (DoubleValue) Kind() Kind { return KindDouble }

func (v DoubleValue) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	return writeTagged(e, "double", strconv.FormatFloat(float64(v), 'f', -1, 64))
}

// DateTimeValue maps to <dateTime.iso8601>.
type DateTimeValue time.Time

func (DateTimeValue) Kind() Kind { return KindDateTime }

// ISO8601CompactLayout is the CCU's canonical dateTime.iso8601 format.
const ISO8601CompactLayout = "20060102T15:04:05"

func (v DateTimeValue) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	return writeTagged(e, "dateTime.iso8601", time.Time(v).Format(ISO8601CompactLayout))
}

// Time returns the wrapped Go time.
func (v DateTimeValue) Time() time.Time { return time.Time(v) }

// Base64Value maps to <base64>.
type Base64Value []byte

func (Base64Value) Kind() Kind { return KindBase64 }

func (v Base64Value) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	return writeTagged(e, "base64", base64.StdEncoding.EncodeToString([]byte(v)))
}

// StructValue is an ordered list of named members.
type StructValue struct {
	Members []Member
}

// Member is one entry in a [StructValue].
type Member struct {
	Name  string
	Value Value
}

func (StructValue) Kind() Kind { return KindStruct }

// Get returns the named member or (nil, false) if absent.
func (s StructValue) Get(name string) (Value, bool) {
	for _, m := range s.Members {
		if m.Name == name {
			return m.Value, true
		}
	}
	return nil, false
}

func (s StructValue) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	if err := e.EncodeToken(valueEnvelope); err != nil {
		return err
	}
	structStart := xml.StartElement{Name: xml.Name{Local: "struct"}}
	if err := e.EncodeToken(structStart); err != nil {
		return err
	}
	for _, m := range s.Members {
		memberStart := xml.StartElement{Name: xml.Name{Local: "member"}}
		if err := e.EncodeToken(memberStart); err != nil {
			return err
		}
		if err := writeBareElement(e, "name", m.Name); err != nil {
			return err
		}
		if m.Value == nil {
			return fmt.Errorf("xmlrpc: struct member %q has nil value", m.Name)
		}
		if err := m.Value.MarshalXML(e, valueEnvelope); err != nil {
			return err
		}
		if err := e.EncodeToken(memberStart.End()); err != nil {
			return err
		}
	}
	if err := e.EncodeToken(structStart.End()); err != nil {
		return err
	}
	return e.EncodeToken(valueEnvelope.End())
}

// ArrayValue maps to <array><data>…</data></array>.
type ArrayValue []Value

func (ArrayValue) Kind() Kind { return KindArray }

func (a ArrayValue) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	if err := e.EncodeToken(valueEnvelope); err != nil {
		return err
	}
	arrayStart := xml.StartElement{Name: xml.Name{Local: "array"}}
	if err := e.EncodeToken(arrayStart); err != nil {
		return err
	}
	dataStart := xml.StartElement{Name: xml.Name{Local: "data"}}
	if err := e.EncodeToken(dataStart); err != nil {
		return err
	}
	for i, v := range a {
		if v == nil {
			return fmt.Errorf("xmlrpc: array element %d is nil", i)
		}
		if err := v.MarshalXML(e, valueEnvelope); err != nil {
			return err
		}
	}
	if err := e.EncodeToken(dataStart.End()); err != nil {
		return err
	}
	if err := e.EncodeToken(arrayStart.End()); err != nil {
		return err
	}
	return e.EncodeToken(valueEnvelope.End())
}

// writeTagged emits <value><tag>content</tag></value>.
func writeTagged(e *xml.Encoder, tag, content string) error {
	if err := e.EncodeToken(valueEnvelope); err != nil {
		return err
	}
	inner := xml.StartElement{Name: xml.Name{Local: tag}}
	if err := e.EncodeToken(inner); err != nil {
		return err
	}
	if content != "" {
		if err := e.EncodeToken(xml.CharData(content)); err != nil {
			return err
		}
	}
	if err := e.EncodeToken(inner.End()); err != nil {
		return err
	}
	return e.EncodeToken(valueEnvelope.End())
}

// writeBareElement emits <tag>content</tag>.
func writeBareElement(e *xml.Encoder, tag, content string) error {
	start := xml.StartElement{Name: xml.Name{Local: tag}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if content != "" {
		if err := e.EncodeToken(xml.CharData(content)); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// Stringify renders a value in a compact debug form. The format is
// stable but unspecified; use only for logging.
func Stringify(v Value) string {
	switch x := v.(type) {
	case nil:
		return "<nil>"
	case NilValue:
		return "nil"
	case IntValue:
		return strconv.FormatInt(int64(x), 10)
	case BoolValue:
		if x {
			return "true"
		}
		return "false"
	case StringValue:
		return strconv.Quote(string(x))
	case DoubleValue:
		return strconv.FormatFloat(float64(x), 'g', -1, 64)
	case DateTimeValue:
		return time.Time(x).Format(ISO8601CompactLayout)
	case Base64Value:
		return base64.StdEncoding.EncodeToString([]byte(x))
	case StructValue:
		var b strings.Builder
		b.WriteByte('{')
		for i, m := range x.Members {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(m.Name)
			b.WriteByte(':')
			b.WriteString(Stringify(m.Value))
		}
		b.WriteByte('}')
		return b.String()
	case ArrayValue:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(Stringify(e))
		}
		b.WriteByte(']')
		return b.String()
	default:
		return fmt.Sprintf("<%T>", v)
	}
}
