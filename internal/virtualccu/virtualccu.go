// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package virtualccu wires the XML-RPC server, JSON-RPC server, ReGa
// engine and state manager into a single orchestrator. It is the Go
// equivalent of pydevccu/server.VirtualCCU.
package virtualccu

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/devicelogic"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/jsonrpc"
	"github.com/SukramJ/godevccu/internal/rega"
	"github.com/SukramJ/godevccu/internal/session"
	"github.com/SukramJ/godevccu/internal/state"
)

// EphemeralPort instructs [New] / [Start] to bind the corresponding
// listener on an OS-assigned port. The resolved port can be read back
// after [VirtualCCU.Start] via [VirtualCCU.XMLRPCAddr] / [VirtualCCU.JSONRPCAddr]
// or via [VirtualCCU.Config], and is written back into the underlying
// configuration so downstream consumers (e.g. JSON-RPC's
// `Interface.listInterfaces`) report the real port, not the sentinel.
//
// We use a negative sentinel rather than the canonical zero of
// [net.Listen] so that a freshly zero-valued [Config] continues to fall
// back to the canonical HomeMatic ports — that matches the historical
// behaviour and is friendlier to callers who construct Config{} without
// going through [Defaults].
const EphemeralPort = -1

// Config configures [New].
//
// Port fields support three values:
//   - 0  → fall back to the canonical default ([Defaults]).
//   - >0 → bind that exact TCP port.
//   - <0 (use [EphemeralPort]) → bind an OS-assigned port; the
//     resolved port is observable after [VirtualCCU.Start].
type Config struct {
	Mode          hmconst.BackendMode
	Host          string
	XMLRPCPort    int
	JSONRPCPort   int
	Username      string
	Password      string
	AuthEnabled   bool
	Devices       []string
	Persistence   bool
	Serial        string
	SetupDefaults bool
	EnableLogic   bool
	LogicConfig   devicelogic.Config
	Logger        *slog.Logger
}

// Defaults returns a Config with the canonical HomeMatic ports
// (RF = 2001 for XML-RPC, 80 for the CCU/OpenCCU web API).
func Defaults() Config {
	return Config{
		Mode:        hmconst.BackendModeOpenCCU,
		Host:        hmconst.IPLocalhostV4,
		XMLRPCPort:  hmconst.PortRF,
		JSONRPCPort: 80,
		Username:    "Admin",
		Password:    "",
		AuthEnabled: true,
		Serial:      "GODEVCCU0001",
	}
}

// VirtualCCU bundles the simulator services.
type VirtualCCU struct {
	cfg    Config
	logger *slog.Logger

	state   *state.Manager
	session *session.Manager

	xmlrpc  *ccu.Server
	jsonrpc *jsonrpc.Server
	rega    *rega.Engine

	mu      sync.Mutex
	running bool
}

// New constructs a VirtualCCU according to cfg. Defaults from
// [Defaults] are merged in for unset fields.
func New(cfg Config) (*VirtualCCU, error) {
	def := Defaults()
	if cfg.Host == "" {
		cfg.Host = def.Host
	}
	switch {
	case cfg.XMLRPCPort == 0:
		cfg.XMLRPCPort = def.XMLRPCPort
	case cfg.XMLRPCPort < 0:
		cfg.XMLRPCPort = 0 // ephemeral: let the OS pick at bind time
	}
	switch {
	case cfg.JSONRPCPort == 0:
		cfg.JSONRPCPort = def.JSONRPCPort
	case cfg.JSONRPCPort < 0:
		cfg.JSONRPCPort = 0
	}
	if cfg.Username == "" {
		cfg.Username = def.Username
	}
	if cfg.Mode == 0 {
		cfg.Mode = def.Mode
	}
	if cfg.Serial == "" {
		cfg.Serial = def.Serial
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	stateMgr := state.New(cfg.Mode, cfg.Serial)
	if cfg.SetupDefaults {
		state.SetupDefaults(stateMgr)
	}
	sessionMgr := session.New(cfg.Username, cfg.Password, 0, cfg.AuthEnabled)

	return &VirtualCCU{
		cfg:     cfg,
		logger:  logger,
		state:   stateMgr,
		session: sessionMgr,
	}, nil
}

// State returns the underlying state manager (handy for fixtures).
func (v *VirtualCCU) State() *state.Manager { return v.state }

// Session returns the session manager.
func (v *VirtualCCU) Session() *session.Manager { return v.session }

// RPC returns the XML-RPC RPCFunctions facade. Only valid after Start.
func (v *VirtualCCU) RPC() *ccu.RPCFunctions {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.xmlrpc == nil {
		return nil
	}
	return v.xmlrpc.RPC()
}

// XMLRPCAddr returns the local XML-RPC address (only after Start).
func (v *VirtualCCU) XMLRPCAddr() net.Addr {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.xmlrpc == nil {
		return nil
	}
	return v.xmlrpc.LocalAddr()
}

// JSONRPCAddr returns the local JSON-RPC address (only after Start).
func (v *VirtualCCU) JSONRPCAddr() net.Addr {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.jsonrpc == nil {
		return nil
	}
	return v.jsonrpc.LocalAddr()
}

// Mode returns the configured backend mode.
func (v *VirtualCCU) Mode() hmconst.BackendMode { return v.cfg.Mode }

// Config returns a copy of the resolved configuration. After [Start]
// the XMLRPCPort / JSONRPCPort fields reflect the real bound port,
// even if the caller passed [EphemeralPort].
func (v *VirtualCCU) Config() Config {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cfg
}

// IsRunning reports whether Start was called and Stop hasn't been
// invoked yet.
func (v *VirtualCCU) IsRunning() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.running
}

