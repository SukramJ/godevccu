// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// FromAny converts an arbitrary Go value (typically the result of
// json.Unmarshal into any) to its XML-RPC equivalent. Nil yields
// [NilValue]. Numeric types collapse to [IntValue] or [DoubleValue];
// maps become [StructValue] sorted by key (deterministic output);
// slices become [ArrayValue].
func FromAny(v any) Value {
	switch x := v.(type) {
	case nil:
		return NilValue{}
	case Value:
		return x
	case bool:
		return BoolValue(x)
	case string:
		return StringValue(x)
	case []byte:
		return Base64Value(x)
	case time.Time:
		return DateTimeValue(x)
	case int:
		return IntValue(int32(x)) //nolint:gosec
	case int8:
		return IntValue(int32(x))
	case int16:
		return IntValue(int32(x))
	case int32:
		return IntValue(x)
	case int64:
		return IntValue(int32(x)) //nolint:gosec
	case uint:
		return IntValue(int32(x)) //nolint:gosec
	case uint8:
		return IntValue(int32(x))
	case uint16:
		return IntValue(int32(x))
	case uint32:
		return IntValue(int32(x)) //nolint:gosec
	case uint64:
		return IntValue(int32(x)) //nolint:gosec
	case float32:
		if isIntegralFloat(float64(x)) {
			return IntValue(int32(x))
		}
		return DoubleValue(x)
	case float64:
		// JSON unmarshal renders all numbers as float64 — keep the
		// integer subset as IntValue so paramsets carrying flags or
		// counters round-trip cleanly.
		if isIntegralFloat(x) && x >= math.MinInt32 && x <= math.MaxInt32 {
			return IntValue(int32(x))
		}
		return DoubleValue(x)
	case []any:
		out := make(ArrayValue, len(x))
		for i, e := range x {
			out[i] = FromAny(e)
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := StructValue{Members: make([]Member, 0, len(keys))}
		for _, k := range keys {
			out.Members = append(out.Members, Member{Name: k, Value: FromAny(x[k])})
		}
		return out
	case []map[string]any:
		out := make(ArrayValue, len(x))
		for i, e := range x {
			out[i] = FromAny(e)
		}
		return out
	case []string:
		out := make(ArrayValue, len(x))
		for i, e := range x {
			out[i] = StringValue(e)
		}
		return out
	case []int:
		out := make(ArrayValue, len(x))
		for i, e := range x {
			out[i] = IntValue(int32(e)) //nolint:gosec
		}
		return out
	default:
		// Fall back to a stringified representation rather than
		// failing at the protocol boundary.
		return StringValue(fmt.Sprintf("%v", x))
	}
}

// ToAny converts an XML-RPC value into its natural Go counterpart:
// struct→map[string]any, array→[]any, leaves to primitives.
func ToAny(v Value) any {
	switch x := v.(type) {
	case nil, NilValue:
		return nil
	case IntValue:
		return int(x)
	case BoolValue:
		return bool(x)
	case StringValue:
		return string(x)
	case DoubleValue:
		return float64(x)
	case DateTimeValue:
		return time.Time(x)
	case Base64Value:
		return []byte(x)
	case StructValue:
		out := make(map[string]any, len(x.Members))
		for _, m := range x.Members {
			out[m.Name] = ToAny(m.Value)
		}
		return out
	case ArrayValue:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = ToAny(e)
		}
		return out
	default:
		return nil
	}
}

// AsString extracts a Go string from v, accepting either StringValue or
// chardata that the decoder surfaced as such.
func AsString(v Value) (string, bool) {
	if s, ok := v.(StringValue); ok {
		return string(s), true
	}
	return "", false
}

// AsInt extracts a Go int from v.
func AsInt(v Value) (int, bool) {
	if i, ok := v.(IntValue); ok {
		return int(i), true
	}
	return 0, false
}

// AsBool extracts a Go bool from v.
func AsBool(v Value) (bool, bool) {
	if b, ok := v.(BoolValue); ok {
		return bool(b), true
	}
	return false, false
}

// AsArray extracts an array from v.
func AsArray(v Value) (ArrayValue, bool) {
	if a, ok := v.(ArrayValue); ok {
		return a, true
	}
	return nil, false
}

// AsStruct extracts a struct from v.
func AsStruct(v Value) (StructValue, bool) {
	if s, ok := v.(StructValue); ok {
		return s, true
	}
	return StructValue{}, false
}

func isIntegralFloat(f float64) bool {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return false
	}
	return f == math.Trunc(f)
}
