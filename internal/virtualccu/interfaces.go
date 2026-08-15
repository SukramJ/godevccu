// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package virtualccu

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/jsonrpc"
	"github.com/SukramJ/godevccu/internal/ssdp"
)

// Separate interface listeners.
//
// A CCU is not one process serving every device: rfd serves BidCos-RF
// on 2001, the HMIPServer serves HomeMatic IP on 2010, hs485d serves
// the wired bus on 2000, and the group interface answers on 9292 under
// the /groups path. Each keeps its own callback registry and answers
// listDevices with only its own devices — a client connects to one of
// them and expects exactly that subset.
//
// The simulator serves everything from a single endpoint, and
// listInterfaces advertises several names on that one port. With
// [Config.InterfacePorts] set it builds one instance per interface
// instead: own listener, own device partition, own callback registry.

// interfaceInstance is one simulated interface process.
type interfaceInstance struct {
	name   string
	server *ccu.Server
	rpc    *ccu.RPCFunctions
	port   int
}

// startInterfaces builds and starts one listener per configured
// interface. It returns the started instances in the CCU's listing
// order; the caller stops them on failure.
func (v *VirtualCCU) startInterfaces(version string) ([]*interfaceInstance, error) {
	names := make([]string, 0, len(v.cfg.InterfacePorts))
	for name := range v.cfg.InterfacePorts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return interfaceRank(names[i]) < interfaceRank(names[j])
	})

	instances := make([]*interfaceInstance, 0, len(names))
	for _, name := range names {
		instance, err := v.startInterface(name, v.cfg.InterfacePorts[name], version)
		if err != nil {
			for _, started := range instances {
				_ = started.server.Stop()
			}
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

// startInterface builds a single interface listener.
func (v *VirtualCCU) startInterface(name string, port int, version string) (*interfaceInstance, error) {
	if port == 0 {
		port = hmconst.DefaultInterfacePorts[name]
	}
	bindPort := port
	if bindPort < 0 {
		bindPort = 0
	}

	rpcFns, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:         v.cfg.Devices,
		Persistence:     v.cfg.Persistence,
		Version:         version,
		Logger:          v.logger,
		OnSetValue:      v.cfg.OnSetValue,
		InterfaceID:     name,
		InterfaceFilter: name,
		NormalizeData:   v.cfg.Realism.NormalizeData,
	})
	if err != nil {
		return nil, fmt.Errorf("virtualccu: interface %s: %w", name, err)
	}
	v.applyRealism(rpcFns)

	srv := ccu.NewServer(ccu.ServerConfig{
		Address:     net.JoinHostPort(v.cfg.Host, strconv.Itoa(bindPort)),
		Logger:      v.logger,
		RPC:         rpcFns,
		EnableLogic: v.cfg.EnableLogic,
		LogicConfig: v.cfg.LogicConfig,
	})
	srv.SetReady(!v.cfg.StartNotReady)
	v.applyServerRealism(srv)
	if err := srv.Start(); err != nil {
		return nil, fmt.Errorf("virtualccu: interface %s: %w", name, err)
	}
	if addr, ok := srv.LocalAddr().(*net.TCPAddr); ok && addr != nil {
		port = addr.Port
		v.cfg.InterfacePorts[name] = port
	}
	if v.cfg.TLS.Enabled {
		if err := srv.StartTLS(net.JoinHostPort(v.cfg.Host, strconv.Itoa(tlsBindPort(v.cfg.TLS, port))), v.tlsCert, v.tlsKey); err != nil {
			_ = srv.Stop()
			return nil, err
		}
	}
	return &interfaceInstance{name: name, server: srv, rpc: rpcFns, port: port}, nil
}

// tlsBindPort resolves the TLS port for a plaintext interface port,
// mapping the ephemeral sentinel onto the OS-assigned 0.
func tlsBindPort(cfg TLSConfig, plaintext int) int {
	return bindablePort(cfg.xmlRPCPort(plaintext))
}

