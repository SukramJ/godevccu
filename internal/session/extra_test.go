// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package session_test

import (
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/session"
)

func TestAge(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	id := m.Login("Admin", "secret")
	s, ok := m.Get(id)
	if !ok {
		t.Fatal("Get failed")
	}
	if s.Age() < 0 {
		t.Fatal("Age() must be non-negative")
	}
}

func TestAuthEnabled(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	if !m.AuthEnabled() {
		t.Fatal("expected auth enabled")
	}
	m.SetAuthEnabled(false)
	if m.AuthEnabled() {
		t.Fatal("expected auth disabled after SetAuthEnabled(false)")
	}
}

func TestUsername(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	if got := m.Username(); got != "Admin" {
		t.Fatalf("Username() = %q, want Admin", got)
	}
}

func TestGet(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	id := m.Login("Admin", "secret")
	s, ok := m.Get(id)
	if !ok {
		t.Fatal("Get returned false for live session")
	}
	if s.Username != "Admin" {
		t.Fatalf("Get returned username %q, want Admin", s.Username)
	}
	// Missing token
	_, ok = m.Get("nonexistent-token")
	if ok {
		t.Fatal("Get returned true for nonexistent session")
	}
}

func TestGetExpiredSession(t *testing.T) {
	m := session.New("Admin", "secret", time.Millisecond, true)
	id := m.Login("Admin", "secret")
	time.Sleep(5 * time.Millisecond)
	_, ok := m.Get(id)
	if ok {
		t.Fatal("Get should return false for expired session")
	}
}

func TestActiveCount(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	m.Login("Admin", "secret")
	m.Login("Admin", "secret")
	if got := m.ActiveCount(); got != 2 {
		t.Fatalf("ActiveCount = %d, want 2", got)
	}
}

func TestActiveCountWithExpired(t *testing.T) {
	m := session.New("Admin", "secret", time.Millisecond, true)
	m.Login("Admin", "secret")
	time.Sleep(5 * time.Millisecond)
	if got := m.ActiveCount(); got != 0 {
		t.Fatalf("ActiveCount with expired = %d, want 0", got)
	}
}

func TestInvalidateAll(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	m.Login("Admin", "secret")
	m.Login("Admin", "secret")
	n := m.InvalidateAll()
	if n != 2 {
		t.Fatalf("InvalidateAll returned %d, want 2", n)
	}
	if got := m.ActiveCount(); got != 0 {
		t.Fatalf("ActiveCount after InvalidateAll = %d, want 0", got)
	}
}

func TestRenewExpiredSession(t *testing.T) {
	m := session.New("Admin", "secret", time.Millisecond, true)
	id := m.Login("Admin", "secret")
	time.Sleep(5 * time.Millisecond)
	newID := m.Renew(id)
	if newID != "" {
		t.Fatalf("Renew of expired session returned %q, want empty", newID)
	}
}

func TestRenewMissingSession(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	if got := m.Renew("no-such-session"); got != "" {
		t.Fatalf("Renew of missing session returned %q, want empty", got)
	}
}

func TestValidateEmptyID(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	if m.Validate("") {
		t.Fatal("Validate with empty id should return false when auth enabled")
	}
}

func TestLogoutMissing(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	if m.Logout("nonexistent") {
		t.Fatal("Logout of nonexistent session should return false")
	}
}

func TestNewWithZeroTimeout(t *testing.T) {
	// When timeout=0 the constructor must substitute DefaultTimeout.
	m := session.New("Admin", "secret", 0, true)
	id := m.Login("Admin", "secret")
	if !m.Validate(id) {
		t.Fatal("session invalid immediately after login with default timeout")
	}
}

func TestValidateExpiredRemovesSession(t *testing.T) {
	m := session.New("Admin", "secret", time.Millisecond, true)
	id := m.Login("Admin", "secret")
	time.Sleep(5 * time.Millisecond)
	if m.Validate(id) {
		t.Fatal("expired session should not validate")
	}
	// After failed validate, the count should be 0.
	if got := m.ActiveCount(); got != 0 {
		t.Fatalf("ActiveCount after expired validate = %d, want 0", got)
	}
}
