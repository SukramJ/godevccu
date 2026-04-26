// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultClientTimeout applies when the caller's context has no
// deadline.
const DefaultClientTimeout = 10 * time.Second

// Client is a tiny XML-RPC client used by the simulator to push events
// back to registered callback endpoints (newDevices, deleteDevices,
// event, …). It is not exported through the public API.
type Client struct {
	url    string
	client *http.Client
	mu     sync.Mutex // serialises requests per [LockingServerProxy] semantics
}

// NewClient returns a client targeting url.
func NewClient(url string) *Client {
	return &Client{
		url:    url,
		client: &http.Client{Timeout: DefaultClientTimeout},
	}
}

// URL returns the configured endpoint.
func (c *Client) URL() string { return c.url }

// Call invokes method with params and returns the decoded value. A CCU
// fault is returned wrapped as [*Fault].
func (c *Client) Call(ctx context.Context, method string, params []Value) (Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var body bytes.Buffer
	if err := EncodeCall(&body, &MethodCall{Method: method, Params: params}); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=UTF-8")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, DefaultRequestLimit))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	mr, err := DecodeResponse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if mr.Fault != nil {
		return nil, mr.Fault
	}
	if len(mr.Params) == 0 {
		return NilValue{}, nil
	}
	return mr.Params[0], nil
}

// IsTransport returns true when err looks like a recoverable network
// problem rather than a CCU-level fault.
func IsTransport(err error) bool {
	var f *Fault
	return !errors.As(err, &f)
}
