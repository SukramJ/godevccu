// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/SukramJ/godevccu/internal/converter"
	"github.com/SukramJ/godevccu/internal/deviceresponses"
	"github.com/SukramJ/godevccu/internal/hmconst"
	"github.com/SukramJ/godevccu/internal/xmlrpc"
)

// ErrRPC is the simulator-side error. It is the equivalent of pydevccu's
// RPCError. Surfaced as XML-RPC fault code -1 over the wire.
var ErrRPC = errors.New("rpc error")

// EventCallback is invoked whenever the simulator fires an event into a
// registered remote (or via [RPCFunctions.RegisterParamsetCallback]).
type EventCallback func(interfaceID, address, valueKey string, value any)

// RPCFunctions implements every XML-RPC method exposed by a HomeMatic
// CCU. It mirrors the surface area of pydevccu/ccu.py:RPCFunctions.
//
// All methods are safe for concurrent use.
type RPCFunctions struct {
	logger *slog.Logger

	mu sync.Mutex

	version     string
	interfaceID string

	persistence     bool
	persistencePath string

	// device universe
	devices            []map[string]any
	deviceByAddress    map[string]map[string]any
	paramsetDescByAddr map[string]map[string]any
	supportedDevices   map[string]string
	activeDevices      map[string]struct{}

	// per-channel paramset values
	paramsets        map[string]map[string]map[string]any
	paramsetDefaults map[paramsetKey]map[string]any
	paramsetCompiled map[paramsetKey]map[string]any
	paramsetDirty    map[paramsetKey]struct{}

	// callback wiring
	remotes           map[string]*xmlrpc.Client
	paramsetCallbacks []EventCallback

	knownDevices []map[string]any

	// runtime flag toggled by the surrounding ServerThread.
	active bool
}

type paramsetKey struct {
	address string
	kind    string
}

// Options is the constructor argument set for [NewRPCFunctions].
type Options struct {
	// Devices restricts the loaded device-type catalogue. Empty means
	// "load every embedded type".
	Devices []string
	// Persistence toggles paramset persistence to PersistencePath.
	Persistence bool
	// PersistencePath is the file used when Persistence is true.
	// Defaults to [hmconst.ParamsetsDB] in the working directory.
	PersistencePath string
	// Version is the string returned by getVersion. When empty, it
	// defaults to "pydevccu-<PydevccuVersion>" (Homegear-mode), which
	// matches what upstream pydevccu reports. CCU/OpenCCU callers
	// override this with the real CCU firmware version.
	Version string
	// InterfaceID is the identifier the simulator reports to remote
	// callbacks; defaults to "godevccu".
	InterfaceID string
	// Logger sinks structured log output. Defaults to slog.Default().
	Logger *slog.Logger
}

// NewRPCFunctions constructs the simulator. The embedded JSON catalogue
// is consulted; the optional restrict list is honoured.
func NewRPCFunctions(opts Options) (*RPCFunctions, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	version := opts.Version
	if version == "" {
		// Mirror pydevccu: Homegear-mode getVersion returns
		// "pydevccu-<VERSION>" so clients that detect pydevccu by
		// string-prefix (for example aiohomematic) treat us as the
		// upstream simulator.
		version = "pydevccu-" + hmconst.PydevccuVersion
	}
	ifID := opts.InterfaceID
	if ifID == "" {
		ifID = "godevccu"
	}
	persistencePath := opts.PersistencePath
	if persistencePath == "" {
		persistencePath = hmconst.ParamsetsDB
	}

	rpc := &RPCFunctions{
		logger:             logger,
		version:            version,
		interfaceID:        ifID,
		persistence:        opts.Persistence,
		persistencePath:    persistencePath,
		deviceByAddress:    make(map[string]map[string]any),
		paramsetDescByAddr: make(map[string]map[string]any),
		supportedDevices:   make(map[string]string),
		activeDevices:      make(map[string]struct{}),
		paramsets:          make(map[string]map[string]map[string]any),
		paramsetDefaults:   make(map[paramsetKey]map[string]any),
		paramsetCompiled:   make(map[paramsetKey]map[string]any),
		paramsetDirty:      make(map[paramsetKey]struct{}),
		remotes:            make(map[string]*xmlrpc.Client),
	}

	if _, err := rpc.loadDevices(opts.Devices); err != nil {
		// Match the Python behaviour: reset the device list on load
		// failure rather than refusing to start.
		logger.Warn("ccu: device load failed", "err", err)
		rpc.devices = nil
	}

	if rpc.persistence {
		if err := rpc.loadParamsetsFromDisk(); err != nil {
			logger.Debug("ccu: persistence load skipped", "err", err)
		}
	}

	return rpc, nil
}

