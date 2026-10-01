// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package ccu

import (
	"errors"
	"fmt"
)

// Interface processes that stop, restart or lose their clients.
//
// A client has to notice that rfd or hmipserver went away and came
// back: a port that refuses connections, open connections that are
// reset, and afterwards a process that either forgot every registered
// client (no events until the client calls init again, and nothing
// tells it) or remembered them and calls each back. The operations and
// their semantics are hm-simulator's stopInterface, startInterface,
// restartInterface and dropConnection
// (https://github.com/hobbyquaker/hm-simulator, MIT, Sebastian Raff).
//
// The device state, the paramsets and the injected faults survive; only
// the listeners and, on request, the registrations are lost.

// errNotRunning reports a restart operation on a server that was never
// started or has been stopped for good.
var errNotRunning = errors.New("ccu: server not running")

// Suspend stops the interface process: every listener of the server —
// XML-RPC, its TLS twin, BIN-RPC — is closed and every open connection
// reset, so the port refuses connections. Calls held by an injected
// hang or delay are let go without an answer. The registered clients
// stay known until [Server.Resume] decides what the process remembers.
// Suspending a suspended server does nothing.
func (s *Server) Suspend() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return errNotRunning
	}
	if s.suspended {
		s.mu.Unlock()
		return nil
	}
	s.suspended = true
	s.resumeAddr = s.listener.Addr().String()
	srv := s.httpSrv
	s.httpSrv = nil
	s.listener = nil
	tlsSrv := s.tls.srv
	s.tls.srv = nil
	s.tls.listener = nil
	s.mu.Unlock()

	s.releaseHeldCalls()
	var firstErr error
	if err := srv.Close(); err != nil {
		firstErr = err
	}
	if tlsSrv != nil {
		if err := tlsSrv.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if addr := s.BINRPCLocalAddr(); addr != nil {
		s.binrpc.mu.Lock()
		s.binrpc.resumeAddr = addr.String()
		s.binrpc.mu.Unlock()
		if err := s.StopBINRPC(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Resume starts a suspended interface process again on the same ports.
//
// With forgetClients the registrations are dropped, as by a process
// that keeps no list of them. Without, every remembered client is
// called back as after an init (see [RPCFunctions.ReannounceClients]).
// Resuming a server that is not suspended does nothing.
func (s *Server) Resume(forgetClients bool) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return errNotRunning
	}
	if !s.suspended {
		s.mu.Unlock()
		return nil
	}
	if err := s.listenLocked(s.resumeAddr); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("ccu: resume: %w", err)
	}
	if s.tls.addr != "" {
		if err := s.startTLSLocked(s.tls.addr, s.tls.certPEM, s.tls.keyPEM); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("ccu: resume tls: %w", err)
		}
	}
	s.suspended = false
	s.mu.Unlock()

	s.binrpc.mu.Lock()
	binAddr := s.binrpc.resumeAddr
	s.binrpc.resumeAddr = ""
	s.binrpc.mu.Unlock()
	if binAddr != "" {
		if err := s.StartBINRPC(binAddr); err != nil {
			return fmt.Errorf("ccu: resume: %w", err)
		}
	}

	if forgetClients {
		s.rpc.ForgetClients()
	} else {
		s.rpc.ReannounceClients()
	}
	return nil
}

// Suspended reports whether the interface process is stopped.
func (s *Server) Suspended() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suspended
}

// DropConnections is an interface process that restarts in an instant:
// every open connection is reset and every registered client forgotten.
func (s *Server) DropConnections() error {
	if err := s.Suspend(); err != nil {
		return err
	}
	return s.Resume(true)
}
