// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package ccu

import (
	"context"
	"sync"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// Introspection logs.
//
// A test sees what the simulator answered, but not what the code under
// test wrote or how fast its callback server took a callback. The two
// logs below record exactly that — the idea and the bound of 10000
// entries are hm-simulator's (getWriteLog, getCallbackLog;
// https://github.com/hobbyquaker/hm-simulator, MIT, Sebastian Raff).
// Both are always on: recording costs nothing a client can observe.

// logCapacity bounds each log; the oldest entries are dropped first.
const logCapacity = 10000

// RejectedParameter is one parameter of a write that was not stored as
// sent: dropped, ignored, or stored but invalid.
type RejectedParameter struct {
	Name   string `json:"name"`
	Value  any    `json:"value"`
	Reason string `json:"reason"`
}

// WriteEntry is one write a client made through setValue or
// putParamset, over XML-RPC, BIN-RPC or JSON-RPC. Writes the simulator
// makes itself (device behaviour, SimulateDeviceEvent) are not
// recorded.
type WriteEntry struct {
	Time      time.Time      `json:"time"`
	Interface string         `json:"interface"`
	Method    string         `json:"method"`
	Address   string         `json:"address"`
	Paramset  string         `json:"paramset"`
	Values    map[string]any `json:"values"`
	// Rejected lists what a MASTER write model did not take as sent;
	// see mastermodel.go.
	Rejected []RejectedParameter `json:"rejected,omitempty"`
	// Error is the fault text the write was answered with, empty when
	// it was accepted.
	Error string `json:"error,omitempty"`
}

// CallbackEntry is one call the simulator made to a registered client.
type CallbackEntry struct {
	Interface string    `json:"interface"`
	URL       string    `json:"url"`
	Method    string    `json:"method"`
	SentAt    time.Time `json:"sentAt"`
	// AnsweredAt is zero while the client has not answered, and stays
	// zero when the call failed.
	AnsweredAt time.Time `json:"answeredAt"`
	// Error is the transport error or fault, empty on success.
	Error string `json:"error,omitempty"`
}

// boundedLog is a mutex-guarded slice that keeps the newest entries.
type boundedLog[T any] struct {
	mu      sync.Mutex
	entries []*T
}

func (l *boundedLog[T]) add(entry *T) {
	l.mu.Lock()
	l.entries = append(l.entries, entry)
	if over := len(l.entries) - logCapacity; over > 0 {
		l.entries = append(l.entries[:0:0], l.entries[over:]...)
	}
	l.mu.Unlock()
}

func (l *boundedLog[T]) update(fn func()) {
	l.mu.Lock()
	fn()
	l.mu.Unlock()
}

func (l *boundedLog[T]) snapshot() []T {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]T, len(l.entries))
	for i, e := range l.entries {
		out[i] = *e
	}
	return out
}

func (l *boundedLog[T]) clear() {
	l.mu.Lock()
	l.entries = nil
	l.mu.Unlock()
}

// loggedRemote records every call to a client in the callback log.
type loggedRemote struct {
	inner       remoteCaller
	log         *boundedLog[CallbackEntry]
	interfaceID string
}

func (l *loggedRemote) URL() string { return l.inner.URL() }

func (l *loggedRemote) Call(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error) {
	entry := &CallbackEntry{Interface: l.interfaceID, URL: l.inner.URL(), Method: method, SentAt: time.Now()}
	l.log.add(entry)
	result, err := l.inner.Call(ctx, method, params)
	l.log.update(func() {
		if err != nil {
			entry.Error = err.Error()
			return
		}
		entry.AnsweredAt = time.Now()
	})
	return result, err
}

// WriteLog returns every recorded client write, oldest first.
func (r *RPCFunctions) WriteLog() []WriteEntry { return r.writeLog.snapshot() }

// CallbackLog returns every recorded call to a client, oldest first.
func (r *RPCFunctions) CallbackLog() []CallbackEntry { return r.callbackLog.snapshot() }

// ClearLogs empties the write and the callback log.
func (r *RPCFunctions) ClearLogs() {
	r.writeLog.clear()
	r.callbackLog.clear()
}

// recordWrite appends a client write to the write log.
func (r *RPCFunctions) recordWrite(method, address, paramset string, values map[string]any, rejected []RejectedParameter, err error) {
	entry := &WriteEntry{
		Time:      time.Now(),
		Interface: r.interfaceID,
		Method:    method,
		Address:   address,
		Paramset:  paramset,
		Values:    cloneStringMap(values),
		Rejected:  rejected,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	r.writeLog.add(entry)
}

// ClientSetValue is [RPCFunctions.SetValue] as a client calls it: the
// write is recorded in the write log.
func (r *RPCFunctions) ClientSetValue(address, valueKey string, value any, force bool) error {
	rejected, err := r.setValue(address, valueKey, value, force)
	r.recordWrite("setValue", address, hmconst.ParamsetAttrValues, map[string]any{valueKey: value}, rejected, err)
	return err
}

// ClientPutParamset is [RPCFunctions.PutParamset] as a client calls it:
// the write is recorded in the write log.
func (r *RPCFunctions) ClientPutParamset(address, paramsetKey string, paramset map[string]any, force bool) error {
	rejected, err := r.putParamset(address, paramsetKey, paramset, force)
	r.recordWrite("putParamset", address, paramsetKey, paramset, rejected, err)
	return err
}
