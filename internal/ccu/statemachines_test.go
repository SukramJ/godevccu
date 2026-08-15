// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Tests for the device state machines and the batched event dispatcher.
// All three behaviours are opt-in, so each test also pins that nothing
// changes for a run that did not ask for them.

package ccu_test

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
)

// eventRecorder collects events fired into the in-process callbacks.
type eventRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	address  string
	valueKey string
	value    any
}

func (r *eventRecorder) record(_, address, valueKey string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{address, valueKey, value})
}

// valuesOf returns every value reported for a parameter, in order.
func (r *eventRecorder) valuesOf(parameter string) []any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []any
	for _, ev := range r.events {
		if ev.valueKey == parameter {
			out = append(out, ev.value)
		}
	}
	return out
}

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestUnreachableLatchesStickyUnreach covers the acknowledgement
// gesture: UNREACH follows the device, STICKY_UNREACH latches until a
// client writes it back.
func TestUnreachableLatchesStickyUnreach(t *testing.T) {
	rpc := newRPC(t)
	rpc.EnableReachability(0)
	root := rootAddress(t, rpc)

	rec := &eventRecorder{}
	rpc.RegisterParamsetCallback(rec.record)

	if err := rpc.SetDeviceUnreachable(root, true); err != nil {
		t.Fatalf("SetDeviceUnreachable: %v", err)
	}
	maintenance := root + ":0"
	if got, _ := rpc.GetValue(maintenance, "UNREACH"); got != true {
		t.Errorf("UNREACH = %v, want true", got)
	}
	if got, _ := rpc.GetValue(maintenance, "STICKY_UNREACH"); got != true {
		t.Errorf("STICKY_UNREACH = %v, want true", got)
	}

	// Recovering clears UNREACH but leaves the latch standing — that is
	// the whole point of a sticky flag.
	if err := rpc.SetDeviceUnreachable(root, false); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if got, _ := rpc.GetValue(maintenance, "UNREACH"); got != false {
		t.Errorf("UNREACH after recovery = %v, want false", got)
	}
	if got, _ := rpc.GetValue(maintenance, "STICKY_UNREACH"); got != true {
		t.Errorf("STICKY_UNREACH must survive recovery, got %v", got)
	}

	if values := rec.valuesOf("UNREACH"); len(values) != 2 {
		t.Errorf("expected both UNREACH edges as events, got %v", values)
	}
}

func TestSetDeviceUnreachableRejectsUnknownDevice(t *testing.T) {
	rpc := newRPC(t)
	rpc.EnableReachability(0)
	if err := rpc.SetDeviceUnreachable("VCU0000404", true); err == nil {
		t.Fatal("expected an error for an unknown device")
	}
}

// TestConfigPendingPulse covers the rising and falling edge a MASTER
// write produces.
func TestConfigPendingPulse(t *testing.T) {
	rpc := newRPC(t)
	rpc.EnableReachability(50 * time.Millisecond)
	root := rootAddress(t, rpc)

	rec := &eventRecorder{}
	rpc.RegisterParamsetCallback(rec.record)

	master, err := rpc.GetParamsetDescription(root+":0", "MASTER")
	if err != nil || len(master) == 0 {
		t.Skipf("device has no MASTER paramset on channel 0: %v", err)
	}
	var parameter string
	for name := range master {
		parameter = name
		break
	}
	current, _ := rpc.GetParamset(root+":0", "MASTER")
	if err := rpc.PutParamset(root+":0", "MASTER", map[string]any{parameter: current[parameter]}, true); err != nil {
		t.Fatalf("PutParamset: %v", err)
	}

	if !waitFor(t, func() bool {
		values := rec.valuesOf("CONFIG_PENDING")
		return len(values) >= 2
	}) {
		t.Fatalf("expected a rising and a falling edge, got %v", rec.valuesOf("CONFIG_PENDING"))
	}
	values := rec.valuesOf("CONFIG_PENDING")
	if values[0] != true {
		t.Errorf("first edge = %v, want true", values[0])
	}
	if values[len(values)-1] != false {
		t.Errorf("last edge = %v, want false", values[len(values)-1])
	}
}

