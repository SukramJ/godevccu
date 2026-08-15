// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"strings"
	"time"

	"github.com/SukramJ/godevccu/internal/deviceresponses"
	"github.com/SukramJ/godevccu/internal/hmconst"
)

// Actuator ramps.
//
// A blind or dimmer does not reach its target instantly: the CCU
// reports ACTIVITY_STATE as the device travels and clears it once the
// device arrives. The simulator applies the target value immediately —
// which is what keeps it deterministic — so by default the activity
// state it reports is already idle.
//
// With ramps enabled the *state sequence* is modelled: the write is
// answered with a moving state in the reported direction, and the idle
// state follows after the configured travel time. The value itself
// still lands immediately, so a test never has to wait to read it back;
// only the activity phase is stretched.

// defaultRampDuration is the travel time a simulated actuator takes.
// Short enough for a test to observe both edges without waiting.
const defaultRampDuration = 200 * time.Millisecond

// EnableRamps turns the moving phase on and sets the travel time. A
// zero duration selects the default.
func (r *RPCFunctions) EnableRamps(travel time.Duration) {
	if travel <= 0 {
		travel = defaultRampDuration
	}
	r.mu.Lock()
	r.ramps = true
	r.rampDuration = travel
	r.mu.Unlock()
}

// notifyRamp reports the moving phase of a value write and schedules
// the idle state that ends it. previous is the value the parameter held
// before the write; a move that changes nothing reports nothing.
func (r *RPCFunctions) notifyRamp(address, valueKey string, target, previous any) {
	progressParam, ramped := deviceresponses.RampParameters[valueKey]
	if !ramped {
		return
	}
	r.mu.Lock()
	if !r.ramps || r.timersStopped {
		r.mu.Unlock()
		return
	}
	duration := r.rampDuration
	interfaceID := r.interfaceID
	key := strings.ToUpper(address) + "." + progressParam
	if existing, pending := r.rampTimers[key]; pending {
		// Still travelling: retarget rather than emitting a second
		// rising edge, the way a device redirected mid-move behaves.
		existing.Reset(duration)
		r.mu.Unlock()
		return
	}
	// A parameter that was never written holds its paramset default,
	// which for a position is the resting one. Treating "unknown" as
	// "not moving" would swallow the first move of every device.
	if previous == nil {
		previous = 0.0
	}
	direction := deviceresponses.ActivityForMove(target, previous)
	if direction == deviceresponses.ActivityIdle {
		r.mu.Unlock()
		return
	}
	timer := time.AfterFunc(duration, func() {
		r.mu.Lock()
		delete(r.rampTimers, key)
		stopped := r.timersStopped
		r.mu.Unlock()
		if stopped {
			return
		}
		r.setValueSilently(address, progressParam, deviceresponses.ActivityIdle)
		r.fireEvent(interfaceID, address, progressParam, deviceresponses.ActivityIdle)
	})
	r.rampTimers[key] = timer
	r.mu.Unlock()

	r.setValueSilently(address, progressParam, direction)
	r.fireEvent(interfaceID, address, progressParam, direction)
}

// setValueSilently writes a VALUES parameter into the paramset store
// without producing an event, so callers control the reporting.
func (r *RPCFunctions) setValueSilently(address, parameter string, value any) {
	key := strings.ToUpper(address)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.paramsets[key]; !ok {
		r.paramsets[key] = make(map[string]map[string]any)
	}
	if _, ok := r.paramsets[key][hmconst.ParamsetAttrValues]; !ok {
		r.paramsets[key][hmconst.ParamsetAttrValues] = make(map[string]any)
	}
	r.paramsets[key][hmconst.ParamsetAttrValues][parameter] = value
	r.paramsetDirty[psKey(key, hmconst.ParamsetAttrValues)] = struct{}{}
}

// stopRampTimers cancels every pending ramp. Called from stopTimers.
func (r *RPCFunctions) stopRampTimers() {
	r.mu.Lock()
	timers := r.rampTimers
	r.rampTimers = make(map[string]*time.Timer)
	r.mu.Unlock()
	for _, t := range timers {
		t.Stop()
	}
}
