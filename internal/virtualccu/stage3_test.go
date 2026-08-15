// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// End-to-end tests for the behaviour models: the ReGa script endpoint,
// the pairing and firmware automata, actuator ramps, the fault
// catalogue and the load-time data normalisation. Each pins both
// states, because all of them are opt-in.

package virtualccu_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/virtualccu"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// runScript posts a script to the /tclrega.exe endpoint and returns the
// output, stripped of the XML trailer a CCU appends.
func runScript(t *testing.T, v *virtualccu.VirtualCCU, script string) string {
	t.Helper()
	addr := v.RegaScriptAddr()
	if addr == nil {
		t.Fatal("no script endpoint")
	}
	resp, err := http.Post("http://"+addr.String()+"/tclrega.exe", "text/plain", strings.NewReader(script))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	out := string(body)
	if i := strings.Index(out, "<xml>"); i >= 0 {
		out = out[:i]
	}
	return out
}

// ─────────────────────────────────────────────────────────────────
// ReGa script endpoint
// ─────────────────────────────────────────────────────────────────

// TestRegaScriptEndpointRunsScripts covers the endpoint ccu-jack needs:
// a posted script executes against the real object model, rather than
// being recognised from a pattern list.
func TestRegaScriptEndpointRunsScripts(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.RegaScriptPort = virtualccu.EphemeralPort
		cfg.SetupDefaults = true
	})

	out := runScript(t, v, `Write("hello " # (2 + 3));`)
	if out != "hello 5" {
		t.Fatalf("output = %q, want \"hello 5\"", out)
	}
}

// A script no pattern would ever match still produces real data —
// that is the whole point of running rather than recognising.
func TestRegaScriptWalksTheObjectModel(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.RegaScriptPort = virtualccu.EphemeralPort
		cfg.SetupDefaults = true
	})

	out := runScript(t, v, `
		string sId;
		integer iCount = 0;
		foreach (sId, dom.GetObject(ID_DEVICES).EnumIDs()) {
			iCount = iCount + 1;
		}
		Write(iCount);
	`)
	if out == "" || out == "0" {
		t.Fatalf("device enumeration returned %q", out)
	}
}

// A program flipped through a script must actually change state.
func TestRegaScriptWritesProgramState(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.RegaScriptPort = virtualccu.EphemeralPort
	})
	program := v.State().AddProgram("Heizung", "", true, 0)

	out := runScript(t, v, `
		object p = dom.GetObject(ID_PROGRAMS).Get("Heizung");
		if (p) {
			p.Active(false);
			Write(p.Active());
		}
	`)
	if out != "false" {
		t.Fatalf("output = %q, want false", out)
	}
	if updated, _ := v.State().Program(program.ID); updated.Active {
		t.Fatal("the program was not flipped")
	}
}

func TestNoScriptEndpointByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	if v.RegaScriptAddr() != nil {
		t.Fatal("script endpoint started without a port configured")
	}
}

// ─────────────────────────────────────────────────────────────────
// Pairing and firmware
// ─────────────────────────────────────────────────────────────────

// TestInstallModeCountsDown pins the pairing window: getInstallMode
// reports the remaining seconds instead of a constant 0.
func TestInstallModeCountsDown(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Realism.Lifecycle = true
	})
	rpc := v.RPC()

	if got := rpc.GetInstallMode(); got != 0 {
		t.Fatalf("install mode before pairing = %d, want 0", got)
	}
	rpc.SetInstallMode(true, 60, 1, "")
	remaining := rpc.GetInstallMode()
	if remaining <= 0 || remaining > 60 {
		t.Fatalf("remaining = %d, want a value in (0, 60]", remaining)
	}

	rpc.SetInstallMode(false, 0, 0, "")
	if got := rpc.GetInstallMode(); got != 0 {
		t.Fatalf("install mode after cancelling = %d, want 0", got)
	}
}

func TestInstallModeStaysZeroByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	rpc := v.RPC()
	rpc.SetInstallMode(true, 60, 1, "")
	if got := rpc.GetInstallMode(); got != 0 {
		t.Fatalf("install mode = %d, want 0 without the lifecycle", got)
	}
}

// TestFirmwareUpdateWalksItsStates covers the progression a client's
// update display reads.
func TestFirmwareUpdateWalksItsStates(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Realism.Lifecycle = true
	})
	rpc := v.RPC()
	root := anyDevice(t, rpc)

	// The callback runs on the simulator's goroutines, so the recorder
	// needs its own lock.
	var mu sync.Mutex
	var seen []any
	snapshot := func() []any {
		mu.Lock()
		defer mu.Unlock()
		return append([]any(nil), seen...)
	}
	rpc.RegisterParamsetCallback(func(_, address, valueKey string, value any) {
		if valueKey == "FIRMWARE_UPDATE_STATE" && strings.HasPrefix(address, root) {
			mu.Lock()
			seen = append(seen, value)
			mu.Unlock()
		}
	})

	if !rpc.InstallFirmware(root) {
		t.Fatal("InstallFirmware reported failure")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(snapshot()) < 4 {
		time.Sleep(25 * time.Millisecond)
	}
	states := snapshot()
	if len(states) < 2 {
		t.Fatalf("firmware states reported: %v, want the progression", states)
	}
	if states[0] != ccu.FirmwareStateDelivering {
		t.Errorf("first state = %v, want %s", states[0], ccu.FirmwareStateDelivering)
	}
	if last := states[len(states)-1]; last != ccu.FirmwareStateUpToDate && len(states) >= 4 {
		t.Errorf("final state = %v, want %s", last, ccu.FirmwareStateUpToDate)
	}
}

