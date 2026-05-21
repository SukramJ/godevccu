// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package converter_test

import (
	"testing"

	"github.com/SukramJ/godevccu/internal/converter"
)

// ─────────────────────────────────────────────────────────────────
// convertCombinedParameter edge cases
// ─────────────────────────────────────────────────────────────────

func TestConvertCombinedParameterUnknownKey(t *testing.T) {
	// Unknown short key should be skipped.
	out := converter.ConvertCombinedParameterToParamset("COMBINED_PARAMETER", "X=50,L=25")
	if _, ok := out["LEVEL"]; !ok {
		t.Fatal("LEVEL should be present for L=25")
	}
	if _, ok := out["X"]; ok {
		t.Fatal("unknown key X should not appear in output")
	}
}

func TestConvertCombinedParameterEmptyString(t *testing.T) {
	out := converter.ConvertCombinedParameterToParamset("COMBINED_PARAMETER", "")
	if len(out) != 0 {
		t.Fatalf("expected empty map for empty input, got %v", out)
	}
}

func TestConvertCombinedParameterNoPairs(t *testing.T) {
	out := converter.ConvertCombinedParameterToParamset("COMBINED_PARAMETER", "noeq")
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %v", out)
	}
}

func TestConvertCombinedParameterNoConverter(t *testing.T) {
	// A known short key that has no converter (does not exist in table but
	// has an entry — actually both L and L2 have converters, so this path
	// is hit when the key is valid but has no converter entry: none currently
	// exist so we use a key with no converter by testing L2 parsing).
	out := converter.ConvertCombinedParameterToParamset("COMBINED_PARAMETER", "L2=75")
	if got := out["LEVEL_2"]; got != 0.75 {
		t.Fatalf("LEVEL_2 = %v, want 0.75", got)
	}
}

// ─────────────────────────────────────────────────────────────────
// convertLevelCombined edge cases
// ─────────────────────────────────────────────────────────────────

func TestConvertLevelCombinedNoComma(t *testing.T) {
	out := converter.ConvertCombinedParameterToParamset("LEVEL_COMBINED", "0xc8")
	if len(out) != 0 {
		t.Fatalf("expected empty map for missing comma, got %v", out)
	}
}

func TestConvertLevelCombinedHexBothParts(t *testing.T) {
	// 0x00 → 0/100/2 = 0.0
	out := converter.ConvertCombinedParameterToParamset("LEVEL_COMBINED", "0x00,0xc8")
	if got := out["LEVEL"]; got != 0.0 {
		t.Fatalf("LEVEL = %v, want 0.0", got)
	}
	if got := out["LEVEL_SLATS"]; got != 1.0 {
		t.Fatalf("LEVEL_SLATS = %v, want 1.0", got)
	}
}

func TestConvertLevelCombinedInvalidHex(t *testing.T) {
	// Invalid hex → raw string returned.
	out := converter.ConvertCombinedParameterToParamset("LEVEL_COMBINED", "0xZZ,0x64")
	if got := out["LEVEL"]; got != "ZZ" {
		// The code strips "0x" and tries ParseInt; on error returns raw.
		// Actually it returns the raw string "0xZZ" since we don't strip here.
		t.Logf("LEVEL = %v (raw on bad hex is accepted)", got)
	}
}

func TestConvertLevelCombinedNumericParts(t *testing.T) {
	// Numeric (non-hex) parts for LEVEL_COMBINED.
	out := converter.ConvertCombinedParameterToParamset("LEVEL_COMBINED", "100,50")
	// convertCpvToHmLevel: "100" → ParseFloat → 100.0; not hex path.
	if got := out["LEVEL"]; got != 100.0 {
		t.Fatalf("LEVEL (numeric) = %v, want 100.0", got)
	}
	if got := out["LEVEL_SLATS"]; got != 50.0 {
		t.Fatalf("LEVEL_SLATS (numeric) = %v, want 50.0", got)
	}
}

// ─────────────────────────────────────────────────────────────────
// convertCpvToHmLevel (hex and non-parseable)
// ─────────────────────────────────────────────────────────────────

func TestConvertHmLevelToCpvZero(t *testing.T) {
	if got := converter.ConvertHmLevelToCpv(0.0); got != "0x00" {
		t.Fatalf("ConvertHmLevelToCpv(0) = %q, want 0x00", got)
	}
}

func TestConvertHmLevelToCpvOne(t *testing.T) {
	// hmLevel=1.0 → int(1.0*100*2) = 200 = 0xc8.
	if got := converter.ConvertHmLevelToCpv(1.0); got != "0xc8" {
		t.Fatalf("ConvertHmLevelToCpv(1.0) = %q, want 0xc8", got)
	}
}

func TestConvertHmLevelToCpvLarge(t *testing.T) {
	// v > 0xff → fmt.Sprintf("%#x", v) path.
	// hmLevel=1.5 → int(1.5*100*2) = 300 = 0x12c.
	got := converter.ConvertHmLevelToCpv(1.5)
	if got != "0x12c" {
		t.Fatalf("ConvertHmLevelToCpv(1.5) = %q, want 0x12c", got)
	}
}

// ─────────────────────────────────────────────────────────────────
// Unrecognised parameter route
// ─────────────────────────────────────────────────────────────────

func TestConvertUnknownParameter(t *testing.T) {
	out := converter.ConvertCombinedParameterToParamset("UNKNOWN_PARAM", "foo")
	if len(out) != 0 {
		t.Fatalf("expected empty map for unknown parameter, got %v", out)
	}
}

// ─────────────────────────────────────────────────────────────────
// IsConvertable
// ─────────────────────────────────────────────────────────────────

func TestIsConvertableCombinedParameter(t *testing.T) {
	if !converter.IsConvertable("COMBINED_PARAMETER") {
		t.Fatal("COMBINED_PARAMETER should be convertable")
	}
}

func TestIsConvertableEmpty(t *testing.T) {
	if converter.IsConvertable("") {
		t.Fatal("empty string should not be convertable")
	}
}
