// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package litefake

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// DefaultToken is the API token a fake accepts when [Options.Tokens] is
// nil. It holds the wildcard scope, so it passes every check.
const DefaultToken = "olt_0123456789abcdef0123456789abcdef"

// Scope names from the occulited auth API.
const (
	scopeAll          = "*"
	scopeRPCRead      = "rpc:read"
	scopeRPCOperate   = "rpc:operate"
	scopeRPCConfigure = "rpc:configure"
	scopeRPCAdmin     = "rpc:admin"
)

// tokenEntry is one API token as the box stores it: a name and the
// scopes it was granted, unexpanded.
type tokenEntry struct {
	name   string
	scopes []string
}

// tokenName derives the stored name of a test token. The box names
// tokens explicitly; the fake keeps the knob a plain secret→scopes map
// and derives a stable name from the secret instead, so the subject a
// stream limit counts ("token:<name>") and the user /api/auth/v1/state
// reports are predictable from the secret alone.
func tokenName(secret string) string {
	body := strings.TrimPrefix(secret, "olt_")
	if len(body) > 8 {
		body = body[:8]
	}
	return "tok-" + body
}

// TokenName returns the name the fake reports for secret, as it appears
// in /api/auth/v1/state ("token:<name>").
func TokenName(secret string) string { return tokenName(secret) }

// implied lists the scopes a stored scope grants in addition to itself.
// The table is already closed under implication, so one lookup suffices.
func implied(scope string) []string {
	switch scope {
	case "meta:write":
		return []string{"meta:read"}
	case "system:write":
		return []string{"system:read", "led"}
	case "addons:write":
		return []string{"system:read"}
	case "auth:admin":
		return []string{"self"}
	case scopeRPCOperate:
		return []string{scopeRPCRead}
	case scopeRPCConfigure:
		return []string{scopeRPCOperate, scopeRPCRead}
	case scopeRPCAdmin:
		return []string{scopeRPCConfigure, scopeRPCOperate, scopeRPCRead}
	default:
		return nil
	}
}

// hasScope reports whether the stored scopes grant want, following the
// implications. Checks expand; /api/auth/v1/state does not.
func hasScope(stored []string, want string) bool {
	for _, s := range stored {
		if s == scopeAll || s == want {
			return true
		}
		for _, imp := range implied(s) {
			if imp == want {
				return true
			}
		}
	}
	return false
}

// credential extracts the API token a request presents: a bearer token,
// or the password of Basic auth (the user name is ignored). It returns
// "" when the request carries neither.
func credential(r *http.Request) string {
	if _, pass, ok := r.BasicAuth(); ok {
		return pass
	}
	scheme, rest, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(rest)
	}
	return ""
}

