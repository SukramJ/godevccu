// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package state

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// SysVarCallback is fired when a system variable value changes.
type SysVarCallback func(name string, value any)

// ProgramCallback is fired when a program is executed.
type ProgramCallback func(id int, success bool)

// Manager owns every piece of mutable simulation state. Methods are
// safe for concurrent use; the public API mirrors pydevccu/state.
type Manager struct {
	mu sync.RWMutex

	mode   hmconst.BackendMode
	serial string

	backendInfo BackendInfo

	programs      map[int]*Program
	nextProgramID int

	sysvars      map[int]*SystemVariable
	sysvarByName map[string]*SystemVariable
	nextSysVarID int

	rooms          map[int]*Room
	nextRoomID     int
	functions      map[int]*Function
	nextFunctionID int

	serviceMessages []*ServiceMessage
	nextServiceID   int

	inboxDevices []*InboxDevice

	backupStatus BackupStatus
	backupData   []byte

	updateInfo UpdateInfo

	deviceValues map[string]any
	deviceNames  map[string]string

	sysvarCallbacks  []SysVarCallback
	programCallbacks []ProgramCallback
}

// New returns an empty Manager.
func New(mode hmconst.BackendMode, serial string) *Manager {
	return &Manager{
		mode:            mode,
		serial:          serial,
		backendInfo:     NewBackendInfo(mode),
		programs:        make(map[int]*Program),
		nextProgramID:   1000,
		sysvars:         make(map[int]*SystemVariable),
		sysvarByName:    make(map[string]*SystemVariable),
		nextSysVarID:    2000,
		rooms:           make(map[int]*Room),
		nextRoomID:      3000,
		functions:       make(map[int]*Function),
		nextFunctionID:  4000,
		serviceMessages: make([]*ServiceMessage, 0),
		nextServiceID:   5000,
		inboxDevices:    make([]*InboxDevice, 0),
		backupStatus:    BackupStatus{Status: "idle"},
		updateInfo:      NewUpdateInfo(),
		deviceValues:    make(map[string]any),
		deviceNames:     make(map[string]string),
	}
}

// Mode returns the configured backend mode.
func (m *Manager) Mode() hmconst.BackendMode { return m.mode }

// ─────────────────────────────────────────────────────────────────
// Backend Info
// ─────────────────────────────────────────────────────────────────

// BackendInfo returns a copy of the backend info struct.
func (m *Manager) BackendInfo() BackendInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backendInfo
}

// SetBackendInfo updates the backend info.
func (m *Manager) SetBackendInfo(info BackendInfo) {
	m.mu.Lock()
	m.backendInfo = info
	m.mu.Unlock()
}

// Serial returns the last 10 chars of the configured serial number, as
// the CCU reports it.
func (m *Manager) Serial() string {
	if len(m.serial) <= 10 {
		return m.serial
	}
	return m.serial[len(m.serial)-10:]
}

// ─────────────────────────────────────────────────────────────────
// Programs
// ─────────────────────────────────────────────────────────────────

// AddProgram inserts a program. When id is zero an auto-incrementing
// id is assigned. Returns the inserted program (with id populated).
func (m *Manager) AddProgram(name, description string, active bool, id int) *Program {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == 0 {
		id = m.nextProgramID
		m.nextProgramID++
	}
	p := &Program{ID: id, Name: name, Description: description, Active: active}
	m.programs[id] = p
	return p
}

// Programs returns a copy of all programs.
func (m *Manager) Programs() []*Program {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Program, 0, len(m.programs))
	for _, p := range m.programs {
		copy := *p
		out = append(out, &copy)
	}
	return out
}

// Program returns the program with the given id.
func (m *Manager) Program(id int) (*Program, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.programs[id]
	if !ok {
		return nil, false
	}
	c := *p
	return &c, true
}

// ProgramByName returns the program with the given name.
func (m *Manager) ProgramByName(name string) (*Program, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.programs {
		if p.Name == name {
			c := *p
			return &c, true
		}
	}
	return nil, false
}

// ExecuteProgram marks the program as executed and fires callbacks.
func (m *Manager) ExecuteProgram(id int) bool {
	m.mu.Lock()
	p, ok := m.programs[id]
	if !ok || !p.Active {
		m.mu.Unlock()
		return false
	}
	p.LastExecuteTime = nowFloat()
	cbs := append([]ProgramCallback(nil), m.programCallbacks...)
	m.mu.Unlock()
	for _, cb := range cbs {
		safeCallProgram(cb, id, true)
	}
	return true
}

// SetProgramActive enables or disables a program.
func (m *Manager) SetProgramActive(id int, active bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.programs[id]
	if !ok {
		return false
	}
	p.Active = active
	return true
}

// DeleteProgram removes a program.
func (m *Manager) DeleteProgram(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.programs[id]; !ok {
		return false
	}
	delete(m.programs, id)
	return true
}

