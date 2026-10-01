// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package litefake_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/pkg/litefake"
)

// TestFakeAuthStateReportsStoredScopes pins /api/auth/v1/state: open,
// scopes exactly as stored (rpc:operate is not expanded to rpc:read),
// and only the two base fields without a valid credential.
func TestFakeAuthStateReportsStoredScopes(t *testing.T) {
	f := startFake(t, litefake.Options{Tokens: map[string][]string{operateToken: {"rpc:operate"}}})

	resp, body := get(t, f, "/api/auth/v1/state", operateToken)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auth state: %d", resp.StatusCode)
	}
	st := decode[struct {
		SetupRequired bool     `json:"setup_required"`
		Authenticated bool     `json:"authenticated"`
		User          string   `json:"user"`
		Scopes        []string `json:"scopes"`
	}](t, string(body))
	if !st.Authenticated || st.User != "token:"+litefake.TokenName(operateToken) {
		t.Errorf("state %s", body)
	}
	if strings.Join(st.Scopes, ",") != "rpc:operate" {
		t.Errorf("scopes %v, want the stored [rpc:operate] unexpanded", st.Scopes)
	}

	_, body = get(t, f, "/api/auth/v1/state", "")
	if string(body) != `{"setup_required":false,"authenticated":false}` {
		t.Errorf("anonymous state %s", body)
	}
}

// TestFakeEnforcesRouteScopeOnLiteRPC pins the route check: 401 without
// a credential, 403 naming rpc:read for a token without it, 400 for a
// query-string credential; the implied rpc:read of rpc:operate passes.
func TestFakeEnforcesRouteScopeOnLiteRPC(t *testing.T) {
	f := startFake(t, litefake.Options{Tokens: map[string][]string{
		metaToken:    {"meta:read"},
		operateToken: {"rpc:operate"},
	}})

	resp, body := get(t, f, "/api/rpc/v1/interfaces", "")
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, body) != "unauthenticated" {
		t.Errorf("no credential: %d %s", resp.StatusCode, body)
	}
	resp, body = get(t, f, "/api/rpc/v1/interfaces", metaToken)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"scope":"rpc:read"`) {
		t.Errorf("meta token: %d %s", resp.StatusCode, body)
	}
	resp, body = get(t, f, "/api/rpc/v1/events?sid=abcdefghij", "")
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, body) != "bad-request" {
		t.Errorf("query credential: %d %s", resp.StatusCode, body)
	}
	if resp, body := get(t, f, "/api/rpc/v1/interfaces", operateToken); resp.StatusCode != http.StatusOK {
		t.Errorf("rpc:operate implies rpc:read: %d %s", resp.StatusCode, body)
	}
}

// TestFakeLoginSessionAndLogout pins the account path: login answers a
// 26-character session id with the account's identity, the session
// passes as a bearer credential with the account's scopes, auth state
// reports the account fields, and logout ends the session.
func TestFakeLoginSessionAndLogout(t *testing.T) {
	f := startFake(t, litefake.Options{Accounts: []litefake.Account{{
		Username: "admin", Password: "secret", Role: "admin", Level: "administer",
		AccountID: "1", Scopes: []string{"rpc:read"},
	}}})
	if resp, raw := send(t, f, http.MethodPost, "/api/auth/v1/login", "", `{"username":"admin","password":"wrong"}`); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password: %d %s", resp.StatusCode, raw)
	}
	resp, raw := send(t, f, http.MethodPost, "/api/auth/v1/login", "", `{"username":"admin","password":"secret"}`)
	login := decode[struct {
		SID   string `json:"sid"`
		User  string `json:"user"`
		Level string `json:"level"`
	}](t, string(raw))
	if resp.StatusCode != http.StatusOK || login.User != "admin" || login.Level != "administer" {
		t.Fatalf("login: %d %s", resp.StatusCode, raw)
	}
	if !isSessionIDShape(login.SID) {
		t.Errorf("session id %q is not 26 characters of A-Z2-7", login.SID)
	}
	if resp, _ := get(t, f, "/api/rpc/v1/interfaces", login.SID); resp.StatusCode != http.StatusOK {
		t.Errorf("session on lite-rpc: %d", resp.StatusCode)
	}
	if resp, _ := get(t, f, "/api/meta/v1/snapshot", login.SID); resp.StatusCode != http.StatusForbidden {
		t.Errorf("session beyond its scopes: %d", resp.StatusCode)
	}
	_, raw = get(t, f, "/api/auth/v1/state", login.SID)
	for _, want := range []string{`"role":"admin"`, `"level":"administer"`, `"account_id":"1"`, `"sid":"` + login.SID + `"`, `"scopes":["rpc:read"]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("session state lacks %s: %s", want, raw)
		}
	}
	if resp, _ := send(t, f, http.MethodPost, "/api/auth/v1/logout", login.SID, `{}`); resp.StatusCode != http.StatusOK {
		t.Errorf("logout: %d", resp.StatusCode)
	}
	if resp, _ := get(t, f, "/api/rpc/v1/interfaces", login.SID); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("session after logout: %d", resp.StatusCode)
	}
}

