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

// Engine routes incoming scripts to handlers. Scripts that carry the
// "!# name: <script>.fn" header a real client ships are dispatched by
// that name; everything else falls back to content pattern matching.
type Engine struct {
	state *state.Manager
	rpc   RPC

	byName   map[string]func(script string) string
	patterns []patternHandler
}

type patternHandler struct {
	re *regexp.Regexp
	fn func(script string) string
}

// reScriptName extracts the script identity from the header comment
// that aiohomematic and gohomematic ship verbatim with every script
// ("!# name: fetch_all_device_data.fn"). Only "##var##" placeholders
// are substituted before sending, so the header always survives.
var reScriptName = regexp.MustCompile(`(?im)^\s*!#\s*name:\s*([A-Za-z0-9_.-]+)`)

// New constructs an engine bound to the given state and RPC.
func New(stateMgr *state.Manager, rpc RPC) *Engine {
	e := &Engine{state: stateMgr, rpc: rpc}

	// Name dispatch takes precedence over the pattern list below.
	// Without it the generic patterns shadow the specific scripts:
	// set_program_state.fn contains "dom.GetObject(ID_PROGRAMS)" and
	// was answered with a program listing, set_system_variable.fn was
	// answered with a sysvar listing, accept_device_in_inbox.fn was
	// caught by the "INBOX" catch-all and create_backup_status.fn by
	// the "/VERSION" pattern — all four reported success while
	// changing nothing.
	e.byName = map[string]func(string) string{
		"get_backend_info.fn":                 e.handleBackendInfo,
		"get_serial.fn":                       e.handleGetSerial,
		"fetch_all_device_data.fn":            e.handleFetchDeviceData,
		"get_alarm_messages.fn":               e.handleGetAlarmMessages,
		"get_service_messages.fn":             e.handleGetServiceMessages,
		"get_inbox_devices.fn":                e.handleGetInbox,
		"accept_device_in_inbox.fn":           e.handleAcceptInboxDevice,
		"acknowledge_message.fn":              e.handleAcknowledgeMessage,
		"set_program_state.fn":                e.handleSetProgramState,
		"set_system_variable.fn":              e.handleSetSysvar,
		"get_program_descriptions.fn":         e.handleGetProgramDescriptions,
		"get_system_variable_descriptions.fn": e.handleGetSysvarDescriptions,
		"create_backup_start.fn":              e.handleBackupStart,
		"create_backup_status.fn":             e.handleBackupStatus,
		"get_system_update_info.fn":           e.handleUpdateInfo,
		"trigger_firmware_update.fn":          e.handleTriggerUpdate,
	}

	e.patterns = []patternHandler{
		{regexp.MustCompile(`(?is)system\.Exec.*cat.*/VERSION`), e.handleBackendInfo},
		{regexp.MustCompile(`(?is)grep.*VERSION.*grep.*PRODUCT`), e.handleBackendInfo},
		{regexp.MustCompile(`(?i)\bget_serial(\.fn)?\b`), e.handleGetSerial},
		{regexp.MustCompile(`(?i)system\.GetVar\s*\(\s*["']?SERIALNO["']?\s*\)`), e.handleGetSerial},
		{regexp.MustCompile(`(?i)name:\s*fetch_all_device_data\.fn`), e.handleFetchDeviceData},
		{regexp.MustCompile(`(?i)name:\s*get_alarm_messages\.fn`), e.handleGetAlarmMessages},
		{regexp.MustCompile(`(?is)foreach\s*\(\s*\w+\s*,\s*dom\.GetObject\s*\(\s*ID_DATAPOINTS`), e.handleFetchDeviceData},
		{regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*ID_PROGRAMS\s*\)`), e.handleGetPrograms},
		{regexp.MustCompile(`(?i)\.DPInfo\s*\(\s*\)`), e.handleGetSysvarDescriptions},
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

// utf8BOM is the byte sequence that real CCU firmwares (verified
// against an OpenCCU on 2026-04-28) treat as a poison pill: any
// script body starting with this prefix is silently dropped and
// runScript returns an empty result. We mirror that behaviour so
// integration tests against godevccu catch accidental BOM injection
// in the same way the production CCU would.
const utf8BOM = "\xef\xbb\xbf"

// Execute returns the result of running script.
func (e *Engine) Execute(script string) Result {
	if strings.HasPrefix(script, utf8BOM) {
		// Mirror real-CCU behaviour: BOM-prefixed scripts → empty result.
		return Result{Output: "", Success: true}
	}
	if m := reScriptName.FindStringSubmatch(script); m != nil {
		if fn, ok := e.byName[strings.ToLower(m[1])]; ok {
			return Result{Output: fn(script), Success: true}
		}
	}
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

// handleBackendInfo mirrors get_backend_info.fn, whose final Write emits
// the key "is_ha_app" — not "is_ha_addon". Clients read the script's key,
// so the old spelling silently degraded to the client-side default.
func (e *Engine) handleBackendInfo(_ string) string {
	info := e.state.BackendInfo()
	return mustJSON(map[string]any{
		"version":   info.Version,
		"product":   info.Product,
		"hostname":  info.Hostname,
		"is_ha_app": info.IsHaAddon,
	})
}

func (e *Engine) handleGetSerial(_ string) string {
	return mustJSON(map[string]any{"serial": e.state.Serial()})
}

var (
	reInterfaceAssign = regexp.MustCompile(`interface\s*=\s*"([^"]+)"`)
	reParamHeader     = regexp.MustCompile(`param:\s*"([^"]+)"`)
)

