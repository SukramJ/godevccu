// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package virtualccu

import (
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/regavm"
	"github.com/SukramJ/godevccu/internal/state"
)

// regaSource exposes the simulator's state as the ReGa object model.
//
// The interpreter in internal/regavm knows nothing about this
// simulator; this adapter is the only place the two meet, which keeps
// the language implementation testable on its own.
type regaSource struct {
	state *state.Manager
	rpc   *ccu.RPCFunctions
}

// Devices lists the root device addresses.
func (s *regaSource) Devices() []string {
	if s.rpc == nil {
		return nil
	}
	var out []string
	for _, device := range s.rpc.ListDevices() {
		address, _ := device[hmconst.AttrAddress].(string)
		if address != "" && !strings.Contains(address, ":") {
			out = append(out, address)
		}
	}
	return out
}

// Channels lists a device's channel addresses.
func (s *regaSource) Channels(deviceAddress string) []string {
	if s.rpc == nil {
		return nil
	}
	description, err := s.rpc.GetDeviceDescription(deviceAddress)
	if err != nil {
		return nil
	}
	children, _ := description[hmconst.AttrChildren].([]any)
	out := make([]string, 0, len(children))
	for _, child := range children {
		if address, ok := child.(string); ok {
			out = append(out, address)
		}
	}
	return out
}

// DeviceField reads an attribute off a device or channel.
func (s *regaSource) DeviceField(address, field string) (any, bool) {
	if s.rpc == nil {
		return nil, false
	}
	switch field {
	case "Address":
		return address, true
	case "Name":
		if name, ok := s.state.DeviceName(address); ok {
			return name, true
		}
		return address, true
	case "TypeName":
		if strings.Contains(address, ":") {
			return "CHANNEL", true
		}
		return "DEVICE", true
	case "ReadyConfig":
		// Everything in the catalogue is configured; the inbox holds
		// what is not.
		return !s.inInbox(address), true
	case "SetReadyConfig":
		return s.state.AcceptInboxDevice(rootOf(address)), true
	}

	description, err := s.rpc.GetDeviceDescription(address)
	if err != nil {
		return nil, false
	}
	switch field {
	case "HssType":
		if typeName, ok := description[hmconst.AttrType].(string); ok {
			return typeName, true
		}
		return "", true
	case "Interface":
		typeName, _ := description[hmconst.AttrType].(string)
		if parent, ok := description[hmconst.AttrParentType].(string); ok && parent != "" {
			typeName = parent
		}
		return hmconst.InterfaceForType(typeName), true
	case "Channels":
		return s.Channels(address), true
	default:
		if v, ok := description[strings.ToUpper(field)]; ok {
			return v, true
		}
		return nil, false
	}
}

// inInbox reports whether an address is still awaiting pairing.
func (s *regaSource) inInbox(address string) bool {
	root := rootOf(address)
	for _, device := range s.state.InboxDevices() {
		if strings.EqualFold(device.Address, root) {
			return true
		}
	}
	return false
}

// Datapoints lists a channel's parameter names.
func (s *regaSource) Datapoints(channelAddress string) []string {
	if s.rpc == nil {
		return nil
	}
	values, err := s.rpc.GetParamset(channelAddress, hmconst.ParamsetAttrValues)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for name := range values {
		out = append(out, name)
	}
	return out
}

// DatapointValue reads a channel parameter.
func (s *regaSource) DatapointValue(channelAddress, parameter string) (any, bool) {
	if s.rpc == nil {
		return nil, false
	}
	v, err := s.rpc.GetValue(channelAddress, parameter)
	if err != nil {
		return nil, false
	}
	return v, true
}

// DatapointTimestamp reports when a parameter was last written.
//
// The simulator keeps no per-parameter timestamps, so this answers the
// only distinction the scripts actually make: the bulk-fetch script
// skips datapoints without a valid timestamp, so a known parameter
// reports one and an unknown parameter reports zero.
func (s *regaSource) DatapointTimestamp(channelAddress, parameter string) int64 {
	if _, ok := s.DatapointValue(channelAddress, parameter); !ok {
		return 0
	}
	return time.Now().Unix()
}

