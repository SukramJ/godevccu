// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Tests for the two callback-path behaviours a real CCU exhibits and the
// simulator did not: answering a ping with a CENTRAL/PONG event, and
// keeping a remote registered when it replies with a fault rather than
// dropping off the network.

package ccu_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingRemote is an XML-RPC callback receiver that records every
// request body and answers with the configured reply.
type recordingRemote struct {
	mu     sync.Mutex
	bodies []string
	srv    *httptest.Server
}

const okResponse = `<?xml version="1.0"?><methodResponse><params><param>` +
	`<value><i4>0</i4></value></param></params></methodResponse>`

// faultResponse is an application-level error: the client answered, it
// is not gone.
const faultResponse = `<?xml version="1.0"?><methodResponse><fault><value><struct>` +
	`<member><name>faultCode</name><value><i4>-1</i4></value></member>` +
	`<member><name>faultString</name><value><string>boom</string></value></member>` +
	`</struct></value></fault></methodResponse>`

func newRecordingRemote(t *testing.T, reply string) *recordingRemote {
	t.Helper()
	rr := &recordingRemote{}
	rr.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rr.mu.Lock()
		rr.bodies = append(rr.bodies, string(body))
		rr.mu.Unlock()
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(rr.srv.Close)
	return rr
}

func (rr *recordingRemote) received() []string {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return append([]string(nil), rr.bodies...)
}

// containing reports whether any recorded body contains all fragments.
func (rr *recordingRemote) containing(fragments ...string) bool {
	for _, body := range rr.received() {
		all := true
		for _, f := range fragments {
			if !strings.Contains(body, f) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// TestPingFiresPongEvent covers the handshake a client uses to keep its
// connection state healthy: ping(callerID) must be answered with an
// event(interfaceID, "CENTRAL", "PONG", callerID) carrying the caller id
// back unchanged, so the client can match it against the token it sent.
func TestPingFiresPongEvent(t *testing.T) {
	rpc := newRPC(t)
	remote := newRecordingRemote(t, okResponse)

	const interfaceID = "ccu-test"
	rpc.Init(remote.srv.URL, interfaceID)
	time.Sleep(150 * time.Millisecond)

	callerID := interfaceID + "#2026-08-15 12:00:00.000"
	if !rpc.Ping(callerID) {
		t.Fatal("Ping returned false")
	}
	time.Sleep(150 * time.Millisecond)

	if !remote.containing("event", "CENTRAL", "PONG", callerID) {
		t.Fatalf("no CENTRAL/PONG event with the caller id; bodies: %v", remote.received())
	}
}

// A ping without a caller id (the plain liveness probe) must not
// generate an event.
func TestPingWithoutCallerIDIsSilent(t *testing.T) {
	rpc := newRPC(t)
	remote := newRecordingRemote(t, okResponse)

	rpc.Init(remote.srv.URL, "ccu-test")
	time.Sleep(150 * time.Millisecond)
	before := len(remote.received())

	rpc.Ping("")
	time.Sleep(150 * time.Millisecond)

	for _, body := range remote.received()[before:] {
		if strings.Contains(body, "PONG") {
			t.Fatalf("unexpected PONG for an empty caller id: %s", body)
		}
	}
}

// TestFaultKeepsRemoteRegistered pins the callback-robustness fix: an
// application-level fault is the client *answering*. A real CCU keeps
// delivering; the simulator used to drop the remote on the first fault,
// silently ending event delivery for the rest of the session.
func TestFaultKeepsRemoteRegistered(t *testing.T) {
	rpc := newRPC(t)
	remote := newRecordingRemote(t, faultResponse)

	const interfaceID = "faulty-client"
	rpc.Init(remote.srv.URL, interfaceID)
	time.Sleep(150 * time.Millisecond)

	rpc.FireEvent(interfaceID, "VCU0000001:1", "STATE", true)
	time.Sleep(150 * time.Millisecond)

	if !rpc.ClientServerInitialized(interfaceID) {
		t.Fatal("remote was deregistered after a fault — only transport errors mean the client is gone")
	}

	// And it must still receive the next event.
	before := len(remote.received())
	rpc.FireEvent(interfaceID, "VCU0000001:1", "STATE", false)
	time.Sleep(150 * time.Millisecond)
	if len(remote.received()) <= before {
		t.Fatal("no further event delivered after the fault")
	}
}

// A dead remote (connection refused) must still be dropped.
func TestTransportErrorDeregistersRemote(t *testing.T) {
	rpc := newRPC(t)
	remote := newRecordingRemote(t, okResponse)

	const interfaceID = "dead-client"
	url := remote.srv.URL
	rpc.Init(url, interfaceID)
	time.Sleep(150 * time.Millisecond)
	if !rpc.ClientServerInitialized(interfaceID) {
		t.Fatal("remote not registered")
	}

	remote.srv.Close()

	rpc.FireEvent(interfaceID, "VCU0000001:1", "STATE", true)
	time.Sleep(200 * time.Millisecond)

	if rpc.ClientServerInitialized(interfaceID) {
		t.Fatal("unreachable remote stayed registered")
	}
}

// blockingRemote is a callback receiver whose *event* handling stalls
// while registration still answers normally — the shape of a client
// that is up but slow to process what it receives.
type blockingRemote struct{ srv *httptest.Server }

func newBlockingRemote(t *testing.T, release <-chan struct{}) *blockingRemote {
	t.Helper()
	br := &blockingRemote{}
	br.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "event") {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(br.srv.Close)
	return br
}

func (br *blockingRemote) URL() string { return br.srv.URL }