// handleFetchDeviceData mirrors fetch_all_device_data.fn, which emits a
// JSON *object* keyed by the ReGa datapoint name — Write('"'), the
// UriEncode()d oDP.Name(), Write('":'), the value. Clients index it as
// "<interface>.<channel_address>.<parameter>" and iterate it as a
// mapping, so the previous array of {address,param,value} objects made
// the whole bulk fetch unusable.
func (e *Engine) handleFetchDeviceData(script string) string {
	iface := ""
	if m := reInterfaceAssign.FindStringSubmatch(script); m != nil {
		iface = m[1]
	} else if m := reParamHeader.FindStringSubmatch(script); m != nil {
		iface = m[1]
	}
	values := e.state.AllDeviceValues(iface)
	out := make(map[string]any, len(values))
	for key, val := range values {
		idx := strings.LastIndex(key, ":")
		if idx <= 0 || idx == len(key)-1 {
			continue
		}
		address, param := key[:idx], key[idx+1:]
		name := address + "." + param
		if iface != "" {
			name = iface + "." + name
		}
		// The script URI-encodes the datapoint name and string values;
		// the client URL-decodes both sides again.
		out[uriEncode(name)] = encodeDeviceValue(val)
	}
	return mustJSON(out)
}

// encodeDeviceValue mirrors the value branches of the script: strings
// are URI-encoded, an empty string collapses to 0, everything else goes
// out as its native JSON type.
func encodeDeviceValue(val any) any {
	s, ok := val.(string)
	if !ok {
		return val
	}
	if s == "" {
		return 0
	}
	return uriEncode(s)
}

