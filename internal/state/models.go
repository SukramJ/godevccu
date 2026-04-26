// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package state holds the in-memory simulation state of the virtual
// CCU: programs, system variables, rooms, functions, service messages,
// inbox devices, backup status, firmware update state and per-channel
// device values. The data structures mirror those of pydevccu.state.
package state

import (
	"time"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// BackendInfo carries the strings the JSON-RPC backend exposes via
// /VERSION and the get_backend_info ReGa script.
type BackendInfo struct {
	Version   string
	Product   string
	Hostname  string
	IsHaAddon bool
}

// NewBackendInfo seeds the struct with sensible defaults for the given
// backend mode.
func NewBackendInfo(mode hmconst.BackendMode) BackendInfo {
	product := "CCU"
	if mode == hmconst.BackendModeOpenCCU {
		product = "OpenCCU"
	}
	return BackendInfo{
		Version:  hmconst.CCUFirmwareVersion,
		Product:  product,
		Hostname: "godevccu",
	}
}

// Program is a CCU program definition.
type Program struct {
	ID              int
	Name            string
	Description     string
	Active          bool
	LastExecuteTime float64
}

// SystemVariable is a CCU system variable.
type SystemVariable struct {
	ID          int
	Name        string
	VarType     string // BOOL, FLOAT, STRING, ENUM
	Value       any
	Description string
	Unit        string
	ValueList   string // semicolon-separated for ENUM
	MinValue    float64
	MaxValue    float64
	Timestamp   float64
}

// Room is a CCU room with the channels assigned to it.
type Room struct {
	ID          int
	Name        string
	Description string
	ChannelIDs  []string
}

// Function (Gewerk) groups channels by purpose.
type Function struct {
	ID          int
	Name        string
	Description string
	ChannelIDs  []string
}

// ServiceMessage represents a single entry in the CCU service message
// queue (CONFIG_PENDING, LOWBAT, UNREACH, …).
type ServiceMessage struct {
	ID         int
	Name       string
	Timestamp  float64
	MsgType    string
	Address    string
	DeviceName string
}

// InboxDevice represents a device awaiting pairing approval.
type InboxDevice struct {
	DeviceID   string
	Address    string
	Name       string
	DeviceType string
	Interface  string
}

// BackupStatus tracks an in-flight CCU backup operation.
type BackupStatus struct {
	Status   string // idle | running | completed | failed
	PID      string
	Filename string
	Filepath string
	Size     int
}

// UpdateInfo carries the firmware update state shown by the CCU UI.
type UpdateInfo struct {
	CurrentFirmware   string
	AvailableFirmware string
	UpdateAvailable   bool
}

// NewUpdateInfo returns the default firmware update state ("up-to-date").
func NewUpdateInfo() UpdateInfo {
	return UpdateInfo{
		CurrentFirmware:   hmconst.CCUFirmwareVersion,
		AvailableFirmware: hmconst.CCUFirmwareVersion,
		UpdateAvailable:   false,
	}
}

// nowFloat returns the current Unix time as a float, matching the
// representation pydevccu uses for timestamps.
func nowFloat() float64 {
	return float64(time.Now().UnixNano()) / float64(time.Second)
}
