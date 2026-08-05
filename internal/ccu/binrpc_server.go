// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/SukramJ/godevccu/internal/binrpc"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// binrpcState holds the optional BIN-RPC listener. It is separate from
// the HTTP server's fields so a simulator run without a CUxD interface
// carries no extra lifecycle.
type binrpcState struct {
	mu     sync.Mutex
	srv    *binrpc.Server
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StartBINRPC binds a BIN-RPC listener on addr and serves the same
// method set as the XML-RPC surface — CUxD answers `listDevices`,
// `getValue`, `setValue`, `ping`, `init` and the rest over BIN-RPC, so
// sharing the mux is what makes the simulated interface behave like the
// real one instead of a subset of it.
//
// Callbacks in the other direction are wrapped in `system.multicall`;
// that happens in [binrpcRemote], keyed off the registered URL's scheme.
func (s *Server) StartBINRPC(addr string) error {
	s.binrpc.mu.Lock()
	defer s.binrpc.mu.Unlock()
	if s.binrpc.srv != nil {
		return fmt.Errorf("ccu: BIN-RPC listener already started")
	}
	srv, err := binrpc.NewServer(addr, binrpc.DispatcherFunc(
		func(ctx context.Context, method string, params []xmlrpc.Value) (xmlrpc.Value, error) {
			return s.mux.Dispatch(ctx, method, params)
		}), s.logger)
	if err != nil {
		return fmt.Errorf("ccu: bin-rpc start: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.binrpc.srv = srv
	s.binrpc.cancel = cancel
	s.binrpc.wg.Add(1)
	go func() {
		defer s.binrpc.wg.Done()
		if serveErr := srv.Serve(ctx); serveErr != nil {
			s.logger.Error("ccu: bin-rpc serve stopped", "err", serveErr)
		}
	}()
	return nil
}

// BINRPCLocalAddr returns the bound BIN-RPC address, or nil when no
// listener is running. Callers resolve an ephemeral port through it.
func (s *Server) BINRPCLocalAddr() net.Addr {
	s.binrpc.mu.Lock()
	defer s.binrpc.mu.Unlock()
	if s.binrpc.srv == nil {
		return nil
	}
	return s.binrpc.srv.Addr()
}

// StopBINRPC shuts the listener down and waits for in-flight handlers.
// Safe to call when none was started.
func (s *Server) StopBINRPC() error {
	s.binrpc.mu.Lock()
	srv := s.binrpc.srv
	cancel := s.binrpc.cancel
	s.binrpc.srv = nil
	s.binrpc.cancel = nil
	s.binrpc.mu.Unlock()

	if srv == nil {
		return nil
	}
	cancel()
	err := srv.Close()
	s.binrpc.wg.Wait()
	return err
}
