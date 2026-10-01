// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Tests for the behaviours taken over from hm-simulator
// (https://github.com/hobbyquaker/hm-simulator): init semantics, fault
// injection, interface restarts, the write and callback logs, the
// MASTER write models and the interface-process quirks. Each opt-in
// behaviour is also checked to stay absent by default.

package ccu_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/binrpc"
	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// newRPCWith builds an RPCFunctions over the given device types and
// interface filter.
func newRPCWith(t *testing.T, filter string, devices ...string) *ccu.RPCFunctions {
	t.Helper()
	rpc, err := ccu.NewRPCFunctions(ccu.Options{Devices: devices, InterfaceFilter: filter, InterfaceID: filter})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	return rpc
}

// startServer serves rpc on an ephemeral port.
func serveRPC(t *testing.T, rpc *ccu.RPCFunctions) (*ccu.Server, string) {
	t.Helper()
	s := ccu.NewServer(ccu.ServerConfig{Address: "127.0.0.1:0", RPC: rpc})
	if err := s.Start(); err != nil {
		t.Fatalf("Server.Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s, "http://" + s.LocalAddr().String() + "/"
}

// rootOf returns the root address of a loaded device type.
func rootOf(t *testing.T, rpc *ccu.RPCFunctions, deviceType string) string {
	t.Helper()
	root, ok := rpc.SupportedDevices()[deviceType]
	if !ok {
		t.Fatalf("%s not loaded", deviceType)
	}
	return root
}

// scriptedRemote is a callback receiver that records the method of
// every call in order and answers listDevices with a scripted list.
type scriptedRemote struct {
	mu      sync.Mutex
	methods []string
	bodies  []string
	srv     *httptest.Server
}

var methodNamePattern = regexp.MustCompile(`<methodName>([^<]+)</methodName>`)

func newScriptedRemote(t *testing.T, listDevicesReply string) *scriptedRemote {
	t.Helper()
	sr := &scriptedRemote{}
	sr.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		method := ""
		if m := methodNamePattern.FindSubmatch(body); m != nil {
			method = string(m[1])
		}
		sr.mu.Lock()
		sr.methods = append(sr.methods, method)
		sr.bodies = append(sr.bodies, string(body))
		sr.mu.Unlock()
		w.Header().Set("Content-Type", "text/xml")
		if method == "listDevices" && listDevicesReply != "" {
			_, _ = io.WriteString(w, listDevicesReply)
			return
		}
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(sr.srv.Close)
	return sr
}

func (sr *scriptedRemote) calls() ([]string, []string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	return append([]string(nil), sr.methods...), append([]string(nil), sr.bodies...)
}

// waitFor polls cond for up to two seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// deviceListReply renders a listDevices answer of (address, VERSION)
// pairs.
func deviceListReply(devices map[string]int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><methodResponse><params><param><value><array><data>`)
	for addr, version := range devices {
		fmt.Fprintf(&b, `<value><struct><member><name>ADDRESS</name><value><string>%s</string></value></member>`+
			`<member><name>VERSION</name><value><i4>%d</i4></value></member></struct></value>`, addr, version)
	}
	b.WriteString(`</data></array></value></param></params></methodResponse>`)
	return b.String()
}

// findMasterParameter returns a writable MASTER parameter of the given
// type, read from the channel's own description.
func findMasterParameter(t *testing.T, rpc *ccu.RPCFunctions, address, paramType string) (string, map[string]any) {
	t.Helper()
	desc, err := rpc.GetParamsetDescription(address, hmconst.ParamsetAttrMaster)
	if err != nil {
		t.Fatalf("MASTER description of %s: %v", address, err)
	}
	names := make([]string, 0, len(desc))
	for name := range desc {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pd, ok := desc[name].(map[string]any)
		if !ok || pd[hmconst.AttrType] != paramType {
			continue
		}
		ops, _ := pd[hmconst.ParamsetAttrOperations].(float64)
		if opsInt, ok := pd[hmconst.ParamsetAttrOperations].(int); ok {
			ops = float64(opsInt)
		}
		if int(ops)&hmconst.ParamsetOperationsWrite == 0 {
			continue
		}
		maxValue, ok := toNumber(pd[hmconst.ParamsetAttrMax])
		if !ok && paramType != hmconst.ParamsetTypeBool {
			continue
		}
		// A default already at MAX would hide a clamp.
		if def, ok := toNumber(pd[hmconst.ParamsetAttrDefault]); ok && def == maxValue {
			continue
		}
		return name, pd
	}
	t.Skipf("%s has no writable MASTER %s parameter", address, paramType)
	return "", nil
}

func toNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	}
	return 0, false
}

// ─────────────────────────────────────────────────────────────────────────────
// init semantics
// ─────────────────────────────────────────────────────────────────────────────

// pydevccu removes a registration with init(url) by a substring match;
// the default keeps that, init semantics remove only the exact url.
func TestInitDeregistrationMatching(t *testing.T) {
	for _, tc := range []struct {
		name      string
		semantics bool
		removed   bool
	}{
		{"pydevccu substring match", false, true},
		{"exact url with init semantics", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := newRPC(t)
			if tc.semantics {
				rpc.EnableInitSemantics()
			}
			remote := newRecordingRemote(t, okResponse)
			rpc.Init(remote.srv.URL, "client")
			// A prefix of the registered url, as a client with another
			// port on the same host would send it.
			rpc.Init("http://127.0.0.1", "")
			if got := !rpc.ClientServerInitialized("client"); got != tc.removed {
				t.Fatalf("removed = %v, want %v", got, tc.removed)
			}
			rpc.Init(remote.srv.URL, "")
			if rpc.ClientServerInitialized("client") {
				t.Fatal("init(url, \"\") did not remove the exact url")
			}
		})
	}
}

// Two logic layers sharing an interface id overwrite each other under
// pydevccu's keying; keyed by url both stay registered.
func TestInitSemanticsKeepsClientsSharingAnInterfaceID(t *testing.T) {
	for _, tc := range []struct {
		name      string
		semantics bool
		bothGet   bool
	}{
		{"keyed by interface id", false, false},
		{"keyed by url", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := newRPC(t)
			if tc.semantics {
				rpc.EnableInitSemantics()
			}
			first := newRecordingRemote(t, okResponse)
			second := newRecordingRemote(t, okResponse)
			rpc.Init(first.srv.URL, "shared")
			rpc.Init(second.srv.URL, "shared")
			rpc.FireEvent("shared", "VCU0000001:1", "STATE", true)
			eventually(t, "event at the second client", func() bool { return second.containing("STATE") })
			if got := first.containing("STATE"); got != tc.bothGet {
				t.Fatalf("first client got the event = %v, want %v", got, tc.bothGet)
			}
		})
	}
}

// Under init semantics deleteDevices precedes newDevices, a device in
// another VERSION is sent again, and one the client has at the same
// VERSION is left alone.
func TestInitSemanticsDeviceDiff(t *testing.T) {
	rpc := newRPCWith(t, "", "HM-LC-Sw1-Pl-2")
	rpc.EnableInitSemantics()
	root := rootOf(t, rpc, "HM-LC-Sw1-Pl-2")
	desc, err := rpc.GetDeviceDescription(root)
	if err != nil {
		t.Fatal(err)
	}
	channel := root + ":1"
	chDesc, err := rpc.GetDeviceDescription(channel)
	if err != nil {
		t.Fatal(err)
	}
	version := func(d map[string]any) int {
		n, _ := toNumber(d["VERSION"])
		return int(n)
	}
	remote := newScriptedRemote(t, deviceListReply(map[string]int{
		"GONE0000001": 1,                 // unknown to the simulator
		root:          version(desc) + 1, // another VERSION
		channel:       version(chDesc),   // up to date
	}))
	rpc.Init(remote.srv.URL, "client")
	eventually(t, "newDevices", func() bool {
		methods, _ := remote.calls()
		return len(methods) >= 3
	})
	methods, bodies := remote.calls()
	if methods[0] != "listDevices" || methods[1] != "deleteDevices" || methods[2] != "newDevices" {
		t.Fatalf("call order = %v, want listDevices, deleteDevices, newDevices", methods)
	}
	if !strings.Contains(bodies[1], "GONE0000001") || !strings.Contains(bodies[1], root+"<") {
		t.Errorf("deleteDevices lacks the unknown or the outdated device: %s", bodies[1])
	}
	if strings.Contains(bodies[1], channel) {
		t.Errorf("deleteDevices names the up-to-date channel: %s", bodies[1])
	}
	// The resent root lists the channel among its CHILDREN; only an
	// ADDRESS member would mean the channel itself went out again.
	resent := regexp.MustCompile(`<name>ADDRESS</name>\s*<value>\s*(<string>)?` + regexp.QuoteMeta(channel) + `<`)
	if resent.MatchString(bodies[2]) {
		t.Errorf("newDevices resends the up-to-date channel")
	}
	// The same pattern finds the outdated root, so the check above can
	// fail at all.
	if !regexp.MustCompile(`<name>ADDRESS</name>\s*<value>\s*(<string>)?` + regexp.QuoteMeta(root) + `<`).MatchString(bodies[2]) {
		t.Errorf("newDevices lacks the outdated root: %s", bodies[2])
	}
}

// HmIP devices are deleted and sent again on every init.
func TestInitSemanticsResendsHmIPDevices(t *testing.T) {
	rpc := newRPCWith(t, "", "HmIP-SWSD")
	rpc.EnableInitSemantics()
	root := rootOf(t, rpc, "HmIP-SWSD")
	desc, _ := rpc.GetDeviceDescription(root)
	v, _ := toNumber(desc["VERSION"])
	remote := newScriptedRemote(t, deviceListReply(map[string]int{root: int(v)}))
	rpc.Init(remote.srv.URL, "client")
	eventually(t, "newDevices", func() bool {
		methods, _ := remote.calls()
		return len(methods) >= 3
	})
	_, bodies := remote.calls()
	if !strings.Contains(bodies[1], root) || !strings.Contains(bodies[2], root) {
		t.Fatalf("known HmIP device was not deleted and resent:\n%s\n%s", bodies[1], bodies[2])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fault injection
// ─────────────────────────────────────────────────────────────────────────────

func TestInjectedFaultAppliesTimesThenClears(t *testing.T) {
	srv, url := serveRPC(t, newRPC(t))
	client := xmlrpc.NewClient(url)
	srv.InjectFault(ccu.FaultRule{Method: "getVersion", Times: 2, Fault: &xmlrpc.Fault{Code: -2, Message: "Invalid device"}})

	for i := range 2 {
		_, err := client.Call(context.Background(), "getVersion", nil)
		var fault *xmlrpc.Fault
		if !errors.As(err, &fault) || fault.Code != -2 {
			t.Fatalf("call %d: err = %v, want fault -2", i+1, err)
		}
	}
	if _, err := client.Call(context.Background(), "getVersion", nil); err != nil {
		t.Fatalf("third call: %v, want the rule used up", err)
	}
	// Other methods are not affected by a method-specific rule.
	srv.InjectFault(ccu.FaultRule{Method: "getVersion", Fault: &xmlrpc.Fault{Code: -1}})
	if _, err := client.Call(context.Background(), "listDevices", nil); err != nil {
		t.Fatalf("listDevices hit by a getVersion rule: %v", err)
	}
}

func TestInjectedCloseAndDelay(t *testing.T) {
	srv, url := serveRPC(t, newRPC(t))
	client := xmlrpc.NewClient(url)

	srv.InjectFault(ccu.FaultRule{CloseConnection: true})
	_, err := client.Call(context.Background(), "getVersion", nil)
	if err == nil || !xmlrpc.IsTransport(err) {
		t.Fatalf("err = %v, want a transport error", err)
	}

	srv.InjectFault(ccu.FaultRule{Method: "getVersion", Delay: 150 * time.Millisecond})
	start := time.Now()
	if _, err := client.Call(context.Background(), "getVersion", nil); err != nil {
		t.Fatalf("delayed call: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("answered after %v, want at least 150ms", elapsed)
	}
}

// A hanging call is answered by nothing until the rule is cleared; then
// the connection closes.
func TestInjectedHangReleasedByClearFaults(t *testing.T) {
	srv, url := serveRPC(t, newRPC(t))
	client := xmlrpc.NewClient(url)
	srv.InjectFault(ccu.FaultRule{Method: "ping", Hang: true})

	done := make(chan error, 1)
	go func() {
		_, err := client.Call(context.Background(), "ping", []xmlrpc.Value{xmlrpc.StringValue("x")})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("hanging call answered: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	srv.ClearFaults()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("released call got an answer, want a closed connection")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ClearFaults did not release the hanging call")
	}
}

func TestInjectedFaultOverBINRPC(t *testing.T) {
	srv, _ := serveRPC(t, newRPC(t))
	if err := srv.StartBINRPC("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.StopBINRPC() })
	client := binrpc.NewClient("xmlrpc_bin://" + srv.BINRPCLocalAddr().String())

	srv.InjectFault(ccu.FaultRule{Method: "getVersion", Fault: &xmlrpc.Fault{Code: -5, Message: "x"}})
	_, err := client.Call(context.Background(), "getVersion", nil)
	var fault *xmlrpc.Fault
	if !errors.As(err, &fault) || fault.Code != -5 {
		t.Fatalf("err = %v, want fault -5", err)
	}
	srv.InjectFault(ccu.FaultRule{Method: "getVersion", CloseConnection: true})
	if _, err := client.Call(context.Background(), "getVersion", nil); err == nil || errors.As(err, &fault) {
		t.Fatalf("err = %v, want a closed connection", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Interface restarts
// ─────────────────────────────────────────────────────────────────────────────

func TestSuspendRefusesAndResumeRebindsSamePort(t *testing.T) {
	rpc := newRPC(t)
	srv, url := serveRPC(t, rpc)
	addr := srv.LocalAddr().String()
	remote := newRecordingRemote(t, okResponse)
	rpc.Init(remote.srv.URL, "client")

	if err := srv.Suspend(); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("suspended interface accepted a connection")
	}
	if !rpc.ClientServerInitialized("client") {
		t.Fatal("suspending forgot the clients; only Resume decides that")
	}
	if err := srv.Resume(true); err != nil {
		t.Fatal(err)
	}
	if got := srv.LocalAddr().String(); got != addr {
		t.Fatalf("resumed on %s, want %s", got, addr)
	}
	if _, err := xmlrpc.NewClient(url).Call(context.Background(), "getVersion", nil); err != nil {
		t.Fatalf("resumed interface: %v", err)
	}
	if rpc.ClientServerInitialized("client") {
		t.Fatal("Resume(forgetClients) kept the registration")
	}
}

// A BidCos process that remembers its clients calls system.listMethods
// and then listDevices back after the restart.
func TestResumeRememberingClientsReannounces(t *testing.T) {
	rpc := newRPCWith(t, hmconst.InterfaceBidCosRF, "HM-LC-Sw1-Pl-2")
	srv, _ := serveRPC(t, rpc)
	remote := newScriptedRemote(t, "")
	rpc.Init(remote.srv.URL, "client")
	eventually(t, "init callbacks", func() bool {
		methods, _ := remote.calls()
		return len(methods) >= 2
	})
	before, _ := remote.calls()

	if err := srv.Suspend(); err != nil {
		t.Fatal(err)
	}
	if err := srv.Resume(false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "reannounce", func() bool {
		methods, _ := remote.calls()
		return len(methods) >= len(before)+2
	})
	methods, _ := remote.calls()
	after := methods[len(before):]
	if after[0] != "system.listMethods" || after[1] != "listDevices" {
		t.Fatalf("restart callbacks = %v, want system.listMethods, listDevices", after)
	}
	if !rpc.ClientServerInitialized("client") {
		t.Fatal("remembered client was forgotten")
	}
}

func TestDropConnectionsForgetsClients(t *testing.T) {
	rpc := newRPC(t)
	srv, url := serveRPC(t, rpc)
	remote := newRecordingRemote(t, okResponse)
	rpc.Init(remote.srv.URL, "client")
	if err := srv.DropConnections(); err != nil {
		t.Fatal(err)
	}
	if rpc.ClientServerInitialized("client") {
		t.Fatal("client still registered after dropConnection")
	}
	if _, err := xmlrpc.NewClient(url).Call(context.Background(), "getVersion", nil); err != nil {
		t.Fatalf("interface not back after dropConnection: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Logs
// ─────────────────────────────────────────────────────────────────────────────

func TestWriteLogRecordsClientWritesOnly(t *testing.T) {
	rpc := newRPC(t)
	_, url := serveRPC(t, rpc)
	root := rootOf(t, rpc, "HmIP-SWSD")
	client := xmlrpc.NewClient(url)

	// Read-only parameter written through the wire: recorded with the
	// fault.
	_, _ = client.Call(context.Background(), "setValue", []xmlrpc.Value{
		xmlrpc.StringValue(root + ":0"), xmlrpc.StringValue("NO_SUCH_PARAMETER"), xmlrpc.BoolValue(true),
	})
	// A device report is the simulator's own write: not recorded.
	_ = rpc.SimulateDeviceEvent(root+":0", "UNREACH", true)

	log := rpc.WriteLog()
	if len(log) != 1 {
		t.Fatalf("write log = %+v, want exactly the client write", log)
	}
	entry := log[0]
	if entry.Method != "setValue" || entry.Address != root+":0" || entry.Paramset != "VALUES" || entry.Error == "" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, ok := entry.Values["NO_SUCH_PARAMETER"]; !ok {
		t.Fatalf("entry values = %v", entry.Values)
	}
	rpc.ClearLogs()
	if len(rpc.WriteLog()) != 0 {
		t.Fatal("ClearLogs left entries")
	}
}

func TestCallbackLogRecordsAnsweredCalls(t *testing.T) {
	rpc := newRPC(t)
	remote := newRecordingRemote(t, okResponse)
	rpc.Init(remote.srv.URL, "client")
	eventually(t, "listDevices callback", func() bool {
		for _, e := range rpc.CallbackLog() {
			if e.Method == "listDevices" && !e.AnsweredAt.IsZero() {
				return true
			}
		}
		return false
	})
	for _, e := range rpc.CallbackLog() {
		if e.URL != remote.srv.URL {
			t.Fatalf("callback to %s, want %s", e.URL, remote.srv.URL)
		}
		if !e.AnsweredAt.IsZero() && e.AnsweredAt.Before(e.SentAt) {
			t.Fatalf("answered before sent: %+v", e)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MASTER write models
// ─────────────────────────────────────────────────────────────────────────────

// Without the model a MASTER write keeps the established path: an
// unknown parameter faults and nothing is poisoned.
func TestMasterModelOffByDefault(t *testing.T) {
	rpc := newRPCWith(t, "", "HmIP-BROLL")
	channel := rootOf(t, rpc, "HmIP-BROLL") + ":0"
	if err := rpc.PutParamset(channel, "MASTER", map[string]any{"NO_SUCH_PARAMETER": 1}, false); err == nil {
		t.Fatal("unknown MASTER parameter accepted without the model")
	}
	if got := rpc.PoisonedChannels(); len(got) != 0 {
		t.Fatalf("poisoned without the model: %v", got)
	}
}

func TestMasterModelHmIP(t *testing.T) {
	rpc := newRPCWith(t, "", "HmIP-BROLL")
	rpc.EnableMasterModel()
	channel := rootOf(t, rpc, "HmIP-BROLL") + ":0"
	name, pd := findMasterParameter(t, rpc, channel, hmconst.ParamsetTypeInteger)
	maxValue, _ := toNumber(pd[hmconst.ParamsetAttrMax])

	// Out of range: accepted and stored as sent — no range check.
	beyond := int(maxValue) + 100
	if err := rpc.PutParamset(channel, "MASTER", map[string]any{name: beyond}, false); err != nil {
		t.Fatalf("out-of-range write: %v", err)
	}
	if got, _ := rpc.GetParamset(channel, "MASTER"); got[name] != beyond {
		t.Fatalf("%s = %v, want %d stored unclamped", name, got[name], beyond)
	}

	// Wrong type: stored, faulted, sticky CONFIG_PENDING.
	err := rpc.PutParamset(channel, "MASTER", map[string]any{name: "abc"}, false)
	if !errors.Is(err, ccu.ErrInvalidValue) {
		t.Fatalf("wrong type: err = %v, want ErrInvalidValue", err)
	}
	if pending := rpc.ConfigPending(); len(pending) != 1 || !pending[0].Sticky || pending[0].Address != channel {
		t.Fatalf("config pending = %+v, want a sticky entry on %s", pending, channel)
	}
	// A write that leaves nothing invalid clears it.
	if err := rpc.PutParamset(channel, "MASTER", map[string]any{name: 1}, false); err != nil {
		t.Fatalf("valid write: %v", err)
	}
	if pending := rpc.ConfigPending(); len(pending) != 0 {
		t.Fatalf("config pending after a valid write = %+v", pending)
	}

	// Unknown parameter: stored, channel poisoned for good — even an
	// empty write faults afterwards.
	if err := rpc.PutParamset(channel, "MASTER", map[string]any{"NO_SUCH_PARAMETER": 1}, false); !errors.Is(err, ccu.ErrInvalidValue) {
		t.Fatalf("unknown parameter: err = %v", err)
	}
	if got := rpc.PoisonedChannels(); len(got) != 1 || got[0] != channel {
		t.Fatalf("poisoned = %v, want [%s]", got, channel)
	}
	if err := rpc.PutParamset(channel, "MASTER", map[string]any{}, false); !errors.Is(err, ccu.ErrInvalidValue) {
		t.Fatalf("empty write on a poisoned channel: err = %v", err)
	}
}

func TestMasterModelBidCos(t *testing.T) {
	rpc := newRPCWith(t, "", "HM-CC-RT-DN")
	rpc.EnableMasterModel()
	rpc.EnableReachability(200 * time.Millisecond)
	root := rootOf(t, rpc, "HM-CC-RT-DN")
	intName, intDesc := findMasterParameter(t, rpc, root, hmconst.ParamsetTypeInteger)
	floatName, _ := findMasterParameter(t, rpc, root, hmconst.ParamsetTypeFloat)
	maxValue, _ := toNumber(intDesc[hmconst.ParamsetAttrMax])
	before, _ := rpc.GetParamset(root, "MASTER")

	rejected := 0
	err := rpc.ClientPutParamset(root, "MASTER", map[string]any{
		"NO_SUCH_PARAMETER": 1,                   // dropped
		intName:             int(maxValue) + 100, // clamped
		floatName:           7,                   // a FLOAT as <int>: ignored
	}, false)
	if err != nil {
		t.Fatalf("rfd never faults on a value: %v", err)
	}
	for _, entry := range rpc.WriteLog() {
		rejected += len(entry.Rejected)
	}
	if rejected != 2 {
		t.Fatalf("rejected = %d, want the unknown parameter and the <int> FLOAT; log %+v", rejected, rpc.WriteLog())
	}
	after, _ := rpc.GetParamset(root, "MASTER")
	if n, _ := toNumber(after[intName]); n != maxValue {
		t.Fatalf("%s = %v, want clamped to %v", intName, after[intName], maxValue)
	}
	if !sameNumber(after[floatName], before[floatName]) {
		t.Fatalf("%s changed to %v by an <int>", floatName, after[floatName])
	}
	if _, ok := after["NO_SUCH_PARAMETER"]; ok {
		t.Fatal("unknown parameter stored")
	}

	// The change is queued: CONFIG_PENDING rises and clears itself.
	maintenance := root + ":0"
	if v, _ := rpc.GetValue(maintenance, "CONFIG_PENDING"); v != true {
		t.Fatalf("CONFIG_PENDING = %v after a change, want true", v)
	}
	if pending := rpc.ConfigPending(); len(pending) != 1 || pending[0].Sticky {
		t.Fatalf("config pending = %+v, want one non-sticky entry", pending)
	}
	eventually(t, "CONFIG_PENDING to clear", func() bool {
		v, _ := rpc.GetValue(maintenance, "CONFIG_PENDING")
		return v == false
	})
}

func sameNumber(a, b any) bool {
	x, okA := toNumber(a)
	y, okB := toNumber(b)
	return okA && okB && x == y
}

// ─────────────────────────────────────────────────────────────────────────────
// Interface quirks
// ─────────────────────────────────────────────────────────────────────────────

func TestQuirkHmIPPingWithoutPong(t *testing.T) {
	for _, tc := range []struct {
		name   string
		quirks bool
		pong   bool
	}{
		{"default", false, true},
		{"quirks", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpc := newRPCWith(t, hmconst.InterfaceHmIPRF, "HmIP-SWSD")
			if tc.quirks {
				rpc.EnableQuirks()
			}
			remote := newRecordingRemote(t, okResponse)
			rpc.Init(remote.srv.URL, "client")
			rpc.Ping("client#token")
			time.Sleep(150 * time.Millisecond)
			if got := remote.containing("PONG"); got != tc.pong {
				t.Fatalf("PONG sent = %v, want %v", got, tc.pong)
			}
		})
	}
}

func TestQuirkServiceMessages(t *testing.T) {
	rpc := newRPCWith(t, hmconst.InterfaceBidCosRF, "HM-LC-Sw1-Pl-2")
	if answer, _ := rpc.ServiceMessagesAnswer(); answer == "" {
		t.Fatal("pinned getServiceMessages answer changed without quirks")
	}
	rpc.EnableQuirks()
	if answer, err := rpc.ServiceMessagesAnswer(); err != nil || answer != "" {
		t.Fatalf("rfd with nothing pending = %#v, %v; want the empty string", answer, err)
	}
	root := rootOf(t, rpc, "HM-LC-Sw1-Pl-2")
	if err := rpc.SetDeviceUnreachable(root, true); err != nil {
		t.Fatal(err)
	}
	answer, _ := rpc.ServiceMessagesAnswer()
	list, ok := answer.([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("answer = %#v, want the raised UNREACH", answer)
	}
	first, _ := list[0].([]any)
	if first[0] != root+":0" || first[1] != "UNREACH" {
		t.Fatalf("first message = %v", first)
	}
}

func TestServiceMessagesFault(t *testing.T) {
	rpc := newRPCWith(t, hmconst.InterfaceHmIPRF, "HmIP-SWSD")
	rpc.EnableServiceMessagesFault()
	_, err := rpc.ServiceMessagesAnswer()
	var fault *xmlrpc.Fault
	if !errors.As(err, &fault) || fault.Message != "Invalid XML-RPC message" {
		t.Fatalf("err = %v, want the unknown-method fault", err)
	}
	bidcos := newRPCWith(t, hmconst.InterfaceBidCosRF, "HM-LC-Sw1-Pl-2")
	bidcos.EnableServiceMessagesFault()
	if _, err := bidcos.ServiceMessagesAnswer(); err != nil {
		t.Fatalf("rfd faulted: %v", err)
	}
}

func TestQuirkMetadata(t *testing.T) {
	rf := newRPCWith(t, hmconst.InterfaceBidCosRF, "HM-LC-Sw1-Pl-2")
	rf.EnableQuirks()
	root := rootOf(t, rf, "HM-LC-Sw1-Pl-2")
	var fault *xmlrpc.Fault
	if _, err := rf.GetMetadata(root, "unset"); !errors.As(err, &fault) || fault.Message != "Failure" {
		t.Fatalf("rfd getMetadata of an unset key: err = %v, want -1 Failure", err)
	}
	if _, err := rf.AllMetadata(root); !errors.As(err, &fault) {
		t.Fatalf("rfd getAllMetadata with nothing stored: err = %v", err)
	}
	rf.SetMetadata(root, "k", "v")
	if v, err := rf.GetMetadata(root, "k"); err != nil || v != "v" {
		t.Fatalf("stored metadata = %v, %v", v, err)
	}

	ip := newRPCWith(t, hmconst.InterfaceHmIPRF, "HmIP-SWSD")
	ip.EnableQuirks()
	if v, err := ip.GetMetadata(rootOf(t, ip, "HmIP-SWSD"), "unset"); err != nil || v != "" {
		t.Fatalf("hmipserver getMetadata of an unset key = %#v, %v; want empty", v, err)
	}
}

func TestQuirkSenderBrokenOnInternalLinks(t *testing.T) {
	rpc := newRPCWith(t, hmconst.InterfaceBidCosRF, "HM-LC-Sw1-Pl-2")
	root := rootOf(t, rpc, "HM-LC-Sw1-Pl-2")
	internal, external := root+":1", "OTHER00001:1"
	rpc.AddLink(internal, internal, "", "")
	rpc.AddLink(internal, external, "", "")

	if _, ok := rpc.GetLinks("", 0)[0].(map[string]any)["FLAGS"]; ok {
		t.Fatal("FLAGS reported without quirks")
	}
	rpc.EnableQuirks()
	flags := func(links []any) map[string]any {
		out := make(map[string]any)
		for _, l := range links {
			m := l.(map[string]any)
			out[m["RECEIVER"].(string)] = m["FLAGS"]
		}
		return out
	}
	all := flags(rpc.GetLinks("", 0))
	if all[internal] != 1 || all[external] != 0 {
		t.Fatalf("unfiltered FLAGS = %v, want SENDER_BROKEN on the internal link only", all)
	}
	filtered := flags(rpc.GetLinks(internal, 0))
	if filtered[internal] != 0 {
		t.Fatalf("filtered FLAGS = %v, want the stored 0", filtered)
	}
}
