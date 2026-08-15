// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"strings"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// Device state machines a CCU runs and the simulator did not.
//
// Two behaviours drive most of a client's device-level logic and were
// simply absent: a device going unreachable, and a configuration write
// taking effect. Both are opt-in — pydevccu has neither, and a test
// that does not ask for them must not suddenly see extra events.

// Maintenance parameters on channel 0 of every device.
const (
	// ParamUnreach is set while a device does not answer.
	ParamUnreach = "UNREACH"
	// ParamStickyUnreach latches an unreachability until a client
	// acknowledges it by writing false.
	ParamStickyUnreach = "STICKY_UNREACH"
	// ParamConfigPending is true between a MASTER write and the moment
	// the device has picked the configuration up.
	ParamConfigPending = "CONFIG_PENDING"
)

// defaultConfigPendingDuration is how long a configuration write stays
// pending. A real device picks its configuration up on its next wakeup,
// which is seconds to minutes out; the simulator uses a short span so a
// test can observe both edges without waiting.
const defaultConfigPendingDuration = 200 * time.Millisecond

// EnableReachability turns the device state machines on and sets how
// long CONFIG_PENDING stays true after a MASTER write. A zero duration
// selects the default.
func (r *RPCFunctions) EnableReachability(configPending time.Duration) {
	if configPending <= 0 {
		configPending = defaultConfigPendingDuration
	}
	r.mu.Lock()
	r.reachability = true
	r.configPendingFor = configPending
	r.mu.Unlock()
}

// SetDeviceUnreachable marks a device unreachable or reachable again.
//
// A CCU reports this on channel 0 as UNREACH, and latches
// STICKY_UNREACH, which stays true until a client explicitly writes it
// back to false — that is the acknowledgement gesture the WebUI offers.
// Recovering clears UNREACH but leaves STICKY_UNREACH standing.
func (r *RPCFunctions) SetDeviceUnreachable(address string, unreach bool) error {
	maintenance := maintenanceChannel(address)
	if _, err := r.GetDeviceDescription(maintenance); err != nil {
		return err
	}
	r.setMaintenanceValue(maintenance, ParamUnreach, unreach)
	if unreach {
		r.setMaintenanceValue(maintenance, ParamStickyUnreach, true)
	}
	return nil
}

// setMaintenanceValue writes a maintenance parameter into the paramset
// store and reports it, so a client sees the same value through
// getValue and through the event.
func (r *RPCFunctions) setMaintenanceValue(channel, parameter string, value any) {
	key := strings.ToUpper(channel)
	r.mu.Lock()
	// The maintenance paramset is created lazily, so a device that has
	// never been written to has no entry yet — build it rather than
	// dropping the value on the floor.
	if _, ok := r.paramsets[key]; !ok {
		r.paramsets[key] = make(map[string]map[string]any)
	}
	if _, ok := r.paramsets[key][hmconst.ParamsetAttrValues]; !ok {
		r.paramsets[key][hmconst.ParamsetAttrValues] = make(map[string]any)
	}
	r.paramsets[key][hmconst.ParamsetAttrValues][parameter] = value
	// Mark the compiled paramset stale, or getValue keeps serving the
	// cached copy and the write is invisible to readers.
	r.paramsetDirty[psKey(key, hmconst.ParamsetAttrValues)] = struct{}{}
	interfaceID := r.interfaceID
	r.mu.Unlock()
	r.fireEvent(interfaceID, channel, parameter, value)
}

// maintenanceChannel returns channel 0 of the device an address belongs
// to — the channel a CCU reports device-level state on.
func maintenanceChannel(address string) string {
	root := address
	if i := strings.IndexByte(address, ':'); i >= 0 {
		root = address[:i]
	}
	return root + ":0"
}

// notifyConfigPending starts the CONFIG_PENDING pulse after a MASTER
// paramset write: true immediately, false once the configured span has
// passed. It is edge-triggered — a second write while a pulse is in
// flight extends it rather than emitting another rising edge, which is
// how a device behaves when several parameters are written in a row.
func (r *RPCFunctions) notifyConfigPending(address string) {
	r.mu.Lock()
	if !r.reachability || r.timersStopped {
		r.mu.Unlock()
		return
	}
	channel := maintenanceChannel(address)
	duration := r.configPendingFor
	key := strings.ToUpper(channel)
	existing, pending := r.configPendingTimers[key]
	if pending {
		// Still pending: push the falling edge out, stay silent.
		existing.Reset(duration)
		r.mu.Unlock()
		return
	}
	timer := time.AfterFunc(duration, func() {
		r.mu.Lock()
		delete(r.configPendingTimers, key)
		stopped := r.timersStopped
		r.mu.Unlock()
		if stopped {
			return
		}
		r.setMaintenanceValue(channel, ParamConfigPending, false)
	})
	r.configPendingTimers[key] = timer
	r.mu.Unlock()

	r.setMaintenanceValue(channel, ParamConfigPending, true)
}

// stopTimers cancels every pending state-machine timer. Called from the
// server shutdown so no goroutine outlives the simulator.
func (r *RPCFunctions) stopTimers() {
	r.mu.Lock()
	r.timersStopped = true
	timers := r.configPendingTimers
	r.configPendingTimers = make(map[string]*time.Timer)
	r.mu.Unlock()
	for _, t := range timers {
		t.Stop()
	}
	r.stopRampTimers()
	r.stopLifecycleTimers()
}
