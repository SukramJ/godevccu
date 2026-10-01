// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package ccu

import (
	"context"
	"sync"
	"time"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// Fault injection.
//
// A client has to survive an interface process that answers late, not
// at all, with a fault, or by closing the connection — the reconnect,
// watchdog and retry paths are only exercised when that happens. The
// rule shape (method, times, fault, delay, hang, close) is
// hm-simulator's injectFault (https://github.com/hobbyquaker/hm-simulator,
// MIT, Sebastian Raff). Rules apply to the top-level call on every
// transport of a server — XML-RPC, its TLS twin and BIN-RPC.

// FaultRule makes the next calls of a method misbehave.
type FaultRule struct {
	// Method is the method name; empty or "*" matches every method.
	Method string
	// Times is how many calls the rule applies to: 0 means one, a
	// negative number every call until [Server.ClearFaults].
	Times int
	// Fault, when set, is answered instead of dispatching the call.
	Fault *xmlrpc.Fault
	// Delay holds the call back before it is answered (or before the
	// other effects of the rule apply).
	Delay time.Duration
	// Hang never answers; the connection stays open until the rule is
	// cleared, the server stops, or the client gives up.
	Hang bool
	// CloseConnection closes the connection instead of answering.
	CloseConnection bool
}

// faultInjector holds the pending rules of one server.
type faultInjector struct {
	mu      sync.Mutex
	rules   []*FaultRule
	release chan struct{}
}

// InjectFault adds a rule. Rules are matched in the order they were
// added.
func (s *Server) InjectFault(rule FaultRule) {
	if rule.Method == "" {
		rule.Method = "*"
	}
	if rule.Times == 0 {
		rule.Times = 1
	}
	s.faults.mu.Lock()
	s.faults.rules = append(s.faults.rules, &rule)
	s.faults.mu.Unlock()
}

// ClearFaults removes every rule that has not fired yet and lets every
// call held by a hang or a delay go: those connections are closed
// without an answer.
func (s *Server) ClearFaults() {
	s.faults.mu.Lock()
	s.faults.rules = nil
	s.faults.mu.Unlock()
	s.releaseHeldCalls()
}

// releaseHeldCalls ends every call a rule is holding.
func (s *Server) releaseHeldCalls() {
	s.faults.mu.Lock()
	if s.faults.release != nil {
		close(s.faults.release)
		s.faults.release = nil
	}
	s.faults.mu.Unlock()
}

// take returns the first rule matching method and consumes one use of
// it, together with the channel that releases a held call.
func (f *faultInjector) take(method string) (*FaultRule, <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, rule := range f.rules {
		if rule.Method != "*" && rule.Method != method {
			continue
		}
		taken := *rule
		if rule.Times > 0 {
			rule.Times--
			if rule.Times == 0 {
				f.rules = append(f.rules[:i:i], f.rules[i+1:]...)
			}
		}
		if f.release == nil {
			f.release = make(chan struct{})
		}
		return &taken, f.release
	}
	return nil, nil
}

// intercept applies the first matching rule to a call before it is
// dispatched. It is the [xmlrpc.Handler.Intercept] of the server.
func (s *Server) intercept(ctx context.Context, method string) error {
	rule, release := s.faults.take(method)
	if rule == nil {
		return nil
	}
	if rule.Delay > 0 {
		timer := time.NewTimer(rule.Delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return xmlrpc.ErrDropConnection
		case <-release:
			return xmlrpc.ErrDropConnection
		}
	}
	switch {
	case rule.Hang:
		s.logger.Debug("ccu: injected hang", "method", method)
		select {
		case <-ctx.Done():
		case <-release:
		}
		return xmlrpc.ErrDropConnection
	case rule.CloseConnection:
		s.logger.Debug("ccu: injected connection close", "method", method)
		return xmlrpc.ErrDropConnection
	case rule.Fault != nil:
		s.logger.Debug("ccu: injected fault", "method", method, "code", rule.Fault.Code)
		return &xmlrpc.Fault{Code: rule.Fault.Code, Message: rule.Fault.Message}
	}
	return nil
}
