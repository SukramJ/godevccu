// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package regavm

import (
	"strconv"
	"strings"
)

// The object model.
//
// This file holds the generic node type the interpreter navigates. What
// it exposes comes from the DataSource the composing layer supplies, so
// this package stays free of any dependency on the simulator's state.

// DataSource is what the object model reads from. The simulator
// implements it over its state manager and device catalogue.
type DataSource interface {
	// Devices returns every root device address.
	Devices() []string
	// Channels returns the channel addresses of a device.
	Channels(deviceAddress string) []string
	// DeviceField reads an attribute of a device or channel:
	// "Address", "TypeName", "HssType", "Interface", "ReadyConfig".
	DeviceField(address, field string) (any, bool)
	// Datapoints returns the parameter names of a channel.
	Datapoints(channelAddress string) []string
	// DatapointValue reads a channel parameter.
	DatapointValue(channelAddress, parameter string) (any, bool)
	// DatapointTimestamp reports when a parameter was last written, as
	// a Unix timestamp; 0 means never.
	DatapointTimestamp(channelAddress, parameter string) int64

	// Programs returns the program ids.
	Programs() []string
	// ProgramField reads "Name", "Active" or "PrgInfo".
	ProgramField(id, field string) (any, bool)
	// SetProgramActive flips a program.
	SetProgramActive(id string, active bool) bool

	// SystemVariables returns the variable ids.
	SystemVariables() []string
	// SysvarField reads "Name", "Value", "ValueType", "ValueTypeStr",
	// "DPInfo", "Channel", "Timestamp".
	SysvarField(id, field string) (any, bool)
	// SetSysvarValue writes a variable, addressed by id or name.
	SetSysvarValue(key string, value any) bool

	// Rooms and Functions return their ids; RoomField/FunctionField
	// read "Name" and "EnumUsedIDs".
	Rooms() []string
	Functions() []string
	GroupField(kind, id, field string) (any, bool)

	// ServiceMessages returns the message ids; ServiceField reads
	// "Name", "AlState", "AlTriggerDP", "AlOccurrenceTime", "AlType".
	ServiceMessages() []string
	ServiceField(id, field string) (any, bool)
	// ReceiptServiceMessage acknowledges a message.
	ReceiptServiceMessage(id string) bool

	// Resolve maps an id, address or name onto a node kind and id, so
	// dom.GetObject() can accept any of the three.
	Resolve(key string) (kind NodeKind, id string, ok bool)
}

// NodeKind classifies an object-model node.
type NodeKind int

const (
	// NodeUnknown is the zero value: nothing resolved.
	NodeUnknown NodeKind = iota
	NodeDevice
	NodeChannel
	NodeDatapoint
	NodeProgram
	NodeSysvar
	NodeRoom
	NodeFunction
	NodeService
	// NodeCollection is one of the ID_* roots.
	NodeCollection
)

// Collection names a script may pass to dom.GetObject.
const (
	CollectionDevices   = "ID_DEVICES"
	CollectionChannels  = "ID_CHANNELS"
	CollectionDatapoint = "ID_DATAPOINTS"
	CollectionPrograms  = "ID_PROGRAMS"
	CollectionSysvars   = "ID_SYSTEM_VARIABLES"
	CollectionRooms     = "ID_ROOMS"
	CollectionFunctions = "ID_FUNCTIONS"
	CollectionServices  = "ID_SERVICES"
)

// node is one object-model entry.
type node struct {
	src  DataSource
	kind NodeKind
	id   string
	// parent carries the channel a datapoint belongs to.
	parent string
}

// domAdapter implements Dom over a DataSource.
type domAdapter struct{ src DataSource }

// NewDom builds the object model root over src.
func NewDom(src DataSource) Dom { return &domAdapter{src: src} }

func (d *domAdapter) Collection(name string) Object {
	switch name {
	case CollectionDevices, CollectionChannels, CollectionDatapoint,
		CollectionPrograms, CollectionSysvars, CollectionRooms,
		CollectionFunctions, CollectionServices:
		return &node{src: d.src, kind: NodeCollection, id: name}
	default:
		return nil
	}
}

func (d *domAdapter) GetObject(key string) Object {
	kind, id, ok := d.src.Resolve(key)
	if !ok {
		return nil
	}
	return &node{src: d.src, kind: kind, id: id}
}

// Name reports the object's display name.
func (n *node) Name() string {
	v, _ := n.field("Name")
	return valueToString(v)
}

// Call dispatches a method. Unknown methods answer with the zero
// Value, which is what a CCU reports for an attribute an object does
// not carry — a script that asks for something irrelevant gets an
// empty string rather than an error.
func (n *node) Call(method string, args []Value) (Value, error) {
	switch method {
	case "ID":
		return stringValue(n.id), nil
	case "EnumIDs", "EnumUsedIDs":
		return n.enumerate(method), nil
	case "Get":
		if len(args) == 0 {
			return nullValue, nil
		}
		return n.get(args[0].String()), nil
	case "Channels":
		return idList(n.src.Channels(n.id)), nil
	case "DPs":
		return idList(n.src.Datapoints(n.id)), nil
	case "Active":
		return n.activeOrSet(args), nil
	case "State", "Value":
		return n.stateOrSet(args), nil
	case "AlReceipt":
		return boolValue(n.src.ReceiptServiceMessage(n.id)), nil
	case "ReadyConfig":
		return n.readyConfig(args), nil
	}
	v, ok := n.field(method)
	if !ok {
		return Value{}, nil
	}
	return goToValue(v), nil
}