// Programs lists the program ids.
func (s *regaSource) Programs() []string {
	programs := s.state.Programs()
	out := make([]string, 0, len(programs))
	for _, p := range programs {
		out = append(out, strconv.Itoa(p.ID))
	}
	return out
}

// ProgramField reads a program attribute.
func (s *regaSource) ProgramField(id, field string) (any, bool) {
	numeric, err := strconv.Atoi(id)
	if err != nil {
		return nil, false
	}
	program, ok := s.state.Program(numeric)
	if !ok {
		return nil, false
	}
	switch field {
	case "Name":
		return program.Name, true
	case "Active":
		return program.Active, true
	case "PrgInfo", "Description":
		return program.Description, true
	case "ID":
		return id, true
	default:
		return nil, false
	}
}

// SetProgramActive flips a program.
func (s *regaSource) SetProgramActive(id string, active bool) bool {
	numeric, err := strconv.Atoi(id)
	if err != nil {
		return false
	}
	return s.state.SetProgramActive(numeric, active)
}

// SystemVariables lists the variable ids.
func (s *regaSource) SystemVariables() []string {
	sysvars := s.state.SystemVariables()
	out := make([]string, 0, len(sysvars))
	for _, sv := range sysvars {
		out = append(out, strconv.Itoa(sv.ID))
	}
	return out
}

// SysvarField reads a variable attribute.
func (s *regaSource) SysvarField(id, field string) (any, bool) {
	numeric, err := strconv.Atoi(id)
	if err != nil {
		return nil, false
	}
	sv, ok := s.state.SystemVariableByID(numeric)
	if !ok {
		return nil, false
	}
	switch field {
	case "Name":
		return sv.Name, true
	case "Value", "State":
		return sv.Value, true
	case "ValueTypeStr":
		return regaValueTypeName(sv.VarType), true
	case "ValueType":
		return regaValueType(sv.VarType), true
	case "DPInfo", "Description":
		return sv.Description, true
	case "Channel":
		return sv.ChannelAddress, true
	case "Timestamp", "LastTimestamp":
		return int64(sv.Timestamp), true
	case "ID":
		return id, true
	default:
		return nil, false
	}
}

// SetSysvarValue writes a variable by id or name.
func (s *regaSource) SetSysvarValue(key string, value any) bool {
	if numeric, err := strconv.Atoi(key); err == nil {
		if sv, ok := s.state.SystemVariableByID(numeric); ok {
			return s.state.SetSystemVariable(sv.Name, value)
		}
	}
	return s.state.SetSystemVariable(key, value)
}

// Rooms and Functions list their ids.
func (s *regaSource) Rooms() []string {
	rooms := s.state.Rooms()
	out := make([]string, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, strconv.Itoa(r.ID))
	}
	return out
}

func (s *regaSource) Functions() []string {
	functions := s.state.Functions()
	out := make([]string, 0, len(functions))
	for _, f := range functions {
		out = append(out, strconv.Itoa(f.ID))
	}
	return out
}

// GroupField reads a room or function attribute.
func (s *regaSource) GroupField(kind, id, field string) (any, bool) {
	numeric, err := strconv.Atoi(id)
	if err != nil {
		return nil, false
	}
	var name string
	var channels []string
	if kind == "room" {
		room, ok := s.state.Room(numeric)
		if !ok {
			return nil, false
		}
		name, channels = room.Name, room.ChannelIDs
	} else {
		function, ok := s.state.Function(numeric)
		if !ok {
			return nil, false
		}
		name, channels = function.Name, function.ChannelIDs
	}
	switch field {
	case "Name":
		return name, true
	case "EnumIDs", "EnumUsedIDs", "Channels":
		return channels, true
	case "ID":
		return id, true
	default:
		return nil, false
	}
}

// ServiceMessages lists the message ids.
func (s *regaSource) ServiceMessages() []string {
	messages := s.state.ServiceMessages()
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		out = append(out, strconv.Itoa(m.ID))
	}
	return out
}