// Version is the string returned by getVersion.
func (r *RPCFunctions) Version() string { return r.version }

// InterfaceID returns the configured interface identifier.
func (r *RPCFunctions) InterfaceID() string { return r.interfaceID }

// Active returns the active flag the surrounding server toggles.
func (r *RPCFunctions) Active() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// SetActive toggles the active flag.
func (r *RPCFunctions) SetActive(v bool) {
	r.mu.Lock()
	r.active = v
	r.mu.Unlock()
}

// SupportedDevices returns the loaded device-type → root-address map.
func (r *RPCFunctions) SupportedDevices() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.supportedDevices))
	for k, v := range r.supportedDevices {
		out[k] = v
	}
	return out
}

// RegisterParamsetCallback adds an in-process observer for value
// changes (used by the JSON-RPC handlers and tests).
func (r *RPCFunctions) RegisterParamsetCallback(cb EventCallback) {
	r.mu.Lock()
	r.paramsetCallbacks = append(r.paramsetCallbacks, cb)
	r.mu.Unlock()
}

// ─────────────────────────────────────────────────────────────────
// Device catalogue
// ─────────────────────────────────────────────────────────────────

// loadDevices loads every embedded device type into the simulator,
// honouring the optional restrict list. Returns the freshly loaded
// device descriptions.
func (r *RPCFunctions) loadDevices(restrict []string) ([]map[string]any, error) {
	sets, err := loadAllDevices(restrict)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	added := make([]map[string]any, 0)
	for _, s := range sets {
		if _, dup := r.activeDevices[s.deviceTypeKey]; dup {
			continue
		}
		r.devices = append(r.devices, s.devices...)
		added = append(added, s.devices...)
		for _, d := range s.devices {
			addr, _ := d[hmconst.AttrAddress].(string)
			if addr != "" {
				r.deviceByAddress[strings.ToUpper(addr)] = d
			}
		}
		for addr, ps := range s.paramsetByAddr {
			r.paramsetDescByAddr[addr] = ps
		}
		if s.rootDeviceAddr != "" {
			r.supportedDevices[s.deviceTypeKey] = s.rootDeviceAddr
		}
		r.activeDevices[s.deviceTypeKey] = struct{}{}
	}
	return added, nil
}

// AddDevices loads additional device types and pushes the descriptions
// into all registered remotes via newDevices.
func (r *RPCFunctions) AddDevices(ctx context.Context, devices []string) error {
	added, err := r.loadDevices(devices)
	if err != nil {
		return err
	}
	if len(added) == 0 {
		return nil
	}
	r.mu.Lock()
	remotes := make(map[string]*xmlrpc.Client, len(r.remotes))
	for k, v := range r.remotes {
		remotes[k] = v
	}
	r.mu.Unlock()

	for ifID, client := range remotes {
		params := []xmlrpc.Value{
			xmlrpc.StringValue(ifID),
			xmlrpc.FromAny(any(toAnySlice(added))),
		}
		if _, err := client.Call(ctx, "newDevices", params); err != nil {
			r.logger.Debug("ccu: newDevices push failed", "interface", ifID, "err", err)
		}
	}
	return nil
}

// RemoveDevices removes the named device types (or all when names is
// nil) and tells callbacks to drop them via deleteDevices.
func (r *RPCFunctions) RemoveDevices(ctx context.Context, devices []string) {
	r.mu.Lock()
	target := devices
	if target == nil {
		target = make([]string, 0, len(r.activeDevices))
		for k := range r.activeDevices {
			target = append(target, k)
		}
	}
	addresses := make([]string, 0)
	for _, devName := range target {
		if _, ok := r.activeDevices[devName]; !ok {
			continue
		}
		delete(r.activeDevices, devName)
		delete(r.supportedDevices, devName)
		filtered := r.devices[:0]
		for _, d := range r.devices {
			if !deviceMatchesType(d, devName) {
				filtered = append(filtered, d)
				continue
			}
			addr, _ := d[hmconst.AttrAddress].(string)
			if addr != "" {
				addresses = append(addresses, addr)
				r.clearAddressCachesLocked(addr)
			}
		}
		r.devices = filtered
	}

	remotes := make(map[string]*xmlrpc.Client, len(r.remotes))
	for k, v := range r.remotes {
		remotes[k] = v
	}
	r.mu.Unlock()

	for ifID, client := range remotes {
		params := []xmlrpc.Value{
			xmlrpc.StringValue(ifID),
			xmlrpc.FromAny(any(addresses)),
		}
		if _, err := client.Call(ctx, "deleteDevices", params); err != nil {
			r.logger.Debug("ccu: deleteDevices push failed", "interface", ifID, "err", err)
		}
	}
}