// A run that did not opt in must see no maintenance traffic at all.
func TestNoConfigPendingByDefault(t *testing.T) {
	rpc := newRPC(t)
	root := rootAddress(t, rpc)

	rec := &eventRecorder{}
	rpc.RegisterParamsetCallback(rec.record)

	master, err := rpc.GetParamsetDescription(root+":0", "MASTER")
	if err != nil || len(master) == 0 {
		t.Skip("device has no MASTER paramset on channel 0")
	}
	var parameter string
	for name := range master {
		parameter = name
		break
	}
	current, _ := rpc.GetParamset(root+":0", "MASTER")
	_ = rpc.PutParamset(root+":0", "MASTER", map[string]any{parameter: current[parameter]}, true)
	time.Sleep(300 * time.Millisecond)

	if values := rec.valuesOf("CONFIG_PENDING"); len(values) != 0 {
		t.Fatalf("CONFIG_PENDING reported without opting in: %v", values)
	}
}

// ─────────────────────────────────────────────────────────────────
// Service messages
// ─────────────────────────────────────────────────────────────────

func TestServiceStatesDerivedFromMaintenance(t *testing.T) {
	rpc := newRPC(t)
	rpc.EnableReachability(0)
	rpc.EnableServiceMessages()
	root := rootAddress(t, rpc)

	if states := rpc.ServiceStates(); len(states) != 0 {
		t.Fatalf("expected no service messages on a healthy device, got %v", states)
	}

	if err := rpc.SetDeviceUnreachable(root, true); err != nil {
		t.Fatalf("SetDeviceUnreachable: %v", err)
	}
	states := rpc.ServiceStates()
	if len(states) == 0 {
		t.Fatal("an unreachable device must raise a service message")
	}
	var sawUnreach bool
	for _, s := range states {
		if s.Parameter == ccu.ParamUnreach && strings.EqualFold(s.Address, root+":0") {
			sawUnreach = true
		}
	}
	if !sawUnreach {
		t.Errorf("no UNREACH service message: %v", states)
	}
}

func TestSuppressionFiltersServiceStates(t *testing.T) {
	rpc := newRPC(t)
	rpc.EnableReachability(0)
	rpc.EnableServiceMessages()
	root := rootAddress(t, rpc)
	maintenance := root + ":0"

	if err := rpc.SetDeviceUnreachable(root, true); err != nil {
		t.Fatalf("SetDeviceUnreachable: %v", err)
	}
	rpc.SuppressServiceMessage(maintenance, ccu.ParamUnreach, true)

	for _, s := range rpc.ServiceStates() {
		if s.Parameter == ccu.ParamUnreach {
			t.Fatalf("suppressed parameter still reported: %v", s)
		}
	}
	if got := rpc.SuppressedServiceMessages(maintenance); len(got) != 1 || got[0] != ccu.ParamUnreach {
		t.Errorf("suppression list = %v, want [UNREACH]", got)
	}

	// Un-suppressing brings it back.
	rpc.SuppressServiceMessage(maintenance, ccu.ParamUnreach, false)
	var back bool
	for _, s := range rpc.ServiceStates() {
		if s.Parameter == ccu.ParamUnreach {
			back = true
		}
	}
	if !back {
		t.Error("un-suppressing did not restore the message")
	}
}

// An empty parameter id silences the whole channel, as the WebUI does.
func TestSuppressWholeChannel(t *testing.T) {
	rpc := newRPC(t)
	rpc.EnableReachability(0)
	rpc.EnableServiceMessages()
	root := rootAddress(t, rpc)

	if err := rpc.SetDeviceUnreachable(root, true); err != nil {
		t.Fatalf("SetDeviceUnreachable: %v", err)
	}
	rpc.SuppressServiceMessage(root+":0", "", true)
	if states := rpc.ServiceStates(); len(states) != 0 {
		t.Fatalf("channel-wide suppression left messages: %v", states)
	}
}

func TestNoServiceStatesByDefault(t *testing.T) {
	rpc := newRPC(t)
	if states := rpc.ServiceStates(); states != nil {
		t.Fatalf("service derivation active without opting in: %v", states)
	}
}

