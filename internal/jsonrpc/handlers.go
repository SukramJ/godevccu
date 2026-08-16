// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package jsonrpc

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

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

	// Interfaces overrides the hard-coded listInterfaces answer with the
	// real interface inventory. Empty keeps the two-entry default.
	Interfaces []InterfaceInfo

	// RealisticSchema reports the CCU's own field names and types
	// instead of the pydevccu-shaped ones; see
	// virtualccu.Realism.JSONSchema.
	RealisticSchema bool

	// RegaIDs reports numeric ReGa object ids for devices and channels;
	// see virtualccu.Realism.RegaIDs.
	RegaIDs bool

	// httpsRedirect backs CCU.getHttpsRedirectEnabled.
	httpsRedirect atomic.Bool
}

// InterfaceInfo is one entry of Interface.listInterfaces: the name a
// client uses to address the interface, the port it listens on and a
// human-readable description.
type InterfaceInfo struct {
	Name string
	Port int
	Info string
}

// SetHTTPSRedirect records whether the CCU enforces HTTPS, which
// CCU.getHttpsRedirectEnabled reports.
func (h *Handlers) SetHTTPSRedirect(enabled bool) { h.httpsRedirect.Store(enabled) }

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
		"CCU.getSerial":               h.getSerial,
		"CCU.getVersion":              h.getCCUVersion,
		"system.listMethods":          h.listMethods,
		"system.methodHelp":           h.methodHelp,
		"system.describe":             h.describe,
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
		"Interface.rssiInfo":               h.rssiInfo,
		"Interface.listBidcosInterfaces":   h.listBidcosInterfaces,
		"Interface.getLinkInfo":            h.getLinkInfo,
		"Interface.setLinkInfo":            h.setLinkInfo,
		"Interface.determineParameter":     h.determineParameter,
		"Interface.getParamsetId":          h.getParamsetID,

		"Interface.getSuppressedServiceMessages": h.getSuppressedServiceMessages,
		"Interface.suppressServiceMessages":      h.suppressServiceMessages,
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
		"SysVar.get":                h.sysvarGet,
		"SysVar.getValueByName":     h.sysvarGetValueByName,
		"SysVar.setBool":            h.sysvarSet,
		"SysVar.setFloat":           h.sysvarSet,
		"SysVar.setString":          h.sysvarSet,
		"SysVar.setEnum":            h.sysvarSetEnum,
		"SysVar.createBool":         h.sysvarCreate("BOOL"),
		"SysVar.createFloat":        h.sysvarCreate("FLOAT"),
		"SysVar.createEnum":         h.sysvarCreate("ENUM"),
		"SysVar.deleteSysVarByName": h.sysvarDelete,
		// Rooms / Functions
		"Room.getAll":       h.roomGetAll,
		"Room.listAll":      h.roomListAll,
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
	// LEVEL NONE on a real CCU, like listMethods.
	"system.methodHelp": {},
	"system.describe":   {},
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
	return h.httpsRedirect.Load(), nil
}

// listMethods answers system.listMethods with the name, privilege level
// and description of every method, sorted by name — the shape a real
// CCU produces from its methods.conf. Reporting the bare name hid the
// level information clients use to reason about permissions.
func (h *Handlers) listMethods(_ context.Context, _ map[string]any) (any, error) {
	methods := h.Methods()
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		m := metaFor(name)
		out = append(out, map[string]any{
			"name":  name,
			"level": m.level,
			"info":  m.info,
		})
	}
	return out, nil
}

// methodHelp answers system.methodHelp with a method's description.
func (h *Handlers) methodHelp(_ context.Context, params map[string]any) (any, error) {
	name := stringParam(params, "name")
	if name == "" {
		return nil, ErrParams("Missing name parameter")
	}
	if _, ok := h.Methods()[name]; !ok {
		return nil, ErrObject("Method", name)
	}
	return metaFor(name).info, nil
}

// describe answers system.describe with the full method catalogue —
// the same records as listMethods, which is what the CCU serves.
func (h *Handlers) describe(ctx context.Context, params map[string]any) (any, error) {
	return h.listMethods(ctx, params)
}

