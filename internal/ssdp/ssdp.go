// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package ssdp implements the SSDP/UPnP discovery a CCU answers on.
//
// This is how a client finds a central without being told its address:
// it multicasts an M-SEARCH to 239.255.255.250:1900, and every CCU on
// the segment answers with a unicast HTTP-over-UDP response pointing at
// its device description. A CCU additionally announces itself with
// periodic ssdp:alive notifications so listeners learn about it without
// searching.
//
// pydevccu has no discovery at all, so the responder is opt-in — see
// virtualccu.Realism.Discovery.
package ssdp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	// multicastAddr is the SSDP group every UPnP participant joins.
	multicastAddr = "239.255.255.250:1900"

	// aliveInterval is how often a CCU re-announces itself. The
	// matching CACHE-CONTROL max-age is twice that, per the UPnP
	// convention of announcing at least twice per lifetime.
	aliveInterval = 30 * time.Minute
	maxAge        = 2 * aliveInterval
)

// Config describes the device to announce.
type Config struct {
	// Location is the absolute URL of the device description, e.g.
	// "http://192.168.1.10:80/upnp/basic_dev.cgi".
	Location string
	// UDN is the unique device name, "uuid:…".
	UDN string
	// Server is the SERVER header value; a CCU reports its firmware.
	Server string
	// Logger sinks diagnostics. Defaults to slog.Default().
	Logger *slog.Logger
	// ListenAddress overrides the multicast address. Tests bind a
	// private port instead of joining the real group.
	ListenAddress string
}

// Responder answers M-SEARCH and sends periodic announcements.
type Responder struct {
	cfg    Config
	logger *slog.Logger

	mu   sync.Mutex
	conn *net.UDPConn
	// cancel stops the receive loop and the announcement ticker. Every
	// goroutine this type starts hangs off it, per the project rule
	// that no goroutine may outlive an explicit stop path.
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New builds a Responder.
func New(cfg Config) *Responder {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = multicastAddr
	}
	if cfg.Server == "" {
		cfg.Server = "godevccu UPnP/1.0"
	}
	return &Responder{cfg: cfg, logger: logger}
}

// searchTargets are the ST values a CCU answers to. "ssdp:all" and the
// root-device target are what discovery clients actually send.
var searchTargets = []string{"ssdp:all", "upnp:rootdevice"}

// Start joins the multicast group and begins serving. Stop tears it
// down again.
func (r *Responder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn != nil {
		return fmt.Errorf("ssdp: already started")
	}
	addr, err := net.ResolveUDPAddr("udp4", r.cfg.ListenAddress)
	if err != nil {
		return fmt.Errorf("ssdp: resolve %q: %w", r.cfg.ListenAddress, err)
	}
	var conn *net.UDPConn
	if addr.IP != nil && addr.IP.IsMulticast() {
		conn, err = net.ListenMulticastUDP("udp4", nil, addr)
	} else {
		conn, err = net.ListenUDP("udp4", addr)
	}
	if err != nil {
		return fmt.Errorf("ssdp: listen: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.conn = conn
	r.cancel = cancel

	r.wg.Add(2)
	go func() {
		defer r.wg.Done()
		r.serve(ctx, conn)
	}()
	go func() {
		defer r.wg.Done()
		r.announce(ctx, conn)
	}()
	return nil
}

// LocalAddr returns the bound address, or nil before Start.
func (r *Responder) LocalAddr() net.Addr {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return nil
	}
	return r.conn.LocalAddr()
}

// Stop shuts the responder down and waits for its goroutines.
func (r *Responder) Stop() error {
	r.mu.Lock()
	cancel := r.cancel
	conn := r.conn
	r.cancel = nil
	r.conn = nil
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	err := conn.Close()
	r.wg.Wait()
	return err
}

// serve reads datagrams and answers the M-SEARCH ones.
func (r *Responder) serve(ctx context.Context, conn *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		if ctx.Err() != nil {
			return
		}
		// A deadline keeps the read from blocking past Stop even if
		// the connection close races with it.
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if ok := asNetError(err, &netErr); ok && netErr.Timeout() {
				continue
			}
			return
		}
		request := string(buf[:n])
		if !strings.HasPrefix(request, "M-SEARCH") {
			continue
		}
		st := headerValue(request, "ST")
		if !r.answers(st) {
			continue
		}
		if _, err := conn.WriteToUDP([]byte(r.searchResponse(st)), from); err != nil {
			r.logger.Debug("ssdp: reply failed", "to", from, "err", err)
		}
	}
}

// answers reports whether a search target addresses this device.
func (r *Responder) answers(st string) bool {
	if st == "" {
		return false
	}
	if st == r.cfg.UDN {
		return true
	}
	for _, target := range searchTargets {
		if strings.EqualFold(st, target) {
			return true
		}
	}
	return false
}

// announce sends ssdp:alive on start and then periodically.
func (r *Responder) announce(ctx context.Context, conn *net.UDPConn) {
	target, err := net.ResolveUDPAddr("udp4", multicastAddr)
	if err != nil {
		return
	}
	ticker := time.NewTicker(aliveInterval)
	defer ticker.Stop()
	for {
		if _, err := conn.WriteToUDP([]byte(r.aliveNotification()), target); err != nil {
			r.logger.Debug("ssdp: announcement failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// searchResponse is the unicast answer to an M-SEARCH.
func (r *Responder) searchResponse(st string) string {
	return strings.Join([]string{
		"HTTP/1.1 200 OK",
		fmt.Sprintf("CACHE-CONTROL: max-age=%d", int(maxAge.Seconds())),
		"EXT:",
		"LOCATION: " + r.cfg.Location,
		"SERVER: " + r.cfg.Server,
		"ST: " + st,
		"USN: " + r.usn(st),
		"", "",
	}, "\r\n")
}

// aliveNotification is the multicast announcement.
func (r *Responder) aliveNotification() string {
	return strings.Join([]string{
		"NOTIFY * HTTP/1.1",
		"HOST: " + multicastAddr,
		fmt.Sprintf("CACHE-CONTROL: max-age=%d", int(maxAge.Seconds())),
		"LOCATION: " + r.cfg.Location,
		"SERVER: " + r.cfg.Server,
		"NT: upnp:rootdevice",
		"NTS: ssdp:alive",
		"USN: " + r.usn("upnp:rootdevice"),
		"", "",
	}, "\r\n")
}

// usn composes the unique service name: the UDN alone when the search
// targets the device itself, otherwise UDN::<target>.
func (r *Responder) usn(st string) string {
	if st == "" || st == r.cfg.UDN {
		return r.cfg.UDN
	}
	return r.cfg.UDN + "::" + st
}

// headerValue extracts a header from an HTTP-over-UDP message. Values
// may be quoted, as M-SEARCH's MAN header is.
func headerValue(message, name string) string {
	prefix := strings.ToLower(name) + ":"
	for _, line := range strings.Split(message, "\r\n") {
		if !strings.HasPrefix(strings.ToLower(line), prefix) {
			continue
		}
		value := strings.TrimSpace(line[len(prefix):])
		return strings.Trim(value, `"`)
	}
	return ""
}

// asNetError is errors.As specialised to net.Error, kept local so the
// read loop stays readable.
func asNetError(err error, target *net.Error) bool {
	return errors.As(err, target)
}
