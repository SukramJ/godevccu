// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package binrpc

import "fmt"

// BIN-RPC strings are ISO-8859-1 (Latin-1) on the wire. Latin-1 maps
// bytes 0x00-0xFF onto the identically-numbered code points, so the
// conversion is a direct rune/byte correspondence and needs no encoding
// table — which keeps this module dependency-free.

// latin1Decode converts Latin-1 bytes to a UTF-8 string.
func latin1Decode(raw []byte) string {
	runes := make([]rune, len(raw))
	for i, b := range raw {
		runes[i] = rune(b)
	}
	return string(runes)
}

// latin1Encode converts a UTF-8 string to Latin-1 bytes. A rune above
// U+00FF has no Latin-1 representation and is an error rather than a
// silent substitution: a mangled device name is harder to diagnose than
// a refused encode.
func latin1Encode(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return nil, fmt.Errorf("binrpc: rune %q is not representable in ISO-8859-1", r)
		}
		out = append(out, byte(r))
	}
	return out, nil
}