// TestFakeAuthOffStateAnswersEveryCaller pins auth mode off on the state
// route: every caller — no credential, an unknown 26-character bearer, a
// valid token, a valid session — gets the same fixed object with
// auth_off true, authenticated true and the administrator role and
// level. The identifier strings are litefake placeholders and are only
// compared against the configured placeholder, never a box value.
// Switching the knob off restores the previous answers, which never
// carry an auth_off member.
func TestFakeAuthOffStateAnswersEveryCaller(t *testing.T) {
	f := startFake(t, litefake.Options{
		Tokens: map[string][]string{operateToken: {"rpc:operate"}},
		Accounts: []litefake.Account{{
			Username: "admin", Password: "secret", Role: "admin", Level: "administer",
			AccountID: "1", Scopes: []string{"rpc:read"},
		}},
	})
	resp, raw := send(t, f, http.MethodPost, "/api/auth/v1/login", "", `{"username":"admin","password":"secret"}`)
	login := decode[struct {
		SID string `json:"sid"`
	}](t, string(raw))
	if resp.StatusCode != http.StatusOK || len(login.SID) != 26 {
		t.Fatalf("login: %d %s", resp.StatusCode, raw)
	}

	callers := []struct {
		name   string
		bearer string
	}{
		{"no credential", ""},
		{"unknown 26-char bearer", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"},
		{"valid token", operateToken},
		{"valid session", login.SID},
	}

	// Normal mode: remember each answer and assert no auth_off member.
	normal := make(map[string]string, len(callers))
	for _, c := range callers {
		_, body := get(t, f, "/api/auth/v1/state", c.bearer)
		normal[c.name] = string(body)
		if _, has := decode[map[string]json.RawMessage](t, string(body))["auth_off"]; has {
			t.Errorf("%s: normal mode emits auth_off: %s", c.name, body)
		}
	}

	f.SetAuthOff(true)
	acct := litefake.DefaultAuthOffAccount()
	var first string
	for _, c := range callers {
		t.Run("off/"+c.name, func(t *testing.T) {
			resp, body := get(t, f, "/api/auth/v1/state", c.bearer)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			st := decode[struct {
				SetupRequired      bool   `json:"setup_required"`
				Authenticated      bool   `json:"authenticated"`
				User               string `json:"user"`
				Role               string `json:"role"`
				Level              string `json:"level"`
				AccountID          string `json:"account_id"`
				SID                string `json:"sid"`
				MustChangePassword bool   `json:"must_change_password"`
				AuthOff            *bool  `json:"auth_off"`
			}](t, string(body))
			if st.AuthOff == nil || !*st.AuthOff {
				t.Errorf("auth_off not true: %s", body)
			}
			if !st.Authenticated || st.SetupRequired || st.MustChangePassword {
				t.Errorf("flags: %s", body)
			}
			if st.Role != "admin" || st.Level != "administer" {
				t.Errorf("role/level not the administrator's: %s", body)
			}
			if st.User != acct.Username || st.AccountID != acct.AccountID || st.SID != litefake.DefaultAuthOffSID {
				t.Errorf("identifiers not the configured placeholders: %s", body)
			}
			if first == "" {
				first = string(body)
			} else if string(body) != first {
				t.Errorf("answer differs from the first caller's:\n got %s\nwant %s", body, first)
			}
		})
	}

	f.SetAuthOff(false)
	for _, c := range callers {
		if _, body := get(t, f, "/api/auth/v1/state", c.bearer); string(body) != normal[c.name] {
			t.Errorf("%s after flip-back:\n got %s\nwant %s", c.name, body, normal[c.name])
		}
	}
}

// TestFakeAuthOffOptionOverridesPlaceholders pins Options.AuthOff and the
// overridable identity: the configured account's strings and session id
// appear, while role and level stay the administrator's.
func TestFakeAuthOffOptionOverridesPlaceholders(t *testing.T) {
	f := startFake(t, litefake.Options{
		AuthOff:        true,
		AuthOffAccount: litefake.Account{Username: "anon", AccountID: "42", Role: "user", Level: "read"},
		AuthOffSID:     "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	})
	_, body := get(t, f, "/api/auth/v1/state", "")
	for _, want := range []string{`"user":"anon"`, `"account_id":"42"`, `"sid":"ABCDEFGHIJKLMNOPQRSTUVWXYZ"`, `"role":"admin"`, `"level":"administer"`, `"auth_off":true`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("state lacks %s: %s", want, body)
		}
	}
}

// isSessionIDShape reports whether s has the box's session id shape:
// 26 characters of uppercase A-Z2-7 (CONTRACT.md §A.2).
func isSessionIDShape(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, c := range s {
		if (c < 'A' || c > 'Z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

// TestFakeSessionIDShapeHoldsAcrossLogins pins the minted id shape over
// many logins, so a lowercase or padded id cannot slip through by luck.
func TestFakeSessionIDShapeHoldsAcrossLogins(t *testing.T) {
	f := startFake(t, litefake.Options{Accounts: []litefake.Account{{Username: "u", Password: "p"}}})
	for range 32 {
		_, raw := send(t, f, http.MethodPost, "/api/auth/v1/login", "", `{"username":"u","password":"p"}`)
		sid := decode[struct {
			SID string `json:"sid"`
		}](t, string(raw)).SID
		if !isSessionIDShape(sid) {
			t.Fatalf("session id %q is not 26 characters of A-Z2-7", sid)
		}
	}
	if !isSessionIDShape(litefake.DefaultAuthOffSID) {
		t.Errorf("DefaultAuthOffSID %q is not 26 characters of A-Z2-7", litefake.DefaultAuthOffSID)
	}
}
