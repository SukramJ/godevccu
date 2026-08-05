// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package binrpc implements the HomeMatic BIN-RPC wire protocol
// (`xmlrpc_bin://`), the binary sibling of XML-RPC that CUxD speaks
// exclusively.
//
// pydevccu has no BIN-RPC and no CUxD, so this package is a deliberate
// extension beyond pydevccu parity rather than a port of it. It exists
// because the CUxD callback direction is otherwise untestable without
// real CUxD hardware, and that gap hid a defect in a downstream consumer
// for its entire lifetime: CUxD wraps every callback in a
// `system.multicall` envelope, which nothing in a simulator-backed test
// suite ever produced.
//
// Values reuse [xmlrpc.Value] — BIN-RPC carries the same value set as
// XML-RPC, only framed differently.
package binrpc

// Wire-format constants. BIN-RPC packets are big-endian throughout.
//
// Every message is framed by an 8-byte header:
//
//	'B' 'i' 'n' <msgType:u8> <payloadSize:u32>
//
// followed by payloadSize bytes. Requests additionally prefix the method
// name (a length-prefixed string with no type tag) before the parameter
// array.
const (
	// Message types.
	msgTypeRequest  uint8 = 0x00
	msgTypeResponse uint8 = 0x01
	msgTypeFault    uint8 = 0xFF

	// Value type tags, written as u32 big-endian.
	typeInt    uint32 = 0x01
	typeBool   uint32 = 0x02
	typeString uint32 = 0x03
	typeDouble uint32 = 0x04
	typeArray  uint32 = 0x100
	typeStruct uint32 = 0x101

	// mantissaScale is the denominator in BIN-RPC's double
	// representation: value = mantissa * 2^exp / 2^30.
	mantissaScale float64 = 1 << 30

	// MaxMessageSize bounds any BIN-RPC message accepted on read.
	MaxMessageSize int64 = 10 * 1024 * 1024

	// initialPayloadCap is the capacity reserved up front for a frame
	// payload. The buffer then grows with the bytes that actually arrive,
	// so a header declaring a large size but sending no body costs at most
	// this much per connection rather than the full declared size.
	initialPayloadCap int64 = 64 * 1024

	// maxDecodeDepth bounds array/struct nesting in a decoded value, so a
	// crafted deeply-nested message cannot drive readValue into unbounded
	// recursion. Real paramsets nest a few levels at most.
	maxDecodeDepth = 64

	// minValueWireBytes and minMemberWireBytes are the smallest number of
	// wire bytes an array element / struct member can consume: every value
	// starts with a 4-byte type tag, and every struct member additionally
	// starts with a 4-byte name-length field. Element and member counts are
	// bounded by remaining()/min so a crafted count cannot pre-allocate far
	// beyond what the payload could ever fill.
	minValueWireBytes  = 4
	minMemberWireBytes = 8
)

// marker is the fixed 3-byte preamble of every BIN-RPC packet.
var marker = [3]byte{'B', 'i', 'n'}
