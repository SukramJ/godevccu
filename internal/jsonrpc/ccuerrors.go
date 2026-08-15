// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package jsonrpc

import "fmt"

// The CCU's own JSON-RPC error model.
//
// A CCU does not speak JSON-RPC 2.0. Its envelope carries "version":
// "1.1" instead of "jsonrpc", and its error object is
//
//	{"name": "JSONRPCError", "code": <int>, "message": "<text>"}
//
// with a small set of numeric codes that are nothing like the 2.0
// range. Clients map those codes onto their own exception types, so a
// simulator sending -32001 never triggers the authentication-failure
// path a real CCU would trigger with 400.
//
// Codes and message wording are taken from the firmware:
// www/api/eq3/jsonrpc.tcl (100/102/103), www/api/homematic.cgi
// (400/401/402) and www/api/eq3/hmscript.tcl (500). Nothing beyond
// those seven exists in the firmware, so nothing beyond them is
// modelled here.
const (
	// CCUErrInvalidRequest — the body could not be parsed.
	CCUErrInvalidRequest = 100
	// CCUErrParamsNotFound — the request carries no params member.
	CCUErrParamsNotFound = 102
	// CCUErrMethodNotInRequest — the request carries no method member.
	CCUErrMethodNotInRequest = 103
	// CCUErrAccessDenied — the session's privilege level is too low.
	CCUErrAccessDenied = 400
	// CCUErrMethodNotFound — no such method.
	CCUErrMethodNotFound = 401
	// CCUErrMissingArgument — a declared argument was not supplied.
	CCUErrMissingArgument = 402
	// CCUErrScriptError — the ReGa script failed.
	CCUErrScriptError = 500
)

// ccuErrorName is the constant "name" member of every CCU error object.
const ccuErrorName = "JSONRPCError"

// ccuCodeFor translates a JSON-RPC 2.0 code into the CCU code that
// covers the same situation. Codes without a CCU counterpart map onto
// the generic invalid-request code, which is what the firmware answers
// when it cannot make sense of a call.
func ccuCodeFor(code int) int {
	switch code {
	case ErrParseError, ErrInvalidRequest:
		return CCUErrInvalidRequest
	case ErrMethodNotFound:
		return CCUErrMethodNotFound
	case ErrInvalidParams:
		return CCUErrMissingArgument
	case ErrAuthRequired, ErrSessionExpired, ErrPermissionDenied:
		return CCUErrAccessDenied
	case ErrInternalError, ErrServerError, ErrInvalidOperation:
		return CCUErrScriptError
	case ErrObjectNotFound:
		// A CCU reports an unknown object as a failed script run — it
		// has no dedicated "not found" code.
		return CCUErrScriptError
	default:
		return CCUErrInvalidRequest
	}
}

// marshalCCU renders the error the way the firmware does.
func (e *Error) marshalCCU() map[string]any {
	return map[string]any{
		"name":    ccuErrorName,
		"code":    ccuCodeFor(e.Code),
		"message": e.Message,
	}
}

// ErrAccessDenied is the privilege-level rejection, worded like the
// firmware: access denied ("ADMIN" needed 0).
func ErrAccessDenied(required string, have int) *Error {
	return &Error{
		Code:    ErrPermissionDenied,
		Message: fmt.Sprintf("access denied (%q needed %d)", required, have),
	}
}

// Privilege levels. A CCU ranks them NONE < GUEST < USER < ADMIN with
// the numeric values below (www/api/homematic.cgi), and compares the
// method's declared level against the session's level.
const (
	levelNone  = 0
	levelGuest = 1
	levelUser  = 2
	levelAdmin = 8
)

// levelValue maps a level name to its rank. Unknown names rank as USER,
// the most common level in methods.conf.
func levelValue(name string) int {
	switch name {
	case "NONE":
		return levelNone
	case "GUEST":
		return levelGuest
	case "ADMIN":
		return levelAdmin
	default:
		return levelUser
	}
}
