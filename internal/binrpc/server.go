// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package binrpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// defaultIOTimeout bounds read and write on one connection.
const defaultIOTimeout = 15 * time.Second

// Dispatcher resolves a method call to a result. Returning an
// [xmlrpc.Fault] sends a BIN-RPC fault packet; any other error is
// wrapped into one with code -1.
type Dispatcher interface {
	Dispatch(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error)
}

// DispatcherFunc adapts a function to [Dispatcher].
type DispatcherFunc func(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error)

// Dispatch implements [Dispatcher].
func (f DispatcherFunc) Dispatch(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error) {
	return f(ctx, method, params)
}

// Server is a BIN-RPC TCP listener. Like CUxD, it serves one request per
// connection and closes afterwards.
type Server struct {
	dispatcher Dispatcher
	logger     *slog.Logger
	listener   net.Listener
	ioTimeout  time.Duration

	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    atomic.Bool
}

// NewServer binds addr and returns a server ready to [Serve].
func NewServer(addr string, d Dispatcher, logger *slog.Logger) (*Server, error) {
	if d == nil {
		return nil, fmt.Errorf("binrpc: NewServer: dispatcher is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("binrpc: listen %s: %w", addr, err)
	}
	return &Server{dispatcher: d, logger: logger, listener: ln, ioTimeout: defaultIOTimeout}, nil
}

// Addr returns the effective listener address, which resolves the port
// when addr was bound with port 0.
func (s *Server) Addr() net.Addr { return s.listener.Addr() }

// Serve accepts connections until ctx is cancelled or [Close] is called.
func (s *Server) Serve(ctx context.Context) error {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			s.closeListener()
		case <-stop:
		}
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.wg.Wait()
			if ctx.Err() != nil || s.closed.Load() {
				return nil
			}
			return fmt.Errorf("binrpc: accept: %w", err)
		}
		if s.closed.Load() {
			_ = conn.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

func (s *Server) closeListener() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		_ = s.listener.Close()
	})
}

// Close stops accepting and waits for in-flight handlers.
func (s *Server) Close() error {
	s.closeListener()
	s.wg.Wait()
	return nil
}

// handleConn reads one request, dispatches it, writes one response.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(s.ioTimeout))

	req, err := ReadRequest(io.LimitReader(conn, MaxMessageSize+8))
	if err != nil {
		s.logger.Debug("binrpc: decode request failed", "remote", conn.RemoteAddr().String(), "err", err)
		return
	}

	result, dispatchErr := s.dispatcher.Dispatch(ctx, req.Method, req.Params)

	var buf bytes.Buffer
	if dispatchErr != nil {
		fault, ok := dispatchErr.(*xmlrpc.Fault) //nolint:errorlint // dispatchers return the fault value directly
		if !ok {
			fault = &xmlrpc.Fault{Code: -1, Message: dispatchErr.Error()}
		}
		s.logger.Debug("binrpc: method returned fault", "method", req.Method, "code", fault.Code)
		if err := WriteFault(&buf, fault); err != nil {
			s.logger.Error("binrpc: encode fault failed", "method", req.Method, "err", err)
			return
		}
	} else if err := WriteResponse(&buf, result); err != nil {
		s.logger.Error("binrpc: encode response failed", "method", req.Method, "err", err)
		return
	}
	if _, err := conn.Write(buf.Bytes()); err != nil {
		s.logger.Debug("binrpc: write response failed", "remote", conn.RemoteAddr().String(), "err", err)
	}
}
