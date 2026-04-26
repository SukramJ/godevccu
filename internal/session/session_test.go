// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package session_test

import (
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/session"
)

func TestLoginLogout(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)

	if id := m.Login("Admin", "wrong"); id != "" {
		t.Fatalf("login with wrong password returned %q", id)
	}

	id := m.Login("Admin", "secret")
	if id == "" {
		t.Fatal("login with correct credentials failed")
	}
	if !m.Validate(id) {
		t.Fatal("session not valid after login")
	}
	if !m.Logout(id) {
		t.Fatal("logout returned false")
	}
	if m.Validate(id) {
		t.Fatal("session still valid after logout")
	}
}

func TestRenewIssuesNewID(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, true)
	id := m.Login("Admin", "secret")
	new := m.Renew(id)
	if new == "" || new == id {
		t.Fatalf("renew returned %q (old %q)", new, id)
	}
	if m.Validate(id) {
		t.Fatal("old id should be invalidated")
	}
	if !m.Validate(new) {
		t.Fatal("new id should be valid")
	}
}

func TestExpiry(t *testing.T) {
	m := session.New("Admin", "secret", time.Millisecond, true)
	id := m.Login("Admin", "secret")
	time.Sleep(5 * time.Millisecond)
	if m.Validate(id) {
		t.Fatal("session should be expired")
	}
}

func TestAuthDisabled(t *testing.T) {
	m := session.New("Admin", "secret", time.Minute, false)
	if !m.Validate("") {
		t.Fatal("validate should pass when auth is disabled")
	}
}

func TestCleanupExpired(t *testing.T) {
	m := session.New("Admin", "secret", time.Millisecond, true)
	m.Login("Admin", "secret")
	m.Login("Admin", "secret")
	time.Sleep(5 * time.Millisecond)
	if got := m.CleanupExpired(); got != 2 {
		t.Fatalf("CleanupExpired = %d, want 2", got)
	}
}
