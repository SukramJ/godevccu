// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package regavm

import (
	"math"
	"strconv"
	"strings"
)

// Value is a runtime value. ReGa is loosely typed: a value converts to
// whatever the context asks for, and the "#" operator concatenates
// anything with anything. The zero Value is the empty string, which is
// also what an unset variable holds.
type Value struct {
	// obj is non-nil for object-typed values.
	obj Object
	// list is non-nil for id arrays and enumeration results.
	list []Value
	// str, num and boolean hold the scalar forms; kind says which one
	// was written last, which decides how the value renders.
	str     string
	num     float64
	boolean bool
	kind    valueKind
}

type valueKind int

const (
	kindString valueKind = iota
	kindNumber
	kindBool
	kindObject
	kindList
)

// Constructors.

func stringValue(s string) Value  { return Value{kind: kindString, str: s} }
func numberValue(f float64) Value { return Value{kind: kindNumber, num: f} }
func boolValue(b bool) Value      { return Value{kind: kindBool, boolean: b} }
func objectValue(o Object) Value  { return Value{kind: kindObject, obj: o} }
func listValue(v []Value) Value   { return Value{kind: kindList, list: v} }

// nullObject is the value a failed lookup yields. It is falsy, so the
// "if (object)" guard every script wraps its lookups in works.
var nullValue = Value{kind: kindObject}

// String renders the value the way Write() would emit it.
func (v Value) String() string {
	switch v.kind {
	case kindString:
		return v.str
	case kindNumber:
		return formatNumber(v.num)
	case kindBool:
		return strconv.FormatBool(v.boolean)
	case kindObject:
		if v.obj == nil {
			return ""
		}
		return v.obj.Name()
	case kindList:
		parts := make([]string, 0, len(v.list))
		for _, item := range v.list {
			parts = append(parts, item.String())
		}
		// ReGa separates enumeration results with tabs, which is what
		// foreach splits on.
		return strings.Join(parts, "\t")
	default:
		return ""
	}
}

// formatNumber renders a float without a trailing ".0", matching how
// ReGa prints whole numbers.
func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Number coerces to a float; a non-numeric string is 0.
func (v Value) Number() float64 {
	switch v.kind {
	case kindNumber:
		return v.num
	case kindBool:
		if v.boolean {
			return 1
		}
		return 0
	case kindString:
		f, err := strconv.ParseFloat(strings.TrimSpace(v.str), 64)
		if err != nil {
			return 0
		}
		return f
	case kindList:
		return float64(len(v.list))
	default:
		return 0
	}
}

// Bool coerces to a truth value. An object is truthy when it exists —
// that is what "if (oDevice)" tests.
func (v Value) Bool() bool {
	switch v.kind {
	case kindBool:
		return v.boolean
	case kindNumber:
		return v.num != 0
	case kindString:
		return v.str != "" && !strings.EqualFold(v.str, "false")
	case kindObject:
		return v.obj != nil
	case kindList:
		return len(v.list) > 0
	default:
		return false
	}
}

// Object returns the object behind the value, or nil.
func (v Value) Object() Object { return v.obj }

// List returns the elements of a list value. A scalar iterates as its
// tab-separated parts, which is how foreach walks an EnumIDs() result
// that was assigned to a string first.
func (v Value) List() []Value {
	if v.kind == kindList {
		return v.list
	}
	if v.kind == kindString && v.str != "" {
		parts := strings.Split(v.str, "\t")
		out := make([]Value, 0, len(parts))
		for _, p := range parts {
			if p != "" {
				out = append(out, stringValue(p))
			}
		}
		return out
	}
	return nil
}

// equals compares two values, numerically when both look numeric and
// textually otherwise.
func (v Value) equals(other Value) bool {
	if v.kind == kindObject || other.kind == kindObject {
		return v.obj == other.obj
	}
	if v.kind == kindBool || other.kind == kindBool {
		return v.Bool() == other.Bool()
	}
	if v.numeric() && other.numeric() {
		return v.Number() == other.Number()
	}
	return v.String() == other.String()
}

// numeric reports whether the value converts to a number cleanly.
func (v Value) numeric() bool {
	switch v.kind {
	case kindNumber, kindBool:
		return true
	case kindString:
		_, err := strconv.ParseFloat(strings.TrimSpace(v.str), 64)
		return err == nil
	default:
		return false
	}
}

// StringValue lifts a Go string into a [Value]. Exported for the
// composing layer and for object implementations outside this package.
func StringValue(s string) Value { return stringValue(s) }

// NumberValue lifts a float.
func NumberValue(f float64) Value { return numberValue(f) }

// BoolValue lifts a bool.
func BoolValue(b bool) Value { return boolValue(b) }

// ListValue lifts a list.
func ListValue(v []Value) Value { return listValue(v) }
