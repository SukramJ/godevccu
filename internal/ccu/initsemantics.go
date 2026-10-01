// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package ccu

import (
	"context"
	"fmt"

	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// init() as the interface processes handle it.
//
// pydevccu keys a registration by its interface id and removes one with
// init(url) by a substring match on the url, in both directions — so
// init("http://h:1", "") also removes "http://h:10", and two logic
// layers that share an interface id overwrite each other. After the
// registration it pushes newDevices before deleteDevices and never
// compares versions. That stays the default for pydevccu parity.
//
// With [RPCFunctions.EnableInitSemantics] the registration follows what
// hm-simulator (https://github.com/hobbyquaker/hm-simulator, MIT,
// Sebastian Raff) documents for rfd and hmipserver in its README
// ("Connecting a client") and lib/sim.js (init, checkDevices):
//
//   - a registration is identified by its url exactly, scheme and path
//     included; init(url, "") removes only that url
//   - deleteDevices goes out before newDevices
//   - a device whose VERSION differs from the client's copy is deleted
//     and sent again
//   - an HmIP device is deleted and sent again on every init, whether
//     the client knew it or not (hm-simulator cites
//     https://github.com/eq-3/occu/issues/45 for that)
//
// None of this was measured for godevccu; the source is hm-simulator.

// attrVersion is the description field a client's copy is compared by.
const attrVersion = "VERSION"

// EnableInitSemantics switches init() to the interface processes'
// behaviour. Call before any client registers.
func (r *RPCFunctions) EnableInitSemantics() {
	r.mu.Lock()
	r.initSemantics = true
	r.mu.Unlock()
}

// reconcileDevices is the device diff under init semantics: deletions
// first, then the devices the client lacks, has in another VERSION, or —
// for HmIP — has at all.
func (r *RPCFunctions) reconcileDevices(reg registration, known []map[string]any) {
	r.mu.Lock()
	clientDevices := make(map[string]map[string]any, len(known))
	for _, d := range known {
		if addr, _ := d[hmconst.AttrAddress].(string); addr != "" {
			clientDevices[addr] = d
		}
	}
	ours := make(map[string]struct{}, len(r.devices))
	var newList []map[string]any
	var deleteList []string
	for _, d := range r.devices {
		addr, _ := d[hmconst.AttrAddress].(string)
		ours[addr] = struct{}{}
		clientCopy, knows := clientDevices[addr]
		switch {
		case !knows:
			newList = append(newList, d)
		case hmconst.InterfaceForType(deviceTypeOf(d)) == hmconst.InterfaceHmIPRF,
			fmt.Sprint(d[attrVersion]) != fmt.Sprint(clientCopy[attrVersion]):
			deleteList = append(deleteList, addr)
			newList = append(newList, d)
		}
	}
	for _, d := range known {
		addr, _ := d[hmconst.AttrAddress].(string)
		if _, ok := ours[addr]; !ok && addr != "" {
			deleteList = append(deleteList, addr)
		}
	}
	r.mu.Unlock()

	ctx := context.Background()
	if len(deleteList) > 0 {
		params := []xmlrpc.Value{xmlrpc.StringValue(reg.interfaceID), xmlrpc.FromAny(any(deleteList))}
		if _, err := reg.client.Call(ctx, "deleteDevices", params); err != nil {
			r.logger.Debug("ccu: deleteDevices push failed", "interface", reg.interfaceID, "err", err)
		}
	}
	if len(newList) > 0 {
		params := []xmlrpc.Value{xmlrpc.StringValue(reg.interfaceID), xmlrpc.FromAny(any(toAnySlice(newList)))}
		if _, err := reg.client.Call(ctx, "newDevices", params); err != nil {
			r.logger.Debug("ccu: newDevices push failed", "interface", reg.interfaceID, "err", err)
		}
	}
}

// deviceTypeOf returns the type a description belongs to: TYPE on a
// device, PARENT_TYPE on a channel.
func deviceTypeOf(d map[string]any) string {
	if pt, _ := d[hmconst.AttrParentType].(string); pt != "" {
		return pt
	}
	t, _ := d[hmconst.AttrType].(string)
	return t
}

// ForgetClients drops every registration, as an interface process that
// restarts without a list of its clients does: nothing tells them, and
// no event reaches them until they call init again.
func (r *RPCFunctions) ForgetClients() {
	r.mu.Lock()
	r.remotes = make(map[string]registration)
	dispatchers := r.dispatchers
	r.dispatchers = make(map[string]*dispatcher)
	r.mu.Unlock()
	for _, d := range dispatchers {
		d.stop()
	}
}

// ReannounceClients calls every remembered client back the way rfd does
// when it starts with its handlers file: system.listMethods on the
// BidCos interfaces, then listDevices and the device diff as after an
// init. A client sees calls it did not ask for, which is how it can
// tell that the process restarted. The sequence is hm-simulator's
// (lib/sim.js startInterface); it runs in the background.
func (r *RPCFunctions) ReannounceClients() {
	r.mu.Lock()
	remotes := r.snapshotRemotesLocked()
	bidcos := r.interfaceFilter == hmconst.InterfaceBidCosRF || r.interfaceFilter == hmconst.InterfaceBidCosWired
	r.mu.Unlock()
	for _, e := range remotes {
		go func(e remoteEntry) {
			if bidcos {
				if _, err := e.client.Call(context.Background(), "system.listMethods",
					[]xmlrpc.Value{xmlrpc.StringValue(e.interfaceID)}); err != nil {
					r.logger.Debug("ccu: system.listMethods on restart failed", "interface", e.interfaceID, "err", err)
				}
			}
			r.askDevices(e.key)
		}(e)
	}
}
