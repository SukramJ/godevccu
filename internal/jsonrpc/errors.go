// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package jsonrpc implements the CCU/OpenCCU JSON-RPC server. The
// public API surface is identical to pydevccu/json_rpc.
package jsonrpc

import "fmt"

// Standard JSON-RPC 2.0 error codes plus CCU-specific entries (mirrors
// pydevccu/json_rpc/errors.py).
const (
	ErrParseError       = -32700
	ErrInvalidRequest   = -32600
	ErrMethodNotFound   = -32601
	ErrInvalidParams    = -32602
	ErrInternalError    = -32603
	ErrServerError      = -32000
	ErrAuthRequired     = -32001
	ErrSessionExpired   = -32002
	ErrPermissionDenied = -32003
	ErrObjectNotFound   = -32004
	ErrInvalidOperation = -32005
)

// Error is a JSON-RPC error returned by handlers.
type Error struct {
	Code    int
	Message string
	Data    any
}

// Error implements the error interface.
func (e *Error) Error() string { return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message) }

// MarshalDict produces a map representation suitable for the response
// payload.
func (e *Error) MarshalDict() map[string]any {
	out := map[string]any{
		"code":    e.Code,
		"message": e.Message,
	}
	if e.Data != nil {
		out["data"] = e.Data
	}
	return out
}

// Convenience constructors mirror pydevccu's exception classes.
func ErrParse(message string) *Error   { return &Error{Code: ErrParseError, Message: message} }
func ErrInvalid(message string) *Error { return &Error{Code: ErrInvalidRequest, Message: message} }
func ErrMethod(method string) *Error {
	return &Error{Code: ErrMethodNotFound, Message: "Method not found: " + method}
}
func ErrParams(message string) *Error     { return &Error{Code: ErrInvalidParams, Message: message} }
func ErrInternal(message string) *Error   { return &Error{Code: ErrInternalError, Message: message} }
func ErrAuth(message string) *Error       { return &Error{Code: ErrAuthRequired, Message: message} }
func ErrSession(message string) *Error    { return &Error{Code: ErrSessionExpired, Message: message} }
func ErrPermission(message string) *Error { return &Error{Code: ErrPermissionDenied, Message: message} }
func ErrObject(kind string, id any) *Error {
	return &Error{Code: ErrObjectNotFound, Message: fmt.Sprintf("%s not found: %v", kind, id)}
}
func ErrOperation(message string) *Error { return &Error{Code: ErrInvalidOperation, Message: message} }
