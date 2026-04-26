// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package jsonrpc

import (
	"context"
	"strconv"
	"strings"

	"github.com/SukramJ/godevccu/internal/ccu"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/rega"
	"github.com/SukramJ/godevccu/internal/session"
	"github.com/SukramJ/godevccu/internal/state"
)

// HandlerFunc is the signature of a JSON-RPC method handler.
type HandlerFunc func(ctx context.Context, params map[string]any) (any, error)

// Handlers groups every RPC method implemented by godevccu. The struct
// is the Go counterpart to pydevccu/json_rpc/handlers.JsonRpcHandlers.
type Handlers struct {
	State   *state.Manager
	Session *session.Manager
	RPC     *ccu.RPCFunctions
	ReGa    *rega.Engine

	// XMLRPCPort is the port exposed via Interface.listInterfaces.
	XMLRPCPort int
}

// NewHandlers builds a Handlers instance.
func NewHandlers(stateMgr *state.Manager, sess *session.Manager, rpc *ccu.RPCFunctions, regaEng *rega.Engine, xmlRPCPort int) *Handlers {
	return &Handlers{
		State:      stateMgr,
		Session:    sess,
		RPC:        rpc,
		ReGa:       regaEng,
		XMLRPCPort: xmlRPCPort,
	}
}

// Methods returns the method-name → handler map.
func (h *Handlers) Methods() map[string]HandlerFunc {
	return map[string]HandlerFunc{
		// Session
		"Session.login":  h.sessionLogin,
		"Session.logout": h.sessionLogout,
		"Session.renew":  h.sessionRenew,
		// CCU
		"CCU.getAuthEnabled":          h.getAuthEnabled,
		"CCU.getHttpsRedirectEnabled": h.getHTTPSRedirectEnabled,
		"system.listMethods":          h.listMethods,
		// Interface
		"Interface.listInterfaces":         h.listInterfaces,
		"Interface.listDevices":            h.listDevices,
		"Interface.getDeviceDescription":   h.getDeviceDescription,
		"Interface.getParamset":            h.getParamset,
		"Interface.getParamsetDescription": h.getParamsetDescription,
		"Interface.getValue":               h.getValue,
		"Interface.setValue":               h.setValue,
		"Interface.putParamset":            h.putParamset,
		"Interface.isPresent":              h.isPresent,
		"Interface.getInstallMode":         h.getInstallMode,
		"Interface.setInstallMode":         h.setInstallMode,
		"Interface.setInstallModeHMIP":     h.setInstallMode,
		"Interface.getMasterValue":         h.getMasterValue,
		"Interface.ping":                   h.ping,
		"Interface.init":                   h.interfaceInit,
		// Device / Channel
		"Device.listAllDetail":  h.deviceListAllDetail,
		"Device.get":            h.deviceGet,
		"Device.setName":        h.setName,
		"Channel.setName":       h.setName,
		"Channel.hasProgramIds": h.channelHasProgramIDs,
		// Programs
		"Program.getAll":    h.programGetAll,
		"Program.execute":   h.programExecute,
		"Program.setActive": h.programSetActive,
		// SysVar
		"SysVar.getAll":             h.sysvarGetAll,
		"SysVar.getValueByName":     h.sysvarGetValueByName,
		"SysVar.setBool":            h.sysvarSet,
		"SysVar.setFloat":           h.sysvarSet,
		"SysVar.setString":          h.sysvarSet,
		"SysVar.deleteSysVarByName": h.sysvarDelete,
		// Rooms / Functions
		"Room.getAll":       h.roomGetAll,
		"Room.listAll":      h.roomGetAll,
		"Subsection.getAll": h.subsectionGetAll,
		// ReGa
		"ReGa.runScript": h.regaRunScript,
	}
}

// PublicMethods is the set of methods that bypass authentication.
var PublicMethods = map[string]struct{}{
	"Session.login":               {},
	"CCU.getAuthEnabled":          {},
	"CCU.getHttpsRedirectEnabled": {},
	"system.listMethods":          {},
}

// ─────────────────────────────────────────────────────────────────
// Session
// ─────────────────────────────────────────────────────────────────

func (h *Handlers) sessionLogin(_ context.Context, params map[string]any) (any, error) {
	username := stringParam(params, "username")
	password := stringParam(params, "password")
	id := h.Session.Login(username, password)
	return id, nil
}

func (h *Handlers) sessionLogout(_ context.Context, params map[string]any) (any, error) {
	id := stringParam(params, "_session_id_")
	h.Session.Logout(id)
	return true, nil
}

