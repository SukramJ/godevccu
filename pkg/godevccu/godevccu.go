// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package godevccu is the public façade of the virtual CCU simulator.
// Most consumers will only ever import this package — internals live
// behind internal/.
package godevccu

import (
	"github.com/SukramJ/godevccu/internal/devicelogic"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/state"
	"github.com/SukramJ/godevccu/internal/virtualccu"
)

// Re-export the protocol constants so callers do not need to reach
// into the internal/ tree.
const (
	Version            = hmconst.Version
	CCUFirmwareVersion = hmconst.CCUFirmwareVersion
	IPLocalhostV4      = hmconst.IPLocalhostV4
	IPAnyV4            = hmconst.IPAnyV4
	PortIP             = hmconst.PortIP
	PortRF             = hmconst.PortRF
	PortWired          = hmconst.PortWired
)

// BackendMode mirrors hmconst.BackendMode for consumers.
type BackendMode = hmconst.BackendMode

// Backend modes.
const (
	BackendModeHomegear = hmconst.BackendModeHomegear
	BackendModeCCU      = hmconst.BackendModeCCU
	BackendModeOpenCCU  = hmconst.BackendModeOpenCCU
)

// EphemeralPort, set on [Config.XMLRPCPort] or [Config.JSONRPCPort],
// asks [New] to bind that listener on an OS-assigned port. The
// resolved port is observable after [VirtualCCU.Start] via
// [VirtualCCU.XMLRPCAddr] / [VirtualCCU.JSONRPCAddr] or
// [VirtualCCU.Config], and is written back into the configuration so
// downstream consumers (e.g. JSON-RPC's `Interface.listInterfaces`)
// advertise the real port rather than the sentinel.
const EphemeralPort = virtualccu.EphemeralPort

// VirtualCCU is the orchestrator combining XML-RPC, JSON-RPC, ReGa and
// state management.
type VirtualCCU = virtualccu.VirtualCCU

// Config configures [New].
type Config = virtualccu.Config

// Defaults returns a sensible default Config.
func Defaults() Config { return virtualccu.Defaults() }

// New constructs a [VirtualCCU] using cfg.
func New(cfg Config) (*VirtualCCU, error) { return virtualccu.New(cfg) }

// LogicConfig configures the optional device behaviour simulators.
type LogicConfig = devicelogic.Config

// DefaultLogicConfig matches pydevccu's default startupdelay=5s,
// interval=60s.
func DefaultLogicConfig() LogicConfig { return devicelogic.DefaultConfig() }

// State manager re-exports.
type (
	// State is the in-memory simulation state.
	State = state.Manager
	// Program is a CCU program description.
	Program = state.Program
	// SystemVariable is a CCU system variable.
	SystemVariable = state.SystemVariable
	// Room is a CCU room.
	Room = state.Room
	// Function is a CCU function (Gewerk).
	Function = state.Function
	// ServiceMessage is a single CCU service message entry.
	ServiceMessage = state.ServiceMessage
	// InboxDevice is a device awaiting pairing approval.
	InboxDevice = state.InboxDevice
	// AddSystemVariableOpts mirrors the optional kwargs for
	// AddSystemVariable.
	AddSystemVariableOpts = state.AddSystemVariableOpts
)
