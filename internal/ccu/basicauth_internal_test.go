// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Internal tests for the basic-auth gate.
//
// These exercise the gate directly rather than through a listener,
// because every listener a test can bind is a loopback address — and
// loopback is exactly the case the gate lets through. Driving the
// handler with a synthetic RemoteAddr is the only way to cover the half
// that actually authenticates.

package ccu

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// gateFixture builds a server whose gate wraps a handler that records
// whether it was reached.
func gateFixture(t *testing.T, auth Authenticator) (http.Handler, *bool) {
	t.Helper()
	reached := false
	s := &Server{authenticator: auth}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	return s.basicAuthGate(inner), &reached
}

// credentials accepts exactly one pair.
func credentials(username, password string) Authenticator {
	return func(u, p string) bool { return u == username && p == password }
}

func TestBasicAuthRejectsRemoteCallerWithoutCredentials(t *testing.T) {
	gate, reached := gateFixture(t, credentials("Admin", "secret"))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "192.168.1.50:41234"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	// The challenge is what makes a client prompt for credentials.
	if got := rec.Header().Get("WWW-Authenticate"); got != `Basic realm="theRealm"` {
		t.Errorf("WWW-Authenticate = %q, want the CCU realm", got)
	}
	if *reached {
		t.Error("request reached the XML-RPC handler despite failing auth")
	}
}

func TestBasicAuthRejectsWrongCredentials(t *testing.T) {
	gate, reached := gateFixture(t, credentials("Admin", "secret"))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "192.168.1.50:41234"
	req.SetBasicAuth("Admin", "wrong")
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if *reached {
		t.Error("wrong password reached the handler")
	}
}

func TestBasicAuthAcceptsCorrectCredentials(t *testing.T) {
	gate, reached := gateFixture(t, credentials("Admin", "secret"))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "192.168.1.50:41234"
	req.SetBasicAuth("Admin", "secret")
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !*reached {
		t.Error("authenticated request did not reach the handler")
	}
}

// TestBasicAuthExemptsLoopback covers the remote-address guard the CCU
// wraps its whole auth block in: an add-on running on the central
// itself reaches the interfaces without credentials.
func TestBasicAuthExemptsLoopback(t *testing.T) {
	gate, reached := gateFixture(t, credentials("Admin", "secret"))

	for _, addr := range []string{"127.0.0.1:51000", "[::1]:51000"} {
		*reached = false
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("loopback %s got status %d, want 200", addr, rec.Code)
		}
		if !*reached {
			t.Errorf("loopback %s was challenged for credentials", addr)
		}
	}
}

// Without an authenticator the gate is transparent, which is the
// default the pydevccu contract requires.
func TestBasicAuthDisabledLetsEveryoneThrough(t *testing.T) {
	gate, reached := gateFixture(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "192.168.1.50:41234"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !*reached {
		t.Error("request was blocked with no authenticator configured")
	}
}

// A malformed RemoteAddr must not be mistaken for loopback, or an
// unparseable address would bypass authentication.
func TestUnparseableRemoteAddrIsNotLoopback(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "not-an-address"
	if isLoopbackRequest(req) {
		t.Fatal("unparseable RemoteAddr treated as loopback")
	}
}
