// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"strings"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// System lifecycle automata.
//
// Two flows a client drives against a CCU had no state at all in the
// simulator: pairing and firmware updates.
//
//   - setInstallMode(true, 60, …) started nothing, and getInstallMode
//     answered a constant 0, so "how much pairing time is left" was
//     always "none" and no device ever appeared
//   - installFirmware/updateFirmware were `return true`, and
//     FIRMWARE_UPDATE_STATE never moved off its initial value, so an
//     update progress display had nothing to display
//
// Both are modelled here as explicit state, resolved lazily on read
// where a clock is involved — that keeps them deterministic and avoids
// a goroutine that would need a stop path of its own. Both are opt-in;
// without them the previous constants are reported unchanged.

// FIRMWARE_UPDATE_STATE values, in the order a CCU walks them.
const (
	FirmwareStateUpToDate     = "UP_TO_DATE"
	FirmwareStateNewAvailable = "NEW_FIRMWARE_AVAILABLE"
	FirmwareStateDelivering   = "DELIVER_FIRMWARE_IMAGE"
	FirmwareStateReady        = "READY_FOR_UPDATE"
	FirmwareStatePerforming   = "PERFORMING_UPDATE"
)

// firmwareSequence is the progression an update walks once triggered.
var firmwareSequence = []string{
	FirmwareStateDelivering,
	FirmwareStateReady,
	FirmwareStatePerforming,
	FirmwareStateUpToDate,
}

// defaultStepInterval is how long each firmware step takes, and how
// long after entering install mode a device turns up in the inbox.
const defaultStepInterval = 150 * time.Millisecond

// EnableLifecycle turns the pairing and firmware automata on. A zero
// interval selects the default step duration.
func (r *RPCFunctions) EnableLifecycle(step time.Duration) {
	if step <= 0 {
		step = defaultStepInterval
	}
	r.mu.Lock()
	r.lifecycle = true
	r.lifecycleStep = step
	r.mu.Unlock()
}

// SetInstallMode enables or disables pairing for the given duration.
//
// A CCU counts the remaining seconds down and reports them from
// getInstallMode; the simulator records the deadline and derives the
// remainder on read.
func (r *RPCFunctions) SetInstallMode(on bool, duration int, mode int, address string) bool {
	r.mu.Lock()
	if !r.lifecycle {
		r.mu.Unlock()
		return true
	}
	if !on {
		r.installUntil = time.Time{}
		r.mu.Unlock()
		return true
	}
	if duration <= 0 {
		duration = 60
	}
	r.installUntil = time.Now().Add(time.Duration(duration) * time.Second)
	r.installMode = mode
	r.installAddress = strings.ToUpper(address)
	r.mu.Unlock()
	return true
}

// GetInstallMode reports the seconds left in pairing mode, or 0.
func (r *RPCFunctions) GetInstallMode() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lifecycle || r.installUntil.IsZero() {
		return 0
	}
	remaining := time.Until(r.installUntil)
	if remaining <= 0 {
		r.installUntil = time.Time{}
		return 0
	}
	// A CCU rounds up: one second left still reads as one.
	return int((remaining + time.Second - 1) / time.Second)
}

// InstallModeAddress reports the device pairing was restricted to, if
// any. Empty means every device may pair.
func (r *RPCFunctions) InstallModeAddress() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.installAddress
}

// InstallFirmware starts a firmware update on a device. With the
// lifecycle enabled this walks FIRMWARE_UPDATE_STATE through the
// progression a CCU reports; otherwise it reports success and does
// nothing, as before.
func (r *RPCFunctions) InstallFirmware(address string) bool {
	return r.startFirmwareUpdate(address)
}

// UpdateFirmware is the second spelling of the same command; a CCU
// exposes both.
func (r *RPCFunctions) UpdateFirmware(address string) bool {
	return r.startFirmwareUpdate(address)
}

// startFirmwareUpdate kicks off the state progression for a device.
func (r *RPCFunctions) startFirmwareUpdate(address string) bool {
	r.mu.Lock()
	if !r.lifecycle || r.timersStopped {
		r.mu.Unlock()
		return true
	}
	key := strings.ToUpper(address)
	if _, running := r.firmwareTimers[key]; running {
		// Already updating: a CCU ignores the second request.
		r.mu.Unlock()
		return true
	}
	step := r.lifecycleStep
	interfaceID := r.interfaceID
	r.mu.Unlock()

	r.advanceFirmware(key, interfaceID, 0, step)
	return true
}

// advanceFirmware reports one step of the progression and schedules
// the next.
func (r *RPCFunctions) advanceFirmware(address, interfaceID string, index int, step time.Duration) {
	if index >= len(firmwareSequence) {
		r.mu.Lock()
		delete(r.firmwareTimers, address)
		r.mu.Unlock()
		return
	}
	state := firmwareSequence[index]
	r.setMaintenanceValue(maintenanceChannel(address), hmconst.AttrFirmwareUpdateState, state)

	r.mu.Lock()
	if r.timersStopped {
		delete(r.firmwareTimers, address)
		r.mu.Unlock()
		return
	}
	r.firmwareTimers[address] = time.AfterFunc(step, func() {
		r.advanceFirmware(address, interfaceID, index+1, step)
	})
	r.mu.Unlock()
}

// stopLifecycleTimers cancels every pending firmware step.
func (r *RPCFunctions) stopLifecycleTimers() {
	r.mu.Lock()
	timers := r.firmwareTimers
	r.firmwareTimers = make(map[string]*time.Timer)
	r.mu.Unlock()
	for _, t := range timers {
		t.Stop()
	}
}
