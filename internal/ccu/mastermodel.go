// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package ccu

import (
	"errors"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// MASTER write models of the interface processes.
//
// The simulator answers a bad MASTER write the way it answers a bad
// VALUES write: a fault for an unknown or read-only parameter, a clamp
// for a number out of range. Neither interface process does that, and
// they do not agree with each other either. hm-simulator
// (https://github.com/hobbyquaker/hm-simulator, MIT, Sebastian Raff)
// documents both as measured for Homematic Manager on CCU firmware
// 3.89.8 (lib/validate.js hmipStoredValue/bidcosStoredValue, lib/sim.js
// putMasterHmip/putMasterBidcos, README "CONFIG_PENDING"). The models
// below follow that description; nothing here was measured for godevccu.
//
// hmipserver stores everything it is given and validates afterwards:
//
//   - a parameter the channel does not have is stored for ever and
//     poisons the channel — every later MASTER write on it faults, even
//     an empty one; only deleting the device clears it
//   - a value of the wrong type is stored too and raises a sticky
//     CONFIG_PENDING, which a write that leaves no invalid value clears
//   - a number outside MIN..MAX is accepted; there is no range check
//   - the write faults (-5 with fault codes on) whenever the stored
//     configuration holds an invalid value or the channel is poisoned
//
// rfd never faults on a value: it drops unknown and read-only
// parameters, ignores what it cannot use, clamps numbers, coerces
// strings, and raises CONFIG_PENDING for a write that changed something
// until the device takes it (the configured CONFIG_PENDING span stands
// in for the device's next wakeup).
//
// Which model applies is decided per device by its protocol family
// ([hmconst.InterfaceForType]); VirtualDevices keep the default path.
//
// One deliberate difference: hm-simulator re-validates every stored
// MASTER value including the description defaults, godevccu only the
// values a client wrote — a mistyped default in the embedded data must
// not fault every write on that channel.

// masterModelKind selects the write model for a device.
type masterModelKind int

const (
	masterModelNone masterModelKind = iota
	masterModelHmIP
	masterModelBidCos
)

// masterModelFor maps a device type onto its interface's write model.
func masterModelFor(deviceType string) masterModelKind {
	switch hmconst.InterfaceForType(deviceType) {
	case hmconst.InterfaceHmIPRF:
		return masterModelHmIP
	case hmconst.InterfaceBidCosRF, hmconst.InterfaceBidCosWired:
		return masterModelBidCos
	default:
		return masterModelNone
	}
}

// errNotTransferable is the cause of the fault hmipserver answers when
// the stored configuration cannot be sent to the device.
var errNotTransferable = errors.New("configuration cannot be transferred to the device")

// ConfigPendingEntry is one device with a pending configuration.
type ConfigPendingEntry struct {
	// Address is the maintenance channel (<device>:0).
	Address string `json:"address"`
	// Sticky is true when only a valid write clears the flag again.
	Sticky bool `json:"sticky"`
}

// EnableMasterModel switches MASTER writes to the interface processes'
// models.
func (r *RPCFunctions) EnableMasterModel() {
	r.mu.Lock()
	r.masterModel = true
	r.mu.Unlock()
}

// ConfigPending lists the devices with a pending configuration raised by
// a MASTER write model, sorted by address.
func (r *RPCFunctions) ConfigPending() []ConfigPendingEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ConfigPendingEntry, 0, len(r.pendingSticky))
	for address, sticky := range r.pendingSticky {
		out = append(out, ConfigPendingEntry{Address: address, Sticky: sticky})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

// PoisonedChannels lists the channels whose stored MASTER holds a
// parameter they do not have, sorted. Every MASTER write on them faults.
func (r *RPCFunctions) PoisonedChannels() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.poisoned))
	for address := range r.poisoned {
		out = append(out, address)
	}
	sort.Strings(out)
	return out
}

