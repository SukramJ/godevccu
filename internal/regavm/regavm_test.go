// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package regavm_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/godevccu/internal/regavm"
)

// fakeSource is a small object model to run scripts against.
type fakeSource struct {
	devices  map[string][]string // device address → channel addresses
	values   map[string]any      // "channel.param" → value
	programs map[string]map[string]any
	sysvars  map[string]map[string]any
	ready    map[string]bool
}

func newSource() *fakeSource {
	return &fakeSource{
		devices: map[string][]string{
			"VCU0000001": {"VCU0000001:0", "VCU0000001:1"},
		},
		values: map[string]any{
			"VCU0000001:1.STATE": true,
			"VCU0000001:1.LEVEL": 0.5,
		},
		programs: map[string]map[string]any{
			"1234": {"Name": "Heizung", "Active": true, "PrgInfo": "Beschreibung mit Leerzeichen"},
		},
		sysvars: map[string]map[string]any{
			"2000": {"Name": "Statustext", "Value": "alt", "ValueTypeStr": "String", "DPInfo": "Info"},
		},
		ready: map[string]bool{"VCU0000001": true},
	}
}

func (f *fakeSource) Devices() []string {
	out := make([]string, 0, len(f.devices))
	for addr := range f.devices {
		out = append(out, addr)
	}
	return out
}

func (f *fakeSource) Channels(device string) []string { return f.devices[device] }

func (f *fakeSource) DeviceField(address, field string) (any, bool) {
	switch field {
	case "Address":
		return address, true
	case "Name":
		return address, true
	case "TypeName":
		if strings.Contains(address, ":") {
			return "CHANNEL", true
		}
		return "DEVICE", true
	case "HssType":
		return "HmIP-PS", true
	case "Interface":
		return "HmIP-RF", true
	case "ReadyConfig":
		return f.ready[strings.SplitN(address, ":", 2)[0]], true
	case "SetReadyConfig":
		f.ready[strings.SplitN(address, ":", 2)[0]] = true
		return true, true
	default:
		return nil, false
	}
}

func (f *fakeSource) Datapoints(channel string) []string {
	var out []string
	for key := range f.values {
		if strings.HasPrefix(key, channel+".") {
			out = append(out, strings.TrimPrefix(key, channel+"."))
		}
	}
	return out
}

func (f *fakeSource) DatapointValue(channel, parameter string) (any, bool) {
	v, ok := f.values[channel+"."+parameter]
	return v, ok
}

func (f *fakeSource) DatapointTimestamp(channel, parameter string) int64 {
	if _, ok := f.values[channel+"."+parameter]; ok {
		return 1_700_000_000
	}
	return 0
}

func (f *fakeSource) Programs() []string {
	out := make([]string, 0, len(f.programs))
	for id := range f.programs {
		out = append(out, id)
	}
	return out
}

func (f *fakeSource) ProgramField(id, field string) (any, bool) {
	p, ok := f.programs[id]
	if !ok {
		return nil, false
	}
	v, ok := p[field]
	return v, ok
}

func (f *fakeSource) SetProgramActive(id string, active bool) bool {
	p, ok := f.programs[id]
	if !ok {
		return false
	}
	p["Active"] = active
	return true
}

func (f *fakeSource) SystemVariables() []string {
	out := make([]string, 0, len(f.sysvars))
	for id := range f.sysvars {
		out = append(out, id)
	}
	return out
}

func (f *fakeSource) SysvarField(id, field string) (any, bool) {
	sv, ok := f.sysvars[id]
	if !ok {
		return nil, false
	}
	v, ok := sv[field]
	return v, ok
}

func (f *fakeSource) SetSysvarValue(key string, value any) bool {
	for _, sv := range f.sysvars {
		if sv["Name"] == key {
			sv["Value"] = value
			return true
		}
	}
	if sv, ok := f.sysvars[key]; ok {
		sv["Value"] = value
		return true
	}
	return false
}

func (f *fakeSource) Rooms() []string     { return nil }
func (f *fakeSource) Functions() []string { return nil }

func (f *fakeSource) GroupField(_, _, _ string) (any, bool) { return nil, false }

func (f *fakeSource) ServiceMessages() []string            { return nil }
func (f *fakeSource) ServiceField(_, _ string) (any, bool) { return nil, false }
func (f *fakeSource) ReceiptServiceMessage(_ string) bool  { return false }

