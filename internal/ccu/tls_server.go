// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// tlsState holds the optional HTTPS twin of the XML-RPC listener.
type tlsState struct {
	srv      *http.Server
	listener net.Listener
}

// StartTLS binds an HTTPS listener on addr serving the same XML-RPC
// surface as the plaintext port. A real CCU exposes both at once
// (2001 and 42001, 2010 and 42010), and a client picks per connection.
//
// The certificate is supplied by the caller; the simulator does not
// care whether it is self-signed, and neither does a client talking to
// a CCU's factory certificate.
func (s *Server) StartTLS(addr string, certPEM, keyPEM []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tls.srv != nil {
		return errors.New("ccu: TLS listener already started")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("ccu: tls keypair: %w", err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ccu: tls listen: %w", err)
	}
	srv := &http.Server{
		Handler:           s.readyGate(s.basicAuthGate(s.handler)),
		ReadHeaderTimeout: 30 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
	}
	s.tls.srv = srv
	s.tls.listener = ln
	go func() {
		if serveErr := srv.ServeTLS(ln, "", ""); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			s.logger.Error("ccu: tls serve failed", "err", serveErr)
		}
	}()
	return nil
}

// TLSLocalAddr returns the bound HTTPS address, or nil when no TLS
// listener is running.
func (s *Server) TLSLocalAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tls.listener == nil {
		return nil
	}
	return s.tls.listener.Addr()
}

// StopTLS shuts the HTTPS listener down. Safe to call when none runs.
func (s *Server) StopTLS() error {
	s.mu.Lock()
	srv := s.tls.srv
	s.tls.srv = nil
	s.tls.listener = nil
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