// ─────────────────────────────────────────────────────────────────
// Interface
// ─────────────────────────────────────────────────────────────────

// listInterfaces reports the interface inventory. With separate
// interface listeners configured it reports them verbatim in the CCU's
// own field set (name/port/info); otherwise it keeps the established
// two-entry answer.
func (h *Handlers) listInterfaces(_ context.Context, _ map[string]any) (any, error) {
	if len(h.Interfaces) > 0 {
		out := make([]map[string]any, 0, len(h.Interfaces))
		for _, iface := range h.Interfaces {
			out = append(out, map[string]any{
				"name": iface.Name,
				"port": iface.Port,
				"info": iface.Info,
			})
		}
		return out, nil
	}
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

// getInstallMode and setInstallMode run the same pairing automaton the
// XML-RPC surface does. They answered a constant 0 and a constant true
// while the automaton existed, so a client that opens its pairing
// window over JSON-RPC — which is the transport the method belongs to —
// read a closed window the whole time it was open.
func (h *Handlers) getInstallMode(_ context.Context, _ map[string]any) (any, error) {
	if h.RPC == nil {
		return 0, nil
	}
	return h.RPC.GetInstallMode(), nil
}

func (h *Handlers) setInstallMode(_ context.Context, params map[string]any) (any, error) {
	if h.RPC == nil {
		return true, nil
	}
	// The CCU spells the duration `time` on this transport and `on` for
	// the switch; `mode` and `address` are the optional restriction to
	// a single device.
	duration, err := intParam(params, "time", "duration")
	if err != nil {
		duration = 0
	}
	mode, err := intParam(params, "mode")
	if err != nil {
		mode = 1
	}
	return h.RPC.SetInstallMode(
		boolParam(params, "on", true),
		duration,
		mode,
		stringParam(params, "address"),
	), nil
}

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
			channelsByParent[parentAddr] = append(channelsByParent[parentAddr], h.channelRecord(address, d))
		} else {
			parents[address] = d
		}
	}
	out := make([]map[string]any, 0, len(parents))
	for address, d := range parents {
		record := map[string]any{
			"id":        h.objectID(address),
			"address":   address,
			"type":      d["TYPE"],
			"name":      h.deviceName(address, d),
			"interface": h.interfaceOf(d),
			"channels":  channelsByParent[address],
		}
		if h.RealisticSchema {
			// A client reads the firmware fields off this record, not
			// off the XML-RPC device description, so without them no
			// firmware information reaches it at all.
			record["paramsets"] = paramsetNames(d)
			record["firmware"] = stringOrDefault(d["FIRMWARE"], "")
			record["availableFirmware"] = stringOrDefault(d["AVAILABLE_FIRMWARE"], "")
			record["updatable"] = asBool(d["UPDATABLE"])
			record["firmwareUpdateState"] = stringOrDefault(d["FIRMWARE_UPDATE_STATE"], "")
			record["readyConfig"] = true
		}
		out = append(out, record)
	}
	return out, nil
}

// channelRecord builds one entry of a device's "channels" array.
func (h *Handlers) channelRecord(address string, d map[string]any) map[string]any {
	record := map[string]any{
		"id":        h.objectID(address),
		"address":   address,
		"type":      d["TYPE"],
		"name":      h.deviceName(address, d),
		"interface": h.interfaceOf(d),
	}
	if h.RealisticSchema {
		record["deviceId"] = h.objectID(parentAddress(address))
		record["index"] = channelIndex(address)
		record["channelType"] = d["TYPE"]
		record["paramsets"] = paramsetNames(d)
		record["mode"] = "MODE_UNKNOWN"
		record["category"] = categoryFor(d["DIRECTION"])
		record["partnerId"] = ""
		record["isReady"] = true
		record["visible"] = true
		record["operateGroupOnly"] = "false"
	}
	return record
}