func (f *fakeSource) Resolve(key string) (regavm.NodeKind, string, bool) {
	if _, ok := f.devices[key]; ok {
		return regavm.NodeDevice, key, true
	}
	for _, channels := range f.devices {
		for _, channel := range channels {
			if channel == key {
				return regavm.NodeChannel, key, true
			}
		}
	}
	if _, ok := f.programs[key]; ok {
		return regavm.NodeProgram, key, true
	}
	if _, ok := f.sysvars[key]; ok {
		return regavm.NodeSysvar, key, true
	}
	for id, sv := range f.sysvars {
		if sv["Name"] == key {
			return regavm.NodeSysvar, id, true
		}
	}
	for id, p := range f.programs {
		if p["Name"] == key {
			return regavm.NodeProgram, id, true
		}
	}
	return regavm.NodeUnknown, "", false
}

// fakeRoot wires the source into the interpreter.
type fakeRoot struct{ src *fakeSource }

func (r fakeRoot) Dom() regavm.Dom                      { return regavm.NewDom(r.src) }
func (r fakeRoot) Interfaces(name string) regavm.Object { return ifaceObject(name) }
func (r fakeRoot) Serial() string                       { return "TEST0001" }

// ifaceObject is the object interfaces.Get() returns.
type ifaceObject string

func (i ifaceObject) Name() string { return string(i) }

func (i ifaceObject) Call(method string, _ []regavm.Value) (regavm.Value, error) {
	if method == "ID" || method == "Name" {
		return regavm.StringValue(string(i)), nil
	}
	return regavm.Value{}, nil
}

func newInterpreter(src *fakeSource) *regavm.Interpreter {
	return &regavm.Interpreter{
		Root: fakeRoot{src: src},
		Exec: func(string) (string, bool) { return "", false },
	}
}

// ─────────────────────────────────────────────────────────────────
// Language
// ─────────────────────────────────────────────────────────────────

