// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package converter implements the combined-parameter conversions used
// by aiohomematic/gohomematic when multiple physical parameters are written through
// a single logical key (LEVEL_COMBINED, COMBINED_PARAMETER).
package converter

import (
	"fmt"
	"strconv"
	"strings"
)

// ConvertableParameters is the set of value keys that carry combined
// parameters and need pre-processing in setValue.
var ConvertableParameters = map[string]struct{}{
	"COMBINED_PARAMETER": {},
	"LEVEL_COMBINED":     {},
}

// IsConvertable reports whether parameter is one of
// [ConvertableParameters].
func IsConvertable(parameter string) bool {
	_, ok := ConvertableParameters[parameter]
	return ok
}

// ConvertCombinedParameterToParamset routes parameter to the matching
// converter and returns the resulting paramset. An empty map is
// returned when the conversion fails — pydevccu logs the failure and
// proceeds with an empty paramset, the same behaviour mirrored here.
func ConvertCombinedParameterToParamset(parameter, value string) map[string]any {
	switch parameter {
	case "COMBINED_PARAMETER":
		return convertCombinedParameter(value)
	case "LEVEL_COMBINED":
		return convertLevelCombined(value)
	default:
		return map[string]any{}
	}
}

// convertCombinedParameter parses keys of the form "L=0x12,L2=0x34".
func convertCombinedParameter(cpv string) map[string]any {
	out := map[string]any{}
	for _, pair := range strings.Split(cpv, ",") {
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			continue
		}
		shortKey := strings.TrimSpace(pair[:eq])
		raw := strings.TrimSpace(pair[eq+1:])
		paramName, ok := combinedParameterNames[shortKey]
		if !ok {
			continue
		}
		conv, hasConv := combinedParameterToHmConverter[paramName]
		if hasConv {
			out[paramName] = conv(raw)
		} else {
			out[paramName] = raw
		}
	}
	return out
}

// convertLevelCombined parses "level1,level2" into LEVEL / LEVEL_SLATS.
func convertLevelCombined(lcv string) map[string]any {
	if !strings.Contains(lcv, ",") {
		return map[string]any{}
	}
	parts := strings.SplitN(lcv, ",", 2)
	conv, ok := combinedParameterToHmConverter["LEVEL_COMBINED"]
	if !ok {
		return map[string]any{}
	}
	return map[string]any{
		"LEVEL":       conv(strings.TrimSpace(parts[0])),
		"LEVEL_SLATS": conv(strings.TrimSpace(parts[1])),
	}
}

// combinedParameterNames maps the short keys used on the wire to their
// canonical paramset names.
var combinedParameterNames = map[string]string{
	"L":  "LEVEL",
	"L2": "LEVEL_2",
}

// combinedParameterToHmConverter maps a parameter name to the function
// that turns its raw value into the float HomeMatic level.
var combinedParameterToHmConverter = map[string]func(string) any{
	"LEVEL_COMBINED": convertCpvToHmLevel,
	"LEVEL":          convertCpvToHmIPLevel,
	"LEVEL_2":        convertCpvToHmIPLevel,
}

// convertCpvToHmLevel mirrors _convert_cpv_to_hm_level in pydevccu.
// It accepts either a hex literal "0xNN" or a numeric value.
func convertCpvToHmLevel(raw string) any {
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		n, err := strconv.ParseInt(raw[2:], 16, 64)
		if err != nil {
			return raw
		}
		return float64(n) / 100.0 / 2.0
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		return n
	}
	return raw
}

// convertCpvToHmIPLevel mirrors _convert_cpv_to_hmip_level.
func convertCpvToHmIPLevel(raw string) any {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return raw
	}
	return float64(n) / 100.0
}

// ConvertHmLevelToCpv mirrors convert_hm_level_to_cpv. Python's
// format(int(...), "#04x") yields "0x04" for tiny values and "0x64"
// for 0.5 — we replicate the minimum-2-digit padding here.
func ConvertHmLevelToCpv(hmLevel float64) string {
	v := int(hmLevel * 100 * 2)
	if v >= 0 && v <= 0xff {
		// Match Python format("%04x", v) padding semantics where
		// width 4 includes the "0x" prefix → minimum two hex digits.
		return fmt.Sprintf("0x%02x", v)
	}
	return fmt.Sprintf("%#x", v)
}
