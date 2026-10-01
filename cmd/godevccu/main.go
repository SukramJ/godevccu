// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Command godevccu starts a virtual HomeMatic CCU on the chosen ports,
// or — with -mode lite — a fake openccu-lite box serving the occulited
// HTTP API over the same simulated devices. It is a thin wrapper around
// pkg/godevccu and pkg/litefake, useful for trying the simulator
// interactively or as a standalone counterpart for integration runs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SukramJ/godevccu/pkg/godevccu"
	"github.com/SukramJ/godevccu/pkg/litefake"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "godevccu:", err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "openccu", "backend mode: homegear | ccu | openccu | lite")
	host := flag.String("host", godevccu.IPLocalhostV4, "bind address")
	xmlRPCPort := flag.Int("xml-rpc-port", godevccu.PortRF, "XML-RPC port")
	jsonRPCPort := flag.Int("json-rpc-port", 8080, "JSON-RPC port (CCU/OpenCCU mode only)")
	username := flag.String("username", "Admin", "JSON-RPC username")
	password := flag.String("password", "", "JSON-RPC password")
	auth := flag.Bool("auth", true, "require authentication on JSON-RPC")
	persistence := flag.Bool("persistence", false, "persist paramset values to disk")
	defaults := flag.Bool("defaults", false, "seed default programs/sysvars/rooms/functions")
	logic := flag.Bool("logic", false, "enable device behaviour simulators (HM-Sec-SC-2, HM-Sen-MDIR-WM55)")
	debug := flag.Bool("debug", false, "enable debug logging")
	showVersion := flag.Bool("version", false, "print godevccu version and exit")
	liteListen := flag.String("lite-listen", ":2121", "lite mode: address the box API listens on; point the client's web-server port at it")
	liteTokens := flag.String("lite-tokens", "", "lite mode: comma-separated API tokens, each with every scope (empty: the built-in default token)")
	liteDevices := flag.String("lite-devices", "", "lite mode: comma-separated device types to load (empty: every embedded type)")
	liteInterfaces := flag.String("lite-interfaces", "", "lite mode: comma-separated interfaces (empty: BidCos-RF, HmIP-RF, VirtualDevices)")
	liteTLS := flag.Bool("lite-tls", false, "lite mode: serve HTTPS with a self-signed certificate, the way a box does")
	realism := flag.Bool("realism", false, "enable every real-CCU behaviour (Realism CCU): fault codes, interface quirks, MASTER write models, ...")
	interfaces := flag.String("interfaces", "", "separate interface listeners, e.g. BidCos-RF=2001,HmIP-RF=2010 (port 0: the system picks one)")
	controlPort := flag.Int("control-port", -1, "serve the scenario API over HTTP on 127.0.0.1 at this port (0: the system picks one; default off)")
	portsJSON := flag.String("ports-json", "", "once every server listens, write the bound ports as JSON to this file, or as one line to stdout with \"-\"")
	flag.Parse()

	if *showVersion {
		fmt.Println("godevccu", godevccu.Version)
		return nil
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	if *mode == "lite" {
		return runLite(logger, *liteListen, *liteTokens, *liteDevices, *liteInterfaces, *liteTLS)
	}

	parsedMode, err := parseMode(*mode)
	if err != nil {
		return err
	}

	cfg := godevccu.Defaults()
	cfg.Mode = parsedMode
	cfg.Host = *host
	// Port 0 lets the system pick, as everywhere else on the command
	// line; Config spells that EphemeralPort.
	cfg.XMLRPCPort = ephemeral(*xmlRPCPort)
	cfg.JSONRPCPort = ephemeral(*jsonRPCPort)
	cfg.Username = *username
	cfg.Password = *password
	cfg.AuthEnabled = *auth
	cfg.Persistence = *persistence
	cfg.SetupDefaults = *defaults
	cfg.EnableLogic = *logic
	cfg.Logger = logger
	if *realism {
		cfg.Realism = godevccu.RealismCCU()
	}
	if *interfaces != "" {
		ifacePorts, parseErr := parseInterfaces(*interfaces)
		if parseErr != nil {
			return parseErr
		}
		cfg.InterfacePorts = ifacePorts
	}

	v, err := godevccu.New(cfg)
	if err != nil {
		return err
	}
	if err := v.Start(); err != nil {
		return err
	}
	logger.Info(
		"godevccu listening",
		"mode", cfg.Mode.String(),
		"xml-rpc", v.XMLRPCAddr(),
		"json-rpc", v.JSONRPCAddr(),
	)

	ports := v.Ports()
	var control *http.Server
	if *controlPort >= 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort(godevccu.IPLocalhostV4, strconv.Itoa(*controlPort)))
		if err != nil {
			_ = v.Stop()
			return fmt.Errorf("control port: %w", err)
		}
		control = &http.Server{Handler: v.ControlHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			if err := control.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("control port failed", "err", err)
			}
		}()
		ports["control"] = ln.Addr().(*net.TCPAddr).Port
		logger.Info("control port listening", "addr", ln.Addr())
	}
	if *portsJSON != "" {
		if err := writePorts(*portsJSON, ports); err != nil {
			_ = v.Stop()
			return err
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.Info("godevccu shutting down")
	if control != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = control.Shutdown(ctx)
		cancel()
	}
	return v.Stop()
}