// deviceMatchesType reproduces _device_matches_type from pydevccu: a
// channel matches when its PARENT_TYPE equals the type, otherwise the
// device's TYPE itself is consulted.
func deviceMatchesType(d map[string]any, typeName string) bool {
	addr, _ := d[hmconst.AttrAddress].(string)
	if !strings.Contains(addr, ":") {
		t, _ := d[hmconst.AttrType].(string)
		return t == typeName
	}
	pt, _ := d[hmconst.AttrParentType].(string)
	return pt == typeName
}

// clearAddressCachesLocked drops every cache entry owned by addr. Holds
// the manager lock implicitly.
func (r *RPCFunctions) clearAddressCachesLocked(address string) {
	addrUp := strings.ToUpper(address)
	delete(r.deviceByAddress, addrUp)
	delete(r.paramsetDescByAddr, address)
	delete(r.paramsets, address)
	for _, k := range []string{hmconst.ParamsetAttrValues, hmconst.ParamsetAttrMaster} {
		key := paramsetKey{address: addrUp, kind: k}
		delete(r.paramsetDefaults, key)
		delete(r.paramsetCompiled, key)
		delete(r.paramsetDirty, key)
	}
}

// ─────────────────────────────────────────────────────────────────
// Public XML-RPC methods
// ─────────────────────────────────────────────────────────────────

// ListDevices returns the full device catalogue.
func (r *RPCFunctions) ListDevices() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, len(r.devices))
	copy(out, r.devices)
	return out
}

// Ping mirrors RPCFunctions.ping.
func (r *RPCFunctions) Ping(_ string) bool { return true }

// GetVersion returns the configured version string.
func (r *RPCFunctions) GetVersion() string { return r.version }

// GetServiceMessages mimics the Python stub which always returns one
// example service message.
func (r *RPCFunctions) GetServiceMessages() [][]any {
	return [][]any{{"VCU0000001:1", hmconst.AttrError, 7}}
}

// GetAllSystemVariables returns the same hard-coded test data as the
// Python implementation.
func (r *RPCFunctions) GetAllSystemVariables() map[string]any {
	return map[string]any{"sys_var1": "str_var", "sys_var2": 13}
}

// GetSystemVariable returns the current timestamp as a string —
// pydevccu does the same.
func (r *RPCFunctions) GetSystemVariable(_ string) string {
	return nowString()
}

// SetSystemVariable is a no-op; matches the Python stub.
func (r *RPCFunctions) SetSystemVariable(_ string, _ any) {}

// DeleteSystemVariable is a no-op; matches the Python stub.
func (r *RPCFunctions) DeleteSystemVariable(_ string) {}

// GetDeviceDescription returns the device description for address.
func (r *RPCFunctions) GetDeviceDescription(address string) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.deviceByAddress[strings.ToUpper(address)]
	if !ok {
		return nil, fmt.Errorf("%w: device %q not found", ErrRPC, address)
	}
	return d, nil
}

// GetParamsetDescription returns the schema for the given paramset.
func (r *RPCFunctions) GetParamsetDescription(address, paramsetType string) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	desc, ok := r.paramsetDescByAddr[strings.ToUpper(address)]
	if !ok {
		// pydevccu lower-cases the address only when storing — try the
		// raw key as well to stay tolerant.
		desc, ok = r.paramsetDescByAddr[address]
	}
	if !ok {
		return nil, fmt.Errorf("%w: paramset description for %q not found", ErrRPC, address)
	}
	ps, ok := desc[paramsetType].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: paramset %q not found on %q", ErrRPC, paramsetType, address)
	}
	return ps, nil
}

