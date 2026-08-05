// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package binrpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// URLScheme is the prefix a BIN-RPC callback address carries. Every
// CCU-side component writes its BIN-RPC handler entries this way.
const URLScheme = "xmlrpc_bin://"

// IsBINRPCURL reports whether url names a BIN-RPC callback endpoint.
func IsBINRPCURL(url string) bool {
	return strings.HasPrefix(url, URLScheme)
}

// AddrFromURL strips the scheme from a BIN-RPC callback URL, yielding
// the dialable `host:port`.
func AddrFromURL(url string) string {
	return strings.TrimPrefix(url, URLScheme)
}

// Client calls a BIN-RPC endpoint. Each call opens a fresh TCP
// connection, because a BIN-RPC peer closes after one request/response
// cycle.
type Client struct {
	addr    string
	url     string
	timeout time.Duration
}

// NewClient builds a client for a `xmlrpc_bin://host:port` URL. A bare
// `host:port` is accepted too.
func NewClient(url string) *Client {
	return &Client{addr: AddrFromURL(url), url: url, timeout: defaultIOTimeout}
}

// URL returns the callback URL the client was built from, so callers can
// match a registration against the address the peer announced.
func (c *Client) URL() string { return c.url }

// Call sends one request and returns the decoded result.
func (c *Client) Call(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error) {
	dialer := &net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, fmt.Errorf("binrpc: dial %s: %w", c.addr, err)
	}
	defer func() { _ = conn.Close() }()

	deadline := time.Now().Add(c.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	var buf bytes.Buffer
	if encErr := WriteRequest(&buf, method, params); encErr != nil {
		return nil, encErr
	}
	if _, writeErr := conn.Write(buf.Bytes()); writeErr != nil {
		return nil, fmt.Errorf("binrpc: write request: %w", writeErr)
	}
	resp, err := ReadResponse(io.LimitReader(conn, MaxMessageSize+8))
	if err != nil {
		return nil, err
	}
	if resp.Fault != nil {
		return nil, resp.Fault
	}
	return resp.Value, nil
}

// CallMulticall sends one call wrapped in a `system.multicall` envelope,
// which is how CUxD delivers every callback — a single value change
// included, never as a bare `event`.
//
// Modelling this is the point of the BIN-RPC support in the simulator. A
// consumer that only ever sees bare calls will parse the interface id out
// of params[0], which holds a string for a bare call and the sub-call
// array for an envelope; such a consumer drops every real CUxD event
// while passing every test written against a bare-call simulator.
func (c *Client) CallMulticall(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error) {
	envelope := xmlrpc.ArrayValue{
		xmlrpc.StructValue{Members: []xmlrpc.Member{
			{Name: "methodName", Value: xmlrpc.StringValue(method)},
			{Name: "params", Value: xmlrpc.ArrayValue(params)},
		}},
	}
	return c.Call(ctx, "system.multicall", []xmlrpc.Value{envelope})
}