// putMasterModel runs a MASTER write through the model. The caller holds
// the lock; it is released here.
func (r *RPCFunctions) putMasterModel(model masterModelKind, address, addrUp string, descs, set map[string]any) ([]RejectedParameter, error) {
	stored := r.masterOverridesLocked(addrUp)
	current := buildDefaults(descs)
	for k, v := range stored {
		current[k] = v
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)

	var rejected []RejectedParameter
	reject := func(name, reason string) {
		rejected = append(rejected, RejectedParameter{Name: name, Value: set[name], Reason: reason})
	}
	changed := false
	accepted := make(map[string]any, len(set))

	for _, name := range names {
		value := set[name]
		desc, known := descs[name].(map[string]any)
		if model == masterModelHmIP {
			if !known {
				stored[name] = value
				r.poisoned[addrUp] = struct{}{}
				reject(name, "not in the paramset description, stored")
				continue
			}
			if readInt(desc[hmconst.ParamsetAttrOperations])&hmconst.ParamsetOperationsWrite == 0 {
				reject(name, "not writeable")
				continue
			}
			v, valid := hmipStoredValue(desc, value)
			if !sameValue(current[name], v) {
				changed = true
			}
			stored[name], current[name] = v, v
			accepted[name] = v
			if !valid {
				reject(name, "not valid for this parameter, stored")
			}
			continue
		}
		if !known {
			reject(name, "not in the paramset description, dropped")
			continue
		}
		if readInt(desc[hmconst.ParamsetAttrOperations])&hmconst.ParamsetOperationsWrite == 0 {
			reject(name, "not writeable, dropped")
			continue
		}
		v, usable := bidcosStoredValue(desc, value)
		if !usable {
			reject(name, "not usable for this parameter, ignored")
			continue
		}
		accepted[name] = v
		if !sameValue(current[name], v) {
			stored[name], current[name] = v, v
			changed = true
		}
	}
	r.paramsetDirty[psKey(addrUp, hmconst.ParamsetAttrMaster)] = struct{}{}

	var fault error
	pending, clear, sticky := false, false, false
	if model == masterModelHmIP {
		invalid := false
		for name, v := range stored {
			if desc, ok := descs[name].(map[string]any); ok {
				if _, valid := hmipStoredValue(desc, v); !valid {
					invalid = true
					break
				}
			}
		}
		switch {
		case invalid && changed:
			pending, sticky = true, true
		case !invalid:
			clear = true
		}
		if _, poisoned := r.poisoned[addrUp]; invalid || poisoned {
			fault = invalidValue(address, hmconst.ParamsetAttrMaster, errNotTransferable)
		}
	} else if changed {
		pending = true
	}
	hook := r.onSetValue
	r.mu.Unlock()

	switch {
	case pending:
		r.raiseConfigPending(address, sticky)
	case clear:
		r.clearConfigPending(address)
	}
	if hook != nil {
		for _, name := range names {
			if v, ok := accepted[name]; ok {
				hook(address, name, v)
			}
		}
	}
	return rejected, fault
}

// masterOverridesLocked returns the stored MASTER values of a channel,
// creating the map. The caller holds the lock.
func (r *RPCFunctions) masterOverridesLocked(addrUp string) map[string]any {
	if _, ok := r.paramsets[addrUp]; !ok {
		r.paramsets[addrUp] = make(map[string]map[string]any)
	}
	if _, ok := r.paramsets[addrUp][hmconst.ParamsetAttrMaster]; !ok {
		r.paramsets[addrUp][hmconst.ParamsetAttrMaster] = make(map[string]any)
	}
	return r.paramsets[addrUp][hmconst.ParamsetAttrMaster]
}

// raiseConfigPending sets CONFIG_PENDING on the device's maintenance
// channel. A sticky flag stays until a valid write clears it; a
// non-sticky one clears itself after the CONFIG_PENDING span, the
// device having taken the configuration. A pending sticky flag is never
// downgraded by a later non-sticky write.
func (r *RPCFunctions) raiseConfigPending(address string, sticky bool) {
	channel := maintenanceChannel(address)
	key := strings.ToUpper(channel)
	r.mu.Lock()
	wasSticky, present := r.pendingSticky[key]
	r.pendingSticky[key] = sticky || wasSticky
	if timer, ok := r.configPendingTimers[key]; ok && (sticky || wasSticky) {
		timer.Stop()
		delete(r.configPendingTimers, key)
	}
	if !sticky && !wasSticky && !r.timersStopped {
		span := r.configPendingFor
		if span <= 0 {
			span = defaultConfigPendingDuration
		}
		if timer, ok := r.configPendingTimers[key]; ok {
			timer.Reset(span)
		} else {
			r.configPendingTimers[key] = time.AfterFunc(span, func() {
				r.mu.Lock()
				delete(r.configPendingTimers, key)
				stillSoft := !r.pendingSticky[key]
				stopped := r.timersStopped
				r.mu.Unlock()
				if stillSoft && !stopped {
					r.clearConfigPending(address)
				}
			})
		}
	}
	reportable := r.hasValuesParameterLocked(key, ParamConfigPending)
	r.mu.Unlock()
	if !present && reportable {
		r.setMaintenanceValue(channel, ParamConfigPending, true)
	}
}

// clearConfigPending drops a pending configuration and reports the
// falling edge.
func (r *RPCFunctions) clearConfigPending(address string) {
	channel := maintenanceChannel(address)
	key := strings.ToUpper(channel)
	r.mu.Lock()
	_, present := r.pendingSticky[key]
	delete(r.pendingSticky, key)
	if timer, ok := r.configPendingTimers[key]; ok {
		timer.Stop()
		delete(r.configPendingTimers, key)
	}
	reportable := r.hasValuesParameterLocked(key, ParamConfigPending)
	r.mu.Unlock()
	if present && reportable {
		r.setMaintenanceValue(channel, ParamConfigPending, false)
	}
}

// hasValuesParameterLocked reports whether a channel's VALUES
// description has the parameter — CONFIG_PENDING is only reported where
// the device describes it. The caller holds the lock.
func (r *RPCFunctions) hasValuesParameterLocked(channelUp, parameter string) bool {
	desc, ok := r.paramsetDescByAddr[channelUp]
	if !ok {
		return false
	}
	values, ok := desc[hmconst.ParamsetAttrValues].(map[string]any)
	if !ok {
		return false
	}
	_, ok = values[parameter]
	return ok
}

