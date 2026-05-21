// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package hmconst_test

import (
	"testing"

	"github.com/SukramJ/godevccu/internal/hmconst"
)

// TestBackendModeString verifies that every defined BackendMode value
// returns the canonical pydevccu string that aiohomematic uses to
// identify the simulator.
func TestBackendModeString(t *testing.T) {
	cases := []struct {
		mode hmconst.BackendMode
		want string
	}{
		{hmconst.BackendModeHomegear, "HOMEGEAR"},
		{hmconst.BackendModeCCU, "CCU"},
		{hmconst.BackendModeOpenCCU, "OPENCCU"},
		{hmconst.BackendMode(99), "UNKNOWN"},
	}
	for _, tc := range cases {
		got := tc.mode.String()
		if got != tc.want {
			t.Errorf("BackendMode(%d).String() = %q, want %q", int(tc.mode), got, tc.want)
		}
	}
}

// TestBackendModeOrdering guards the numeric values that mirror the
// pydevccu enum entries so log-output comparisons stay meaningful.
func TestBackendModeOrdering(t *testing.T) {
	if hmconst.BackendModeHomegear != 1 {
		t.Errorf("BackendModeHomegear = %d, want 1", hmconst.BackendModeHomegear)
	}
	if hmconst.BackendModeCCU != 2 {
		t.Errorf("BackendModeCCU = %d, want 2", hmconst.BackendModeCCU)
	}
	if hmconst.BackendModeOpenCCU != 3 {
		t.Errorf("BackendModeOpenCCU = %d, want 3", hmconst.BackendModeOpenCCU)
	}
}

// TestVersionConstants verifies the two version constants are
// non-empty strings (guards against accidental blanking during edits).
func TestVersionConstants(t *testing.T) {
	if hmconst.Version == "" {
		t.Error("Version must not be empty")
	}
	if hmconst.PydevccuVersion == "" {
		t.Error("PydevccuVersion must not be empty")
	}
	if hmconst.CCUFirmwareVersion == "" {
		t.Error("CCUFirmwareVersion must not be empty")
	}
}

// TestPortConstants verifies that the canonical HomeMatic port numbers
// match the well-known values so godevccu and aiohomematic agree.
func TestPortConstants(t *testing.T) {
	cases := []struct {
		name string
		port int
		want int
	}{
		{"PortWired", hmconst.PortWired, 2000},
		{"PortRF", hmconst.PortRF, 2001},
		{"PortIP", hmconst.PortIP, 2010},
	}
	for _, tc := range cases {
		if tc.port != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.port, tc.want)
		}
	}
}

// TestParamsetOperationsBitmask checks that the bitmask constants are
// distinct powers of two (each can be tested in isolation via bitwise
// AND without overlap).
func TestParamsetOperationsBitmask(t *testing.T) {
	if hmconst.ParamsetOperationsRead != 1 {
		t.Errorf("ParamsetOperationsRead = %d, want 1", hmconst.ParamsetOperationsRead)
	}
	if hmconst.ParamsetOperationsWrite != 2 {
		t.Errorf("ParamsetOperationsWrite = %d, want 2", hmconst.ParamsetOperationsWrite)
	}
	if hmconst.ParamsetOperationsEvent != 4 {
		t.Errorf("ParamsetOperationsEvent = %d, want 4", hmconst.ParamsetOperationsEvent)
	}
	// Combined mask must be distinguishable.
	combined := hmconst.ParamsetOperationsRead | hmconst.ParamsetOperationsWrite | hmconst.ParamsetOperationsEvent
	if combined != 7 {
		t.Errorf("combined operations mask = %d, want 7", combined)
	}
}

// TestParamsetTypeStrings validates the string literals so wire
// consumers (e.g. gohomematic's paramset normalization) can rely on
// exact equality.
func TestParamsetTypeStrings(t *testing.T) {
	types := map[string]string{
		"FLOAT":   hmconst.ParamsetTypeFloat,
		"INTEGER": hmconst.ParamsetTypeInteger,
		"BOOL":    hmconst.ParamsetTypeBool,
		"ENUM":    hmconst.ParamsetTypeEnum,
		"STRING":  hmconst.ParamsetTypeString,
		"ACTION":  hmconst.ParamsetTypeAction,
	}
	for want, got := range types {
		if got != want {
			t.Errorf("ParamsetType constant = %q, want %q", got, want)
		}
	}
}

// TestParamsetAttrKeys guards the attribute key strings against
// accidental rename.
func TestParamsetAttrKeys(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"AttrAddress", hmconst.AttrAddress, "ADDRESS"},
		{"AttrChildren", hmconst.AttrChildren, "CHILDREN"},
		{"AttrType", hmconst.AttrType, "TYPE"},
		{"AttrName", hmconst.AttrName, "NAME"},
		{"ParamsetAttrMaster", hmconst.ParamsetAttrMaster, "MASTER"},
		{"ParamsetAttrValues", hmconst.ParamsetAttrValues, "VALUES"},
		{"ParamsetAttrMin", hmconst.ParamsetAttrMin, "MIN"},
		{"ParamsetAttrMax", hmconst.ParamsetAttrMax, "MAX"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestFilenameConstants guards the embedded-data directory names used
// by the loader.
func TestFilenameConstants(t *testing.T) {
	if hmconst.DeviceDescriptions == "" {
		t.Error("DeviceDescriptions must not be empty")
	}
	if hmconst.ParamsetDescriptions == "" {
		t.Error("ParamsetDescriptions must not be empty")
	}
	if hmconst.ParamsetsDB == "" {
		t.Error("ParamsetsDB must not be empty")
	}
}
