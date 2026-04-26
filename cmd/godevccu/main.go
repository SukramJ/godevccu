// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Command godevccu starts a virtual HomeMatic CCU on the chosen ports.
// It is a thin wrapper around pkg/godevccu — useful for trying the
// simulator interactively.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/SukramJ/godevccu/pkg/godevccu"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "godevccu:", err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "openccu", "backend mode: homegear | ccu | openccu")
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
	logger.Info("godevccu listening",
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
	return 0, fmt.Errorf("unknown mode %q (use homegear|ccu|openccu)", s)
}
