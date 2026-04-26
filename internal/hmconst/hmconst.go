// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package hmconst defines the protocol-level constants used across the
// godevccu simulator: backend modes, paramset attributes/types,
// operation flags and well-known port numbers. The values mirror those
// found in pydevccu/const.py so external clients see identical metadata.
package hmconst

// Version of the godevccu package itself.
const Version = "0.1.0"

// PydevccuVersion is the upstream pydevccu version godevccu emulates
// for wire-level compatibility. The string is returned by getVersion
// in Homegear mode (`pydevccu-<PydevccuVersion>`) so clients that
// branch on the value (for example aiohomematic) recognise the
// simulator.
const PydevccuVersion = "0.2.0"

// BackendMode selects the simulation flavour.
type BackendMode int

// Supported backend modes. The numeric values match the order of the
// pydevccu enum entries (HOMEGEAR=1, CCU=2, OPENCCU=3) so log output
// stays comparable.
const (
	BackendModeHomegear BackendMode = iota + 1
	BackendModeCCU
	BackendModeOpenCCU
)

// String returns the canonical pydevccu name for the mode.
func (b BackendMode) String() string {
	switch b {
	case BackendModeHomegear:
		return "HOMEGEAR"
	case BackendModeCCU:
		return "CCU"
	case BackendModeOpenCCU:
		return "OPENCCU"
	default:
		return "UNKNOWN"
	}
}

// IP / port defaults.
const (
	IPLocalhostV4 = "127.0.0.1"
	IPLocalhostV6 = "::1"
	IPAnyV4       = "0.0.0.0"
	IPAnyV6       = "::"
	PortAny       = 0

	PortWired    = 2000
	PortWiredTLS = 42000
	PortRF       = 2001
	PortRFTLS    = 42001
	PortIP       = 2010
	PortIPTLS    = 42010
	PortGroups   = 9292
	PortGroupTLS = 49292
)

// File / directory names used inside the embedded data set.
const (
	DeviceDescriptions   = "device_descriptions"
	ParamsetDescriptions = "paramset_descriptions"
	ParamsetsDB          = "paramsets_db.json"
)

// Common HomeMatic device-description attribute keys.
const (
	AttrAddress    = "ADDRESS"
	AttrChildren   = "CHILDREN"
	AttrName       = "NAME"
	AttrType       = "TYPE"
	AttrParentType = "PARENT_TYPE"
	AttrParent     = "PARENT"
	AttrFlags      = "FLAGS"
	AttrError      = "ERROR"
)

// Paramset attribute keys.
const (
	ParamsetAttrMaster     = "MASTER"
	ParamsetAttrValues     = "VALUES"
	ParamsetAttrLink       = "LINK"
	ParamsetAttrMin        = "MIN"
	ParamsetAttrMax        = "MAX"
	ParamsetAttrOperations = "OPERATIONS"
	ParamsetAttrDefault    = "DEFAULT"
	ParamsetAttrValueList  = "VALUE_LIST"
	ParamsetAttrSpecial    = "SPECIAL"
	ParamsetAttrUnit       = "UNIT"
	ParamsetAttrControl    = "CONTROL"
)

// Paramset value-type identifiers.
const (
	ParamsetTypeFloat   = "FLOAT"
	ParamsetTypeInteger = "INTEGER"
	ParamsetTypeBool    = "BOOL"
	ParamsetTypeEnum    = "ENUM"
	ParamsetTypeString  = "STRING"
	ParamsetTypeAction  = "ACTION"
)

// Paramset operation bitmask. A parameter is readable / writable /
// event-emitting when the corresponding bit is set in OPERATIONS.
const (
	ParamsetOperationsRead  = 1
	ParamsetOperationsWrite = 2
	ParamsetOperationsEvent = 4
)

// Paramset flag bits. INTERNAL parameters are skipped by getParamset.
const (
	ParamsetFlagInvisible = 0
	ParamsetFlagVisible   = 1
	ParamsetFlagInternal  = 2
	ParamsetFlagTransform = 4
	ParamsetFlagService   = 8
	ParamsetFlagSticky    = 10
)

// CCUFirmwareVersion is the version string returned by getVersion when
// running in CCU / OpenCCU mode (matches pydevccu).
const CCUFirmwareVersion = "3.87.1.20250130"