// objectID reports a channel's or device's id. With ReGa ids enabled it
// is the object id a CCU assigns — a client stores it as the ise_id and
// cross-references it against the channel ids of rooms and functions,
// which never match a textual address.
//
// The id goes out as a *string*: that is what a CCU sends
// (`"id": "18470"`, read back from 3.87), and a client whose DTO says
// string fails to decode the whole document when it arrives as a
// number — losing every entry, not just the id.
func (h *Handlers) objectID(address string) any {
	if !h.RegaIDs || h.State == nil {
		return address
	}
	return strconv.Itoa(h.State.RegisterAddress(address))
}

// interfaceOf derives the interface name from the device description,
// falling back to the type prefix. Reporting a name a client does not
// recognise makes it silently substitute its own default.
func (h *Handlers) interfaceOf(d map[string]any) string {
	if iface, _ := d["INTERFACE"].(string); iface != "" && !strings.EqualFold(iface, "godevccu") {
		return iface
	}
	typeStr, _ := d["TYPE"].(string)
	if parent, ok := d["PARENT_TYPE"].(string); ok && parent != "" {
		typeStr = parent
	}
	return hmconst.InterfaceForType(typeStr)
}

// parentAddress strips the channel suffix from an address.
func parentAddress(address string) string {
	if i := strings.IndexByte(address, ':'); i >= 0 {
		return address[:i]
	}
	return address
}

// channelIndex is the number behind the colon, or 0.
func channelIndex(address string) int {
	i := strings.IndexByte(address, ':')
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(address[i+1:])
	if err != nil {
		return 0
	}
	return n
}

// paramsetNames reports the paramsets a device or channel exposes. The
// description carries them as an array; a client reads the field
// unconditionally.
func paramsetNames(d map[string]any) []string {
	raw, ok := d["PARAMSETS"].([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// categoryFor maps a channel's DIRECTION onto the CCU's category
// vocabulary: 1 = sender, 2 = receiver.
func categoryFor(direction any) string {
	switch asInt(direction) {
	case 1:
		return "CATEGORY_SENDER"
	case 2:
		return "CATEGORY_RECEIVER"
	default:
		return "CATEGORY_NONE"
	}
}

// asBool coerces the loosely typed description values to a bool —
// UPDATABLE arrives as an integer on the older device catalogue.
func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		return x == "1" || strings.EqualFold(x, "true")
	default:
		return false
	}
}

// asInt coerces a description value to an int.
func asInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, err := strconv.Atoi(x)
		if err == nil {
			return n
		}
	}
	return 0
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
		record := map[string]any{
			"id":              strconv.Itoa(p.ID),
			"name":            p.Name,
			"description":     p.Description,
			"isActive":        p.Active,
			"isInternal":      p.Internal,
			"lastExecuteTime": p.LastExecuteTime,
		}
		if h.RealisticSchema {
			// A CCU reports the last execution as a formatted local
			// timestamp, not as a Unix float.
			record["lastExecuteTime"] = formatCCUTime(p.LastExecuteTime)
		}
		out = append(out, record)
	}
	return out, nil
}

// ccuTimeLayout is how a CCU renders timestamps in its JSON API.
const ccuTimeLayout = "2006-01-02 15:04:05"

// formatCCUTime renders a Unix timestamp the CCU way. A zero timestamp
// means "never executed" and stays empty.
func formatCCUTime(ts float64) string {
	if ts <= 0 {
		return ""
	}
	sec := int64(ts)
	return time.Unix(sec, 0).Format(ccuTimeLayout)
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
		if h.RealisticSchema {
			out = append(out, ccuSysvarRecord(sv))
			continue
		}
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
			"isInternal":  sv.Internal,
		})
	}
	return out, nil
}