// enumerate lists the members of a collection or group.
func (n *node) enumerate(method string) Value {
	switch n.kind {
	case NodeCollection:
		switch n.id {
		case CollectionDevices:
			return idList(n.src.Devices())
		case CollectionPrograms:
			return idList(n.src.Programs())
		case CollectionSysvars:
			return idList(n.src.SystemVariables())
		case CollectionRooms:
			return idList(n.src.Rooms())
		case CollectionFunctions:
			return idList(n.src.Functions())
		case CollectionServices:
			return idList(n.src.ServiceMessages())
		case CollectionChannels:
			var all []string
			for _, device := range n.src.Devices() {
				all = append(all, n.src.Channels(device)...)
			}
			return idList(all)
		default:
			return listValue(nil)
		}
	case NodeDevice:
		return idList(n.src.Channels(n.id))
	case NodeChannel:
		return idList(n.src.Datapoints(n.id))
	case NodeRoom, NodeFunction:
		kind := "room"
		if n.kind == NodeFunction {
			kind = "function"
		}
		if v, ok := n.src.GroupField(kind, n.id, method); ok {
			return goToValue(v)
		}
		return listValue(nil)
	default:
		return listValue(nil)
	}
}

// get resolves a member of a collection by name or id, which is what
// dom.GetObject(ID_PROGRAMS).Get(name) does.
func (n *node) get(key string) Value {
	kind, id, ok := n.src.Resolve(key)
	if !ok {
		return nullValue
	}
	return objectValue(&node{src: n.src, kind: kind, id: id})
}

// activeOrSet reads or writes a program's active flag.
func (n *node) activeOrSet(args []Value) Value {
	if len(args) == 0 {
		v, _ := n.field("Active")
		return goToValue(v)
	}
	active := args[0].Bool()
	n.src.SetProgramActive(n.id, active)
	return boolValue(active)
}

// stateOrSet reads or writes a value. On a system variable a write goes
// to the variable; on a datapoint it is a no-op read, because writing a
// device value goes through the RPC layer rather than the object model.
func (n *node) stateOrSet(args []Value) Value {
	if len(args) == 0 {
		v, _ := n.field("Value")
		return goToValue(v)
	}
	if n.kind == NodeSysvar {
		return boolValue(n.src.SetSysvarValue(n.id, valueToGo(args[0])))
	}
	return boolValue(false)
}

// readyConfig reports whether a device is configured. A write marks it
// configured, which is how the inbox-accept script works.
func (n *node) readyConfig(args []Value) Value {
	if len(args) == 0 {
		v, _ := n.field("ReadyConfig")
		return goToValue(v)
	}
	if v, ok := n.src.DeviceField(n.id, "SetReadyConfig"); ok {
		return goToValue(v)
	}
	return boolValue(false)
}

// field reads an attribute from the data source, routed by node kind.
func (n *node) field(name string) (any, bool) {
	switch n.kind {
	case NodeDevice, NodeChannel:
		return n.src.DeviceField(n.id, name)
	case NodeDatapoint:
		if name == "Value" {
			return n.src.DatapointValue(n.parent, n.id)
		}
		if name == "Timestamp" || name == "LastTimestamp" {
			return n.src.DatapointTimestamp(n.parent, n.id), true
		}
		return n.src.DeviceField(n.id, name)
	case NodeProgram:
		return n.src.ProgramField(n.id, name)
	case NodeSysvar:
		return n.src.SysvarField(n.id, name)
	case NodeRoom:
		return n.src.GroupField("room", n.id, name)
	case NodeFunction:
		return n.src.GroupField("function", n.id, name)
	case NodeService:
		return n.src.ServiceField(n.id, name)
	default:
		return nil, false
	}
}

// idList renders a list of ids as a Value.
func idList(ids []string) Value {
	out := make([]Value, 0, len(ids))
	for _, id := range ids {
		out = append(out, stringValue(id))
	}
	return listValue(out)
}

// goToValue lifts a Go value into the interpreter's value type.
func goToValue(v any) Value {
	switch x := v.(type) {
	case nil:
		return Value{}
	case string:
		return stringValue(x)
	case bool:
		return boolValue(x)
	case int:
		return numberValue(float64(x))
	case int64:
		return numberValue(float64(x))
	case float64:
		return numberValue(x)
	case []string:
		return idList(x)
	default:
		return stringValue(valueToString(v))
	}
}

// valueToGo lowers an interpreter value for the data source.
func valueToGo(v Value) any {
	switch v.kind {
	case kindBool:
		return v.boolean
	case kindNumber:
		return v.num
	default:
		return v.String()
	}
}

// valueToString renders any Go value as text.
func valueToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return formatNumber(x)
	case []string:
		return strings.Join(x, "\t")
	default:
		return ""
	}
}
