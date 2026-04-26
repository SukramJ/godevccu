// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package rega implements the simplified ReGa script engine: instead of
// running a full interpreter it pattern-matches the scripts that
// aiohomematic/gohomematic ships and returns the JSON payload that the client
// expects.
package rega

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/SukramJ/godevccu/internal/state"
)

// RPC is the subset of the simulator's XML-RPC surface that the engine
// needs. The production type is *ccu.RPCFunctions; we keep the surface
// small for testability.
type RPC interface {
	GetValue(address, valueKey string) (any, error)
}

// Result is the outcome of an [Engine.Execute] call.
type Result struct {
	Output  string
	Success bool
	Error   string
}

// Engine pattern-matches incoming scripts against a list of handlers.
type Engine struct {
	state *state.Manager
	rpc   RPC

	patterns []patternHandler
}

type patternHandler struct {
	re *regexp.Regexp
	fn func(script string) string
}

// New constructs an engine bound to the given state and RPC.
func New(stateMgr *state.Manager, rpc RPC) *Engine {
	e := &Engine{state: stateMgr, rpc: rpc}
	e.patterns = []patternHandler{
		{regexp.MustCompile(`(?is)system\.Exec.*cat.*/VERSION`), e.handleBackendInfo},
		{regexp.MustCompile(`(?is)grep.*VERSION.*grep.*PRODUCT`), e.handleBackendInfo},
		{regexp.MustCompile(`(?i)name:\s*get_serial\.fn`), e.handleGetSerial},
		{regexp.MustCompile(`(?i)system\.GetVar\s*\(\s*["']?SERIALNO["']?\s*\)`), e.handleGetSerial},
		{regexp.MustCompile(`(?i)name:\s*fetch_all_device_data\.fn`), e.handleFetchDeviceData},
		{regexp.MustCompile(`(?is)foreach\s*\(\s*\w+\s*,\s*dom\.GetObject\s*\(\s*ID_DATAPOINTS`), e.handleFetchDeviceData},
		{regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*ID_PROGRAMS\s*\)`), e.handleGetPrograms},
		{regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*ID_SYSTEM_VARIABLES\s*\)`), e.handleGetSysvars},
		{regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*ID_SERVICES\s*\)`), e.handleGetServiceMessages},
		{regexp.MustCompile(`(?i)INBOX`), e.handleGetInbox},
		{regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*(\d+)\s*\)\.Active\s*\(\s*(true|false)\s*\)`), e.handleSetProgramState},
		{regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*"([^"]+)"\s*\)\.State\s*\(\s*"?([^")]*)"?\s*\)`), e.handleSetSysvar},
		{regexp.MustCompile(`(?i)CreateBackup`), e.handleBackupStart},
		{regexp.MustCompile(`(?i)backup\.pid|backup_status|BACKUP_STATUS`), e.handleBackupStatus},
		{regexp.MustCompile(`(?i)checkFirmwareUpdate|CHECK_FIRMWARE_UPDATE`), e.handleUpdateInfo},
		{regexp.MustCompile(`(?i)nohup.*checkFirmwareUpdate.*-a|TRIGGER_UPDATE`), e.handleTriggerUpdate},
		{regexp.MustCompile(`(?i)ID_ROOMS`), e.handleGetRooms},
		{regexp.MustCompile(`(?i)ID_FUNCTIONS`), e.handleGetFunctions},
		{regexp.MustCompile(`(?i)^Write\s*\(\s*"([^"]*)"\s*\)\s*;?\s*$`), e.handleWrite},
	}
	return e
}

// Execute returns the result of running script.
func (e *Engine) Execute(script string) Result {
	for _, p := range e.patterns {
		if !p.re.MatchString(script) {
			continue
		}
		out := p.fn(script)
		return Result{Output: out, Success: true}
	}
	return Result{Output: "", Success: true}
}

// ─────────────────────────────────────────────────────────────────
// Pattern handlers
// ─────────────────────────────────────────────────────────────────

func (e *Engine) handleBackendInfo(_ string) string {
	info := e.state.BackendInfo()
	return mustJSON(map[string]any{
		"version":     info.Version,
		"product":     info.Product,
		"hostname":    info.Hostname,
		"is_ha_addon": info.IsHaAddon,
	})
}

func (e *Engine) handleGetSerial(_ string) string {
	return mustJSON(e.state.Serial())
}

var (
	reInterfaceAssign = regexp.MustCompile(`interface\s*=\s*"([^"]+)"`)
	reParamHeader     = regexp.MustCompile(`param:\s*"([^"]+)"`)
)

func (e *Engine) handleFetchDeviceData(script string) string {
	iface := ""
	if m := reInterfaceAssign.FindStringSubmatch(script); m != nil {
		iface = m[1]
	} else if m := reParamHeader.FindStringSubmatch(script); m != nil {
		iface = m[1]
	}
	values := e.state.AllDeviceValues(iface)
	out := make([]map[string]any, 0, len(values))
	for key, val := range values {
		parts := strings.Split(key, ":")
		if len(parts) < 2 {
			continue
		}
		address := strings.Join(parts[:len(parts)-1], ":")
		param := parts[len(parts)-1]
		out = append(out, map[string]any{
			"address": address,
			"param":   param,
			"value":   val,
		})
	}
	return mustJSON(out)
}

