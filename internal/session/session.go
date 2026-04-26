// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package session provides token-based authentication compatible with
// the CCU/OpenCCU JSON-RPC API.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// DefaultTimeout is the inactivity timeout after which a session
// expires (30 minutes), matching pydevccu's SESSION_TIMEOUT.
const DefaultTimeout = 30 * time.Minute

// idLength is the number of bytes used to mint a session token.
// pydevccu uses SESSION_ID_LENGTH=32 bytes of hex string (so 16 raw
// bytes); we mirror that.
const idLength = 16

// Session is the per-token state.
type Session struct {
	ID          string
	Username    string
	CreatedAt   time.Time
	LastAccess  time.Time
	Permissions map[string]struct{}
}

// IsExpired reports whether the session is older than timeout.
func (s *Session) IsExpired(timeout time.Duration) bool {
	return time.Since(s.LastAccess) > timeout
}

// Touch updates the last-access timestamp.
func (s *Session) Touch() { s.LastAccess = time.Now() }

// Age returns the time since the session was created.
func (s *Session) Age() time.Duration { return time.Since(s.CreatedAt) }

// Manager owns the session table.
type Manager struct {
	mu       sync.RWMutex
	username string
	password string
	timeout  time.Duration
	enabled  bool
	sessions map[string]*Session
}

// New constructs a Manager.
func New(username, password string, timeout time.Duration, authEnabled bool) *Manager {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Manager{
		username: username,
		password: password,
		timeout:  timeout,
		enabled:  authEnabled,
		sessions: make(map[string]*Session),
	}
}

// AuthEnabled reports whether authentication is required.
func (m *Manager) AuthEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// SetAuthEnabled toggles authentication.
func (m *Manager) SetAuthEnabled(v bool) {
	m.mu.Lock()
	m.enabled = v
	m.mu.Unlock()
}

// Username returns the configured admin username.
func (m *Manager) Username() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.username
}

// Login validates credentials and returns a fresh session id, or empty
// string when authentication failed.
func (m *Manager) Login(username, password string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if username != m.username || password != m.password {
		return ""
	}
	id := mintID()
	now := time.Now()
	m.sessions[id] = &Session{
		ID:          id,
		Username:    username,
		CreatedAt:   now,
		LastAccess:  now,
		Permissions: make(map[string]struct{}),
	}
	return id
}

// Logout invalidates the named session. Returns true when the session
// existed.
func (m *Manager) Logout(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return false
	}
	delete(m.sessions, id)
	return true
}

// Renew swaps the old session for a new one, preserving permissions.
// OpenCCU returns a new session id on renewal; we mirror that.
func (m *Manager) Renew(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.IsExpired(m.timeout) {
		if ok {
			delete(m.sessions, id)
		}
		return ""
	}
	newID := mintID()
	now := time.Now()
	perms := make(map[string]struct{}, len(s.Permissions))
	for p := range s.Permissions {
		perms[p] = struct{}{}
	}
	m.sessions[newID] = &Session{
		ID:          newID,
		Username:    s.Username,
		CreatedAt:   now,
		LastAccess:  now,
		Permissions: perms,
	}
	delete(m.sessions, id)
	return newID
}

// Validate checks whether id refers to a live session. When auth is
// disabled the function always returns true.
func (m *Manager) Validate(id string) bool {
	m.mu.RLock()
	if !m.enabled {
		m.mu.RUnlock()
		return true
	}
	m.mu.RUnlock()
	if id == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return false
	}
	if s.IsExpired(m.timeout) {
		delete(m.sessions, id)
		return false
	}
	s.Touch()
	return true
}

// Get returns the session record (without touching it).
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok || s.IsExpired(m.timeout) {
		return nil, false
	}
	c := *s
	c.Permissions = make(map[string]struct{}, len(s.Permissions))
	for p := range s.Permissions {
		c.Permissions[p] = struct{}{}
	}
	return &c, true
}

// CleanupExpired drops every expired session. Returns the count.
func (m *Manager) CleanupExpired() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, s := range m.sessions {
		if s.IsExpired(m.timeout) {
			delete(m.sessions, id)
			n++
		}
	}
	return n
}

// ActiveCount returns the number of non-expired sessions.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, s := range m.sessions {
		if !s.IsExpired(m.timeout) {
			n++
		}
	}
	return n
}

// InvalidateAll clears the session table.
func (m *Manager) InvalidateAll() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.sessions)
	m.sessions = make(map[string]*Session)
	return n
}

func mintID() string {
	b := make([]byte, idLength)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand is infallible on supported targets; retain a
		// deterministic fallback so we never return an empty token.
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}