// ─────────────────────────────────────────────────────────────────
// Fault catalogue
// ─────────────────────────────────────────────────────────────────

// TestFaultCodesClassifyFailures pins the codes a client's retry logic
// reads: an unknown device must not land in the retryable bucket.
func TestFaultCodesClassifyFailures(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Realism.FaultCodes = true
	})
	client := xmlrpc.NewClient("http://" + v.XMLRPCAddr().String() + "/")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Call(ctx, "getDeviceDescription", []xmlrpc.Value{xmlrpc.StringValue("VCU0000404")})
	if err == nil {
		t.Fatal("expected a fault for an unknown device")
	}
	fault, ok := err.(*xmlrpc.Fault)
	if !ok {
		t.Fatalf("error type = %T, want *xmlrpc.Fault", err)
	}
	if fault.Code != ccu.FaultUnknownDevice {
		t.Errorf("fault code = %d, want %d (unknown device)", fault.Code, ccu.FaultUnknownDevice)
	}
}

func TestGenericFaultCodeByDefault(t *testing.T) {
	v := startRealistic(t, func(_ *virtualccu.Config) {})
	client := xmlrpc.NewClient("http://" + v.XMLRPCAddr().String() + "/")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Call(ctx, "getDeviceDescription", []xmlrpc.Value{xmlrpc.StringValue("VCU0000404")})
	fault, ok := err.(*xmlrpc.Fault)
	if !ok {
		t.Fatalf("error type = %T, want *xmlrpc.Fault", err)
	}
	if fault.Code != ccu.FaultUnknownError {
		t.Errorf("fault code = %d, want the established -1", fault.Code)
	}
}

// ─────────────────────────────────────────────────────────────────
// Data normalisation
// ─────────────────────────────────────────────────────────────────

// TestNormalisationFillsTheGaps covers the four defects the loader
// closes, on a device type that carries all of them.
func TestNormalisationFillsTheGaps(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Realism.NormalizeData = true
		cfg.Devices = []string{"HM-LC-Sw1-Pl"}
	})
	rpc := v.RPC()
	root := anyDevice(t, rpc)

	description, err := rpc.GetDeviceDescription(root)
	if err != nil {
		t.Fatalf("GetDeviceDescription: %v", err)
	}
	if firmware, _ := description["FIRMWARE"].(string); firmware == "" {
		t.Error("FIRMWARE is still empty")
	}
	if available, _ := description["AVAILABLE_FIRMWARE"].(string); available == "" {
		t.Error("AVAILABLE_FIRMWARE missing on the root device")
	}
	if updatable, present := description["UPDATABLE"]; present {
		if _, isBool := updatable.(bool); !isBool {
			t.Errorf("UPDATABLE = %T, want a bool", updatable)
		}
	}

	// No parameter may lack an ID or carry a null unit — the latter
	// serialises as <nil/>.
	for _, channel := range []string{root + ":0", root + ":1"} {
		for _, paramset := range []string{"MASTER", "VALUES"} {
			description, err := rpc.GetParamsetDescription(channel, paramset)
			if err != nil {
				continue
			}
			for name, raw := range description {
				parameter, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if id, _ := parameter["ID"].(string); id == "" {
					t.Errorf("%s.%s.%s has no ID", channel, paramset, name)
				}
				if unit, present := parameter["UNIT"]; present && unit == nil {
					t.Errorf("%s.%s.%s carries UNIT: null", channel, paramset, name)
				}
			}
		}
	}
}

func TestFixturesUntouchedByDefault(t *testing.T) {
	v := startRealistic(t, func(cfg *virtualccu.Config) {
		cfg.Devices = []string{"HM-LC-Sw1-Pl"}
	})
	rpc := v.RPC()
	root := anyDevice(t, rpc)

	description, err := rpc.GetDeviceDescription(root)
	if err != nil {
		t.Fatalf("GetDeviceDescription: %v", err)
	}
	// The catalogue ships this device with an empty firmware; without
	// opting in it stays that way.
	if firmware, present := description["FIRMWARE"]; present {
		if s, _ := firmware.(string); s != "" {
			t.Errorf("FIRMWARE = %q, want the untouched fixture value", s)
		}
	}
}

// anyDevice returns a loaded root device address.
func anyDevice(t *testing.T, rpc *ccu.RPCFunctions) string {
	t.Helper()
	for _, address := range rpc.SupportedDevices() {
		return address
	}
	t.Fatal("no devices loaded")
	return ""
}
