// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// MethodHandler processes one XML-RPC method invocation.
type MethodHandler func(ctx context.Context, params []Value) (Value, error)

// Mux routes XML-RPC method names to handlers. Safe for concurrent use.
type Mux struct {
	mu       sync.RWMutex
	methods  map[string]MethodHandler
	fallback MethodHandler
}

// NewMux returns an empty Mux.
func NewMux() *Mux { return &Mux{methods: make(map[string]MethodHandler)} }

// Handle registers fn for method, replacing any prior registration.
func (m *Mux) Handle(method string, fn MethodHandler) {
	if method == "" {
		panic("xmlrpc: Mux.Handle called with empty method name")
	}
	if fn == nil {
		panic("xmlrpc: Mux.Handle called with nil handler")
	}
	m.mu.Lock()
	m.methods[method] = fn
	m.mu.Unlock()
}

// HandleFallback registers a catch-all handler.
func (m *Mux) HandleFallback(fn MethodHandler) {
	m.mu.Lock()
	m.fallback = fn
	m.mu.Unlock()
}

// Methods returns the registered method names sorted ascending.
func (m *Mux) Methods() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.methods))
	for n := range m.methods {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Has reports whether method is registered.
func (m *Mux) Has(method string) bool {
	m.mu.RLock()
	_, ok := m.methods[method]
	m.mu.RUnlock()
	return ok
}

// Dispatch invokes the handler registered for method.
func (m *Mux) Dispatch(ctx context.Context, method string, params []Value) (Value, error) {
	m.mu.RLock()
	fn, ok := m.methods[method]
	fallback := m.fallback
	m.mu.RUnlock()

	if !ok {
		if fallback != nil {
			return fallback(ctx, params)
		}
		return nil, &Fault{Code: -32601, Message: "method not found: " + method}
	}
	return fn(ctx, params)
}

// RegisterSystemMethods wires system.listMethods, system.methodHelp and
// system.multicall, matching what HomeMatic clients expect.
func (m *Mux) RegisterSystemMethods() {
	m.Handle("system.listMethods", func(_ context.Context, _ []Value) (Value, error) {
		names := m.Methods()
		out := make(ArrayValue, len(names))
		for i, n := range names {
			out[i] = StringValue(n)
		}
		return out, nil
	})

	m.Handle("system.methodHelp", func(_ context.Context, _ []Value) (Value, error) {
		return StringValue(""), nil
	})

	m.Handle("system.multicall", func(ctx context.Context, params []Value) (Value, error) {
		if len(params) != 1 {
			return nil, fmt.Errorf("system.multicall: expected 1 param, got %d", len(params))
		}
		calls, ok := params[0].(ArrayValue)
		if !ok {
			return nil, fmt.Errorf("system.multicall: want array, got %s", params[0].Kind())
		}
		results := make(ArrayValue, 0, len(calls))
		for i, c := range calls {
			s, ok := c.(StructValue)
			if !ok {
				return nil, fmt.Errorf("system.multicall call %d: want struct, got %s", i, c.Kind())
			}
			nameVal, ok := s.Get("methodName")
			if !ok {
				return nil, fmt.Errorf("system.multicall call %d: missing methodName", i)
			}
			name, ok := nameVal.(StringValue)
			if !ok {
				return nil, fmt.Errorf("system.multicall call %d: methodName must be string", i)
			}
			innerParamsVal, ok := s.Get("params")
			if !ok {
				return nil, fmt.Errorf("system.multicall call %d: missing params", i)
			}
			innerParams, ok := innerParamsVal.(ArrayValue)
			if !ok {
				return nil, fmt.Errorf("system.multicall call %d: params must be array", i)
			}
			sub, err := m.Dispatch(ctx, string(name), innerParams)
			if err != nil {
				var fault *Fault
				if !errors.As(err, &fault) {
					fault = &Fault{Code: -1, Message: err.Error()}
				}
				results = append(results, StructValue{Members: []Member{
					{Name: "faultCode", Value: IntValue(int32(fault.Code))}, //nolint:gosec
					{Name: "faultString", Value: StringValue(fault.Message)},
				}})
				continue
			}
			if sub == nil {
				sub = NilValue{}
			}
			results = append(results, ArrayValue{sub})
		}
		return results, nil
	})
}