// ─────────────────────────────────────────────────────────────────
// System variables
// ─────────────────────────────────────────────────────────────────

// AddSystemVariableOpts is the bag of optional parameters for
// AddSystemVariable.
type AddSystemVariableOpts struct {
	Description string
	Unit        string
	ValueList   string
	MinValue    float64
	MaxValue    float64
	ID          int
}

// AddSystemVariable inserts a sysvar.
func (m *Manager) AddSystemVariable(name, varType string, value any, opts AddSystemVariableOpts) *SystemVariable {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := opts.ID
	if id == 0 {
		id = m.nextSysVarID
		m.nextSysVarID++
	}
	sv := &SystemVariable{
		ID:          id,
		Name:        name,
		VarType:     varType,
		Value:       value,
		Description: opts.Description,
		Unit:        opts.Unit,
		ValueList:   opts.ValueList,
		MinValue:    opts.MinValue,
		MaxValue:    opts.MaxValue,
		Timestamp:   nowFloat(),
	}
	m.sysvars[id] = sv
	m.sysvarByName[name] = sv
	return sv
}

// SystemVariables returns a slice with the current sysvar set.
func (m *Manager) SystemVariables() []*SystemVariable {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*SystemVariable, 0, len(m.sysvars))
	for _, sv := range m.sysvars {
		c := *sv
		out = append(out, &c)
	}
	return out
}

// SystemVariable returns the named sysvar.
func (m *Manager) SystemVariable(name string) (*SystemVariable, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sv, ok := m.sysvarByName[name]
	if !ok {
		return nil, false
	}
	c := *sv
	return &c, true
}

// SystemVariableByID returns the sysvar with the given id.
func (m *Manager) SystemVariableByID(id int) (*SystemVariable, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sv, ok := m.sysvars[id]
	if !ok {
		return nil, false
	}
	c := *sv
	return &c, true
}

// SetSystemVariable updates the named sysvar value.
func (m *Manager) SetSystemVariable(name string, value any) bool {
	m.mu.Lock()
	sv, ok := m.sysvarByName[name]
	if !ok {
		m.mu.Unlock()
		return false
	}
	sv.Value = value
	sv.Timestamp = nowFloat()
	cbs := append([]SysVarCallback(nil), m.sysvarCallbacks...)
	m.mu.Unlock()
	for _, cb := range cbs {
		safeCallSysVar(cb, name, value)
	}
	return true
}

// DeleteSystemVariable removes the named sysvar.
func (m *Manager) DeleteSystemVariable(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	sv, ok := m.sysvarByName[name]
	if !ok {
		return false
	}
	delete(m.sysvars, sv.ID)
	delete(m.sysvarByName, name)
	return true
}

// ─────────────────────────────────────────────────────────────────
// Rooms & functions
// ─────────────────────────────────────────────────────────────────

// AddRoom inserts a room. When id is zero an auto-incrementing id is
// assigned.
func (m *Manager) AddRoom(name, description string, channelIDs []string, id int) *Room {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == 0 {
		id = m.nextRoomID
		m.nextRoomID++
	}
	r := &Room{ID: id, Name: name, Description: description, ChannelIDs: append([]string(nil), channelIDs...)}
	m.rooms[id] = r
	return r
}

// Rooms returns all rooms.
func (m *Manager) Rooms() []*Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		c := *r
		c.ChannelIDs = append([]string(nil), r.ChannelIDs...)
		out = append(out, &c)
	}
	return out
}

// Room returns a single room.
func (m *Manager) Room(id int) (*Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[id]
	if !ok {
		return nil, false
	}
	c := *r
	c.ChannelIDs = append([]string(nil), r.ChannelIDs...)
	return &c, true
}

// AddChannelToRoom appends a channel id to the given room.
func (m *Manager) AddChannelToRoom(roomID int, channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return false
	}
	for _, c := range r.ChannelIDs {
		if c == channelID {
			return true
		}
	}
	r.ChannelIDs = append(r.ChannelIDs, channelID)
	return true
}

// RemoveChannelFromRoom removes a channel id from the given room.
func (m *Manager) RemoveChannelFromRoom(roomID int, channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return false
	}
	for i, c := range r.ChannelIDs {
		if c == channelID {
			r.ChannelIDs = append(r.ChannelIDs[:i], r.ChannelIDs[i+1:]...)
			return true
		}
	}
	return true
}

// AddFunction inserts a function (Gewerk).
func (m *Manager) AddFunction(name, description string, channelIDs []string, id int) *Function {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == 0 {
		id = m.nextFunctionID
		m.nextFunctionID++
	}
	f := &Function{ID: id, Name: name, Description: description, ChannelIDs: append([]string(nil), channelIDs...)}
	m.functions[id] = f
	return f
}

