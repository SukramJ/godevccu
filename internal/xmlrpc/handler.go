// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// DefaultRequestLimit bounds the body of an incoming XML-RPC request.
const DefaultRequestLimit = 10 * 1024 * 1024

// Handler is the http.Handler exposing a [Mux] over HTTP.
type Handler struct {
	Mux    *Mux
	Logger *slog.Logger

	// RequestLimit bounds the body in bytes. Zero means
	// [DefaultRequestLimit].
	RequestLimit int64

	// Intercept, when set, runs before every dispatch. A returned
	// [ErrDropConnection] closes the connection without an answer; any
	// other error is answered as a fault instead of dispatching.
	Intercept func(ctx context.Context, method string) error
}

// ErrDropConnection asks a transport to close the connection instead of
// answering — the simulated process vanished mid-call.
var ErrDropConnection = errors.New("xmlrpc: drop connection")

// NewHandler builds a Handler with a fresh [Mux].
func NewHandler() *Handler { return &Handler{Mux: NewMux()} }

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := h.Logger
	if logger == nil {
		logger = slog.Default()
	}

	limit := h.RequestLimit
	if limit <= 0 {
		limit = DefaultRequestLimit
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		logger.Debug("xmlrpc: read request failed", "remote", r.RemoteAddr, "err", err)
		http.Error(w, "request too large or unreadable", http.StatusBadRequest)
		return
	}

	call, err := DecodeCall(bytes.NewReader(raw))
	if err != nil {
		logger.Debug("xmlrpc: decode request failed", "remote", r.RemoteAddr, "err", err)
		http.Error(w, "decode request failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	logger.Debug("xmlrpc: dispatch", "method", call.Method, "params", len(call.Params))

	var result Value
	if h.Intercept != nil {
		err = h.Intercept(r.Context(), call.Method)
		if errors.Is(err, ErrDropConnection) {
			logger.Debug("xmlrpc: connection dropped", "method", call.Method)
			dropConnection(w)
			return
		}
	}
	if err == nil {
		result, err = h.Mux.Dispatch(r.Context(), call.Method, call.Params)
	}
	resp := &MethodResponse{}
	if err != nil {
		resp.Fault = asFault(err)
		logger.Debug("xmlrpc: fault", "method", call.Method, "code", resp.Fault.Code, "msg", resp.Fault.Message)
	} else {
		if result == nil {
			result = NilValue{}
		}
		resp.Params = []Value{result}
	}

	var body bytes.Buffer
	if err := EncodeResponse(&body, resp); err != nil {
		logger.Error("xmlrpc: encode response failed", "method", call.Method, "err", err)
		http.Error(w, "encode response failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/xml; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body.Bytes()); err != nil {
		logger.Debug("xmlrpc: write response failed", "remote", r.RemoteAddr, "err", err)
	}
}

func asFault(err error) *Fault {
	if fault, ok := errors.AsType[*Fault](err); ok {
		return fault
	}
	return &Fault{Code: -1, Message: err.Error()}
}

// dropConnection closes the client connection without writing a
// response.
func dropConnection(w http.ResponseWriter) {
	if hj, ok := w.(http.Hijacker); ok {
		if conn, _, err := hj.Hijack(); err == nil {
			_ = conn.Close()
			return
		}
	}
	// No hijacking (HTTP/2): abort the handler, which makes the server
	// reset the stream without an answer.
	panic(http.ErrAbortHandler)
}