// ccuSysvarRecord renders a system variable the way a CCU does: the
// type names LOGIC/NUMBER/LIST/ALARM/STRING, every value as a string,
// and the type-dependent fields present only where they apply. There is
// no description and no timestamp — a CCU serves the description
// through a separate ReGa script.
func ccuSysvarRecord(sv *state.SystemVariable) map[string]any {
	varType := ccuVarType(sv.VarType)
	record := map[string]any{
		"id":         strconv.Itoa(sv.ID),
		"name":       sv.Name,
		"type":       varType,
		"unit":       sv.Unit,
		"value":      ccuValueString(sv.Value),
		"channelId":  sv.ChannelAddress,
		"isLogged":   sv.Logged,
		"isVisible":  sv.Visible,
		"isInternal": sv.Internal,
	}
	switch varType {
	case "LOGIC", "ALARM":
		record["valueName0"] = valueNameOr(sv.ValueName0, "false")
		record["valueName1"] = valueNameOr(sv.ValueName1, "true")
	case "LIST":
		record["valueList"] = sv.ValueList
	case "NUMBER":
		record["minValue"] = ccuValueString(sv.MinValue)
		record["maxValue"] = ccuValueString(sv.MaxValue)
	}
	return record
}

// valueNameOr falls back to the CCU's default names for a boolean.
func valueNameOr(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// ccuValueString stringifies a value the way the CCU's json_toString
// does — every value in the sysvar payload is a JSON string, whatever
// its underlying type.
func ccuValueString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
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

// getSerial answers CCU.getSerial. Clients used to reach the serial
// only through a ReGa script detour.
func (h *Handlers) getSerial(_ context.Context, _ map[string]any) (any, error) {
	return h.State.Serial(), nil
}

// getCCUVersion answers CCU.getVersion with the firmware version.
func (h *Handlers) getCCUVersion(_ context.Context, _ map[string]any) (any, error) {
	if h.RPC == nil {
		return hmconst.CCUFirmwareVersion, nil
	}
	return h.RPC.GetVersion(), nil
}

// rssiInfo answers Interface.rssiInfo with the nested
// device → partner → [rssi, rssi] structure of a real CCU.
func (h *Handlers) rssiInfo(_ context.Context, _ map[string]any) (any, error) {
	if h.RPC == nil {
		return map[string]any{}, nil
	}
	return h.RPC.RssiInfo(), nil
}

// listBidcosInterfaces answers Interface.listBidcosInterfaces in the
// CCU's JSON field spelling, mapped from the XML-RPC gateway record.
func (h *Handlers) listBidcosInterfaces(_ context.Context, _ map[string]any) (any, error) {
	if h.RPC == nil {
		return []any{}, nil
	}
	gateways := h.RPC.ListBidcosInterfaces()
	out := make([]map[string]any, 0, len(gateways))
	for _, gw := range gateways {
		out = append(out, map[string]any{
			"address":     gw[hmconst.AttrAddress],
			"description": gw[hmconst.AttrDescription],
			"dutyCycle":   gw["DUTY_CYCLE"],
			"isConnected": gw["CONNECTED"],
			"isDefault":   gw["DEFAULT"],
			"fwVersion":   gw["FIRMWARE_VERSION"],
			"type":        gw[hmconst.AttrType],
		})
	}
	return out, nil
}

// getLinkInfo / setLinkInfo carry the name and description of a direct
// link, which the simulator previously dropped entirely.
func (h *Handlers) getLinkInfo(_ context.Context, params map[string]any) (any, error) {
	sender := stringParam(params, "senderAddress")
	receiver := stringParam(params, "receiverAddress")
	if sender == "" || receiver == "" {
		return nil, ErrParams("Missing senderAddress or receiverAddress parameter")
	}
	if h.RPC == nil {
		return nil, ErrParams("No interface available")
	}
	info, err := h.RPC.GetLinkInfo(sender, receiver)
	if err != nil {
		return nil, ErrObject("Link", sender+"->"+receiver)
	}
	return map[string]any{
		"name":        info[hmconst.AttrName],
		"description": info[hmconst.AttrDescription],
	}, nil
}

func (h *Handlers) setLinkInfo(_ context.Context, params map[string]any) (any, error) {
	sender := stringParam(params, "sender")
	receiver := stringParam(params, "receiver")
	if sender == "" || receiver == "" {
		return nil, ErrParams("Missing sender or receiver parameter")
	}
	if h.RPC == nil {
		return nil, ErrParams("No interface available")
	}
	ok, err := h.RPC.SetLinkInfo(sender, receiver, stringParam(params, "name"), stringParam(params, "description"))
	if err != nil {
		return nil, ErrObject("Link", sender+"->"+receiver)
	}
	return ok, nil
}

// determineParameter asks the interface to re-read a parameter.
func (h *Handlers) determineParameter(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	parameterID := stringParam(params, "parameterId")
	if address == "" || parameterID == "" {
		return nil, ErrParams("Missing address or parameterId parameter")
	}
	if h.RPC == nil {
		return nil, ErrParams("No interface available")
	}
	ok, err := h.RPC.DetermineParameter(address, stringParam(params, "paramsetKey"), parameterID)
	if err != nil {
		return nil, ErrObject("Parameter", address+"."+parameterID)
	}
	return ok, nil
}

// getParamsetId returns the identifier a client uses to validate a
// cached paramset description.
func (h *Handlers) getParamsetID(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "address")
	paramsetType := stringParam(params, "paramsetType")
	if address == "" || paramsetType == "" {
		return nil, ErrParams("Missing address or paramsetType parameter")
	}
	if h.RPC == nil {
		return nil, ErrParams("No interface available")
	}
	id, err := h.RPC.GetParamsetID(address, paramsetType)
	if err != nil {
		return nil, ErrObject("Device", address)
	}
	return id, nil
}

