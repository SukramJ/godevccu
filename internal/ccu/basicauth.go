// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"net"
	"net/http"
)

// HTTP basic authentication on the remote API ports.
//
// A CCU with authentication enabled protects its XML-RPC ports with
// basic auth, and only those: the reverse proxy includes the auth
// configuration globally and the WebUI ports switch it back off
// (`auth.require = ()`), because the web API authenticates by session
// instead. Loopback callers are exempt — the configuration guards the
// whole auth block with a remote-address test — which is how add-ons
// running on the CCU itself reach the interfaces without credentials.
//
// The simulator leaves this off by default: every existing fixture
// talks to 127.0.0.1 and would be unaffected either way, but a fixture
// that binds a routable address would suddenly need credentials.

// basicAuthRealm is the realm a CCU announces. Clients display it on
// the credential prompt, so it is observable.
const basicAuthRealm = "theRealm"

// Authenticator reports whether a username/password pair is valid.
type Authenticator func(username, password string) bool

// EnableBasicAuth protects the XML-RPC surface with HTTP basic
// authentication for non-loopback callers. Passing nil disables it
// again.
func (s *Server) EnableBasicAuth(auth Authenticator) {
	s.mu.Lock()
	s.authenticator = auth
	s.mu.Unlock()
}

// basicAuthGate wraps next with the credential check.
func (s *Server) basicAuthGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		auth := s.authenticator
		s.mu.Unlock()

		if auth == nil || isLoopbackRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		username, password, ok := r.BasicAuth()
		if !ok || !auth(username, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+basicAuthRealm+`"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackRequest reports whether the caller is on the local machine,
// mirroring the remote-address test that guards the CCU's auth block.
func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