// Start launches the XML-RPC server (always) and the JSON-RPC server
// (in CCU/OpenCCU mode).
func (v *VirtualCCU) Start() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.running {
		return errors.New("virtualccu: already running")
	}
	version := ""
	if v.cfg.Mode != hmconst.BackendModeHomegear {
		version = hmconst.CCUFirmwareVersion
	}
	rpcFns, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:     v.cfg.Devices,
		Persistence: v.cfg.Persistence,
		Version:     version,
		Logger:      v.logger,
	})
	if err != nil {
		return err
	}
	v.xmlrpc = ccu.NewServer(ccu.ServerConfig{
		Address:     net.JoinHostPort(v.cfg.Host, strconv.Itoa(v.cfg.XMLRPCPort)),
		Logger:      v.logger,
		RPC:         rpcFns,
		EnableLogic: v.cfg.EnableLogic,
		LogicConfig: v.cfg.LogicConfig,
	})
	if err := v.xmlrpc.Start(); err != nil {
		return fmt.Errorf("virtualccu: xml-rpc start: %w", err)
	}
	// When the caller asked for an ephemeral port (cfg.XMLRPCPort == 0
	// at this point), the listener now knows the real number — write it
	// back so listInterfaces and the JSON-RPC handler advertise the
	// actual XML-RPC endpoint, not 0.
	if addr, ok := v.xmlrpc.LocalAddr().(*net.TCPAddr); ok && addr != nil {
		v.cfg.XMLRPCPort = addr.Port
	}

	v.rega = rega.New(v.state, rpcFns)

	if v.cfg.Mode != hmconst.BackendModeHomegear {
		handlers := jsonrpc.NewHandlers(v.state, v.session, rpcFns, v.rega, v.cfg.XMLRPCPort)
		v.jsonrpc = jsonrpc.NewServer(jsonrpc.Config{
			Address:  net.JoinHostPort(v.cfg.Host, strconv.Itoa(v.cfg.JSONRPCPort)),
			Handlers: handlers,
			Logger:   v.logger,
		})
		if err := v.jsonrpc.Start(); err != nil {
			_ = v.xmlrpc.Stop()
			return fmt.Errorf("virtualccu: json-rpc start: %w", err)
		}
		if addr, ok := v.jsonrpc.LocalAddr().(*net.TCPAddr); ok && addr != nil {
			v.cfg.JSONRPCPort = addr.Port
		}
	}
	v.running = true
	return nil
}

// Stop shuts both servers down.
func (v *VirtualCCU) Stop() error {
	v.mu.Lock()
	if !v.running {
		v.mu.Unlock()
		return nil
	}
	v.running = false
	xmlSrv := v.xmlrpc
	jsonSrv := v.jsonrpc
	v.xmlrpc = nil
	v.jsonrpc = nil
	v.mu.Unlock()

	var firstErr error
	if jsonSrv != nil {
		if err := jsonSrv.Stop(); err != nil {
			firstErr = err
		}
	}
	if xmlSrv != nil {
		if err := xmlSrv.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