// GetParamset returns the current values of paramset (defaults + overrides).
func (r *RPCFunctions) GetParamset(address, paramsetKey string) (map[string]any, error) {
	if paramsetKey != hmconst.ParamsetAttrMaster && paramsetKey != hmconst.ParamsetAttrValues {
		return nil, fmt.Errorf("%w: unsupported paramset key %q", ErrRPC, paramsetKey)
	}
	addrUp := strings.ToUpper(address)
	r.mu.Lock()
	defer r.mu.Unlock()

	key := psKey(addrUp, paramsetKey)
	if cached, ok := r.paramsetCompiled[key]; ok {
		if _, dirty := r.paramsetDirty[key]; !dirty {
			return cloneStringMap(cached), nil
		}
	}

	defaults, ok := r.paramsetDefaults[key]
	if !ok {
		desc, ok := r.paramsetDescByAddr[addrUp]
		if !ok {
			desc, ok = r.paramsetDescByAddr[address]
		}
		if !ok {
			return nil, fmt.Errorf("%w: paramset description for %q not found", ErrRPC, address)
		}
		ps, ok := desc[paramsetKey].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: paramset %q not found on %q", ErrRPC, paramsetKey, address)
		}
		built := buildDefaults(ps)
		r.paramsetDefaults[key] = built
		defaults = built
	}

	result := cloneStringMap(defaults)
	if overrides, ok := r.paramsets[addrUp]; ok {
		if ps, ok := overrides[paramsetKey]; ok {
			for k, v := range ps {
				result[k] = v
			}
		}
	}
	r.paramsetCompiled[key] = cloneStringMap(result)
	delete(r.paramsetDirty, key)
	return result, nil
}

// GetValue returns the current value for (address, valueKey).
func (r *RPCFunctions) GetValue(address, valueKey string) (any, error) {
	values, err := r.GetParamset(address, hmconst.ParamsetAttrValues)
	if err != nil {
		return nil, err
	}
	v, ok := values[valueKey]
	if !ok {
		return nil, fmt.Errorf("%w: value key %q not found on %q", ErrRPC, valueKey, address)
	}
	return v, nil
}

// SetValue routes to PutParamset, applying converter expansion when the
// value key is a combined parameter.
func (r *RPCFunctions) SetValue(address, valueKey string, value any, force bool) error {
	if converter.IsConvertable(valueKey) {
		s, _ := value.(string)
		paramset := converter.ConvertCombinedParameterToParamset(valueKey, s)
		return r.PutParamset(address, hmconst.ParamsetAttrValues, paramset, force)
	}
	return r.PutParamset(address, hmconst.ParamsetAttrValues, map[string]any{valueKey: value}, force)
}

// PutParamset writes one or more values into the paramset and fires the
// computed follow-up events.
func (r *RPCFunctions) PutParamset(address, paramsetKey string, paramset map[string]any, force bool) error {
	addrUp := strings.ToUpper(address)
	r.mu.Lock()
	desc, ok := r.paramsetDescByAddr[addrUp]
	if !ok {
		desc = r.paramsetDescByAddr[address]
	}
	if desc == nil {
		r.mu.Unlock()
		return fmt.Errorf("%w: paramset description for %q not found", ErrRPC, address)
	}
	paramDescs, ok := desc[paramsetKey].(map[string]any)
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w: paramset %q not found on %q", ErrRPC, paramsetKey, address)
	}
	deviceType := r.deviceTypeForAddressLocked(addrUp)

	type firedEvent struct {
		key   string
		value any
	}
	var toFire []firedEvent
	for valueKey, value := range paramset {
		paramData, ok := paramDescs[valueKey].(map[string]any)
		if !ok {
			r.mu.Unlock()
			return fmt.Errorf("%w: parameter %q not described on %q", ErrRPC, valueKey, address)
		}
		paramType, _ := paramData[hmconst.AttrType].(string)

		ops := readInt(paramData[hmconst.ParamsetAttrOperations])
		if !force && (ops&hmconst.ParamsetOperationsWrite) == 0 {
			r.mu.Unlock()
			return fmt.Errorf("%w: write not allowed for %s on %s", ErrRPC, valueKey, address)
		}

		if paramType == hmconst.ParamsetTypeAction {
			r.mu.Unlock()
			r.fireEvent(r.interfaceID, address, valueKey, true)
			return nil
		}

		converted := convertParamValue(value, paramType)
		switch paramType {
		case hmconst.ParamsetTypeEnum:
			if err := validateEnumBounds(converted, paramData); err != nil {
				r.mu.Unlock()
				return fmt.Errorf("%w: %s.%s: %v", ErrRPC, address, valueKey, err)
			}
		case hmconst.ParamsetTypeFloat, hmconst.ParamsetTypeInteger:
			converted = clampNumeric(converted, paramData, paramType)
		}

		// Ensure storage maps exist.
		if _, ok := r.paramsets[addrUp]; !ok {
			r.paramsets[addrUp] = make(map[string]map[string]any)
		}
		if _, ok := r.paramsets[addrUp][paramsetKey]; !ok {
			r.paramsets[addrUp][paramsetKey] = make(map[string]any)
		}
		r.paramsets[addrUp][paramsetKey][valueKey] = converted
		r.paramsetDirty[psKey(addrUp, paramsetKey)] = struct{}{}

		current := r.paramsets[addrUp][paramsetKey]
		response := deviceresponses.ComputeEvents(deviceType, valueKey, converted, current)
		for k, v := range response {
			r.paramsets[addrUp][paramsetKey][k] = v
			toFire = append(toFire, firedEvent{key: k, value: v})
		}
	}
	r.mu.Unlock()

	for _, ev := range toFire {
		r.fireEvent(r.interfaceID, address, ev.key, ev.value)
	}
	return nil
}