func TestWriteAndConcatenation(t *testing.T) {
	in := newInterpreter(newSource())
	res, err := in.Run(`
		string sName = "Wohnzimmer";
		integer iCount = 3;
		Write('{"name":"' # sName # '","count":' # iCount # '}');
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != `{"name":"Wohnzimmer","count":3}` {
		t.Fatalf("output = %q", res.Output)
	}
}

func TestControlFlow(t *testing.T) {
	in := newInterpreter(newSource())
	res, err := in.Run(`
		integer i = 0;
		string out = "";
		while (i < 3) {
			if (i == 1) {
				out = out # "one";
			} else {
				out = out # i;
			}
			i = i + 1;
		}
		Write(out);
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "0one2" {
		t.Fatalf("output = %q, want 0one2", res.Output)
	}
}

func TestElseIfChain(t *testing.T) {
	in := newInterpreter(newSource())
	res, err := in.Run(`
		integer n = 2;
		if (n == 1) { Write("one"); }
		elseif (n == 2) { Write("two"); }
		else { Write("many"); }
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "two" {
		t.Fatalf("output = %q, want two", res.Output)
	}
}

func TestStringMethods(t *testing.T) {
	in := newInterpreter(newSource())
	res, err := in.Run(`
		string s = "  Wohnzimmer Licht  ";
		Write(s.Trim().UriEncode());
		Write("|");
		Write("a;b;c".StrValueByIndex(";", 1));
		Write("|");
		Write("hello".Length());
		Write("|");
		Write("EXIT_CODE=7".Substr(10, 1));
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// UriEncode must percent-encode the space, not turn it into "+".
	if !strings.Contains(res.Output, "Wohnzimmer%20Licht") {
		t.Fatalf("UriEncode wrong: %q", res.Output)
	}
	parts := strings.Split(res.Output, "|")
	if parts[1] != "b" || parts[2] != "5" || parts[3] != "7" {
		t.Fatalf("string methods = %v", parts)
	}
}

func TestLoopGuardStopsRunawayScript(t *testing.T) {
	in := newInterpreter(newSource())
	// A script that would never terminate must fail rather than hang
	// the request it arrived on.
	_, err := in.Run(`integer i = 0; while (i >= 0) { i = i + 1; }`)
	if err == nil {
		t.Fatal("expected the loop guard to trip")
	}
}

func TestUnknownFunctionIsAnError(t *testing.T) {
	in := newInterpreter(newSource())
	// Answering an unrecognised script with empty success is the
	// failure mode this package exists to avoid.
	if _, err := in.Run(`NoSuchFunction();`); err == nil {
		t.Fatal("expected an error for an unknown function")
	}
}

// ─────────────────────────────────────────────────────────────────
// Object model
// ─────────────────────────────────────────────────────────────────

func TestForeachOverDevices(t *testing.T) {
	in := newInterpreter(newSource())
	res, err := in.Run(`
		string sId;
		foreach (sId, dom.GetObject(ID_DEVICES).EnumIDs()) {
			object oDev = dom.GetObject(sId);
			if (oDev) {
				Write(oDev.Address());
			}
		}
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "VCU0000001" {
		t.Fatalf("output = %q, want the device address", res.Output)
	}
}

func TestNullObjectGuard(t *testing.T) {
	in := newInterpreter(newSource())
	res, err := in.Run(`
		object o = dom.GetObject("VCU0000404");
		if (o) { Write("found"); } else { Write("missing"); }
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "missing" {
		t.Fatalf("output = %q, want missing", res.Output)
	}
}

func TestProgramActiveRoundTrip(t *testing.T) {
	src := newSource()
	in := newInterpreter(src)
	res, err := in.Run(`
		object program = dom.GetObject(ID_PROGRAMS).Get("1234");
		if (program) {
			program.Active(false);
			Write(program.Active());
		}
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "false" {
		t.Fatalf("output = %q, want false", res.Output)
	}
	if src.programs["1234"]["Active"] != false {
		t.Fatal("the program was not flipped")
	}
}

func TestSysvarWrite(t *testing.T) {
	src := newSource()
	in := newInterpreter(src)
	_, err := in.Run(`
		object target = dom.GetObject(ID_SYSTEM_VARIABLES).Get("Statustext");
		if (target) {
			if (target.ValueTypeStr() == "String") {
				Write(target.State("neu"));
			}
		}
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if src.sysvars["2000"]["Value"] != "neu" {
		t.Fatalf("sysvar = %v, want neu", src.sysvars["2000"]["Value"])
	}
}

func TestSystemExecWritesThroughReference(t *testing.T) {
	in := newInterpreter(newSource())
	in.Exec = func(command string) (string, bool) {
		if strings.Contains(command, "hostname") {
			return "openccu\n", true
		}
		return "", false
	}
	res, err := in.Run(`
		string sHost = "";
		system.Exec("hostname", &sHost);
		Write(sHost.Trim());
	`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "openccu" {
		t.Fatalf("output = %q, want openccu", res.Output)
	}
}

func TestSystemGetVarSerial(t *testing.T) {
	in := newInterpreter(newSource())
	res, err := in.Run(`Write(system.GetVar("SERIALNO"));`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "TEST0001" {
		t.Fatalf("output = %q", res.Output)
	}
}

// ─────────────────────────────────────────────────────────────────
// The real scripts
// ─────────────────────────────────────────────────────────────────

// scriptDir points at the corpus the pattern engine is tested with, so
// both engines answer to the same contract.
var scriptDir = filepath.Join("..", "rega", "testdata", "scripts")

// TestRealScriptsParse is the interpreter's coverage floor: every
// script a client ships must at least parse. A parse error means the
// language surface is incomplete, and the script would be answered with
// an error rather than data.
func TestRealScriptsParse(t *testing.T) {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		t.Skipf("corpus unavailable: %v", err)
	}
	params := map[string]string{
		"interface": "HmIP-RF", "id": "1234", "state": "1", "name": "Statustext",
		"value": "neu", "device_address": "VCU0000001", "message_id": "1",
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".fn") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(scriptDir, entry.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			script := string(raw)
			for k, v := range params {
				script = strings.ReplaceAll(script, "##"+k+"##", v)
			}
			in := newInterpreter(newSource())
			in.Exec = func(string) (string, bool) { return "", false }
			if _, err := in.Run(script); err != nil {
				t.Fatalf("%s: %v", entry.Name(), err)
			}
		})
	}
}

// TestFetchAllDeviceDataProducesAMapping runs the bulk-fetch script and
// checks the container it emits — the defect that made the whole fetch
// unusable in the pattern engine.
func TestFetchAllDeviceDataProducesAMapping(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(scriptDir, "fetch_all_device_data.fn"))
	if err != nil {
		t.Skipf("corpus unavailable: %v", err)
	}
	script := strings.ReplaceAll(string(raw), "##interface##", "HmIP-RF")

	in := newInterpreter(newSource())
	res, err := in.Run(script)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(res.Output), &data); err != nil {
		t.Fatalf("output is not a JSON object: %v — %q", err, res.Output)
	}
}