func (e *Engine) handleGetPrograms(_ string) string {
	progs := e.state.Programs()
	out := make([]map[string]any, 0, len(progs))
	for _, p := range progs {
		out = append(out, map[string]any{
			"id":              p.ID,
			"name":            url.QueryEscape(p.Name),
			"description":     url.QueryEscape(p.Description),
			"isActive":        p.Active,
			"isInternal":      false,
			"lastExecuteTime": p.LastExecuteTime,
		})
	}
	return mustJSON(out)
}

func (e *Engine) handleGetSysvars(_ string) string {
	svs := e.state.SystemVariables()
	out := make([]map[string]any, 0, len(svs))
	for _, sv := range svs {
		out = append(out, map[string]any{
			"id":          sv.ID,
			"name":        url.QueryEscape(sv.Name),
			"description": url.QueryEscape(sv.Description),
			"unit":        sv.Unit,
			"type":        sv.VarType,
			"value":       sv.Value,
			"valueList":   sv.ValueList,
			"minValue":    sv.MinValue,
			"maxValue":    sv.MaxValue,
			"timestamp":   sv.Timestamp,
			"isInternal":  false,
		})
	}
	return mustJSON(out)
}

func (e *Engine) handleGetServiceMessages(_ string) string {
	msgs := e.state.ServiceMessages()
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]any{
			"id":         m.ID,
			"name":       m.Name,
			"timestamp":  m.Timestamp,
			"type":       m.MsgType,
			"address":    m.Address,
			"deviceName": m.DeviceName,
		})
	}
	return mustJSON(out)
}

func (e *Engine) handleGetInbox(_ string) string {
	devs := e.state.InboxDevices()
	out := make([]map[string]any, 0, len(devs))
	for _, d := range devs {
		out = append(out, map[string]any{
			"deviceId":   d.DeviceID,
			"address":    d.Address,
			"name":       d.Name,
			"deviceType": d.DeviceType,
			"interface":  d.Interface,
		})
	}
	return mustJSON(out)
}

var reProgramActive = regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*(\d+)\s*\)\.Active\s*\(\s*(true|false)\s*\)`)

func (e *Engine) handleSetProgramState(script string) string {
	m := reProgramActive.FindStringSubmatch(script)
	if m == nil {
		return ""
	}
	id, _ := strconv.Atoi(m[1])
	active := strings.EqualFold(m[2], "true")
	e.state.SetProgramActive(id, active)
	return ""
}

var reSysVarSet = regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*"([^"]+)"\s*\)\.State\s*\(\s*"?([^")]*)"?\s*\)`)

func (e *Engine) handleSetSysvar(script string) string {
	m := reSysVarSet.FindStringSubmatch(script)
	if m == nil {
		return ""
	}
	name := m[1]
	raw := m[2]
	var value any
	switch {
	case strings.EqualFold(raw, "true"):
		value = true
	case strings.EqualFold(raw, "false"):
		value = false
	case strings.Contains(raw, "."):
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			value = f
		} else {
			value = raw
		}
	default:
		if i, err := strconv.Atoi(raw); err == nil {
			value = i
		} else {
			value = raw
		}
	}
	e.state.SetSystemVariable(name, value)
	return ""
}

func (e *Engine) handleBackupStart(_ string) string {
	pid := e.state.StartBackup()
	return mustJSON(map[string]any{
		"success": true,
		"status":  "started",
		"pid":     pid,
	})
}

func (e *Engine) handleBackupStatus(_ string) string {
	st := e.state.BackupStatus()
	return mustJSON(map[string]any{
		"status":   st.Status,
		"pid":      st.PID,
		"filename": st.Filename,
		"filepath": st.Filepath,
		"size":     st.Size,
	})
}

func (e *Engine) handleUpdateInfo(_ string) string {
	info := e.state.UpdateInfo()
	return mustJSON(map[string]any{
		"currentFirmware":      info.CurrentFirmware,
		"availableFirmware":    info.AvailableFirmware,
		"updateAvailable":      info.UpdateAvailable,
		"checkScriptAvailable": true,
	})
}

func (e *Engine) handleTriggerUpdate(_ string) string {
	e.state.TriggerUpdate()
	return mustJSON(map[string]any{"success": true})
}

func (e *Engine) handleGetRooms(_ string) string {
	rooms := e.state.Rooms()
	out := make([]map[string]any, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, map[string]any{
			"id":          r.ID,
			"name":        url.QueryEscape(r.Name),
			"description": url.QueryEscape(r.Description),
			"channelIds":  r.ChannelIDs,
		})
	}
	return mustJSON(out)
}

func (e *Engine) handleGetFunctions(_ string) string {
	funcs := e.state.Functions()
	out := make([]map[string]any, 0, len(funcs))
	for _, f := range funcs {
		out = append(out, map[string]any{
			"id":          f.ID,
			"name":        url.QueryEscape(f.Name),
			"description": url.QueryEscape(f.Description),
			"channelIds":  f.ChannelIDs,
		})
	}
	return mustJSON(out)
}

var reSimpleWrite = regexp.MustCompile(`(?i)Write\s*\(\s*"([^"]*)"\s*\)`)

func (e *Engine) handleWrite(script string) string {
	if m := reSimpleWrite.FindStringSubmatch(script); m != nil {
		return m[1]
	}
	return ""
}

// mustJSON encodes v; on failure returns "null". Any error here would
// indicate a bug: every callsite hands a serialisable value.
func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(raw)
}
