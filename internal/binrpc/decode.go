// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package binrpc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// Request is the parsed form of a BIN-RPC request.
type Request struct {
	Method string
	Params []xmlrpc.Value
}

// Response is the parsed form of a BIN-RPC response. Exactly one of
// Value / Fault is non-nil.
type Response struct {
	Value xmlrpc.Value
	Fault *xmlrpc.Fault
}

// ReadRequest reads and parses a single BIN-RPC request. Bytes consumed
// from r never exceed [MaxMessageSize] plus the 8-byte header; any
// deadline on r is the caller's responsibility.
func ReadRequest(r io.Reader) (*Request, error) {
	msgType, payload, err := readFrame(r)
	if err != nil {
		return nil, err
	}
	if msgType != msgTypeRequest {
		return nil, fmt.Errorf("binrpc: expected request (0x%02X), got 0x%02X", msgTypeRequest, msgType)
	}
	pr := &bytesReader{b: payload}
	method, err := readRawString(pr)
	if err != nil {
		return nil, fmt.Errorf("binrpc: read method: %w", err)
	}
	var count uint32
	if countErr := binary.Read(pr, binary.BigEndian, &count); countErr != nil {
		return nil, fmt.Errorf("binrpc: read param count: %w", countErr)
	}
	params, err := readNValues(pr, int(count), 0)
	if err != nil {
		return nil, err
	}
	if pr.remaining() != 0 {
		return nil, fmt.Errorf("binrpc: %d trailing bytes after request payload", pr.remaining())
	}
	return &Request{Method: method, Params: params}, nil
}

// ReadResponse reads a BIN-RPC response or fault from r.
func ReadResponse(r io.Reader) (*Response, error) {
	msgType, payload, err := readFrame(r)
	if err != nil {
		return nil, err
	}
	pr := &bytesReader{b: payload}
	switch msgType {
	case msgTypeResponse:
		v, err := readValue(pr, 0)
		if err != nil {
			return nil, err
		}
		if pr.remaining() != 0 {
			return nil, fmt.Errorf("binrpc: %d trailing bytes after response payload", pr.remaining())
		}
		return &Response{Value: v}, nil
	case msgTypeFault:
		v, err := readValue(pr, 0)
		if err != nil {
			return nil, fmt.Errorf("binrpc: read fault: %w", err)
		}
		st, ok := xmlrpc.AsStruct(v)
		if !ok {
			return nil, errors.New("binrpc: fault payload is not a struct")
		}
		fault := &xmlrpc.Fault{}
		for _, m := range st.Members {
			switch m.Name {
			case "faultCode":
				if code, ok := xmlrpc.AsInt(m.Value); ok {
					fault.Code = code
				}
			case "faultString":
				if msg, ok := xmlrpc.AsString(m.Value); ok {
					fault.Message = msg
				}
			}
		}
		return &Response{Fault: fault}, nil
	default:
		return nil, fmt.Errorf("binrpc: unexpected message type 0x%02X", msgType)
	}
}

// readFrame validates the marker and returns (msgType, payload).
func readFrame(r io.Reader) (msgType uint8, payload []byte, err error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, fmt.Errorf("binrpc: read header: %w", err)
	}
	if hdr[0] != marker[0] || hdr[1] != marker[1] || hdr[2] != marker[2] {
		return 0, nil, fmt.Errorf("binrpc: bad marker %q", hdr[:3])
	}
	msgType = hdr[3]
	size := binary.BigEndian.Uint32(hdr[4:])
	if int64(size) > MaxMessageSize {
		return 0, nil, fmt.Errorf("binrpc: payload size %d exceeds limit %d", size, MaxMessageSize)
	}
	// Grow with the bytes that actually arrive rather than committing the
	// declared size up front: an 8-byte header can claim MaxMessageSize
	// while sending no body, and pre-allocating that per connection is a
	// cheap way to pin memory.
	var buf bytes.Buffer
	buf.Grow(int(min(int64(size), initialPayloadCap)))
	if _, err := io.CopyN(&buf, r, int64(size)); err != nil {
		return 0, nil, fmt.Errorf("binrpc: read payload: %w", err)
	}
	return msgType, buf.Bytes(), nil
}

