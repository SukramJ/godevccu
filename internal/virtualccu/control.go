// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package virtualccu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// The control port.
//
// The scenario API over HTTP, for a test that drives the simulator from
// another process or language. The shape is hm-simulator's control port
// (https://github.com/hobbyquaker/hm-simulator, MIT, Sebastian Raff):
// POST /scenario/<call> with a JSON array of the arguments (GET for a
// call without arguments), answered with {"result": …}; a failed call is
// 400 with {"faultCode", "faultString"}, an unknown call 404.
// GET /ports answers the bound ports.
//
// The handler has no authentication: serve it on loopback only.

// controlRequestLimit bounds a control request body.
const controlRequestLimit = 1 << 20

// controlFaultRule is the JSON form of a [ccu.FaultRule].
type controlFaultRule struct {
	Method          string `json:"method"`
	Times           int    `json:"times"`
	FaultCode       *int   `json:"faultCode"`
	FaultString     string `json:"faultString"`
	DelayMs         int    `json:"delayMs"`
	Hang            bool   `json:"hang"`
	CloseConnection bool   `json:"closeConnection"`
}

func (c controlFaultRule) rule() ccu.FaultRule {
	rule := ccu.FaultRule{
		Method:          c.Method,
		Times:           c.Times,
		Delay:           time.Duration(c.DelayMs) * time.Millisecond,
		Hang:            c.Hang,
		CloseConnection: c.CloseConnection,
	}
	if c.FaultCode != nil {
		rule.Fault = &xmlrpc.Fault{Code: *c.FaultCode, Message: c.FaultString}
	}
	return rule
}

// controlCall runs one scenario call on decoded arguments.
type controlCall func(v *VirtualCCU, args []json.RawMessage) (any, error)

// controlCalls is the scenario API by call name.
var controlCalls = map[string]controlCall{
	"injectFault": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var iface string
		var rule controlFaultRule
		if err := decodeArgs(args, &iface, &rule); err != nil {
			return nil, err
		}
		return nil, v.InjectFault(iface, rule.rule())
	},
	"clearFaults": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		iface := "*"
		if err := decodeArgs(args, &iface); err != nil {
			return nil, err
		}
		return nil, v.ClearFaults(iface)
	},
	"dropConnection": ifaceCall((*VirtualCCU).DropConnection),
	"stopInterface":  ifaceCall((*VirtualCCU).StopInterface),
	"startInterface": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var iface string
		forget := true
		if err := decodeArgs(args, &iface, &forget); err != nil {
			return nil, err
		}
		return nil, v.StartInterface(iface, forget)
	},
	"restartInterface": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var iface string
		var downMs int
		forget := true
		if err := decodeArgs(args, &iface, &downMs, &forget); err != nil {
			return nil, err
		}
		return nil, v.RestartInterface(iface, time.Duration(downMs)*time.Millisecond, forget)
	},
	"setDeviceUnreachable": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var address string
		unreach := true
		if err := decodeArgs(args, &address, &unreach); err != nil {
			return nil, err
		}
		return nil, v.SetDeviceUnreachable(address, unreach)
	},
	"simulateDeviceEvent": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var address, key string
		var value any
		if err := decodeArgs(args, &address, &key, &value); err != nil {
			return nil, err
		}
		rpc, err := v.rpcForAddress(address)
		if err != nil {
			return nil, err
		}
		return nil, rpc.SimulateDeviceEvent(address, key, value)
	},
	"fireEvent": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var address, key string
		var value any
		if err := decodeArgs(args, &address, &key, &value); err != nil {
			return nil, err
		}
		rpc, err := v.rpcForAddress(address)
		if err != nil {
			return nil, err
		}
		rpc.FireEvent(rpc.InterfaceID(), address, key, value)
		return nil, nil
	},
	"writeLog": func(v *VirtualCCU, _ []json.RawMessage) (any, error) {
		return nonNil(v.WriteLog()), nil
	},
	"callbackLog": func(v *VirtualCCU, _ []json.RawMessage) (any, error) {
		return nonNil(v.CallbackLog()), nil
	},
	"clearLogs": func(v *VirtualCCU, _ []json.RawMessage) (any, error) {
		v.ClearLogs()
		return nil, nil
	},
	"configPending": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var iface string
		if err := decodeArgs(args, &iface); err != nil {
			return nil, err
		}
		return v.ConfigPending(iface)
	},
	"poisonedChannels": func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var iface string
		if err := decodeArgs(args, &iface); err != nil {
			return nil, err
		}
		return v.PoisonedChannels(iface)
	},
	"ports": func(v *VirtualCCU, _ []json.RawMessage) (any, error) {
		return v.Ports(), nil
	},
}

// ifaceCall adapts a call that takes only an interface name.
func ifaceCall(fn func(*VirtualCCU, string) error) controlCall {
	return func(v *VirtualCCU, args []json.RawMessage) (any, error) {
		var iface string
		if err := decodeArgs(args, &iface); err != nil {
			return nil, err
		}
		return nil, fn(v, iface)
	}
}

// decodeArgs decodes the positional arguments into targets; missing
// trailing arguments keep the targets' preset values.
func decodeArgs(args []json.RawMessage, targets ...any) error {
	if len(args) > len(targets) {
		return fmt.Errorf("expected at most %d arguments, got %d", len(targets), len(args))
	}
	for i, raw := range args {
		if err := json.Unmarshal(raw, targets[i]); err != nil {
			return fmt.Errorf("argument %d: %w", i+1, err)
		}
	}
	return nil
}

// nonNil keeps an empty log a JSON array rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ControlHandler serves the scenario API over HTTP; see control.go.
func (v *VirtualCCU) ControlHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ports" && r.Method == http.MethodGet {
			writeControlJSON(w, http.StatusOK, v.Ports())
			return
		}
		name, ok := strings.CutPrefix(r.URL.Path, "/scenario/")
		call, known := controlCalls[name]
		if !ok || !known {
			writeControlJSON(w, http.StatusNotFound, map[string]any{"faultCode": -1, "faultString": "unknown call: " + r.URL.Path})
			return
		}
		var args []json.RawMessage
		switch r.Method {
		case http.MethodGet:
		case http.MethodPost:
			body, err := io.ReadAll(io.LimitReader(r.Body, controlRequestLimit))
			if err != nil {
				writeControlFault(w, err)
				return
			}
			if len(strings.TrimSpace(string(body))) > 0 {
				if err := json.Unmarshal(body, &args); err != nil {
					writeControlFault(w, fmt.Errorf("arguments must be a JSON array: %w", err))
					return
				}
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		result, err := call(v, args)
		if err != nil {
			writeControlFault(w, err)
			return
		}
		writeControlJSON(w, http.StatusOK, map[string]any{"result": result})
	})
}

func writeControlFault(w http.ResponseWriter, err error) {
	code := -1
	if fault, ok := errors.AsType[*xmlrpc.Fault](err); ok {
		code = fault.Code
	}
	writeControlJSON(w, http.StatusBadRequest, map[string]any{"faultCode": code, "faultString": err.Error()})
}

func writeControlJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