// FireEvent is the public wrapper for fireEvent (used by the device
// logic simulators).
func (r *RPCFunctions) FireEvent(interfaceID, address, valueKey string, value any) {
	r.fireEvent(interfaceID, address, valueKey, value)
}

func (r *RPCFunctions) fireEvent(interfaceID, address, valueKey string, value any) {
	addrUp := strings.ToUpper(address)
	r.mu.Lock()
	cbs := append([]EventCallback(nil), r.paramsetCallbacks...)
	remotes := make(map[string]*xmlrpc.Client, len(r.remotes))
	for k, v := range r.remotes {
		remotes[k] = v
	}
	r.mu.Unlock()

	for _, cb := range cbs {
		safeCallEvent(cb, interfaceID, addrUp, valueKey, value)
	}
	for ifID, client := range remotes {
		params := []xmlrpc.Value{
			xmlrpc.StringValue(ifID),
			xmlrpc.StringValue(addrUp),
			xmlrpc.StringValue(valueKey),
			xmlrpc.FromAny(value),
		}
		if _, err := client.Call(context.Background(), "event", params); err != nil {
			r.logger.Debug("ccu: callback event failed", "interface", ifID, "err", err)
			r.mu.Lock()
			delete(r.remotes, ifID)
			r.mu.Unlock()
		}
	}
}