// ServiceField reads a service-message attribute.
func (s *regaSource) ServiceField(id, field string) (any, bool) {
	numeric, err := strconv.Atoi(id)
	if err != nil {
		return nil, false
	}
	for _, m := range s.state.ServiceMessages() {
		if m.ID != numeric {
			continue
		}
		switch field {
		case "Name":
			return m.Name, true
		case "AlState":
			// A listed message is an active one.
			return 1, true
		case "AlTriggerDP":
			return m.Address, true
		case "AlOccurrenceTime":
			return int64(m.Timestamp), true
		case "AlType", "Type":
			return m.MsgType, true
		case "ID":
			return id, true
		default:
			return nil, false
		}
	}
	return nil, false
}

// ReceiptServiceMessage acknowledges a message.
func (s *regaSource) ReceiptServiceMessage(id string) bool {
	numeric, err := strconv.Atoi(id)
	if err != nil {
		return false
	}
	return s.state.ClearServiceMessage(numeric)
}

// Resolve maps an id, address or name onto an object-model node. A
// script may pass any of the three to dom.GetObject().
func (s *regaSource) Resolve(key string) (regavm.NodeKind, string, bool) {
	if key == "" {
		return regavm.NodeUnknown, "", false
	}
	// Addresses are the most specific form, so they win.
	if s.rpc != nil {
		if _, err := s.rpc.GetDeviceDescription(key); err == nil {
			if strings.Contains(key, ":") {
				return regavm.NodeChannel, key, true
			}
			return regavm.NodeDevice, key, true
		}
	}
	if numeric, err := strconv.Atoi(key); err == nil {
		if _, ok := s.state.Program(numeric); ok {
			return regavm.NodeProgram, key, true
		}
		if _, ok := s.state.SystemVariableByID(numeric); ok {
			return regavm.NodeSysvar, key, true
		}
		if _, ok := s.state.Room(numeric); ok {
			return regavm.NodeRoom, key, true
		}
		if _, ok := s.state.Function(numeric); ok {
			return regavm.NodeFunction, key, true
		}
		for _, m := range s.state.ServiceMessages() {
			if m.ID == numeric {
				return regavm.NodeService, key, true
			}
		}
	}
	if sv, ok := s.state.SystemVariable(key); ok {
		return regavm.NodeSysvar, strconv.Itoa(sv.ID), true
	}
	if program, ok := s.state.ProgramByName(key); ok {
		return regavm.NodeProgram, strconv.Itoa(program.ID), true
	}
	return regavm.NodeUnknown, "", false
}

// rootOf strips the channel suffix from an address.
func rootOf(address string) string {
	if i := strings.IndexByte(address, ':'); i >= 0 {
		return address[:i]
	}
	return address
}

// ReGa ValueType codes, as ValueType() reports them.
const (
	regaTypeBinary  = 2
	regaTypeFloat   = 4
	regaTypeInteger = 16
	regaTypeString  = 20
)

// regaValueTypeName maps the simulator's type names onto the strings
// ValueTypeStr() reports.
func regaValueTypeName(varType string) string {
	switch strings.ToUpper(varType) {
	case "BOOL", "ALARM":
		return "Boolean"
	case "FLOAT":
		return "Float"
	case "ENUM":
		return "Integer"
	default:
		return "String"
	}
}

// regaValueType maps them onto the numeric codes.
func regaValueType(varType string) int {
	switch strings.ToUpper(varType) {
	case "BOOL", "ALARM":
		return regaTypeBinary
	case "FLOAT":
		return regaTypeFloat
	case "ENUM":
		return regaTypeInteger
	default:
		return regaTypeString
	}
}

// regaRoot is the environment scripts start from.
type regaRoot struct {
	source     *regaSource
	interfaces []string
	serial     string
}

func (r *regaRoot) Dom() regavm.Dom { return regavm.NewDom(r.source) }

func (r *regaRoot) Serial() string { return r.serial }

// Interfaces resolves interfaces.Get(name).
func (r *regaRoot) Interfaces(name string) regavm.Object {
	for _, known := range r.interfaces {
		if strings.EqualFold(known, name) {
			return regaInterface(known)
		}
	}
	return nil
}

// regaInterface is the object interfaces.Get() yields.
type regaInterface string

func (i regaInterface) Name() string { return string(i) }

func (i regaInterface) Call(method string, _ []regavm.Value) (regavm.Value, error) {
	switch method {
	case "ID", "Name":
		return regavm.StringValue(string(i)), nil
	default:
		return regavm.Value{}, nil
	}
}
