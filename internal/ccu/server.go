// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/SukramJ/godevccu/internal/devicelogic"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// Server is the threaded XML-RPC server wrapping [RPCFunctions]. It is
// the Go equivalent of pydevccu/ccu.py:ServerThread.
type Server struct {
	logger *slog.Logger
	addr   string

	rpc      *RPCFunctions
	mux      *xmlrpc.Mux
	handler  *xmlrpc.Handler
	httpSrv  *http.Server
	listener net.Listener

	mu          sync.Mutex
	logics      []devicelogic.Device
	running     bool
	logicCfg    devicelogic.Config
	enableLogic bool
}

// ServerConfig configures [NewServer].
type ServerConfig struct {
	Address     string
	Logger      *slog.Logger
	RPC         *RPCFunctions
	EnableLogic bool
	LogicConfig devicelogic.Config
}

// NewServer wires the given RPCFunctions into an XML-RPC HTTP listener.
func NewServer(cfg ServerConfig) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mux := xmlrpc.NewMux()
	handler := xmlrpc.NewHandler()
	handler.Mux = mux
	handler.Logger = logger

	s := &Server{
		logger:      logger,
		addr:        cfg.Address,
		rpc:         cfg.RPC,
		mux:         mux,
		handler:     handler,
		enableLogic: cfg.EnableLogic,
		logicCfg:    cfg.LogicConfig,
	}
	s.registerMethods()
	return s
}

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.addr }

// RPC returns the underlying RPCFunctions.
func (s *Server) RPC() *RPCFunctions { return s.rpc }

// LocalAddr returns the listener address (only valid after Start).
func (s *Server) LocalAddr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Start begins serving on the configured address.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("ccu: server already running")
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("ccu: listen: %w", err)
	}
	s.listener = ln
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 30 * time.Second,
	}
	s.httpSrv = srv
	s.rpc.SetActive(true)
	s.running = true
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("ccu: serve failed", "err", err)
		}
	}()
	if s.enableLogic {
		s.startLogic()
	}
	return nil
}