// Functions returns all functions.
func (m *Manager) Functions() []*Function {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Function, 0, len(m.functions))
	for _, f := range m.functions {
		c := *f
		c.ChannelIDs = append([]string(nil), f.ChannelIDs...)
		out = append(out, &c)
	}
	return out
}

// Function returns a single function.
func (m *Manager) Function(id int) (*Function, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.functions[id]
	if !ok {
		return nil, false
	}
	c := *f
	c.ChannelIDs = append([]string(nil), f.ChannelIDs...)
	return &c, true
}

// AddChannelToFunction appends a channel id to the given function.
func (m *Manager) AddChannelToFunction(functionID int, channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.functions[functionID]
	if !ok {
		return false
	}
	for _, c := range f.ChannelIDs {
		if c == channelID {
			return true
		}
	}
	f.ChannelIDs = append(f.ChannelIDs, channelID)
	return true
}

// ─────────────────────────────────────────────────────────────────
// Service messages
// ─────────────────────────────────────────────────────────────────

// AddServiceMessage stores a service-message entry.
func (m *Manager) AddServiceMessage(name, msgType, address, deviceName string) *ServiceMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := &ServiceMessage{
		ID:         m.nextServiceID,
		Name:       name,
		Timestamp:  nowFloat(),
		MsgType:    msgType,
		Address:    address,
		DeviceName: deviceName,
	}
	m.nextServiceID++
	m.serviceMessages = append(m.serviceMessages, msg)
	return msg
}

// ServiceMessages returns a snapshot of all active service messages.
func (m *Manager) ServiceMessages() []*ServiceMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ServiceMessage, len(m.serviceMessages))
	for i, msg := range m.serviceMessages {
		c := *msg
		out[i] = &c
	}
	return out
}

// ClearServiceMessage removes the message with the given id.
func (m *Manager) ClearServiceMessage(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, msg := range m.serviceMessages {
		if msg.ID == id {
			m.serviceMessages = append(m.serviceMessages[:i], m.serviceMessages[i+1:]...)
			return true
		}
	}
	return false
}

// ClearAllServiceMessages drops every queued message and returns the
// number that were cleared.
func (m *Manager) ClearAllServiceMessages() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.serviceMessages)
	m.serviceMessages = m.serviceMessages[:0]
	return n
}

// ─────────────────────────────────────────────────────────────────
// Inbox devices
// ─────────────────────────────────────────────────────────────────

// AddInboxDevice queues a device pending pairing approval.
func (m *Manager) AddInboxDevice(address, name, deviceType, iface string) *InboxDevice {
	m.mu.Lock()
	defer m.mu.Unlock()
	dev := &InboxDevice{
		DeviceID:   randomID(8),
		Address:    address,
		Name:       name,
		DeviceType: deviceType,
		Interface:  iface,
	}
	m.inboxDevices = append(m.inboxDevices, dev)
	return dev
}

// InboxDevices returns all queued devices.
func (m *Manager) InboxDevices() []*InboxDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*InboxDevice, len(m.inboxDevices))
	for i, d := range m.inboxDevices {
		c := *d
		out[i] = &c
	}
	return out
}

// AcceptInboxDevice removes a device from the inbox; rejection uses the
// same operation in pydevccu, mirrored here.
func (m *Manager) AcceptInboxDevice(address string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, d := range m.inboxDevices {
		if d.Address == address {
			m.inboxDevices = append(m.inboxDevices[:i], m.inboxDevices[i+1:]...)
			return true
		}
	}
	return false
}

// RejectInboxDevice is an alias for [AcceptInboxDevice].
func (m *Manager) RejectInboxDevice(address string) bool { return m.AcceptInboxDevice(address) }

// ─────────────────────────────────────────────────────────────────
// Backup
// ─────────────────────────────────────────────────────────────────

// StartBackup transitions the backup state machine to "running" and
// returns the freshly minted PID.
func (m *Manager) StartBackup() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	pid := randomID(4)
	m.backupStatus = BackupStatus{Status: "running", PID: pid}
	return pid
}

// CompleteBackup stores backup data and marks the operation complete.
func (m *Manager) CompleteBackup(data []byte, filename string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backupData = append([]byte(nil), data...)
	m.backupStatus = BackupStatus{
		Status:   "completed",
		Filename: filename,
		Filepath: "/tmp/" + filename,
		Size:     len(data),
	}
}

// FailBackup marks the operation as failed.
func (m *Manager) FailBackup(_ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backupStatus = BackupStatus{Status: "failed"}
}

// BackupStatus returns the current backup status.
func (m *Manager) BackupStatus() BackupStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.backupStatus
}

// BackupData returns the backup payload (empty before completion).
func (m *Manager) BackupData() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]byte(nil), m.backupData...)
}