func (e *Engine) handleGetPrograms(_ string) string {
	progs := e.state.Programs()
	out := make([]map[string]any, 0, len(progs))
	for _, p := range progs {
		out = append(out, map[string]any{
			"id":              p.ID,
			"name":            uriEncode(p.Name),
			"description":     uriEncode(p.Description),
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
			"name":        uriEncode(sv.Name),
			"description": uriEncode(sv.Description),
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

// handleGetSysvarDescriptions answers the sysvar-description script
// family (loom's get_system_variable_descriptions.fn and equivalents):
// scripts that walk ID_SYSTEM_VARIABLES and emit each variable's
// DPInfo(). The real script frames ids as STRINGS and URL-encodes the
// free-text fields; channel_address carries the CCU WebUI channel
// assignment ("Kanalzuordnung", oVar.Channel() resolved to the channel
// address) and is empty for unassigned variables. Registered ahead of
// the generic ID_SYSTEM_VARIABLES handler so the description script is
// answered in its own wire shape.
func (e *Engine) handleGetSysvarDescriptions(_ string) string {
	svs := e.state.SystemVariables()
	out := make([]map[string]any, 0, len(svs))
	for _, sv := range svs {
		out = append(out, map[string]any{
			"id":              strconv.Itoa(sv.ID),
			"description":     uriEncode(sv.Description),
			"channel_address": uriEncode(sv.ChannelAddress),
		})
	}
	return mustJSON(out)
}

// handleGetAlarmMessages mirrors aiohomematic's get_alarm_messages.fn: the
// script lists ID_SYSTEM_VARIABLES entries of TypeName ALARMDP with an
// active AlState. godevccu's state does not model alarm datapoints, so the
// active-alarm list is always empty — matching a real CCU without pending
// alarms. Without this pattern the script's DPInfo()/ID_SYSTEM_VARIABLES
// body is misrouted to the sysvar handlers, whose entries lack the "name"
// key the alarm parser requires (KeyError, entry setup fails).
func (e *Engine) handleGetAlarmMessages(_ string) string {
	return "[]"
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

// handleGetInbox mirrors get_inbox_devices.fn, which writes the keys
// "id", "address", "name", "type" and "interface" — not "deviceId" and
// "deviceType". Clients index the entries by the script's key names, so
// the old spelling raised a KeyError on the very first inbox device.
func (e *Engine) handleGetInbox(_ string) string {
	devs := e.state.InboxDevices()
	out := make([]map[string]any, 0, len(devs))
	for _, d := range devs {
		out = append(out, map[string]any{
			"id":        d.DeviceID,
			"address":   d.Address,
			"name":      uriEncode(d.Name),
			"type":      d.DeviceType,
			"interface": d.Interface,
		})
	}
	return mustJSON(out)
}

var reInboxAddress = regexp.MustCompile(`(?i)sDeviceAddress\s*=\s*"([^"]*)"`)

// handleAcceptInboxDevice mirrors accept_device_in_inbox.fn: it sets
// ReadyConfig on the matching device and answers with the object
// {"success":bool,"error":string}. It used to be swallowed by the
// generic "INBOX" pattern, which returned the inbox *listing* — a JSON
// array on which the client's .get("success") raised AttributeError.
func (e *Engine) handleAcceptInboxDevice(script string) string {
	m := reInboxAddress.FindStringSubmatch(script)
	if m == nil || m[1] == "" {
		return regaResult(false, "Device not found")
	}
	if !e.state.AcceptInboxDevice(m[1]) {
		return regaResult(false, "Device not found")
	}
	return regaResult(true, "")
}

var reMessageID = regexp.MustCompile(`(?i)sMessageId\s*=\s*"([^"]*)"`)

// handleAcknowledgeMessage mirrors acknowledge_message.fn: it receipts
// the service message with the given ID and answers with
// {"success":bool,"error":string}. Previously the script's ID_SERVICES
// loop routed it to the service-message listing.
func (e *Engine) handleAcknowledgeMessage(script string) string {
	m := reMessageID.FindStringSubmatch(script)
	if m == nil {
		return regaResult(false, "Message not found")
	}
	id, err := strconv.Atoi(m[1])
	if err != nil || !e.state.ClearServiceMessage(id) {
		return regaResult(false, "Message not found")
	}
	return regaResult(true, "")
}

// regaResult renders the {"success":…,"error":…} object that the inbox
// and acknowledge scripts assemble with literal Write() calls.
func regaResult(success bool, errMsg string) string {
	return mustJSON(map[string]any{"success": success, "error": errMsg})
}

var (
	reProgramActive = regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*(\d+)\s*\)\.Active\s*\(\s*(true|false)\s*\)`)
	reProgramID     = regexp.MustCompile(`(?i)p_id\s*=\s*"([^"]*)"`)
	reProgramState  = regexp.MustCompile(`(?i)p_state\s*=\s*([A-Za-z0-9]+)`)
)

// handleSetProgramState mirrors set_program_state.fn, which resolves the
// program via dom.GetObject(ID_PROGRAMS).Get(p_id), flips Active(p_state)
// and writes back the resulting Active() value. The substituted script
// never matches the old dom.GetObject(<id>).Active(<bool>) pattern, so
// the handler was dead code and the script fell through to the program
// *listing* — reporting success while the program state never changed.
func (e *Engine) handleSetProgramState(script string) string {
	id, active, ok := parseProgramState(script)
	if !ok {
		return ""
	}
	if !e.state.SetProgramActive(id, active) {
		// Unknown program: the script's "if (program)" guard fails and
		// nothing is written.
		return ""
	}
	return strconv.FormatBool(active)
}

// parseProgramState reads the parameters out of either the substituted
// set_program_state.fn (p_id / p_state, where the client sends "1" or
// "0") or the legacy inline dom.GetObject(<id>).Active(<bool>) form.
func parseProgramState(script string) (id int, active, ok bool) {
	if m := reProgramID.FindStringSubmatch(script); m != nil {
		parsed, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, false, false
		}
		s := reProgramState.FindStringSubmatch(script)
		if s == nil {
			return 0, false, false
		}
		return parsed, isRegaTrue(s[1]), true
	}
	if m := reProgramActive.FindStringSubmatch(script); m != nil {
		parsed, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, false, false
		}
		return parsed, strings.EqualFold(m[2], "true"), true
	}
	return 0, false, false
}

// isRegaTrue accepts every truthy spelling a client may substitute into
// a script: "1", "true" and Python's "True".
func isRegaTrue(s string) bool {
	return s == "1" || strings.EqualFold(s, "true")
}