// ephemeral maps the command line's port 0 onto [godevccu.EphemeralPort].
func ephemeral(port int) int {
	if port == 0 {
		return godevccu.EphemeralPort
	}
	return port
}

// parseInterfaces parses "Name=port,Name=port" into InterfacePorts.
func parseInterfaces(s string) (map[string]int, error) {
	out := make(map[string]int)
	for _, item := range splitList(s) {
		name, portText, ok := strings.Cut(item, "=")
		if !ok {
			// A bare name serves the interface on its CCU port.
			out[item] = godevccu.DefaultInterfacePorts()[item]
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(portText))
		if err != nil {
			return nil, fmt.Errorf("interface %q: port %q: %w", name, portText, err)
		}
		out[strings.TrimSpace(name)] = ephemeral(port)
	}
	return out, nil
}

// writePorts writes the bound ports as JSON: one line to stdout for
// "-", otherwise atomically to a file, so a reader never sees half of
// it.
func writePorts(target string, ports map[string]int) error {
	data, err := json.Marshal(ports)
	if err != nil {
		return err
	}
	if target == "-" {
		_, err := fmt.Fprintln(os.Stdout, string(data))
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("ports-json: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("ports-json: %w", err)
	}
	return nil
}

func parseMode(s string) (godevccu.BackendMode, error) {
	switch s {
	case "homegear":
		return godevccu.BackendModeHomegear, nil
	case "ccu":
		return godevccu.BackendModeCCU, nil
	case "openccu":
		return godevccu.BackendModeOpenCCU, nil
	}
	return 0, fmt.Errorf("unknown mode %q (use homegear|ccu|openccu|lite)", s)
}

// runLite starts a fake openccu-lite box on a fixed address and blocks
// until SIGINT/SIGTERM. With no device restriction the whole embedded
// fleet is loaded, so a client sees every device type godevccu carries.
func runLite(logger *slog.Logger, listen, tokens, devices, interfaces string, tls bool) error {
	opts := litefake.Options{
		ListenAddr: listen,
		Devices:    []string{}, // every embedded type
		TLS:        tls,
		Logger:     logger,
	}
	if devices != "" {
		opts.Devices = splitList(devices)
	}
	if interfaces != "" {
		opts.Interfaces = splitList(interfaces)
	}
	token := litefake.DefaultToken
	if tokens != "" {
		all := map[string][]string{}
		for _, t := range splitList(tokens) {
			all[t] = []string{"*"}
		}
		opts.Tokens = all
		token = splitList(tokens)[0]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	f, err := litefake.Start(ctx, opts)
	cancel()
	if err != nil {
		return err
	}

	for _, iface := range ifaceNames(opts.Interfaces) {
		rpc := f.V().InterfaceRPC(iface)
		if rpc == nil {
			continue
		}
		devs, channels := 0, 0
		for _, d := range rpc.ListDevices() {
			if p, ok := d["PARENT"].(string); !ok || p == "" {
				devs++
				continue
			}
			channels++
		}
		logger.Info("lite interface up", "interface", iface, "devices", devs, "channels", channels)
	}
	logger.Info("fake openccu-lite box listening", "url", f.URL(), "token", token)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.Info("fake openccu-lite box shutting down")
	return f.Close()
}

// ifaceNames resolves the interface set the fake was started with.
func ifaceNames(interfaces []string) []string {
	if interfaces == nil {
		return litefake.DefaultInterfaces()
	}
	return interfaces
}

// splitList parses a comma-separated flag value, trimming blanks.
func splitList(s string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
