// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package devicelogic ports the optional device behaviour simulators in
// pydevccu/device_logic. Each simulator runs a goroutine that toggles
// device values on a configurable interval to produce a steady stream
// of events for integration tests.
package devicelogic

import (
	"context"
	"math/rand/v2"
	"time"
)

// RPC is the subset of the CCU API required by a logic simulator. The
// production type is *ccu.RPCFunctions but the package consumes the
// surface via this interface so unit tests can stub it.
type RPC interface {
	Active() bool
	GetValue(address, valueKey string) (any, error)
	SetValue(address, valueKey string, value any, force bool) error
	FireEvent(interfaceID, address, valueKey string, value any)
}

// Device is one running simulator.
type Device interface {
	Name() string
	Stop()
}

// Constructor builds a Device. interval and startupDelay are taken from
// the user-supplied logic configuration.
type Constructor func(rpc RPC, startupDelay, interval time.Duration) Device

// Registry maps device-type names to their simulator constructors.
var Registry = map[string]Constructor{
	"HM-Sec-SC-2":      newHMSecSC2,
	"HM-Sen-MDIR-WM55": newHMSenMDIRWM55,
}

// Config controls the work loops.
type Config struct {
	StartupDelay time.Duration
	Interval     time.Duration
}

// DefaultConfig matches pydevccu's default startupdelay=5s, interval=60s.
func DefaultConfig() Config {
	return Config{
		StartupDelay: 5 * time.Second,
		Interval:     60 * time.Second,
	}
}

// runner provides the scaffolding shared by all logic simulators.
type runner struct {
	name   string
	cfg    Config
	cancel context.CancelFunc
	done   chan struct{}
}

func newRunner(name string, cfg Config) *runner {
	return &runner{name: name, cfg: cfg, done: make(chan struct{})}
}

func (r *runner) Name() string { return r.name }

// start spawns the work loop in a goroutine. The closure runs until
// ctx is cancelled or the underlying RPC reports inactive.
func (r *runner) start(work func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		work(ctx)
	}()
}

// Stop signals the simulator to exit and blocks until it does.
func (r *runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	<-r.done
}

// sleepWithCancel naps for d but bails early when ctx is done.
func sleepWithCancel(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// randomDelay returns a duration in [0, max].
func randomDelay(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(max) + 1))
}