var (
	reSysVarSet   = regexp.MustCompile(`(?i)dom\.GetObject\s*\(\s*"([^"]+)"\s*\)\.State\s*\(\s*"?([^")]*)"?\s*\)`)
	reSysVarName  = regexp.MustCompile(`(?i)sv_name\s*=\s*"([^"]*)"`)
	reSysVarValue = regexp.MustCompile(`(?i)sv_value\s*=\s*"([^"]*)"`)
)

// handleSetSysvar mirrors set_system_variable.fn, which resolves the
// variable via dom.GetObject(ID_SYSTEM_VARIABLES).Get(sv_name) and
// writes sv_value only when the variable is of type String. The
// substituted script never matched the old inline .State() pattern, so
// it fell through to the sysvar *listing* and the write was silently
// dropped.
func (e *Engine) handleSetSysvar(script string) string {
	if m := reSysVarName.FindStringSubmatch(script); m != nil {
		v := reSysVarValue.FindStringSubmatch(script)
		if v == nil {
			return ""
		}
		sv, found := e.state.SystemVariable(m[1])
		// The script guards on "if (target_sv)" and
		// "ValueTypeStr() == \"String\"": anything else writes nothing.
		if !found || !strings.EqualFold(sv.VarType, "STRING") {
			return ""
		}
		return strconv.FormatBool(e.state.SetSystemVariable(m[1], v[1]))
	}
	return e.setSysvarInline(script)
}

// setSysvarInline handles the legacy dom.GetObject("name").State(value)
// one-liner that clients without the script header still send.
func (e *Engine) setSysvarInline(script string) string {
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

// handleBackupStatus mirrors create_backup_status.fn: only a completed
// backup carries the file details, and the key is "file" (the path on
// the CCU) — the previous "filepath" plus always-present "pid" never
// existed in the script's output.
func (e *Engine) handleBackupStatus(_ string) string {
	st := e.state.BackupStatus()
	if st.Status != "completed" {
		return mustJSON(map[string]any{"status": st.Status})
	}
	return mustJSON(map[string]any{
		"status":   st.Status,
		"file":     st.Filepath,
		"filename": st.Filename,
		"size":     st.Size,
	})
}

// handleUpdateInfo mirrors get_system_update_info.fn, which writes
// snake_case keys. The previous camelCase spelling meant every client
// fell back to its defaults — empty firmware strings and "no update".
func (e *Engine) handleUpdateInfo(_ string) string {
	info := e.state.UpdateInfo()
	return mustJSON(map[string]any{
		"current_firmware":       info.CurrentFirmware,
		"available_firmware":     info.AvailableFirmware,
		"update_available":       info.UpdateAvailable,
		"check_script_available": true,
	})
}

// handleTriggerUpdate mirrors trigger_firmware_update.fn, which reports
// script availability and a message alongside the success flag.
func (e *Engine) handleTriggerUpdate(_ string) string {
	ok := e.state.TriggerUpdate()
	msg := "Firmware update triggered, system will reboot when ready"
	if !ok {
		msg = "No firmware update available"
	}
	return mustJSON(map[string]any{
		"success":          ok,
		"script_available": true,
		"message":          msg,
	})
}

// handleGetProgramDescriptions mirrors get_program_descriptions.fn,
// which emits {"id": "<string>", "description": "<uri-encoded>"} per
// program. The generic ID_PROGRAMS pattern used to answer it with the
// full Program.getAll shape, whose integer ids the client cannot match
// against its string program ids.
func (e *Engine) handleGetProgramDescriptions(_ string) string {
	progs := e.state.Programs()
	out := make([]map[string]any, 0, len(progs))
	for _, p := range progs {
		out = append(out, map[string]any{
			"id":          strconv.Itoa(p.ID),
			"description": uriEncode(p.Description),
		})
	}
	return mustJSON(out)
}

func (e *Engine) handleGetRooms(_ string) string {
	rooms := e.state.Rooms()
	out := make([]map[string]any, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, map[string]any{
			"id":          r.ID,
			"name":        uriEncode(r.Name),
			"description": uriEncode(r.Description),
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
			"name":        uriEncode(f.Name),
			"description": uriEncode(f.Description),
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

// uriEncode mirrors ReGa's String.UriEncode(): percent-encoding with
// spaces as %20. [url.QueryEscape] would emit "+" instead, which
// clients decode with unquote() (not unquote_plus), so "Wohnzimmer
// Licht" would reach them as "Wohnzimmer+Licht".
func uriEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
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
