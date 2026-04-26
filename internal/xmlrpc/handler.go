// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package xmlrpc

import (
	"bytes"
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
}

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

	result, err := h.Mux.Dispatch(r.Context(), call.Method, call.Params)
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
	var fault *Fault
	if errors.As(err, &fault) {
		return fault
	}
	return &Fault{Code: -1, Message: err.Error()}
}
