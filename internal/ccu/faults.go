// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"errors"
	"fmt"
)

// HomeMatic fault codes.
//
// The XML-RPC specification of the HomeMatic interface processes
// defines a small set of negative fault codes, and clients classify
// their error handling by them — aiohomematic, for one, decides from
// the code whether a call is worth retrying. The simulator answers
// every failure with -1 and the Go error text, which lands in the
// retryable bucket: an unknown device is retried forever instead of
// failing fast.
//
// Reporting the real codes changes what a client does with an error, so
// it is opt-in; the -1 default stays for pydevccu parity.
const (
	// FaultUnknownError is the catch-all a CCU falls back to. It is
	// also what the simulator reports for everything by default.
	FaultUnknownError = -1
	// FaultUnknownParamset — the requested paramset does not exist on
	// the addressed channel.
	FaultUnknownParamset = -2
	// FaultUnknownValue — the parameter does not exist in the paramset.
	FaultUnknownValue = -3
	// FaultUnknownDevice — no device or channel under that address.
	FaultUnknownDevice = -4
	// FaultUnknownParameter — the parameter name is not known.
	FaultUnknownParameter = -5
	// FaultInvalidValue — the value is outside the allowed range or of
	// the wrong type.
	FaultInvalidValue = -6
)

// Typed failure causes. Handlers wrap these so the transport layer can
// translate them into a fault code without matching on message text.
var (
	// ErrUnknownDevice reports an address no device answers to.
	ErrUnknownDevice = errors.New("unknown device")
	// ErrUnknownParamset reports a paramset the channel does not have.
	ErrUnknownParamset = errors.New("unknown paramset")
	// ErrUnknownParameter reports a parameter outside the paramset.
	ErrUnknownParameter = errors.New("unknown parameter")
	// ErrInvalidValue reports a value the parameter cannot take.
	ErrInvalidValue = errors.New("invalid value")
)

// EnableFaultCodes makes failures report the HomeMatic fault code that
// matches their cause instead of the generic -1.
func (s *Server) EnableFaultCodes(enabled bool) {
	s.mu.Lock()
	s.faultCodes = enabled
	s.mu.Unlock()
}

// faultCodeFor classifies an error. Causes without a defined code —
// and every error when the catalogue is off — report -1, which is what
// a CCU does for anything it cannot attribute.
func faultCodeFor(err error, catalogue bool) int {
	if !catalogue {
		return FaultUnknownError
	}
	switch {
	case errors.Is(err, ErrUnknownDevice):
		return FaultUnknownDevice
	case errors.Is(err, ErrUnknownParamset):
		return FaultUnknownParamset
	case errors.Is(err, ErrUnknownParameter):
		return FaultUnknownParameter
	case errors.Is(err, ErrInvalidValue):
		return FaultInvalidValue
	default:
		return FaultUnknownError
	}
}

// unknownDevice builds the error for an address nothing answers to.
func unknownDevice(address string) error {
	return fmt.Errorf("%w: %w: device %q not found", ErrRPC, ErrUnknownDevice, address)
}

// unknownParamset builds the error for a missing paramset.
func unknownParamset(address, paramsetKey string) error {
	return fmt.Errorf("%w: %w: paramset %q not found on %q", ErrRPC, ErrUnknownParamset, paramsetKey, address)
}

// unknownParameter builds the error for a parameter outside a paramset.
func unknownParameter(address, parameter string) error {
	return fmt.Errorf("%w: %w: value key %q not found on %q", ErrRPC, ErrUnknownParameter, parameter, address)
}

// invalidValue builds the error for a rejected value.
func invalidValue(address, parameter string, cause error) error {
	return fmt.Errorf("%w: %w: %s.%s: %w", ErrRPC, ErrInvalidValue, address, parameter, cause)
}