func (h *Handlers) sessionRenew(_ context.Context, params map[string]any) (any, error) {
	id := stringParam(params, "_session_id_")
	if h.Session.Renew(id) == "" {
		return nil, ErrSession("Session expired or invalid")
	}
	return true, nil
}

// ─────────────────────────────────────────────────────────────────
// CCU
// ─────────────────────────────────────────────────────────────────

func (h *Handlers) getAuthEnabled(_ context.Context, _ map[string]any) (any, error) {
	return h.Session.AuthEnabled(), nil
}

func (h *Handlers) getHTTPSRedirectEnabled(_ context.Context, _ map[string]any) (any, error) {
	return false, nil
}

func (h *Handlers) listMethods(_ context.Context, _ map[string]any) (any, error) {
	methods := h.Methods()
	out := make([]map[string]any, 0, len(methods))
	for name := range methods {
		out = append(out, map[string]any{"name": name})
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────
// Interface
// ─────────────────────────────────────────────────────────────────

func (h *Handlers) listInterfaces(_ context.Context, _ map[string]any) (any, error) {
	return []map[string]any{
		{
			"name":      "HmIP-RF",
			"port":      h.XMLRPCPort,
			"info":      "HomeMatic IP RF Interface",
			"type":      "HmIP-RF",
			"available": true,
		},
		{
			"name":      "BidCos-RF",
			"port":      h.XMLRPCPort,
			"info":      "HomeMatic RF Interface",
			"type":      "BidCos-RF",
			"available": true,
		},
	}, nil
}

func (h *Handlers) listDevices(_ context.Context, _ map[string]any) (any, error) {
	if h.RPC == nil {
		return []any{}, nil
	}
	return h.RPC.ListDevices(), nil
}

func (h *Handlers) getDeviceDescription(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	if address == "" {
		return nil, ErrParams("Missing address parameter")
	}
	if h.RPC == nil {
		return nil, ErrObject("Device", address)
	}
	d, err := h.RPC.GetDeviceDescription(address)
	if err != nil {
		return nil, ErrObject("Device", address)
	}
	return d, nil
}

func (h *Handlers) getParamset(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	key := paramsetKeyParam(params)
	if address == "" {
		return nil, ErrParams("Missing address parameter")
	}
	if h.RPC == nil {
		return map[string]any{}, nil
	}
	d, err := h.RPC.GetParamset(address, key)
	if err != nil {
		return map[string]any{}, nil
	}
	return d, nil
}

func (h *Handlers) getParamsetDescription(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	key := paramsetKeyParam(params)
	if address == "" {
		return nil, ErrParams("Missing address parameter")
	}
	if h.RPC == nil {
		return map[string]any{}, nil
	}
	d, err := h.RPC.GetParamsetDescription(address, key)
	if err != nil {
		return map[string]any{}, nil
	}
	return d, nil
}

func (h *Handlers) getValue(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	valueKey := valueKeyParam(params)
	if address == "" || valueKey == "" {
		return nil, ErrParams("Missing address or valueKey parameter")
	}
	if h.RPC == nil {
		return nil, nil
	}
	v, err := h.RPC.GetValue(address, valueKey)
	if err != nil {
		return nil, nil
	}
	return v, nil
}

func (h *Handlers) setValue(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	valueKey := valueKeyParam(params)
	if address == "" || valueKey == "" {
		return nil, ErrParams("Missing address or valueKey parameter")
	}
	value := params["value"]
	if h.RPC == nil {
		return false, nil
	}
	if err := h.RPC.SetValue(address, valueKey, value, false); err != nil {
		return false, nil
	}
	return true, nil
}

func (h *Handlers) putParamset(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	key := paramsetKeyParam(params)
	if address == "" {
		return nil, ErrParams("Missing address parameter")
	}
	var paramset map[string]any
	if v, ok := params["set"]; ok {
		paramset, _ = v.(map[string]any)
	}
	if paramset == nil {
		if v, ok := params["paramset"]; ok {
			paramset, _ = v.(map[string]any)
		}
	}
	if h.RPC == nil {
		return false, nil
	}
	if err := h.RPC.PutParamset(address, key, paramset, false); err != nil {
		return false, nil
	}
	return true, nil
}

func (h *Handlers) isPresent(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	if h.RPC == nil {
		return false, nil
	}
	if _, err := h.RPC.GetDeviceDescription(address); err != nil {
		return false, nil
	}
	return true, nil
}

func (h *Handlers) getInstallMode(_ context.Context, _ map[string]any) (any, error) { return 0, nil }
func (h *Handlers) setInstallMode(_ context.Context, _ map[string]any) (any, error) { return true, nil }

func (h *Handlers) getMasterValue(_ context.Context, _ map[string]any) (any, error) { return "", nil }

func (h *Handlers) ping(_ context.Context, _ map[string]any) (any, error) { return true, nil }

func (h *Handlers) interfaceInit(_ context.Context, params map[string]any) (any, error) {
	url := stringParam(params, "url")
	ifID := stringParam(params, "interfaceId")
	if ifID == "" {
		ifID = stringParam(params, "interface_id")
	}
	if h.RPC == nil {
		return "", nil
	}
	return h.RPC.Init(url, ifID), nil
}

// ─────────────────────────────────────────────────────────────────
// Device / Channel
// ─────────────────────────────────────────────────────────────────

func (h *Handlers) deviceListAllDetail(_ context.Context, _ map[string]any) (any, error) {
	if h.RPC == nil {
		return []any{}, nil
	}
	all := h.RPC.ListDevices()
	parents := make(map[string]map[string]any)
	channelsByParent := make(map[string][]map[string]any)
	for _, d := range all {
		address, _ := d["ADDRESS"].(string)
		if strings.Contains(address, ":") {
			parentAddr := address[:strings.IndexByte(address, ':')]
			channelsByParent[parentAddr] = append(channelsByParent[parentAddr], map[string]any{
				"id":        address,
				"address":   address,
				"type":      d["TYPE"],
				"name":      h.deviceName(address, d),
				"interface": stringOrDefault(d["INTERFACE"], "HmIP-RF"),
			})
		} else {
			parents[address] = d
		}
	}
	out := make([]map[string]any, 0, len(parents))
	for address, d := range parents {
		out = append(out, map[string]any{
			"id":        address,
			"address":   address,
			"type":      d["TYPE"],
			"name":      h.deviceName(address, d),
			"interface": stringOrDefault(d["INTERFACE"], "HmIP-RF"),
			"channels":  channelsByParent[address],
		})
	}
	return out, nil
}

func (h *Handlers) deviceGet(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	if address == "" {
		address = stringParam(params, "id")
	}
	if address == "" {
		return nil, ErrParams("Missing address parameter")
	}
	if h.RPC == nil {
		return nil, ErrObject("Device", address)
	}
	d, err := h.RPC.GetDeviceDescription(address)
	if err != nil {
		return nil, ErrObject("Device", address)
	}
	return map[string]any{
		"id":      address,
		"address": address,
		"type":    d["TYPE"],
		"name":    h.deviceName(address, d),
	}, nil
}

func (h *Handlers) setName(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	if address == "" {
		address = stringParam(params, "id")
	}
	if address == "" {
		return nil, ErrParams("Missing address parameter")
	}
	name := stringParam(params, "name")
	h.State.SetDeviceName(address, name)
	return true, nil
}

func (h *Handlers) channelHasProgramIDs(_ context.Context, _ map[string]any) (any, error) {
	return []any{}, nil
}

// ─────────────────────────────────────────────────────────────────
// Programs
// ─────────────────────────────────────────────────────────────────

func (h *Handlers) programGetAll(_ context.Context, _ map[string]any) (any, error) {
	progs := h.State.Programs()
	out := make([]map[string]any, 0, len(progs))
	for _, p := range progs {
		out = append(out, map[string]any{
			"id":              strconv.Itoa(p.ID),
			"name":            p.Name,
			"description":     p.Description,
			"isActive":        p.Active,
			"isInternal":      false,
			"lastExecuteTime": p.LastExecuteTime,
		})
	}
	return out, nil
}

func (h *Handlers) programExecute(_ context.Context, params map[string]any) (any, error) {
	id, err := intParam(params, "id", "programId")
	if err != nil {
		return nil, ErrParams(err.Error())
	}
	return map[string]any{"success": h.State.ExecuteProgram(id)}, nil
}

func (h *Handlers) programSetActive(_ context.Context, params map[string]any) (any, error) {
	id, err := intParam(params, "id", "programId")
	if err != nil {
		return nil, ErrParams(err.Error())
	}
	active := boolParam(params, "active", true)
	if v, ok := params["isActive"]; ok {
		if b, ok := v.(bool); ok {
			active = b
		}
	}
	return map[string]any{"success": h.State.SetProgramActive(id, active)}, nil
}

// ─────────────────────────────────────────────────────────────────
// SysVar
// ─────────────────────────────────────────────────────────────────

func (h *Handlers) sysvarGetAll(_ context.Context, _ map[string]any) (any, error) {
	svs := h.State.SystemVariables()
	out := make([]map[string]any, 0, len(svs))
	for _, sv := range svs {
		out = append(out, map[string]any{
			"id":          strconv.Itoa(sv.ID),
			"name":        sv.Name,
			"description": sv.Description,
			"type":        sv.VarType,
			"value":       sv.Value,
			"unit":        sv.Unit,
			"valueList":   sv.ValueList,
			"minValue":    sv.MinValue,
			"maxValue":    sv.MaxValue,
			"timestamp":   sv.Timestamp,
			"isInternal":  false,
		})
	}
	return out, nil
}

func (h *Handlers) sysvarGetValueByName(_ context.Context, params map[string]any) (any, error) {
	name := stringParam(params, "name")
	if name == "" {
		return nil, ErrParams("Missing name parameter")
	}
	sv, ok := h.State.SystemVariable(name)
	if !ok {
		return nil, ErrObject("SystemVariable", name)
	}
	return sv.Value, nil
}

func (h *Handlers) sysvarSet(_ context.Context, params map[string]any) (any, error) {
	name := stringParam(params, "name")
	if name == "" {
		return nil, ErrParams("Missing name parameter")
	}
	value := params["value"]
	return map[string]any{"success": h.State.SetSystemVariable(name, value)}, nil
}

func (h *Handlers) sysvarDelete(_ context.Context, params map[string]any) (any, error) {
	name := stringParam(params, "name")
	if name == "" {
		return nil, ErrParams("Missing name parameter")
	}
	return map[string]any{"success": h.State.DeleteSystemVariable(name)}, nil
}

// ─────────────────────────────────────────────────────────────────
// Rooms / Subsections
// ─────────────────────────────────────────────────────────────────

func (h *Handlers) roomGetAll(_ context.Context, _ map[string]any) (any, error) {
	rooms := h.State.Rooms()
	out := make([]map[string]any, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, map[string]any{
			"id":          strconv.Itoa(r.ID),
			"name":        r.Name,
			"description": r.Description,
			"channelIds":  r.ChannelIDs,
		})
	}
	return out, nil
}

func (h *Handlers) subsectionGetAll(_ context.Context, _ map[string]any) (any, error) {
	funcs := h.State.Functions()
	out := make([]map[string]any, 0, len(funcs))
	for _, f := range funcs {
		out = append(out, map[string]any{
			"id":          strconv.Itoa(f.ID),
			"name":        f.Name,
			"description": f.Description,
			"channelIds":  f.ChannelIDs,
		})
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────
// ReGa
// ─────────────────────────────────────────────────────────────────

func (h *Handlers) regaRunScript(_ context.Context, params map[string]any) (any, error) {
	script := stringParam(params, "script")
	if script == "" {
		return nil, ErrParams("Missing script parameter")
	}
	if h.ReGa == nil {
		return "", nil
	}
	return h.ReGa.Execute(script).Output, nil
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	v, ok := params[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func paramsetKeyParam(params map[string]any) string {
	if v := stringParam(params, "paramsetKey"); v != "" {
		return v
	}
	if v := stringParam(params, "paramset_key"); v != "" {
		return v
	}
	return hmconst.ParamsetAttrValues
}

func valueKeyParam(params map[string]any) string {
	if v := stringParam(params, "valueKey"); v != "" {
		return v
	}
	return stringParam(params, "value_key")
}

func boolParam(params map[string]any, key string, def bool) bool {
	if v, ok := params[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

func intParam(params map[string]any, keys ...string) (int, error) {
	for _, k := range keys {
		if v, ok := params[k]; ok {
			switch x := v.(type) {
			case float64:
				return int(x), nil
			case int:
				return x, nil
			case int64:
				return int(x), nil
			case string:
				if i, err := strconv.Atoi(x); err == nil {
					return i, nil
				}
			}
		}
	}
	return 0, &Error{Code: ErrInvalidParams, Message: "Missing or invalid id parameter"}
}

func stringOrDefault(v any, def string) string {
	if v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return def
		}
		return s
	}
	return def
}

func (h *Handlers) deviceName(address string, d map[string]any) string {
	if name, ok := h.State.DeviceName(address); ok {
		return name
	}
	if t, ok := d["TYPE"].(string); ok && t != "" {
		return t
	}
	return address
}
