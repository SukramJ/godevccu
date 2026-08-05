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
