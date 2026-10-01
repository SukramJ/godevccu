// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package virtualccu

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
)

// Scenario API.
//
// What a test calls on the simulator to make an interface process
// misbehave and to look at what the client under test did. The set of
// calls follows hm-simulator's scenario API
// (https://github.com/hobbyquaker/hm-simulator, MIT, Sebastian Raff);
// the same calls are served over HTTP by [VirtualCCU.ControlHandler].
//
// An interface is named as in [Config.InterfacePorts]. The empty name
// is the main XML-RPC endpoint — the only one without InterfacePorts.

// ErrUnknownInterface reports an interface name the run did not start.
var ErrUnknownInterface = errors.New("virtualccu: unknown interface")

// errNotRunning reports a scenario call before Start or after Stop.
var errNotRunning = errors.New("virtualccu: not running")

// namedServer is one started XML-RPC server with its interface name.
type namedServer struct {
	name   string
	server *ccu.Server
}

// servers lists every started server: the main endpoint first, then the
// interface listeners in the CCU's order.
func (v *VirtualCCU) servers() []namedServer {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]namedServer, 0, len(v.interfaces)+1)
	if v.xmlrpc != nil {
		out = append(out, namedServer{name: "", server: v.xmlrpc})
	}
	for _, instance := range v.interfaces {
		out = append(out, namedServer{name: instance.name, server: instance.server})
	}
	return out
}