// hmipStoredValue is what hmipserver stores for a MASTER value, and
// whether the value is acceptable for the parameter. It checks the type
// only: an ENUM name is turned into its index, an <int> is accepted for
// a FLOAT, and MIN..MAX is not looked at.
func hmipStoredValue(desc map[string]any, raw any) (any, bool) {
	switch desc[hmconst.AttrType] {
	case hmconst.ParamsetTypeBool, hmconst.ParamsetTypeAction:
		if b, ok := raw.(bool); ok {
			return b, true
		}
		if n, ok := integral(raw); ok && (n == 0 || n == 1) {
			return n == 1, true
		}
		return raw, false
	case hmconst.ParamsetTypeInteger:
		if n, ok := integral(raw); ok {
			return int(n), true
		}
		return raw, false
	case hmconst.ParamsetTypeFloat:
		if _, ok := number(raw); ok {
			return raw, true
		}
		return raw, false
	case hmconst.ParamsetTypeEnum:
		list := valueList(desc)
		if s, ok := raw.(string); ok {
			if i := indexOf(list, s); i >= 0 {
				return i, true
			}
			return raw, false
		}
		if n, ok := integral(raw); ok && n >= 0 && n < int64(len(list)) {
			return int(n), true
		}
		return raw, false
	case hmconst.ParamsetTypeString:
		_, ok := raw.(string)
		return raw, ok
	default:
		return raw, true
	}
}

// bidcosStoredValue is what rfd stores for a MASTER value; false means
// rfd ignores the value. A FLOAT that arrives as an integer is ignored:
// hm-simulator records that rfd does so (which is why every FLOAT has to
// go out as an explicit double) but cannot model it, since JavaScript
// loses the distinction — the Go decoders keep it.
func bidcosStoredValue(desc map[string]any, raw any) (any, bool) {
	switch desc[hmconst.AttrType] {
	case hmconst.ParamsetTypeBool, hmconst.ParamsetTypeAction:
		b, ok := raw.(bool)
		return b, ok
	case hmconst.ParamsetTypeInteger:
		n, ok := number(raw)
		if !ok {
			n = leadingNumber(raw, true)
		}
		return int(clampTo(desc, math.Round(n))), true
	case hmconst.ParamsetTypeFloat:
		if _, isInt := integralType(raw); isInt {
			return nil, false
		}
		n, ok := number(raw)
		if !ok {
			n = leadingNumber(raw, false)
		}
		return clampTo(desc, n), true
	case hmconst.ParamsetTypeEnum:
		list := valueList(desc)
		if s, ok := raw.(string); ok {
			if i := indexOf(list, s); i >= 0 {
				return i, true
			}
			return nil, false
		}
		n, ok := integral(raw)
		if !ok {
			return nil, false
		}
		if len(list) > 0 {
			n = max(0, min(n, int64(len(list)-1)))
		}
		return int(n), true
	case hmconst.ParamsetTypeString:
		s, ok := raw.(string)
		return s, ok
	default:
		return raw, true
	}
}

// integralType reports whether v has a Go integer type.
func integralType(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	}
	return 0, false
}

// integral reports v as an integer when it is one: an integer type, or a
// float without a fractional part.
func integral(v any) (int64, bool) {
	if n, ok := integralType(v); ok {
		return n, true
	}
	if f, ok := v.(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) && f == math.Trunc(f) {
		return int64(f), true
	}
	return 0, false
}

// number reports v as a float when it has a numeric type.
func number(v any) (float64, bool) {
	if n, ok := integralType(v); ok {
		return float64(n), true
	}
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x)
	case float32:
		return float64(x), true
	}
	return 0, false
}

var (
	leadingInt   = regexp.MustCompile(`^\s*[+-]?\d+`)
	leadingFloat = regexp.MustCompile(`^\s*[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?`)
)

// leadingNumber parses the number a string starts with, as JavaScript's
// parseInt/parseFloat do; anything unparseable is 0, which is what rfd's
// coercion is described to store.
func leadingNumber(v any, integer bool) float64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	pattern := leadingFloat
	if integer {
		pattern = leadingInt
	}
	match := strings.TrimSpace(pattern.FindString(s))
	f, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}
	return f
}

// clampTo clamps n into the parameter's numeric MIN..MAX, where given.
func clampTo(desc map[string]any, n float64) float64 {
	if lo, ok := number(desc[hmconst.ParamsetAttrMin]); ok && n < lo {
		n = lo
	}
	if hi, ok := number(desc[hmconst.ParamsetAttrMax]); ok && n > hi {
		n = hi
	}
	return n
}

// valueList returns an ENUM's VALUE_LIST as strings.
func valueList(desc map[string]any) []string {
	raw, _ := desc[hmconst.ParamsetAttrValueList].([]any)
	out := make([]string, len(raw))
	for i, v := range raw {
		out[i], _ = v.(string)
	}
	return out
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// sameValue compares two stored values, treating numbers by value.
func sameValue(a, b any) bool {
	if x, ok := number(a); ok {
		if y, ok := number(b); ok {
			return x == y
		}
	}
	return reflect.DeepEqual(a, b)
}