// Stop tears the server down. It is safe to call multiple times.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	for _, d := range s.logics {
		go d.Stop()
	}
	s.logics = nil
	s.rpc.SetActive(false)
	srv := s.httpSrv
	s.httpSrv = nil
	s.listener = nil
	s.mu.Unlock()

	if err := s.rpc.SaveParamsets(); err != nil {
		s.logger.Debug("ccu: save paramsets failed", "err", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func (s *Server) startLogic() {
	cfg := s.logicCfg
	if cfg.Interval == 0 && cfg.StartupDelay == 0 {
		cfg = devicelogic.DefaultConfig()
	}
	for typeName := range s.rpc.SupportedDevices() {
		ctor, ok := devicelogic.Registry[typeName]
		if !ok {
			continue
		}
		dev := ctor(s.rpc, cfg.StartupDelay, cfg.Interval)
		s.logics = append(s.logics, dev)
	}
}

// registerMethods wires every public XML-RPC method to the mux.
func (s *Server) registerMethods() {
	rpc := s.rpc
	mux := s.mux
	mux.RegisterSystemMethods()

	mux.Handle("listDevices", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.FromAny(any(toAnySlice(rpc.ListDevices()))), nil
	})

	mux.Handle("ping", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		caller := ""
		if len(params) > 0 {
			caller, _ = xmlrpc.AsString(params[0])
		}
		return xmlrpc.BoolValue(rpc.Ping(caller)), nil
	})

	mux.Handle("getVersion", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.StringValue(rpc.GetVersion()), nil
	})

	mux.Handle("getServiceMessages", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.FromAny(any(rpc.GetServiceMessages())), nil
	})

	mux.Handle("getAllSystemVariables", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.FromAny(any(rpc.GetAllSystemVariables())), nil
	})

	mux.Handle("getSystemVariable", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 1 {
			return nil, fmt.Errorf("getSystemVariable: name required")
		}
		name, _ := xmlrpc.AsString(params[0])
		return xmlrpc.StringValue(rpc.GetSystemVariable(name)), nil
	})

	mux.Handle("setSystemVariable", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 2 {
			return nil, fmt.Errorf("setSystemVariable: name and value required")
		}
		name, _ := xmlrpc.AsString(params[0])
		rpc.SetSystemVariable(name, xmlrpc.ToAny(params[1]))
		return xmlrpc.NilValue{}, nil
	})

	mux.Handle("deleteSystemVariable", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 1 {
			return nil, fmt.Errorf("deleteSystemVariable: name required")
		}
		name, _ := xmlrpc.AsString(params[0])
		rpc.DeleteSystemVariable(name)
		return xmlrpc.NilValue{}, nil
	})

	mux.Handle("getDeviceDescription", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 1 {
			return nil, fmt.Errorf("getDeviceDescription: address required")
		}
		address, _ := xmlrpc.AsString(params[0])
		d, err := rpc.GetDeviceDescription(address)
		if err != nil {
			return nil, faultFromErr(err)
		}
		return xmlrpc.FromAny(any(d)), nil
	})

	mux.Handle("getParamsetDescription", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 2 {
			return nil, fmt.Errorf("getParamsetDescription: address and paramset required")
		}
		address, _ := xmlrpc.AsString(params[0])
		paramsetType, _ := xmlrpc.AsString(params[1])
		d, err := rpc.GetParamsetDescription(address, paramsetType)
		if err != nil {
			return nil, faultFromErr(err)
		}
		return xmlrpc.FromAny(any(d)), nil
	})

	mux.Handle("getParamset", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 2 {
			return nil, fmt.Errorf("getParamset: address and paramsetKey required")
		}
		address, _ := xmlrpc.AsString(params[0])
		key, _ := xmlrpc.AsString(params[1])
		d, err := rpc.GetParamset(address, key)
		if err != nil {
			return nil, faultFromErr(err)
		}
		return xmlrpc.FromAny(any(d)), nil
	})

	mux.Handle("getValue", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 2 {
			return nil, fmt.Errorf("getValue: address and valueKey required")
		}
		address, _ := xmlrpc.AsString(params[0])
		valueKey, _ := xmlrpc.AsString(params[1])
		v, err := rpc.GetValue(address, valueKey)
		if err != nil {
			return nil, faultFromErr(err)
		}
		return xmlrpc.FromAny(v), nil
	})

	mux.Handle("setValue", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 3 {
			return nil, fmt.Errorf("setValue: address, valueKey, value required")
		}
		address, _ := xmlrpc.AsString(params[0])
		valueKey, _ := xmlrpc.AsString(params[1])
		value := xmlrpc.ToAny(params[2])
		force := false
		if len(params) > 3 {
			force, _ = xmlrpc.AsBool(params[3])
		}
		if err := rpc.SetValue(address, valueKey, value, force); err != nil {
			return nil, faultFromErr(err)
		}
		return xmlrpc.StringValue(""), nil
	})

	mux.Handle("putParamset", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 3 {
			return nil, fmt.Errorf("putParamset: address, paramsetKey, paramset required")
		}
		address, _ := xmlrpc.AsString(params[0])
		key, _ := xmlrpc.AsString(params[1])
		raw := xmlrpc.ToAny(params[2])
		paramset, _ := raw.(map[string]any)
		force := false
		if len(params) > 3 {
			force, _ = xmlrpc.AsBool(params[3])
		}
		if err := rpc.PutParamset(address, key, paramset, force); err != nil {
			return nil, faultFromErr(err)
		}
		return xmlrpc.NilValue{}, nil
	})

	mux.Handle("init", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 1 {
			return nil, fmt.Errorf("init: url required")
		}
		url, _ := xmlrpc.AsString(params[0])
		ifID := ""
		if len(params) > 1 {
			ifID, _ = xmlrpc.AsString(params[1])
		}
		return xmlrpc.StringValue(rpc.Init(url, ifID)), nil
	})

	mux.Handle("getMetadata", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 2 {
			return nil, fmt.Errorf("getMetadata: object_id and data_id required")
		}
		objectID, _ := xmlrpc.AsString(params[0])
		dataID, _ := xmlrpc.AsString(params[1])
		v, err := rpc.GetMetadata(objectID, dataID)
		if err != nil {
			return nil, faultFromErr(err)
		}
		return xmlrpc.FromAny(v), nil
	})

	mux.Handle("setMetadata", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 3 {
			return nil, fmt.Errorf("setMetadata: address, data_id, value required")
		}
		addr, _ := xmlrpc.AsString(params[0])
		dataID, _ := xmlrpc.AsString(params[1])
		return xmlrpc.BoolValue(rpc.SetMetadata(addr, dataID, xmlrpc.ToAny(params[2]))), nil
	})

	mux.Handle("addLink", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 4 {
			return nil, fmt.Errorf("addLink: 4 params required")
		}
		s1, _ := xmlrpc.AsString(params[0])
		s2, _ := xmlrpc.AsString(params[1])
		s3, _ := xmlrpc.AsString(params[2])
		s4, _ := xmlrpc.AsString(params[3])
		return xmlrpc.BoolValue(rpc.AddLink(s1, s2, s3, s4)), nil
	})

	mux.Handle("removeLink", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 2 {
			return nil, fmt.Errorf("removeLink: 2 params required")
		}
		s1, _ := xmlrpc.AsString(params[0])
		s2, _ := xmlrpc.AsString(params[1])
		return xmlrpc.BoolValue(rpc.RemoveLink(s1, s2)), nil
	})

	mux.Handle("getLinkPeers", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		addr := ""
		if len(params) > 0 {
			addr, _ = xmlrpc.AsString(params[0])
		}
		return xmlrpc.FromAny(any(rpc.GetLinkPeers(addr))), nil
	})

	mux.Handle("getLinks", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		addr := ""
		flags := 0
		if len(params) > 0 {
			addr, _ = xmlrpc.AsString(params[0])
		}
		if len(params) > 1 {
			flags, _ = xmlrpc.AsInt(params[1])
		}
		return xmlrpc.FromAny(any(rpc.GetLinks(addr, flags))), nil
	})

	mux.Handle("getInstallMode", func(_ context.Context, _ []xmlrpc.Value) (xmlrpc.Value, error) {
		return xmlrpc.IntValue(int32(rpc.GetInstallMode())), nil
	})

	mux.Handle("setInstallMode", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		on := true
		duration := 60
		mode := 1
		address := ""
		if len(params) > 0 {
			on, _ = xmlrpc.AsBool(params[0])
		}
		if len(params) > 1 {
			duration, _ = xmlrpc.AsInt(params[1])
		}
		if len(params) > 2 {
			mode, _ = xmlrpc.AsInt(params[2])
		}
		if len(params) > 3 {
			address, _ = xmlrpc.AsString(params[3])
		}
		return xmlrpc.BoolValue(rpc.SetInstallMode(on, duration, mode, address)), nil
	})

	mux.Handle("reportValueUsage", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 3 {
			return nil, fmt.Errorf("reportValueUsage: 3 params required")
		}
		ch, _ := xmlrpc.AsString(params[0])
		valueID, _ := xmlrpc.AsString(params[1])
		ref, _ := xmlrpc.AsInt(params[2])
		return xmlrpc.BoolValue(rpc.ReportValueUsage(ch, valueID, ref)), nil
	})

	mux.Handle("installFirmware", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		address := ""
		if len(params) > 0 {
			address, _ = xmlrpc.AsString(params[0])
		}
		return xmlrpc.BoolValue(rpc.InstallFirmware(address)), nil
	})

	mux.Handle("updateFirmware", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		address := ""
		if len(params) > 0 {
			address, _ = xmlrpc.AsString(params[0])
		}
		return xmlrpc.BoolValue(rpc.UpdateFirmware(address)), nil
	})

	mux.Handle("clientServerInitialized", func(_ context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		ifID := ""
		if len(params) > 0 {
			ifID, _ = xmlrpc.AsString(params[0])
		}
		return xmlrpc.BoolValue(rpc.ClientServerInitialized(ifID)), nil
	})

	mux.Handle("deleteDevice", func(ctx context.Context, params []xmlrpc.Value) (xmlrpc.Value, error) {
		if len(params) < 1 {
			return nil, fmt.Errorf("deleteDevice: address required")
		}
		address, _ := xmlrpc.AsString(params[0])
		flags := 0
		if len(params) >= 2 {
			flags, _ = xmlrpc.AsInt(params[1])
		}
		// DeleteDevice is idempotent and never returns an error to the caller.
		// The flags parameter is accepted for wire compatibility; see RPCFunctions.DeleteDevice.
		rpc.DeleteDevice(ctx, address, flags)
		return xmlrpc.IntValue(0), nil
	})
}

// faultFromErr translates an internal error into an XML-RPC fault.
func faultFromErr(err error) error {
	if err == nil {
		return nil
	}
	var fault *xmlrpc.Fault
	if errors.As(err, &fault) {
		return fault
	}
	return &xmlrpc.Fault{Code: -1, Message: err.Error()}
}