// serverFor resolves an interface name.
func (v *VirtualCCU) serverFor(iface string) (*ccu.Server, error) {
	all := v.servers()
	if len(all) == 0 {
		return nil, errNotRunning
	}
	for _, ns := range all {
		if ns.name == iface {
			return ns.server, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownInterface, iface)
}

// selectServers resolves an interface name, where "*" means every
// server.
func (v *VirtualCCU) selectServers(iface string) ([]*ccu.Server, error) {
	if iface != "*" {
		srv, err := v.serverFor(iface)
		if err != nil {
			return nil, err
		}
		return []*ccu.Server{srv}, nil
	}
	all := v.servers()
	if len(all) == 0 {
		return nil, errNotRunning
	}
	out := make([]*ccu.Server, len(all))
	for i, ns := range all {
		out[i] = ns.server
	}
	return out, nil
}

// InjectFault makes the next calls of a method on an interface
// misbehave; "*" applies the rule to every interface. See
// [ccu.FaultRule].
func (v *VirtualCCU) InjectFault(iface string, rule ccu.FaultRule) error {
	targets, err := v.selectServers(iface)
	if err != nil {
		return err
	}
	for _, srv := range targets {
		srv.InjectFault(rule)
	}
	return nil
}

// ClearFaults removes the injected rules of an interface ("*": of every
// interface) and lets every held call go without an answer.
func (v *VirtualCCU) ClearFaults(iface string) error {
	targets, err := v.selectServers(iface)
	if err != nil {
		return err
	}
	for _, srv := range targets {
		srv.ClearFaults()
	}
	return nil
}

// DropConnection restarts an interface process in an instant: every
// open connection is reset and every registered client forgotten.
// Nothing tells the clients; they receive no event until they call init
// again.
func (v *VirtualCCU) DropConnection(iface string) error {
	srv, err := v.serverFor(iface)
	if err != nil {
		return err
	}
	return srv.DropConnections()
}

// StopInterface stops an interface process: its ports refuse
// connections and open connections are reset. The registered clients
// stay known until [VirtualCCU.StartInterface].
func (v *VirtualCCU) StopInterface(iface string) error {
	srv, err := v.serverFor(iface)
	if err != nil {
		return err
	}
	return srv.Suspend()
}

// StartInterface starts a stopped interface process on the same ports.
// With forgetClients the registrations are gone; without, every
// remembered client is called back (system.listMethods on the BidCos
// interfaces, then listDevices and the device diff).
func (v *VirtualCCU) StartInterface(iface string, forgetClients bool) error {
	srv, err := v.serverFor(iface)
	if err != nil {
		return err
	}
	return srv.Resume(forgetClients)
}

// RestartInterface stops an interface process, keeps it down for the
// given span and starts it again; see [VirtualCCU.StartInterface].
func (v *VirtualCCU) RestartInterface(iface string, down time.Duration, forgetClients bool) error {
	srv, err := v.serverFor(iface)
	if err != nil {
		return err
	}
	if err := srv.Suspend(); err != nil {
		return err
	}
	if down > 0 {
		time.Sleep(down)
	}
	return srv.Resume(forgetClients)
}

// rpcs lists the RPC facades of every started server.
func (v *VirtualCCU) rpcs() []*ccu.RPCFunctions {
	all := v.servers()
	out := make([]*ccu.RPCFunctions, len(all))
	for i, ns := range all {
		out[i] = ns.server.RPC()
	}
	return out
}

// WriteLog returns every write a client made through setValue or
// putParamset on any interface, oldest first.
func (v *VirtualCCU) WriteLog() []ccu.WriteEntry {
	var out []ccu.WriteEntry
	for _, rpc := range v.rpcs() {
		out = append(out, rpc.WriteLog()...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// CallbackLog returns every call the simulator made to a registered
// client on any interface, oldest first.
func (v *VirtualCCU) CallbackLog() []ccu.CallbackEntry {
	var out []ccu.CallbackEntry
	for _, rpc := range v.rpcs() {
		out = append(out, rpc.CallbackLog()...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SentAt.Before(out[j].SentAt) })
	return out
}

// ClearLogs empties the write and callback logs of every interface.
func (v *VirtualCCU) ClearLogs() {
	for _, rpc := range v.rpcs() {
		rpc.ClearLogs()
	}
}

// ConfigPending lists the devices of an interface with a pending
// configuration raised by [Realism.MasterModel].
func (v *VirtualCCU) ConfigPending(iface string) ([]ccu.ConfigPendingEntry, error) {
	srv, err := v.serverFor(iface)
	if err != nil {
		return nil, err
	}
	return srv.RPC().ConfigPending(), nil
}

// PoisonedChannels lists the channels of an interface whose stored
// MASTER holds a parameter they do not have ([Realism.MasterModel],
// HmIP devices).
func (v *VirtualCCU) PoisonedChannels(iface string) ([]string, error) {
	srv, err := v.serverFor(iface)
	if err != nil {
		return nil, err
	}
	return srv.RPC().PoisonedChannels(), nil
}

// SetDeviceUnreachable marks a device unreachable or reachable again on
// the interface that serves it; see [ccu.RPCFunctions.SetDeviceUnreachable].
func (v *VirtualCCU) SetDeviceUnreachable(address string, unreach bool) error {
	rpc, err := v.rpcForAddress(address)
	if err != nil {
		return err
	}
	return rpc.SetDeviceUnreachable(address, unreach)
}

// rpcForAddress finds the interface that serves an address, preferring
// the interface listeners over the main endpoint, whose catalogue is
// the union.
func (v *VirtualCCU) rpcForAddress(address string) (*ccu.RPCFunctions, error) {
	all := v.rpcs()
	if len(all) == 0 {
		return nil, errNotRunning
	}
	for i := len(all) - 1; i >= 0; i-- {
		if _, err := all[i].GetDeviceDescription(address); err == nil {
			return all[i], nil
		}
	}
	return nil, fmt.Errorf("virtualccu: no interface serves %q", address)
}

// Ports reports the bound ports by name: "xmlrpc", "jsonrpc", "binrpc",
// "rega", the interface names of [Config.InterfacePorts], and
// "xmlrpc-tls"/"jsonrpc-tls" when TLS is on. A listener that is not
// running is left out.
func (v *VirtualCCU) Ports() map[string]int {
	out := make(map[string]int)
	put := func(name string, addr net.Addr) {
		if tcp, ok := addr.(*net.TCPAddr); ok && tcp != nil {
			out[name] = tcp.Port
		}
	}
	put("xmlrpc", v.XMLRPCAddr())
	put("jsonrpc", v.JSONRPCAddr())
	put("binrpc", v.BINRPCAddr())
	put("rega", v.RegaScriptAddr())
	put("xmlrpc-tls", v.XMLRPCTLSAddr())
	put("jsonrpc-tls", v.TLSAddr())
	v.mu.Lock()
	for _, instance := range v.interfaces {
		out[instance.name] = instance.port
	}
	v.mu.Unlock()
	return out
}
