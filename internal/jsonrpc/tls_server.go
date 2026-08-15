// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package jsonrpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// tlsState holds the optional HTTPS twin of the web API listener.
type tlsState struct {
	srv      *http.Server
	listener net.Listener
}

// routes builds the HTTP surface. Both the plaintext and the TLS
// listener serve it, the way a CCU answers the same web API on 80 and
// 443.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/homematic.cgi", s.handleJSONRPC)
	mux.HandleFunc("/config/cp_security.cgi", s.handleBackupDownload)
	mux.HandleFunc("/config/cp_maintenance.cgi", s.handleMaintenance)
	mux.HandleFunc("/VERSION", s.handleVersion)
	mux.HandleFunc("/ise/checkrega.cgi", s.handleCheckRega)
	s.registerExtraRoutes(mux)
	return mux
}

// SetHTTPSRedirect makes the plaintext listener answer 302 towards
// https://<host>:<port> for every request. Pass 0 to disable. A CCU
// configured to enforce HTTPS behaves this way, and its
// CCU.getHttpsRedirectEnabled reports true.
func (s *Server) SetHTTPSRedirect(port int) {
	s.httpsRedirectPort.Store(int64(port))
	if s.handlers != nil {
		s.handlers.SetHTTPSRedirect(port > 0)
	}
}

// redirectGate redirects plaintext requests to the HTTPS twin when the
// redirect is armed. /VERSION and the readiness probe stay reachable so
// a client can still identify a booting CCU.
func (s *Server) redirectGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port := int(s.httpsRedirectPort.Load())
		if port == 0 || r.TLS != nil {
			next.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case "/VERSION", "/ise/checkrega.cgi":
			next.ServeHTTP(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		target := "https://" + net.JoinHostPort(host, strconv.Itoa(port)) + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusFound)
	})
}

// StartTLS binds the HTTPS listener serving the same routes as the
// plaintext one.
func (s *Server) StartTLS(addr string, certPEM, keyPEM []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tls.srv != nil {
		return errors.New("jsonrpc: TLS listener already started")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("jsonrpc: tls keypair: %w", err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("jsonrpc: tls listen: %w", err)
	}
	srv := &http.Server{
		Handler:           s.routes(),
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
			s.logger.Error("jsonrpc: tls serve failed", "err", serveErr)
		}
	}()
	return nil
}

// TLSLocalAddr returns the bound HTTPS address, or nil when no TLS
// listener runs.
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
