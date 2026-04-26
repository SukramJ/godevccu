// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package converter_test

import (
	"testing"

	"github.com/SukramJ/godevccu/internal/converter"
)

func TestConvertCombinedParameter(t *testing.T) {
	out := converter.ConvertCombinedParameterToParamset("COMBINED_PARAMETER", "L=50,L2=25")
	if got := out["LEVEL"]; got != 0.5 {
		t.Fatalf("LEVEL = %v, want 0.5", got)
	}
	if got := out["LEVEL_2"]; got != 0.25 {
		t.Fatalf("LEVEL_2 = %v, want 0.25", got)
	}
}

func TestConvertLevelCombined(t *testing.T) {
	out := converter.ConvertCombinedParameterToParamset("LEVEL_COMBINED", "0xc8,0x64")
	// 0xc8 = 200 → 200/100/2 = 1.0
	if got := out["LEVEL"]; got != 1.0 {
		t.Fatalf("LEVEL = %v, want 1.0", got)
	}
	// 0x64 = 100 → 100/100/2 = 0.5
	if got := out["LEVEL_SLATS"]; got != 0.5 {
		t.Fatalf("LEVEL_SLATS = %v, want 0.5", got)
	}
}

func TestRoundTripCpvHmLevel(t *testing.T) {
	cpv := converter.ConvertHmLevelToCpv(0.5)
	if cpv != "0x64" {
		t.Fatalf("ConvertHmLevelToCpv(0.5) = %q, want \"0x64\"", cpv)
	}
}

func TestIsConvertable(t *testing.T) {
	if !converter.IsConvertable("LEVEL_COMBINED") {
		t.Fatal("LEVEL_COMBINED should be convertable")
	}
	if converter.IsConvertable("LEVEL") {
		t.Fatal("LEVEL alone is not convertable")
	}
}