// ResetBackup clears the backup state.
func (m *Manager) ResetBackup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backupStatus = BackupStatus{Status: "idle"}
	m.backupData = nil
}

// ─────────────────────────────────────────────────────────────────
// Firmware update
// ─────────────────────────────────────────────────────────────────

// SetUpdateInfo seeds firmware update metadata.
func (m *Manager) SetUpdateInfo(current, available string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateInfo = UpdateInfo{
		CurrentFirmware:   current,
		AvailableFirmware: available,
		UpdateAvailable:   current != available,
	}
}

// UpdateInfo returns the current firmware update state.
func (m *Manager) UpdateInfo() UpdateInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.updateInfo
}

// TriggerUpdate simulates "applying the update" — returns false when
// nothing was pending.
func (m *Manager) TriggerUpdate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.updateInfo.UpdateAvailable {
		return false
	}
	m.updateInfo.CurrentFirmware = m.updateInfo.AvailableFirmware
	m.updateInfo.UpdateAvailable = false
	return true
}

// ─────────────────────────────────────────────────────────────────
// Device value cache
// ─────────────────────────────────────────────────────────────────

// SetDeviceValue stores a value indexed by "ADDRESS:VALUE_KEY".
func (m *Manager) SetDeviceValue(address, valueKey string, value any) {
	m.mu.Lock()
	m.deviceValues[address+":"+valueKey] = value
	m.mu.Unlock()
}

// DeviceValue returns the cached value (nil, false) when absent.
func (m *Manager) DeviceValue(address, valueKey string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.deviceValues[address+":"+valueKey]
	return v, ok
}

// AllDeviceValues returns a copy of the entire device value cache. The
// optional iface parameter is currently informational; pydevccu also
// returns the full set regardless of interface.
func (m *Manager) AllDeviceValues(_ string) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]any, len(m.deviceValues))
	for k, v := range m.deviceValues {
		out[k] = v
	}
	return out
}

// ClearDeviceValues drops the cache.
func (m *Manager) ClearDeviceValues() {
	m.mu.Lock()
	m.deviceValues = make(map[string]any)
	m.mu.Unlock()
}

// ─────────────────────────────────────────────────────────────────
// Device names
// ─────────────────────────────────────────────────────────────────

// SetDeviceName sets a custom name for a device or channel.
func (m *Manager) SetDeviceName(address, name string) {
	m.mu.Lock()
	m.deviceNames[strings.ToUpper(address)] = name
	m.mu.Unlock()
}

// DeviceName returns the custom name for the given address, if any.
func (m *Manager) DeviceName(address string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.deviceNames[strings.ToUpper(address)]
	return n, ok
}

// AllDeviceNames returns a copy of the address→name map.
func (m *Manager) AllDeviceNames() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.deviceNames))
	for k, v := range m.deviceNames {
		out[k] = v
	}
	return out
}

// ─────────────────────────────────────────────────────────────────
// Callbacks
// ─────────────────────────────────────────────────────────────────

// RegisterSysVarCallback adds a sysvar change observer.
func (m *Manager) RegisterSysVarCallback(cb SysVarCallback) {
	m.mu.Lock()
	m.sysvarCallbacks = append(m.sysvarCallbacks, cb)
	m.mu.Unlock()
}

// RegisterProgramCallback adds a program execution observer.
func (m *Manager) RegisterProgramCallback(cb ProgramCallback) {
	m.mu.Lock()
	m.programCallbacks = append(m.programCallbacks, cb)
	m.mu.Unlock()
}

// ClearAll resets every map / list / counter to its initial state.
// Useful in tests.
func (m *Manager) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.programs = make(map[int]*Program)
	m.sysvars = make(map[int]*SystemVariable)
	m.sysvarByName = make(map[string]*SystemVariable)
	m.rooms = make(map[int]*Room)
	m.functions = make(map[int]*Function)
	m.serviceMessages = m.serviceMessages[:0]
	m.inboxDevices = m.inboxDevices[:0]
	m.deviceValues = make(map[string]any)
	m.deviceNames = make(map[string]string)
	m.backupStatus = BackupStatus{Status: "idle"}
	m.backupData = nil
	m.updateInfo = NewUpdateInfo()
}

// randomID returns a hex-encoded random identifier of length n bytes
// (so 2*n hex chars).
func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Should never happen with crypto/rand on supported platforms.
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}

// safeCallSysVar invokes cb but recovers from panics so a faulty
// observer cannot wedge the manager.
func safeCallSysVar(cb SysVarCallback, name string, value any) {
	defer func() { _ = recover() }()
	cb(name, value)
}

// safeCallProgram is the program callback equivalent of
// safeCallSysVar.
func safeCallProgram(cb ProgramCallback, id int, success bool) {
	defer func() { _ = recover() }()
	cb(id, success)
}
