// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package ccu

import (
	"sort"
	"strings"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// Interface-process quirks.
//
// rfd, hs485d and hmipserver answer a handful of calls in ways no
// specification mentions and clients nonetheless trip over. The shapes
// below are what hm-simulator (https://github.com/hobbyquaker/hm-simulator,
// MIT, Sebastian Raff) records in its README ("Interface behaviour,
// measured", "The calls of Homematic Manager") as measured on CCU
// firmware 3.89.8/3.89.11; none of them was measured for godevccu.
//
// They only apply to an instance that serves one interface
// ([Options.InterfaceFilter], i.e. separate interface listeners): a
// single endpoint serving every device is no particular process.
//
//   - hmipserver answers ping without the CENTRAL/PONG event
//     (hm-simulator cites https://github.com/eq-3/occu/issues/42)
//   - getServiceMessages reports the raised maintenance parameters, and
//     rfd/hs485d answer an empty string instead of an empty array when
//     nothing is pending
//   - rfd's unfiltered getLinks("") carries FLAGS bit 1 (SENDER_BROKEN)
//     on every device-internal link; filtered by address the same link
//     reports its stored flags
//   - getMetadata of a key that is not set: rfd/hs485d fault -1
//     "Failure" (for an unknown address too), hmipserver answers an
//     empty value; getAllMetadata with nothing stored faults -1
//     "Failure" on rfd/hs485d
//
// The getServiceMessages fault VirtualDevices, CUxD and hmipserver were
// seen to answer on 3.89.x is a switch of its own
// ([RPCFunctions.EnableServiceMessagesFault]): hm-simulator records it
// as observed, not consistently, and keeps it off by default.

// attrFlags is the link struct field the SENDER_BROKEN bit lives in.
const attrFlags = "FLAGS"

// linkSenderBroken is getLinks' FLAGS bit 1.
const linkSenderBroken = 1

// errFailure is rfd's generic fault.
var errFailure = &xmlrpc.Fault{Code: FaultUnknownError, Message: "Failure"}

// errInvalidMessage is the unknown-method fault of hmipserver's table.
var errInvalidMessage = &xmlrpc.Fault{Code: FaultUnknownError, Message: "Invalid XML-RPC message"}

// EnableQuirks switches the interface-process quirks on.
func (r *RPCFunctions) EnableQuirks() {
	r.mu.Lock()
	r.quirks = true
	r.mu.Unlock()
}

// EnableServiceMessagesFault makes getServiceMessages on HmIP-RF and
// VirtualDevices answer the unknown-method fault.
func (r *RPCFunctions) EnableServiceMessagesFault() {
	r.mu.Lock()
	r.serviceMessagesFault = true
	r.mu.Unlock()
}

// quirkFamily returns the interface this instance serves when quirks
// apply, empty otherwise.
func (r *RPCFunctions) quirkFamily() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.quirks {
		return ""
	}
	return r.interfaceFilter
}

// isBidCosFamily reports whether an interface is served by rfd or hs485d.
func isBidCosFamily(family string) bool {
	return family == hmconst.InterfaceBidCosRF || family == hmconst.InterfaceBidCosWired
}

// ServiceMessagesAnswer is what getServiceMessages answers on the wire.
// Without quirks it is the pinned [RPCFunctions.GetServiceMessages].
func (r *RPCFunctions) ServiceMessagesAnswer() (any, error) {
	r.mu.Lock()
	faulty := r.serviceMessagesFault &&
		(r.interfaceFilter == hmconst.InterfaceHmIPRF || r.interfaceFilter == hmconst.InterfaceVirtualDevices)
	r.mu.Unlock()
	if faulty {
		return nil, errInvalidMessage
	}
	family := r.quirkFamily()
	if family == "" {
		return r.GetServiceMessages(), nil
	}
	r.mu.Lock()
	states := r.deriveServiceStatesLocked()
	r.mu.Unlock()
	if len(states) == 0 && isBidCosFamily(family) {
		return "", nil
	}
	out := make([]any, 0, len(states))
	for _, st := range states {
		out = append(out, []any{st.Address, st.Parameter, st.Value})
	}
	return out, nil
}

// quirkGetMetadata answers getMetadata the way the interface process
// does; handled is false when the regular lookup should run.
func (r *RPCFunctions) quirkGetMetadata(family, objectID, dataID string) (any, error, bool) { //nolint:revive // the flag reads best last
	r.mu.Lock()
	stored, ok := r.metadata[strings.ToUpper(objectID)][dataID]
	r.mu.Unlock()
	if ok {
		return stored, nil, true
	}
	switch {
	case isBidCosFamily(family):
		return nil, errFailure, true
	case family == hmconst.InterfaceHmIPRF:
		return "", nil, true
	}
	return nil, nil, false
}

// AllMetadata is what getAllMetadata answers on the wire: rfd and
// hs485d fault when nothing is stored for the object.
func (r *RPCFunctions) AllMetadata(objectID string) (map[string]any, error) {
	all := r.GetAllMetadata(objectID)
	if len(all) == 0 && isBidCosFamily(r.quirkFamily()) {
		return nil, errFailure
	}
	return all, nil
}

// linkFlags is the FLAGS a link reports: SENDER_BROKEN on rfd's
// unfiltered list for a link inside one device, 0 otherwise.
func linkFlags(family string, unfiltered bool, lk linkKey) int {
	if family == hmconst.InterfaceBidCosRF && unfiltered && deviceOf(lk.sender) == deviceOf(lk.receiver) {
		return linkSenderBroken
	}
	return 0
}

// deviceOf returns the device part of a channel address.
func deviceOf(address string) string {
	if before, _, ok := strings.Cut(address, ":"); ok {
		return before
	}
	return address
}

// deriveServiceStatesLocked collects the raised, unsuppressed
// maintenance parameters, sorted by channel and in the canonical
// parameter order. The caller holds the lock.
func (r *RPCFunctions) deriveServiceStatesLocked() []ServiceState {
	addresses := make([]string, 0)
	for address := range r.paramsets {
		if strings.HasSuffix(address, ":0") {
			addresses = append(addresses, address)
		}
	}
	sort.Strings(addresses)
	var out []ServiceState
	for _, address := range addresses {
		values, ok := r.paramsets[address][hmconst.ParamsetAttrValues]
		if !ok {
			continue
		}
		for _, parameter := range serviceParameters {
			value, present := values[parameter]
			if !present || !isRaised(value) || r.isSuppressedLocked(address, parameter) {
				continue
			}
			out = append(out, ServiceState{Address: address, Parameter: parameter, Value: value})
		}
	}
	return out
}
