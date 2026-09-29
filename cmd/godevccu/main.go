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
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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
	cfg.XMLRPCPort = *xmlRPCPort
	cfg.JSONRPCPort = *jsonRPCPort
	cfg.Username = *username
	cfg.Password = *password
	cfg.AuthEnabled = *auth
	cfg.Persistence = *persistence
	cfg.SetupDefaults = *defaults
	cfg.EnableLogic = *logic
	cfg.Logger = logger

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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.Info("godevccu shutting down")
	return v.Stop()
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