// lookupToken resolves a presented secret against the token table.
func (f *Fake) lookupToken(secret string) (tokenEntry, bool) {
	if secret == "" {
		return tokenEntry{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.tokens[secret]
	return e, ok
}

// principal is who a request acts as: an API token or an account
// session. The scopes are the stored ones, unexpanded.
type principal struct {
	subject string
	user    string
	scopes  []string
	session *session
}

// resolve maps a presented secret to a principal: an API token first,
// then a session id (both arrive as a bearer credential).
func (f *Fake) resolve(secret string) (principal, bool) {
	if e, ok := f.lookupToken(secret); ok {
		return principal{subject: "token:" + e.name, user: "token:" + e.name, scopes: e.scopes}, true
	}
	if secret == "" {
		return principal{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[secret]
	if !ok {
		return principal{}, false
	}
	return principal{
		subject: "session:" + s.account.Username,
		user:    s.account.Username,
		scopes:  s.account.Scopes,
		session: s,
	}, true
}

// authorize runs the route check: a valid credential holding scope. On
// failure it writes the error answer and returns false. rpcRoute
// selects the lite-rpc rule that a ?sid= query credential is refused
// with 400 rather than considered; a valid header credential is
// consulted first, since the first valid credential wins. The 403 names
// the route's scope.
func (f *Fake) authorize(w http.ResponseWriter, r *http.Request, scope string, rpcRoute bool) (who principal, secret string, ok bool) {
	secret = credential(r)
	who, ok = f.resolve(secret)
	if !ok {
		if rpcRoute && r.URL.Query().Has("sid") {
			writeError(w, http.StatusBadRequest, "bad-request",
				"credentials are not accepted in the query string here: use the Authorization header")
			return principal{}, "", false
		}
		writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
		return principal{}, "", false
	}
	if !hasScope(who.scopes, scope) {
		writeJSON(w, http.StatusForbidden, forbiddenBody{
			Error:   "forbidden",
			Message: "the scope " + scope + " is required",
			Scope:   scope,
		})
		return principal{}, "", false
	}
	recordSubject(r, who.subject)
	return who, secret, true
}

// forbiddenBody is the 403 answer; it names the missing scope.
type forbiddenBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Scope   string `json:"scope"`
}

// authStateAnon is the /api/auth/v1/state answer without a valid
// credential.
type authStateAnon struct {
	SetupRequired bool `json:"setup_required"`
	Authenticated bool `json:"authenticated"`
}

// authStateToken is the /api/auth/v1/state answer for an API token.
type authStateToken struct {
	SetupRequired      bool     `json:"setup_required"`
	Authenticated      bool     `json:"authenticated"`
	User               string   `json:"user"`
	Scopes             []string `json:"scopes"`
	MustChangePassword bool     `json:"must_change_password"`
}

// authStateSession is the /api/auth/v1/state answer for an account
// session: the token fields plus the account's identity.
type authStateSession struct {
	authStateToken
	Role      string `json:"role"`
	Level     string `json:"level"`
	AccountID string `json:"account_id"`
	SID       string `json:"sid"`
	Method    string `json:"method"`
}

// authStateOff is the /api/auth/v1/state answer in auth mode off: the
// anonymous administrator session's object plus the auth_off marker. It
// is a separate type so the normal-mode answers never carry the member.
type authStateOff struct {
	authStateSession
	AuthOff bool `json:"auth_off"`
}

// The administrator role and level the contract fixes for the auth-off
// session (CONTRACT.md §A.2).
const (
	authOffRole  = "admin"
	authOffLevel = "administer"
)

// DefaultAuthOffSID is the session id auth mode off reports unless
// [Options.AuthOffSID] names another. It is a litefake placeholder in the
// shape of a session id (26 characters of uppercase A-Z2-7): the contract does not fix
// the real box's value, so a test must not treat it as one.
const DefaultAuthOffSID = "LITEFAKEAUTHOFFSESSION2222"

// authOffMethod is the login method auth mode off reports. The contract
// does not fix it; it is a litefake placeholder.
const authOffMethod = "password"

// DefaultAuthOffAccount is the placeholder identity of the anonymous
// administrator session auth mode off reports unless
// [Options.AuthOffAccount] names another. The contract fixes only the
// auth_off marker and the administrator role and level (CONTRACT.md
// §A.2), not the user, account id or scopes; these strings are
// litefake's own and must not be treated as the real box's values.
func DefaultAuthOffAccount() Account {
	return Account{
		Username:  "litefake-anonymous",
		Role:      authOffRole,
		Level:     authOffLevel,
		AccountID: "litefake-auth-off",
		Scopes:    []string{scopeAll},
	}
}

// authOffState builds the fixed auth-off answer from the configured
// placeholder identity; role, level and must_change_password are the
// contract's, whatever the account says.
func (f *Fake) authOffState() authStateOff {
	a := f.opts.AuthOffAccount
	return authStateOff{
		authStateSession: authStateSession{
			authStateToken: authStateToken{
				Authenticated: true,
				User:          a.Username,
				Scopes:        append([]string{}, a.Scopes...),
			},
			Role:      authOffRole,
			Level:     authOffLevel,
			AccountID: a.AccountID,
			SID:       f.opts.AuthOffSID,
			Method:    authOffMethod,
		},
		AuthOff: true,
	}
}

// handleAuthState answers GET /api/auth/v1/state. It is open; with a
// credential it reports the scopes exactly as stored, implications not
// expanded, so a client has to expand them itself. In auth mode off it
// answers every request, whatever credential it carries, with the fixed
// anonymous administrator session object.
func (f *Fake) handleAuthState(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	off := f.authOff
	f.mu.Unlock()
	if off {
		writeJSON(w, http.StatusOK, f.authOffState())
		return
	}
	secret := credential(r)
	who, ok := f.resolve(secret)
	if !ok {
		writeJSON(w, http.StatusOK, authStateAnon{})
		return
	}
	recordSubject(r, who.subject)
	base := authStateToken{
		Authenticated: true,
		User:          who.user,
		Scopes:        append([]string{}, who.scopes...),
	}
	if who.session == nil {
		writeJSON(w, http.StatusOK, base)
		return
	}
	a := who.session.account
	base.MustChangePassword = a.MustChangePassword
	writeJSON(w, http.StatusOK, authStateSession{
		authStateToken: base,
		Role:           a.Role,
		Level:          a.Level,
		AccountID:      a.AccountID,
		SID:            secret,
		Method:         "password",
	})
}

// Account is a box user account that can log in with a password. The
// contract states the level vocabulary but not which scopes a session
// carries, so the test names them.
type Account struct {
	Username           string
	Password           string
	Role               string
	Level              string // read, operate, configure or administer
	AccountID          string
	Scopes             []string
	MustChangePassword bool
}

// session is one logged-in account.
type session struct {
	account Account
}

// newSessionID returns a session id of 26 characters of uppercase
// A-Z2-7, the standard base32 alphabet without padding (CONTRACT.md §A.2).
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
}

// loginRequest is the body of POST /api/auth/v1/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginAnswer is the successful login answer.
type loginAnswer struct {
	SID                string `json:"sid"`
	User               string `json:"user"`
	Role               string `json:"role"`
	Level              string `json:"level"`
	AccountID          string `json:"account_id"`
	MustChangePassword bool   `json:"must_change_password"`
}

// handleLogin answers the open POST /api/auth/v1/login.
func (f *Fake) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request", "invalid body")
		return
	}
	f.mu.Lock()
	acct, ok := f.accounts[req.Username]
	f.mu.Unlock()
	if !ok || acct.Password != req.Password {
		// One answer for an unknown user and a wrong password alike (CONTRACT §A.2).
		writeError(w, http.StatusUnauthorized, "invalid-credentials", "invalid credentials")
		return
	}
	sid := newSessionID()
	f.mu.Lock()
	f.sessions[sid] = &session{account: acct}
	f.mu.Unlock()
	recordSubject(r, "session:"+acct.Username)
	writeJSON(w, http.StatusOK, loginAnswer{
		SID:                sid,
		User:               acct.Username,
		Role:               acct.Role,
		Level:              acct.Level,
		AccountID:          acct.AccountID,
		MustChangePassword: acct.MustChangePassword,
	})
}

// handleLogout answers POST /api/auth/v1/logout, which needs the session
// itself as the bearer credential.
func (f *Fake) handleLogout(w http.ResponseWriter, r *http.Request) {
	sid := credential(r)
	who, ok := f.resolve(sid)
	if !ok || who.session == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
		return
	}
	recordSubject(r, who.subject)
	f.mu.Lock()
	delete(f.sessions, sid)
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, okAnswer{OK: true})
}

// okAnswer is a bare success answer.
type okAnswer struct {
	OK bool `json:"ok"`
}