// getSuppressedServiceMessages lists the service parameters silenced on
// a channel.
func (h *Handlers) getSuppressedServiceMessages(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "channelAddress")
	if address == "" {
		return nil, ErrParams("Missing channelAddress parameter")
	}
	if h.RPC == nil {
		return []string{}, nil
	}
	return h.RPC.SuppressedServiceMessages(address), nil
}

// suppressServiceMessages silences or un-silences a service parameter.
// An empty parameterId covers every service parameter of the channel.
func (h *Handlers) suppressServiceMessages(_ context.Context, params map[string]any) (any, error) {
	address := stringParam(params, "channelAddress")
	if address == "" {
		return nil, ErrParams("Missing channelAddress parameter")
	}
	if h.RPC == nil {
		return false, nil
	}
	return h.RPC.SuppressServiceMessage(
		address,
		stringParam(params, "parameterId"),
		boolParam(params, "suppress", true),
	), nil
}

// ccuVarType maps the simulator's internal type name to the CCU
// nomenclature the JSON API reports. Only the methods added here use
// it — SysVar.getAll keeps its established shape.
func ccuVarType(varType string) string {
	switch strings.ToUpper(varType) {
	case "BOOL":
		return "LOGIC"
	case "FLOAT":
		return "NUMBER"
	case "ENUM":
		return "LIST"
	case "ALARM":
		return "ALARM"
	default:
		return "STRING"
	}
}

// sysvarGet answers SysVar.get: the full detail record of one variable,
// addressed by its ReGa id. Conditional fields follow the CCU: names
// only for LOGIC/ALARM, the value list only for LIST, bounds only for
// NUMBER.
func (h *Handlers) sysvarGet(_ context.Context, params map[string]any) (any, error) {
	raw := stringParam(params, "id")
	if raw == "" {
		return nil, ErrParams("Missing id parameter")
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return nil, ErrParams("Invalid id parameter")
	}
	sv, ok := h.State.SystemVariableByID(id)
	if !ok {
		return nil, ErrObject("SystemVariable", raw)
	}
	varType := ccuVarType(sv.VarType)
	out := map[string]any{
		"id":         strconv.Itoa(sv.ID),
		"name":       sv.Name,
		"type":       varType,
		"unit":       sv.Unit,
		"value":      sv.Value,
		"channelId":  sv.ChannelAddress,
		"isLogged":   false,
		"isVisible":  true,
		"isInternal": false,
	}
	switch varType {
	case "LOGIC", "ALARM":
		out["valueName0"] = "false"
		out["valueName1"] = "true"
	case "LIST":
		out["valueList"] = sv.ValueList
	case "NUMBER":
		out["minValue"] = sv.MinValue
		out["maxValue"] = sv.MaxValue
	}
	return out, nil
}