// bindablePort turns the ephemeral sentinel into the 0 that net.Listen
// understands.
func bindablePort(port int) int {
	if port < 0 {
		return 0
	}
	return port
}

// interfaceRank orders interfaces the way a CCU lists them; unknown
// names sort last, in name order.
func interfaceRank(name string) int {
	for i, known := range hmconst.InterfaceOrder {
		if known == name {
			return i
		}
	}
	return len(hmconst.InterfaceOrder)
}

// interfaceInventory renders the started interfaces for
// Interface.listInterfaces.
func (v *VirtualCCU) interfaceInventory() []jsonrpc.InterfaceInfo {
	out := make([]jsonrpc.InterfaceInfo, 0, len(v.interfaces))
	for _, instance := range v.interfaces {
		info := hmconst.InterfaceDescriptions[instance.name]
		if info == "" {
			info = instance.name
		}
		out = append(out, jsonrpc.InterfaceInfo{
			Name: instance.name,
			Port: instance.port,
			Info: info,
		})
	}
	return out
}

// applyRealism switches on the opted-in behaviours of an RPC instance.
func (v *VirtualCCU) applyRealism(rpcFns *ccu.RPCFunctions) {
	if v.cfg.Realism.Reachability {
		rpcFns.EnableReachability(0)
	}
	if v.cfg.Realism.ServiceMessages {
		rpcFns.EnableServiceMessages()
	}
	if v.cfg.Realism.BatchEvents {
		rpcFns.EnableBatchEvents()
	}
	if v.cfg.Realism.PersistInit {
		rpcFns.EnableInitPersistence()
	}
	if v.cfg.Realism.Lifecycle {
		rpcFns.EnableLifecycle(0)
	}
	if v.cfg.Realism.Ramps {
		rpcFns.EnableRamps(0)
	}
}

// applyServerRealism switches on the behaviours that live on the
// listener rather than on the RPC facade.
func (v *VirtualCCU) applyServerRealism(srv *ccu.Server) {
	// Basic auth only means anything with authentication configured —
	// a CCU includes its auth block only when authEnabled is set.
	if v.cfg.Realism.BasicAuth && v.cfg.AuthEnabled {
		srv.EnableBasicAuth(v.session.CheckCredentials)
	}
	srv.EnableFaultCodes(v.cfg.Realism.FaultCodes)
}

// backupCompletionDelay is how long a simulated backup runs before it
// reports "completed". Short enough for a test to await, long enough
// that a client observes the running state at all.
const backupCompletionDelay = 250 * time.Millisecond

// stopInterfaces shuts every per-protocol listener down.
func (v *VirtualCCU) stopInterfaces() {
	for _, instance := range v.interfaces {
		_ = instance.server.StopTLS()
		_ = instance.server.Stop()
	}
	v.interfaces = nil
}

// startDiscovery brings the SSDP responder up, pointing at the web
// API's UPnP description.
func (v *VirtualCCU) startDiscovery() error {
	location := fmt.Sprintf("http://%s/upnp/basic_dev.cgi",
		net.JoinHostPort(v.cfg.Host, strconv.Itoa(v.cfg.JSONRPCPort)))
	responder := ssdp.New(ssdp.Config{
		Location: location,
		UDN:      "uuid:" + upnpUUID(v.state.Serial()),
		Server:   "godevccu/" + hmconst.CCUFirmwareVersion + " UPnP/1.0",
		Logger:   v.logger,
	})
	if err := responder.Start(); err != nil {
		return err
	}
	v.ssdp = responder
	return nil
}

// upnpUUID derives the announced UDN from the serial, matching what the
// device description reports.
func upnpUUID(serial string) string {
	padded := serial
	for len(padded) < 12 {
		padded += "0"
	}
	return "30303030-3030-3030-3030-" + padded[:12]
}
