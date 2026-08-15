// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"strings"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// Service messages.
//
// A CCU derives its service messages from the maintenance channel of
// every device: UNREACH, STICKY_UNREACH, LOWBAT/LOW_BAT,
// CONFIG_PENDING, UPDATE_PENDING and a non-zero ERROR/ERROR_CODE each
// raise one. The simulator answers getServiceMessages with a fixed
// entry instead, which CLAUDE.md pins deliberately — existing
// integration tests in both ecosystems assert exactly that shape.
//
// So the derivation is opt-in and additive: [RPCFunctions.ServiceStates]
// reports the derived set for callers that want it, while
// GetServiceMessages keeps answering the pinned entry.

// serviceParameters are the maintenance parameters that raise a service
// message, in the order a CCU reports them.
var serviceParameters = []string{
	ParamUnreach,
	ParamStickyUnreach,
	"LOWBAT",
	"LOW_BAT",
	ParamConfigPending,
	"UPDATE_PENDING",
	hmconst.AttrError,
	"ERROR_CODE",
}

// ServiceState is one derived service message: the channel it sits on,
// the parameter that raised it and its current value.
type ServiceState struct {
	Address   string
	Parameter string
	Value     any
}

// EnableServiceMessages turns the derivation and the suppression store
// on.
func (r *RPCFunctions) EnableServiceMessages() {
	r.mu.Lock()
	r.serviceMessages = true
	r.mu.Unlock()
}

// ServiceStates reports the currently raised service messages, derived
// from the maintenance channels. Suppressed (channel, parameter) pairs
// are filtered out, as a CCU filters them.
//
// Returns nil unless the derivation was enabled.
func (r *RPCFunctions) ServiceStates() []ServiceState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.serviceMessages {
		return nil
	}
	var out []ServiceState
	for address, paramsets := range r.paramsets {
		if !strings.HasSuffix(address, ":0") {
			continue
		}
		values, ok := paramsets[hmconst.ParamsetAttrValues]
		if !ok {
			continue
		}
		for _, parameter := range serviceParameters {
			value, present := values[parameter]
			if !present || !isRaised(value) {
				continue
			}
			if r.isSuppressedLocked(address, parameter) {
				continue
			}
			out = append(out, ServiceState{
				Address:   address,
				Parameter: parameter,
				Value:     value,
			})
		}
	}
	return out
}

// isRaised reports whether a maintenance value constitutes a message: a
// true flag, or a non-zero error code.
func isRaised(value any) bool {
	switch x := value.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}

// SuppressServiceMessage adds or removes a suppression. An empty
// parameter suppresses every service parameter of the channel, which is
// what a CCU does when the WebUI silences a device.
func (r *RPCFunctions) SuppressServiceMessage(channelAddress, parameterID string, suppress bool) bool {
	key := strings.ToUpper(channelAddress)
	r.mu.Lock()
	defer r.mu.Unlock()
	parameters, ok := r.suppressed[key]
	if !ok {
		if !suppress {
			return true
		}
		parameters = make(map[string]struct{})
		r.suppressed[key] = parameters
	}
	switch {
	case suppress && parameterID == "":
		for _, p := range serviceParameters {
			parameters[p] = struct{}{}
		}
	case suppress:
		parameters[parameterID] = struct{}{}
	case parameterID == "":
		delete(r.suppressed, key)
	default:
		delete(parameters, parameterID)
	}
	return true
}

// SuppressedServiceMessages lists the suppressed parameters of a
// channel.
func (r *RPCFunctions) SuppressedServiceMessages(channelAddress string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	parameters := r.suppressed[strings.ToUpper(channelAddress)]
	out := make([]string, 0, len(parameters))
	// Report in the canonical order rather than map order, so repeated
	// calls answer identically.
	for _, p := range serviceParameters {
		if _, ok := parameters[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

// isSuppressedLocked reports whether a (channel, parameter) pair is
// silenced. The caller must hold the lock.
func (r *RPCFunctions) isSuppressedLocked(channelAddress, parameter string) bool {
	parameters, ok := r.suppressed[strings.ToUpper(channelAddress)]
	if !ok {
		return false
	}
	_, suppressed := parameters[parameter]
	return suppressed
}