// sysvarCreate backs SysVar.createBool / createFloat / createEnum. The
// CCU answers with the name, id and value of the new variable; without
// these methods a client could never exercise the create→set→read→
// delete lifecycle against the simulator.
func (h *Handlers) sysvarCreate(varType string) HandlerFunc {
	return func(_ context.Context, params map[string]any) (any, error) {
		name := stringParam(params, "name")
		if name == "" {
			return nil, ErrParams("Missing name parameter")
		}
		if _, exists := h.State.SystemVariable(name); exists {
			return nil, ErrParams("SystemVariable already exists")
		}
		opts := state.AddSystemVariableOpts{
			ChannelAddress: stringParam(params, "chnID"),
		}
		var value any
		switch varType {
		case "BOOL":
			value = boolParam(params, "init_val", false)
		case "FLOAT":
			opts.MinValue = floatParam(params, "minValue", 0)
			opts.MaxValue = floatParam(params, "maxValue", 100)
			value = opts.MinValue
		case "ENUM":
			opts.ValueList = stringParam(params, "valList")
			value = 0
		}
		sv := h.State.AddSystemVariable(name, varType, value, opts)
		return map[string]any{
			"name":  sv.Name,
			"id":    strconv.Itoa(sv.ID),
			"value": sv.Value,
		}, nil
	}
}

// sysvarSetEnum backs SysVar.setEnum, which replaces the value list of
// an enum variable and echoes it back (-1 on failure).
func (h *Handlers) sysvarSetEnum(_ context.Context, params map[string]any) (any, error) {
	name := stringParam(params, "name")
	valueList := stringParam(params, "valueList")
	if name == "" {
		return nil, ErrParams("Missing name parameter")
	}
	sv, ok := h.State.SystemVariable(name)
	if !ok {
		return -1, nil
	}
	sv.ValueList = valueList
	return valueList, nil
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
			"channelIds":  h.channelIDs(r.ChannelIDs),
		})
	}
	return out, nil
}

// roomListAll answers Room.listAll, which on a CCU returns nothing but
// the room ids — Room.getAll is the detailed variant. Without the
// realistic schema both stay aliases, as they have been.
func (h *Handlers) roomListAll(ctx context.Context, params map[string]any) (any, error) {
	if !h.RealisticSchema {
		return h.roomGetAll(ctx, params)
	}
	rooms := h.State.Rooms()
	out := make([]string, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, strconv.Itoa(r.ID))
	}
	return out, nil
}

// channelIDs converts stored channel addresses into the numeric ReGa
// ids a client matches against Device.listAllDetail. Without ReGa ids
// the addresses are reported unchanged.
// channelIDs maps a room's or function's member addresses onto the ids
// a client cross-references them by. Like [Handlers.objectID] they are
// strings on the wire — a live CCU answers
// `"channelIds": ["38552", "38524"]`.
func (h *Handlers) channelIDs(addresses []string) any {
	if !h.RegaIDs || h.State == nil {
		return addresses
	}
	ids := h.State.ChannelIDsForAddresses(addresses)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, strconv.Itoa(id))
	}
	return out
}

func (h *Handlers) subsectionGetAll(_ context.Context, _ map[string]any) (any, error) {
	funcs := h.State.Functions()
	out := make([]map[string]any, 0, len(funcs))
	for _, f := range funcs {
		out = append(out, map[string]any{
			"id":          strconv.Itoa(f.ID),
			"name":        f.Name,
			"description": f.Description,
			"channelIds":  h.channelIDs(f.ChannelIDs),
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

// boolParam reads a boolean parameter. The CCU's web API takes several
// of its booleans as the strings "true"/"false" — a client that mirrors
// that wire shape would otherwise fall through to the default and, on
// setInstallMode, close a window it was asked to open.
func boolParam(params map[string]any, key string, def bool) bool {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "1":
			return true
		case "false", "0":
			return false
		}
	}
	return def
}

// floatParam reads a number that a client may send as a JSON number or
// as a string (the CCU's own API accepts both).
func floatParam(params map[string]any, key string, def float64) float64 {
	v, ok := params[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f
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