// ─────────────────────────────────────────────────────────────────
// Batched delivery
// ─────────────────────────────────────────────────────────────────

// TestBatchedEventsUseMulticall covers the delivery shape: several
// events accumulated within the linger window travel as one
// system.multicall, the way a CCU's dispatcher sends them.
func TestBatchedEventsUseMulticall(t *testing.T) {
	rpc := newRPC(t)
	rpc.EnableBatchEvents()
	remote := newRecordingRemote(t, okResponse)

	const interfaceID = "batch-client"
	rpc.Init(remote.srv.URL, interfaceID)
	time.Sleep(150 * time.Millisecond)

	for i := 0; i < 5; i++ {
		rpc.FireEvent(interfaceID, "VCU0000001:1", "STATE", i%2 == 0)
	}

	if !waitFor(t, func() bool { return remote.containing("system.multicall") }) {
		t.Fatalf("events were not bundled; bodies: %v", remote.received())
	}
}

// Delivery must not block the caller: a receiver that stalls still lets
// FireEvent return immediately.
func TestBatchedDeliveryDoesNotBlockCaller(t *testing.T) {
	rpc := newRPC(t)
	rpc.EnableBatchEvents()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	remote := newBlockingRemote(t, release)

	const interfaceID = "slow-client"
	rpc.Init(remote.URL(), interfaceID)
	time.Sleep(150 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		rpc.FireEvent(interfaceID, "VCU0000001:1", "STATE", true)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("FireEvent blocked on a stalled callback receiver")
	}
}

// ─────────────────────────────────────────────────────────────────
// Persisted callback registrations
// ─────────────────────────────────────────────────────────────────

// TestInitRegistrationsSurviveRestart pins the reconnect a rebooting
// CCU performs: a client that registered before the restart is pushed
// to again afterwards, without re-registering.
func TestInitRegistrationsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "paramsets.json")
	remote := newRecordingRemote(t, okResponse)
	const interfaceID = "persistent-client"

	first, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:         []string{"HmIP-SWSD"},
		Persistence:     true,
		PersistencePath: dbPath,
	})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	first.EnableInitPersistence()
	first.Init(remote.srv.URL, interfaceID)
	time.Sleep(150 * time.Millisecond)
	if err := first.SaveRegistrations(); err != nil {
		t.Fatalf("SaveRegistrations: %v", err)
	}

	// A fresh instance over the same persistence path is the restart.
	second, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:         []string{"HmIP-SWSD"},
		Persistence:     true,
		PersistencePath: dbPath,
	})
	if err != nil {
		t.Fatalf("NewRPCFunctions (restart): %v", err)
	}
	second.EnableInitPersistence()

	if !second.ClientServerInitialized(interfaceID) {
		t.Fatal("registration was not restored after restart")
	}

	before := len(remote.received())
	second.FireEvent(interfaceID, "VCU0000001:1", "STATE", true)
	if !waitFor(t, func() bool { return len(remote.received()) > before }) {
		t.Fatal("restored client received no events")
	}
}

// Without the opt-in a restart forgets its clients, as before.
func TestInitRegistrationsForgottenByDefault(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "paramsets.json")
	remote := newRecordingRemote(t, okResponse)
	const interfaceID = "transient-client"

	first, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:         []string{"HmIP-SWSD"},
		Persistence:     true,
		PersistencePath: dbPath,
	})
	if err != nil {
		t.Fatalf("NewRPCFunctions: %v", err)
	}
	first.Init(remote.srv.URL, interfaceID)
	time.Sleep(150 * time.Millisecond)
	if err := first.SaveRegistrations(); err != nil {
		t.Fatalf("SaveRegistrations: %v", err)
	}

	second, err := ccu.NewRPCFunctions(ccu.Options{
		Devices:         []string{"HmIP-SWSD"},
		Persistence:     true,
		PersistencePath: dbPath,
	})
	if err != nil {
		t.Fatalf("NewRPCFunctions (restart): %v", err)
	}
	if second.ClientServerInitialized(interfaceID) {
		t.Fatal("registration restored without opting in")
	}
}