// readValue reads one type-tagged value. depth tracks nesting and errors
// past [maxDecodeDepth].
func readValue(r *bytesReader, depth int) (xmlrpc.Value, error) {
	if depth > maxDecodeDepth {
		return nil, fmt.Errorf("binrpc: nesting exceeds max depth %d", maxDecodeDepth)
	}
	var tag uint32
	if err := binary.Read(r, binary.BigEndian, &tag); err != nil {
		return nil, fmt.Errorf("binrpc: read type tag: %w", err)
	}
	switch tag {
	case typeInt:
		var n int32
		if err := binary.Read(r, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		return xmlrpc.IntValue(n), nil
	case typeBool:
		var b uint8
		if err := binary.Read(r, binary.BigEndian, &b); err != nil {
			return nil, err
		}
		return xmlrpc.BoolValue(b != 0), nil
	case typeString:
		s, err := readRawString(r)
		if err != nil {
			return nil, err
		}
		return xmlrpc.StringValue(s), nil
	case typeDouble:
		var mant, exp int32
		if err := binary.Read(r, binary.BigEndian, &mant); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &exp); err != nil {
			return nil, err
		}
		return xmlrpc.DoubleValue(math.Pow(2, float64(exp)) * float64(mant) / mantissaScale), nil
	case typeArray:
		var count uint32
		if err := binary.Read(r, binary.BigEndian, &count); err != nil {
			return nil, err
		}
		vs, err := readNValues(r, int(count), depth+1)
		if err != nil {
			return nil, err
		}
		return xmlrpc.ArrayValue(vs), nil
	case typeStruct:
		return readStruct(r, depth)
	default:
		return nil, fmt.Errorf("binrpc: unknown value type tag 0x%X", tag)
	}
}

func readStruct(r *bytesReader, depth int) (xmlrpc.Value, error) {
	var count uint32
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, err
	}
	// int(count) can wrap negative on 32-bit builds, so validate the
	// non-negative domain and bound by the minimum wire footprint per
	// member before allocating.
	n := int(count)
	if n < 0 || n > r.remaining()/minMemberWireBytes {
		return nil, fmt.Errorf("binrpc: struct member count %d exceeds remaining %d bytes", count, r.remaining())
	}
	members := make([]xmlrpc.Member, n)
	for i := range n {
		name, err := readRawString(r)
		if err != nil {
			return nil, fmt.Errorf("binrpc: struct member %d name: %w", i, err)
		}
		val, err := readValue(r, depth+1)
		if err != nil {
			return nil, fmt.Errorf("binrpc: struct member %d value: %w", i, err)
		}
		members[i] = xmlrpc.Member{Name: name, Value: val}
	}
	return xmlrpc.StructValue{Members: members}, nil
}

func readNValues(r *bytesReader, n, depth int) ([]xmlrpc.Value, error) {
	if n < 0 {
		return nil, fmt.Errorf("binrpc: negative value count %d", n)
	}
	if n > r.remaining()/minValueWireBytes {
		return nil, fmt.Errorf("binrpc: value count %d exceeds remaining %d bytes", n, r.remaining())
	}
	out := make([]xmlrpc.Value, n)
	for i := range n {
		v, err := readValue(r, depth)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// readRawString reads `<length:u32><ISO-8859-1 bytes>`.
func readRawString(r *bytesReader) (string, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return "", err
	}
	raw, err := r.readN(int(length))
	if err != nil {
		return "", err
	}
	return latin1Decode(raw), nil
}

// bytesReader is a minimal in-memory reader that can report its
// remaining byte count, which the allocation bounds above rely on.
type bytesReader struct {
	b   []byte
	off int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.off >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.off:])
	r.off += n
	return n, nil
}

func (r *bytesReader) readN(n int) ([]byte, error) {
	if n < 0 {
		return nil, errors.New("binrpc: negative read length")
	}
	// Compare against remaining() rather than r.off+n: on 32-bit builds a
	// large n can make r.off+n overflow negative and slip past the bound.
	if n > r.remaining() {
		return nil, fmt.Errorf("binrpc: truncated: need %d bytes, have %d", n, r.remaining())
	}
	out := r.b[r.off : r.off+n]
	r.off += n
	return out, nil
}

func (r *bytesReader) remaining() int { return len(r.b) - r.off }
