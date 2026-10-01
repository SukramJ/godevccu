// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"context"

	"github.com/SukramJ/godevccu/internal/binrpc"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// remoteCaller is a registered callback receiver. Both transports the
// simulator can push over satisfy it, so every fan-out site stays
// transport-agnostic.
type remoteCaller interface {
	Call(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error)
	URL() string
}

// binrpcRemote pushes callbacks over BIN-RPC the way CUxD does: wrapped
// in a `system.multicall` envelope, always, even for a single event.
//
// The wrapping lives here rather than at the call sites so that a
// consumer talking to the simulated CUxD interface sees exactly the
// envelope real CUxD sends. A simulator that delivered bare calls would
// let a consumer that cannot parse the envelope pass every test.
type binrpcRemote struct{ client *binrpc.Client }

func (r binrpcRemote) Call(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error) {
	return r.client.CallMulticall(ctx, method, params)
}

func (r binrpcRemote) URL() string { return r.client.URL() }

// newRemote builds the callback client for url, picking the transport
// from its scheme: `xmlrpc_bin://` means BIN-RPC, anything else HTTP
// XML-RPC.
func newRemote(url string) remoteCaller {
	if binrpc.IsBINRPCURL(url) {
		return binrpcRemote{client: binrpc.NewClient(url)}
	}
	return xmlrpc.NewClient(url)
}

// newRemote builds the callback client for url and records every call
// it makes in the callback log; see callbacklog.go.
func (r *RPCFunctions) newRemote(url string) remoteCaller {
	return &loggedRemote{inner: newRemote(url), log: r.callbackLog, interfaceID: r.interfaceID}
}

// registration is one logic layer registered through init: the
// interface id it is called back with, and the client that reaches it.
type registration struct {
	interfaceID string
	client      remoteCaller
}

// remoteEntry is a registration together with the key it is stored
// under — the interface id by default, the url with init semantics.
type remoteEntry struct {
	key string
	registration
}

// snapshotRemotesLocked copies the registrations so callbacks can be
// made without the lock. The caller must hold the lock.
func (r *RPCFunctions) snapshotRemotesLocked() []remoteEntry {
	out := make([]remoteEntry, 0, len(r.remotes))
	for key, reg := range r.remotes {
		out = append(out, remoteEntry{key: key, registration: reg})
	}
	return out
}