// Init registers a callback URL or removes the matching remote when
// interfaceID is empty.
func (r *RPCFunctions) Init(url, interfaceID string) string {
	if interfaceID != "" {
		client := xmlrpc.NewClient(url)
		r.mu.Lock()
		r.remotes[interfaceID] = client
		r.mu.Unlock()
		go r.askDevices(interfaceID)
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for ifID, client := range r.remotes {
		if strings.Contains(client.URL(), url) || strings.Contains(url, client.URL()) {
			delete(r.remotes, ifID)
			break
		}
	}
	return ""
}

// askDevices queries the remote for its known device list and
// reconciles it against ours, just like _ask_devices in Python.
func (r *RPCFunctions) askDevices(interfaceID string) {
	r.mu.Lock()
	client, ok := r.remotes[interfaceID]
	r.mu.Unlock()
	if !ok {
		return
	}
	resp, err := client.Call(context.Background(), "listDevices", []xmlrpc.Value{xmlrpc.StringValue(interfaceID)})
	if err != nil {
		r.logger.Debug("ccu: listDevices on remote failed", "interface", interfaceID, "err", err)
		return
	}
	known := make([]map[string]any, 0)
	if arr, ok := resp.(xmlrpc.ArrayValue); ok {
		for _, e := range arr {
			if m, ok := xmlrpc.ToAny(e).(map[string]any); ok {
				known = append(known, m)
			}
		}
	}
	r.mu.Lock()
	r.knownDevices = known
	r.mu.Unlock()
	r.pushDevices(interfaceID)
}

// pushDevices sends newDevices/deleteDevices for the diff between our
// catalogue and the client's known set.
func (r *RPCFunctions) pushDevices(interfaceID string) {
	r.mu.Lock()
	client, ok := r.remotes[interfaceID]
	if !ok {
		r.mu.Unlock()
		return
	}
	knownAddresses := make(map[string]struct{}, len(r.knownDevices))
	var deleteList []string
	for _, d := range r.knownDevices {
		addr, _ := d[hmconst.AttrAddress].(string)
		if _, ok := r.paramsetDescByAddr[addr]; !ok {
			deleteList = append(deleteList, addr)
		} else {
			knownAddresses[addr] = struct{}{}
		}
	}
	var newList []map[string]any
	for _, d := range r.devices {
		addr, _ := d[hmconst.AttrAddress].(string)
		if _, ok := knownAddresses[addr]; !ok {
			newList = append(newList, d)
		}
	}
	r.mu.Unlock()

	if len(newList) > 0 {
		params := []xmlrpc.Value{
			xmlrpc.StringValue(interfaceID),
			xmlrpc.FromAny(any(toAnySlice(newList))),
		}
		if _, err := client.Call(context.Background(), "newDevices", params); err != nil {
			r.logger.Debug("ccu: newDevices push failed", "interface", interfaceID, "err", err)
		}
	}
	if len(deleteList) > 0 {
		params := []xmlrpc.Value{
			xmlrpc.StringValue(interfaceID),
			xmlrpc.FromAny(any(deleteList)),
		}
		if _, err := client.Call(context.Background(), "deleteDevices", params); err != nil {
			r.logger.Debug("ccu: deleteDevices push failed", "interface", interfaceID, "err", err)
		}
	}
}

// ClientServerInitialized mirrors the helper of the same name.
func (r *RPCFunctions) ClientServerInitialized(interfaceID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.remotes[interfaceID]
	return ok
}

// SetMetadata, GetMetadata, link helpers mirror the Python stubs.

// GetMetadata returns the requested metadata field.
func (r *RPCFunctions) GetMetadata(objectID, dataID string) (any, error) {
	addr := strings.ToUpper(objectID)
	if i := strings.IndexByte(addr, ':'); i >= 0 {
		addr = addr[:i]
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.deviceByAddress[addr]
	if !ok {
		return nil, fmt.Errorf("%w: device %q not found", ErrRPC, objectID)
	}
	if v, ok := d[dataID]; ok {
		return v, nil
	}
	if dataID == hmconst.AttrName {
		typeStr, _ := d[hmconst.AttrType].(string)
		parentType, _ := d[hmconst.AttrParentType].(string)
		address, _ := d[hmconst.AttrAddress].(string)
		if children, ok := d[hmconst.AttrChildren].([]any); ok && len(children) > 0 {
			return fmt.Sprintf("%s %s", typeStr, address), nil
		}
		return fmt.Sprintf("%s %s", parentType, address), nil
	}
	return nil, nil
}

// SetMetadata accepts and discards metadata writes.
func (r *RPCFunctions) SetMetadata(_, _ string, _ any) bool { return true }

// AddLink and friends are no-op stubs matching pydevccu.
func (r *RPCFunctions) AddLink(_, _, _, _ string) bool { return true }
func (r *RPCFunctions) RemoveLink(_, _ string) bool    { return true }
func (r *RPCFunctions) GetLinkPeers(_ string) []string { return []string{} }
func (r *RPCFunctions) GetLinks(_ string, _ int) []any { return []any{} }
func (r *RPCFunctions) GetInstallMode() int            { return 0 }
func (r *RPCFunctions) SetInstallMode(_ bool, _ int, _ int, _ string) bool {
	return true
}
func (r *RPCFunctions) ReportValueUsage(_, _ string, _ int) bool { return true }
func (r *RPCFunctions) InstallFirmware(_ string) bool            { return true }
func (r *RPCFunctions) UpdateFirmware(_ string) bool             { return true }

// ─────────────────────────────────────────────────────────────────
// Persistence
// ─────────────────────────────────────────────────────────────────

// SaveParamsets writes the current paramset values to disk when
// persistence is enabled.
func (r *RPCFunctions) SaveParamsets() error {
	if !r.persistence {
		return nil
	}
	r.mu.Lock()
	data, err := json.Marshal(r.paramsets)
	r.mu.Unlock()
	if err != nil {
		return err
	}
	return os.WriteFile(r.persistencePath, data, 0o644) //nolint:gosec
}

func (r *RPCFunctions) loadParamsetsFromDisk() error {
	raw, err := os.ReadFile(r.persistencePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialise an empty file like pydevccu does.
			return os.WriteFile(r.persistencePath, []byte("{}"), 0o644) //nolint:gosec
		}
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	var loaded map[string]map[string]map[string]any
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return err
	}
	r.mu.Lock()
	r.paramsets = loaded
	r.mu.Unlock()
	return nil
}

// ─────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────

func psKey(address, kind string) paramsetKey {
	return paramsetKey{address: address, kind: kind}
}

func cloneStringMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func buildDefaults(ps map[string]any) map[string]any {
	out := make(map[string]any, len(ps))
	for name, raw := range ps {
		desc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		flags := readInt(desc[hmconst.AttrFlags])
		if flags&hmconst.ParamsetFlagInternal != 0 {
			continue
		}
		def := desc[hmconst.ParamsetAttrDefault]
		if t, _ := desc[hmconst.AttrType].(string); t == hmconst.ParamsetTypeEnum {
			if _, ok := def.(int); !ok {
				if list, ok := desc[hmconst.ParamsetAttrValueList].([]any); ok {
					if s, ok := def.(string); ok {
						for i, v := range list {
							if vs, ok := v.(string); ok && vs == s {
								def = i
								break
							}
						}
					}
				}
			}
		}
		out[name] = def
	}
	return out
}

func convertParamValue(value any, paramType string) any {
	switch paramType {
	case hmconst.ParamsetTypeBool:
		return toBool(value)
	case hmconst.ParamsetTypeString:
		return toString(value)
	case hmconst.ParamsetTypeInteger, hmconst.ParamsetTypeEnum:
		return int(toFloat(value))
	case hmconst.ParamsetTypeFloat:
		return toFloat(value)
	}
	return value
}

func validateEnumBounds(value any, desc map[string]any) error {
	maxRaw, ok := desc[hmconst.ParamsetAttrMax]
	if !ok {
		return nil
	}
	if _, isStr := maxRaw.(string); isStr {
		// String enum bounds are not numerically comparable.
		return nil
	}
	max := toFloat(maxRaw)
	min := toFloat(desc[hmconst.ParamsetAttrMin])
	v := toFloat(value)
	if v > max {
		return fmt.Errorf("value %v exceeds max %v", v, max)
	}
	if v < min {
		return fmt.Errorf("value %v below min %v", v, min)
	}
	return nil
}

func clampNumeric(value any, desc map[string]any, paramType string) any {
	special := map[float64]struct{}{}
	if entries, ok := desc[hmconst.ParamsetAttrSpecial].([]any); ok {
		for _, e := range entries {
			pair, ok := e.([]any)
			if !ok {
				continue
			}
			for _, item := range pair {
				special[toFloat(item)] = struct{}{}
			}
		}
	}
	v := toFloat(value)
	if _, isSpecial := special[v]; isSpecial {
		if paramType == hmconst.ParamsetTypeInteger {
			return int(v)
		}
		return v
	}
	max := toFloat(desc[hmconst.ParamsetAttrMax])
	min := toFloat(desc[hmconst.ParamsetAttrMin])
	if v > max {
		v = max
	}
	if v < min {
		v = min
	}
	if paramType == hmconst.ParamsetTypeInteger {
		return int(v)
	}
	return v
}

func (r *RPCFunctions) deviceTypeForAddressLocked(addrUp string) string {
	d, ok := r.deviceByAddress[addrUp]
	if !ok {
		return ""
	}
	if pt, _ := d[hmconst.AttrParentType].(string); pt != "" {
		return pt
	}
	if t, _ := d[hmconst.AttrType].(string); t != "" {
		return t
	}
	return ""
}

func toAnySlice(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func toBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case float32:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x == "true" || x == "True" || x == "1"
	}
	return false
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprintf("%v", x)
	}
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		f, err := parseFloat(x)
		if err == nil {
			return f
		}
	}
	return 0
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%g", &f)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("non-finite float %q", s)
	}
	return f, nil
}

func readInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	case bool:
		if x {
			return 1
		}
		return 0
	}
	return 0
}

// safeCallEvent calls cb with panic recovery.
func safeCallEvent(cb EventCallback, interfaceID, address, valueKey string, value any) {
	defer func() { _ = recover() }()
	cb(interfaceID, address, valueKey, value)
}

func nowString() string {
	return fmt.Sprintf("%d", nowSeconds())
}

func nowSeconds() int64 {
	return nowFunc().Unix()
}
