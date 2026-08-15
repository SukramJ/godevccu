// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ssdp_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/godevccu/internal/ssdp"
)

const (
	testLocation = "http://127.0.0.1:8080/upnp/basic_dev.cgi"
	testUDN      = "uuid:30303030-3030-3030-3030-303030303031"
)

// startResponder binds a responder on a private loopback port rather
// than joining the real multicast group, so the test never depends on
// the machine's network configuration.
func startResponder(t *testing.T) *ssdp.Responder {
	t.Helper()
	r := ssdp.New(ssdp.Config{
		Location:      testLocation,
		UDN:           testUDN,
		Server:        "godevccu/3.87.1 UPnP/1.0",
		ListenAddress: "127.0.0.1:0",
	})
	if err := r.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = r.Stop() })
	return r
}

// search sends an M-SEARCH with the given target and returns the reply.
func search(t *testing.T, r *ssdp.Responder, searchTarget string) string {
	t.Helper()
	addr, ok := r.LocalAddr().(*net.UDPAddr)
	if !ok || addr == nil {
		t.Fatal("responder not bound")
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	request := strings.Join([]string{
		"M-SEARCH * HTTP/1.1",
		"HOST: 239.255.255.250:1900",
		`MAN: "ssdp:discover"`,
		"MX: 1",
		"ST: " + searchTarget,
		"", "",
	}, "\r\n")
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		return ""
	}
	return string(buf[:n])
}

func TestSearchResponseCarriesLocationAndUSN(t *testing.T) {
	r := startResponder(t)
	reply := search(t, r, "upnp:rootdevice")
	if reply == "" {
		t.Fatal("no answer to an M-SEARCH for upnp:rootdevice")
	}
	for _, want := range []string{
		"HTTP/1.1 200 OK",
		"LOCATION: " + testLocation,
		"ST: upnp:rootdevice",
		"USN: " + testUDN + "::upnp:rootdevice",
		"CACHE-CONTROL: max-age=",
	} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply missing %q:\n%s", want, reply)
		}
	}
}

// ssdp:all is the target a broad discovery sweep sends.
func TestSearchAllIsAnswered(t *testing.T) {
	r := startResponder(t)
	if reply := search(t, r, "ssdp:all"); reply == "" {
		t.Fatal("no answer to ssdp:all")
	}
}

// A search for the device's own UDN reports the bare USN.
func TestSearchByUDN(t *testing.T) {
	r := startResponder(t)
	reply := search(t, r, testUDN)
	if reply == "" {
		t.Fatal("no answer to a UDN-targeted search")
	}
	if !strings.Contains(reply, "USN: "+testUDN+"\r\n") {
		t.Errorf("USN should be the bare UDN:\n%s", reply)
	}
}

// A search for someone else's device must stay unanswered, or a
// discovering client attributes the reply to the wrong device.
func TestForeignSearchTargetIgnored(t *testing.T) {
	r := startResponder(t)
	if reply := search(t, r, "urn:schemas-upnp-org:device:MediaServer:1"); reply != "" {
		t.Fatalf("answered a foreign search target:\n%s", reply)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	r := startResponder(t)
	if err := r.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := r.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if r.LocalAddr() != nil {
		t.Error("LocalAddr still reports an address after Stop")
	}
}

func TestDoubleStartRejected(t *testing.T) {
	r := startResponder(t)
	if err := r.Start(); err == nil {
		t.Fatal("expected an error when starting twice")
	}
}
