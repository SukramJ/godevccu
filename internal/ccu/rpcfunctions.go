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
	"time"

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
	// interfaceFilter restricts this instance to one protocol family;
	// see [Options.InterfaceFilter].
	interfaceFilter string

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

	// LINK paramset values: keyed by (sender, receiver), both upper-case.
	// Populated by AddLink; dropped by RemoveLink.
	linkParamsets map[linkKey]map[string]any

	// linkInfo holds the name/description a client attaches to a direct
	// link via setLinkInfo, keyed like linkParamsets.
	linkInfo map[linkKey]linkDetails

	// metadata is the object-id → data-id → value store behind
	// getMetadata/setMetadata. Writes used to be discarded, which made
	// a set→get round trip impossible.
	metadata map[string]map[string]any

	// callback wiring
	remotes           map[string]remoteCaller
	paramsetCallbacks []EventCallback

	// onSetValue is invoked synchronously after every successful
	// PutParamset paramset write. Used by tests to script CCU-side
	// echo events for ACTION DPs (the AUTO_MODE→CONTROL_MODE pair on
	// RF thermostats, for example). Nil = no hook.
	onSetValue func(address, valueKey string, value any)

	knownDevices []map[string]any

	// persistInit keeps callback registrations across restarts; see
	// initpersistence.go.
	persistInit bool

	// Batched, asynchronous event delivery; see dispatcher.go.
	batchEvents bool
	dispatchers map[string]*dispatcher

	// Service messages; see servicemessages.go.
	serviceMessages bool
	suppressed      map[string]map[string]struct{}

	// Device state machines; see reachability.go.
	reachability        bool
	configPendingFor    time.Duration
	configPendingTimers map[string]*time.Timer
	timersStopped       bool

	// runtime flag toggled by the surrounding ServerThread.
	active bool
}

type paramsetKey struct {
	address string
	kind    string
}

// linkKey is the composite key for a directed link between two channels.
// Both fields are stored upper-case for case-insensitive comparison.
type linkKey struct {
	sender   string
	receiver string
}

// linkDetails is the name/description pair a CCU stores per direct link
// and returns from getLinkInfo.
type linkDetails struct {
	name        string
	description string
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
	// InterfaceFilter restricts the loaded catalogue to the devices of
	// one protocol family ("BidCos-RF", "HmIP-RF", "BidCos-Wired",
	// "VirtualDevices"), classified by type prefix. Empty loads
	// everything into one instance, which is the single-endpoint
	// behaviour. See [hmconst.InterfaceForType].
	InterfaceFilter string
	// Logger sinks structured log output. Defaults to slog.Default().
	Logger *slog.Logger
	// OnSetValue is invoked after every successful SetValue /
	// PutParamset for each parameter that landed in the paramset
	// store. Tests use the hook to simulate CCU-side echo events on
	// ACTION parameters (e.g. an RF-thermostat AUTO_MODE-action
	// write triggers a CONTROL_MODE=AUTO-MODE sensor event) by
	// calling FireEvent from inside the hook.
	//
	// The callback runs synchronously on the writer's goroutine —
	// long-running work must be dispatched to a separate goroutine
	// inside the hook. Nil disables the hook.
	OnSetValue func(address, valueKey string, value any)
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
		logger:              logger,
		version:             version,
		interfaceID:         ifID,
		persistence:         opts.Persistence,
		interfaceFilter:     opts.InterfaceFilter,
		persistencePath:     persistencePath,
		deviceByAddress:     make(map[string]map[string]any),
		paramsetDescByAddr:  make(map[string]map[string]any),
		supportedDevices:    make(map[string]string),
		activeDevices:       make(map[string]struct{}),
		paramsets:           make(map[string]map[string]map[string]any),
		paramsetDefaults:    make(map[paramsetKey]map[string]any),
		paramsetCompiled:    make(map[paramsetKey]map[string]any),
		paramsetDirty:       make(map[paramsetKey]struct{}),
		linkParamsets:       make(map[linkKey]map[string]any),
		linkInfo:            make(map[linkKey]linkDetails),
		metadata:            make(map[string]map[string]any),
		remotes:             make(map[string]remoteCaller),
		configPendingTimers: make(map[string]*time.Timer),
		suppressed:          make(map[string]map[string]struct{}),
		dispatchers:         make(map[string]*dispatcher),
		onSetValue:          opts.OnSetValue,
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
		// With an interface filter set this instance only serves the
		// devices of one protocol family, the way a CCU's rfd,
		// HMIPServer and hs485d each serve their own.
		if r.interfaceFilter != "" && hmconst.InterfaceForType(s.deviceTypeKey) != r.interfaceFilter {
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
	remotes := make(map[string]remoteCaller, len(r.remotes))
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

// DeleteDevice removes all device entries whose root address matches addr
// (i.e. the root device itself and every channel belonging to it) and
// pushes a deleteDevices callback to all registered remotes.
//
// The flags parameter is accepted for wire compatibility but is otherwise
// ignored — godevccu does not model the RX-flag bits that the real CCU uses
// to decide whether to send an over-the-air deregistration. The call is
// idempotent: an unknown address returns success without pushing a callback.
func (r *RPCFunctions) DeleteDevice(ctx context.Context, address string, _ int) {
	addrUp := strings.ToUpper(address)

	r.mu.Lock()
	// Collect every entry (root + channels) belonging to this root address.
	// A channel belongs to the root when its own address starts with
	// "<rootAddr>:" (case-insensitive).
	prefix := addrUp + ":"
	addresses := make([]string, 0)
	filtered := r.devices[:0]
	for _, d := range r.devices {
		dAddr, _ := d[hmconst.AttrAddress].(string)
		dAddrUp := strings.ToUpper(dAddr)
		if dAddrUp == addrUp || strings.HasPrefix(dAddrUp, prefix) {
			if dAddr != "" {
				addresses = append(addresses, dAddr)
				r.clearAddressCachesLocked(dAddr)
			}
		} else {
			filtered = append(filtered, d)
		}
	}
	r.devices = filtered

	// Also remove from the activeDevices / supportedDevices index when the
	// root address matches a supported device type root.
	for typeName, rootAddr := range r.supportedDevices {
		if strings.ToUpper(rootAddr) == addrUp {
			delete(r.activeDevices, typeName)
			delete(r.supportedDevices, typeName)
			break
		}
	}

	if len(addresses) == 0 {
		// Unknown address — idempotent success, no callback.
		r.mu.Unlock()
		return
	}

	remotes := make(map[string]remoteCaller, len(r.remotes))
	for k, v := range r.remotes {
		remotes[k] = v
	}
	r.mu.Unlock()

	// Push deleteDevices to every registered callback receiver after the
	// state mutation is complete.
	for ifID, client := range remotes {
		params := []xmlrpc.Value{
			xmlrpc.StringValue(ifID),
			xmlrpc.FromAny(any(addresses)),
		}
		if _, err := client.Call(ctx, "deleteDevices", params); err != nil {
			r.logger.Debug("ccu: deleteDevices push failed (deleteDevice)", "interface", ifID, "err", err)
		}
	}
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

	remotes := make(map[string]remoteCaller, len(r.remotes))
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

// ReplaceDevice pushes a replaceDevice system event to every registered
// callback receiver, telling them the device at oldAddress has been swapped
// for newAddress. The real CCU emits this during a teach-in replacement; the
// wire shape is (interfaceID, oldDeviceAddress, newDeviceAddress).
func (r *RPCFunctions) ReplaceDevice(ctx context.Context, oldAddress, newAddress string) {
	r.mu.Lock()
	remotes := make(map[string]remoteCaller, len(r.remotes))
	for k, v := range r.remotes {
		remotes[k] = v
	}
	r.mu.Unlock()

	for ifID, client := range remotes {
		params := []xmlrpc.Value{
			xmlrpc.StringValue(ifID),
			xmlrpc.StringValue(oldAddress),
			xmlrpc.StringValue(newAddress),
		}
		if _, err := client.Call(ctx, "replaceDevice", params); err != nil {
			r.logger.Debug("ccu: replaceDevice push failed", "interface", ifID, "err", err)
		}
	}
}

// ReaddedDevice pushes a readdedDevice system event to every registered
// callback receiver. The real CCU emits this when devices re-pair via install
// mode; addresses are the entries the client should drop and re-fetch as part
// of the re-add. The wire shape is (interfaceID, addresses[]).
func (r *RPCFunctions) ReaddedDevice(ctx context.Context, addresses []string) {
	r.mu.Lock()
	remotes := make(map[string]remoteCaller, len(r.remotes))
	for k, v := range r.remotes {
		remotes[k] = v
	}
	r.mu.Unlock()

	for ifID, client := range remotes {
		params := []xmlrpc.Value{
			xmlrpc.StringValue(ifID),
			xmlrpc.FromAny(any(addresses)),
		}
		if _, err := client.Call(ctx, "readdedDevice", params); err != nil {
			r.logger.Debug("ccu: readdedDevice push failed", "interface", ifID, "err", err)
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

// Ping mirrors RPCFunctions.ping. A real CCU answers true and then
// delivers a CENTRAL/PONG event carrying the caller's id back to the
// registered client — that event is how a client matches its ping and
// keeps its connection state healthy. Without it aiohomematic reports a
// permanent PING_PONG_MISMATCH.
//
// The caller id has the shape "<interface_id>#<token>"; the event goes
// to the remote that owns the interface, or to all remotes when the id
// carries no routable prefix.
func (r *RPCFunctions) Ping(callerID string) bool {
	r.firePong(callerID)
	return true
}

func (r *RPCFunctions) firePong(callerID string) {
	if callerID == "" {
		return
	}
	target := callerID
	if idx := strings.Index(callerID, "#"); idx > 0 {
		target = callerID[:idx]
	}
	r.mu.Lock()
	_, addressed := r.remotes[target]
	r.mu.Unlock()

	if addressed {
		r.fireEventTo(target, target, hmconst.CentralAddress, hmconst.AttrPong, callerID)
		return
	}
	r.fireEvent(target, hmconst.CentralAddress, hmconst.AttrPong, callerID)
}

// GetVersion returns the configured version string.
func (r *RPCFunctions) GetVersion() string { return r.version }

// GetServiceMessages mimics the Python stub which always returns one
// example service message.
func (r *RPCFunctions) GetServiceMessages() [][]any {
	return [][]any{{"VCU0000001:1", hmconst.AttrError, 7}}
}

// ListBidcosInterfaces returns the BidCoS gateway inventory. A real CCU
// always reports at least its built-in radio module, and clients read
// DUTY_CYCLE and CONNECTED from that entry — an empty list left them
// with no gateway at all. The field names are the XML-RPC spelling the
// CCU's JSON layer maps to address/dutyCycle/isConnected/isDefault/
// fwVersion/type.
func (r *RPCFunctions) ListBidcosInterfaces() []map[string]any {
	return []map[string]any{{
		hmconst.AttrAddress:     r.interfaceID,
		hmconst.AttrDescription: "eQ-3 Default",
		"DUTY_CYCLE":            0,
		"CONNECTED":             true,
		"DEFAULT":               true,
		"FIRMWARE_VERSION":      hmconst.CCUFirmwareVersion,
		hmconst.AttrType:        "CCU2",
	}}
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

// GetParamset returns the current values of a paramset.
//
// When paramsetKey is "VALUES" or "MASTER" the standard per-channel store is
// consulted. When paramsetKey is a channel address (the LINK calling convention
// used by gohomematic's CcuBackend.GetLinkParamset), the LINK paramset for
// that (address, peer) pair is returned — an empty map when no link has been
// registered via AddLink.
func (r *RPCFunctions) GetParamset(address, paramsetKey string) (map[string]any, error) {
	// Detect the LINK peer-address form: anything that is not one of the
	// three known literal keys is treated as a peer channel address.
	if paramsetKey != hmconst.ParamsetAttrMaster && paramsetKey != hmconst.ParamsetAttrValues {
		if paramsetKey == hmconst.ParamsetAttrLink {
			// Caller passed the literal "LINK" key — not the peer-address form.
			// Return defaults from the description.
			return r.getLinkParamsetDefaults(address)
		}
		// Treat paramsetKey as a peer channel address (LINK pair form).
		return r.GetLinkParamset(address, paramsetKey)
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

// GetLinkParamset returns the LINK paramset values for the (sender, peer) pair.
// Returns an empty map when no link entry exists (matches real CCU behaviour
// for a channel with no links configured).
func (r *RPCFunctions) GetLinkParamset(senderAddress, peerAddress string) (map[string]any, error) {
	lk := linkKey{
		sender:   strings.ToUpper(senderAddress),
		receiver: strings.ToUpper(peerAddress),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if vals, ok := r.linkParamsets[lk]; ok {
		return cloneStringMap(vals), nil
	}
	return map[string]any{}, nil
}

// getLinkParamsetDefaults returns the default values built from the LINK
// paramset description for address. Holds the mutex on entry.
func (r *RPCFunctions) getLinkParamsetDefaults(address string) (map[string]any, error) {
	addrUp := strings.ToUpper(address)
	r.mu.Lock()
	defer r.mu.Unlock()
	desc, ok := r.paramsetDescByAddr[addrUp]
	if !ok {
		desc, ok = r.paramsetDescByAddr[address]
	}
	if !ok {
		return nil, fmt.Errorf("%w: paramset description for %q not found", ErrRPC, address)
	}
	ps, ok := desc[hmconst.ParamsetAttrLink].(map[string]any)
	if !ok {
		// No LINK description for this channel — return empty map.
		return map[string]any{}, nil
	}
	return buildDefaults(ps), nil
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
		// Surface the raw combined-parameter write to OnSetValue
		// before expansion so callers can observe the wire-shape
		// gohomematic actually emitted (a real CCU receives the
		// same string and decomposes it internally).
		r.mu.Lock()
		hook := r.onSetValue
		r.mu.Unlock()
		if hook != nil {
			hook(address, valueKey, value)
		}
		paramset := converter.ConvertCombinedParameterToParamset(valueKey, s)
		return r.PutParamset(address, hmconst.ParamsetAttrValues, paramset, force)
	}
	return r.PutParamset(address, hmconst.ParamsetAttrValues, map[string]any{valueKey: value}, force)
}

// SimulateDeviceEvent emulates the CCU RF/HmIP layer delivering an
// unsolicited device-originated value change to subscribers; bypasses
// the operator-write permission gate on purpose (read-only telemetry
// params like ACTUAL_TEMPERATURE are ops=RE and would otherwise reject
// the write). Use this to drive RECEIVE-direction test scenarios where
// a "device" reports a new sensor reading rather than an operator
// writing a controllable parameter.
func (r *RPCFunctions) SimulateDeviceEvent(address, valueKey string, value any) error {
	return r.PutParamset(address, hmconst.ParamsetAttrValues, map[string]any{valueKey: value}, true)
}

// PutParamset writes one or more values into the paramset and fires the
// computed follow-up events.
//
// When paramsetKey is a channel address (the LINK calling convention used by
// gohomematic's CcuBackend.PutLinkParamset), the values are stored in the LINK
// paramset for the (address, peerAddress) pair. The link must have been
// registered via AddLink first; values are accepted even when the entry does not
// yet exist so that callers can create a link implicitly.
func (r *RPCFunctions) PutParamset(address, paramsetKey string, paramset map[string]any, force bool) error {
	// Detect LINK peer-address form.
	if paramsetKey != hmconst.ParamsetAttrMaster && paramsetKey != hmconst.ParamsetAttrValues &&
		paramsetKey != hmconst.ParamsetAttrLink {
		return r.PutLinkParamset(address, paramsetKey, paramset)
	}
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
			// ACTION parameters are stateless triggers on the wire: the
			// CCU always fires the boolean `true` echo regardless of the
			// caller-supplied value, and the write is never persisted
			// into the paramset (mirrors pydevccu's fire-only ACTION
			// semantics). A caller must not read the value back via
			// GetValue/GetParamset — assert the write by capturing the
			// fired callback event instead.
			hook := r.onSetValue
			r.mu.Unlock()
			r.fireEvent(r.interfaceID, address, valueKey, true)
			// Invoke the user-supplied OnSetValue hook after the
			// echo event so the hook can script additional side-DP
			// callbacks for ACTION writes (e.g. AUTO_MODE →
			// CONTROL_MODE on RF thermostats). The hook is allowed
			// to call FireEvent / SetValue back into the rpc.
			if hook != nil {
				hook(address, valueKey, value)
			}
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
	hook := r.onSetValue
	r.mu.Unlock()

	for _, ev := range toFire {
		r.fireEvent(r.interfaceID, address, ev.key, ev.value)
	}
	// Hook fires once per outer PutParamset call, after the
	// auto-computed deviceresponses but BEFORE the caller sees the
	// return — this lets a hook stage its side-DPs and have them
	// land in the same logical batch from the caller's perspective.
	if hook != nil {
		for valueKey, value := range paramset {
			hook(address, valueKey, value)
		}
	}
	if paramsetKey == hmconst.ParamsetAttrMaster && len(paramset) > 0 {
		// A configuration write does not take effect immediately: the
		// device has to pick it up, and the CCU reports that window as
		// CONFIG_PENDING on channel 0.
		r.notifyConfigPending(address)
	}
	return nil
}

// FireEvent is the public wrapper for fireEvent (used by the device
// logic simulators).
func (r *RPCFunctions) FireEvent(interfaceID, address, valueKey string, value any) {
	r.fireEvent(interfaceID, address, valueKey, value)
}

func (r *RPCFunctions) fireEvent(interfaceID, address, valueKey string, value any) {
	r.dispatchEvent("", interfaceID, address, valueKey, value)
}

// fireEventTo delivers an event to a single registered remote, the way
// a CCU answers a ping only towards the interface that sent it.
func (r *RPCFunctions) fireEventTo(target, interfaceID, address, valueKey string, value any) {
	r.dispatchEvent(target, interfaceID, address, valueKey, value)
}

// dispatchEvent notifies the in-process callbacks and every registered
// remote; when target is non-empty only that remote is called.
func (r *RPCFunctions) dispatchEvent(target, interfaceID, address, valueKey string, value any) {
	addrUp := strings.ToUpper(address)
	r.mu.Lock()
	cbs := append([]EventCallback(nil), r.paramsetCallbacks...)
	remotes := make(map[string]remoteCaller, len(r.remotes))
	for k, v := range r.remotes {
		if target != "" && k != target {
			continue
		}
		remotes[k] = v
	}
	batched := r.batchEvents
	r.mu.Unlock()

	for _, cb := range cbs {
		safeCallEvent(cb, interfaceID, addrUp, valueKey, value)
	}
	if batched {
		for ifID, client := range remotes {
			r.mu.Lock()
			d := r.dispatcherFor(ifID, client)
			r.mu.Unlock()
			d.enqueue(pendingEvent{
				interfaceID: ifID,
				address:     addrUp,
				valueKey:    valueKey,
				value:       value,
			})
		}
		return
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
			// Only a transport problem means the client is gone. A
			// fault is the client answering — it stays registered, the
			// way a real CCU keeps delivering after an application
			// error. Dropping the remote on the first fault made the
			// simulator stricter than both the CCU and pydevccu.
			if xmlrpc.IsTransport(err) {
				r.mu.Lock()
				delete(r.remotes, ifID)
				r.mu.Unlock()
			}
		}
	}
}

// Init registers a callback URL or removes the matching remote when
// interfaceID is empty.
func (r *RPCFunctions) Init(url, interfaceID string) string {
	if interfaceID != "" {
		client := newRemote(url)
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

// PutLinkParamset stores LINK paramset values for the (sender, peer) pair.
// It is idempotent — writing to a non-existent link entry creates it, and
// writing to an existing entry merges the provided values.
func (r *RPCFunctions) PutLinkParamset(senderAddress, peerAddress string, paramset map[string]any) error {
	lk := linkKey{
		sender:   strings.ToUpper(senderAddress),
		receiver: strings.ToUpper(peerAddress),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.linkParamsets[lk]
	if !ok {
		existing = make(map[string]any, len(paramset))
		r.linkParamsets[lk] = existing
	}
	for k, v := range paramset {
		existing[k] = v
	}
	return nil
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
	// A previously written value wins over the description default,
	// the way a CCU serves what was last stored for the object.
	if stored, ok := r.metadata[strings.ToUpper(objectID)]; ok {
		if v, ok := stored[dataID]; ok {
			return v, nil
		}
	}
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

// SetMetadata stores a metadata value for an object. Writes used to be
// discarded, so a client could never read back what it had written —
// the CCU WebUI builds on exactly that round trip (operateGroupOnly,
// channelMode).
func (r *RPCFunctions) SetMetadata(objectID, dataID string, value any) bool {
	if objectID == "" || dataID == "" {
		return false
	}
	key := strings.ToUpper(objectID)
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.metadata[key]
	if !ok {
		stored = make(map[string]any)
		r.metadata[key] = stored
	}
	stored[dataID] = value
	return true
}

// GetAllMetadata returns every stored metadata value for an object.
func (r *RPCFunctions) GetAllMetadata(objectID string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]any)
	for k, v := range r.metadata[strings.ToUpper(objectID)] {
		out[k] = v
	}
	return out
}

// DetermineParameter asks the CCU to re-read a parameter from the
// device. A real CCU answers true and delivers the freshly determined
// value as an event; the simulator replays the cached value so a client
// observes the same round trip. paramsetKey is optional — clients call
// this with either two or three arguments.
// The paramset key is accepted but not evaluated: only VALUES
// parameters carry a device-side value that could be re-read.
func (r *RPCFunctions) DetermineParameter(address, paramsetKey, parameterID string) (bool, error) {
	if parameterID == "" {
		// Two-argument form: the second argument is the parameter.
		parameterID = paramsetKey
	}
	if parameterID == "" {
		return false, fmt.Errorf("%w: parameter required", ErrRPC)
	}
	value, err := r.GetValue(address, parameterID)
	if err != nil {
		return false, err
	}
	r.fireEvent(r.interfaceID, address, parameterID, value)
	return true, nil
}

// GetParamsetID returns the identifier of a channel's paramset. Clients
// use it to decide whether a cached paramset description is still
// valid, so it only has to be stable and unique per (type, paramset) —
// the CCU's own encoding is not observable through the API.
func (r *RPCFunctions) GetParamsetID(address, paramsetType string) (string, error) {
	addrUp := strings.ToUpper(address)
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.deviceByAddress[addrUp]
	if !ok {
		return "", fmt.Errorf("%w: device %q not found", ErrRPC, address)
	}
	typeStr, _ := d[hmconst.AttrType].(string)
	if typeStr == "" {
		typeStr, _ = d[hmconst.AttrParentType].(string)
	}
	return typeStr + ":" + strings.ToUpper(paramsetType), nil
}

// ActivateLinkParamset activates a link paramset for the given peer.
// The simulator holds no radio state, so it validates the pair and
// reports success the way a CCU does once the command is queued.
func (r *RPCFunctions) ActivateLinkParamset(address, peerAddress string, _ bool) (bool, error) {
	lk := linkKey{sender: strings.ToUpper(address), receiver: strings.ToUpper(peerAddress)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.linkParamsets[lk]; !ok {
		// Links are directional in storage but activatable from either
		// end.
		if _, ok := r.linkParamsets[linkKey{sender: lk.receiver, receiver: lk.sender}]; !ok {
			return false, fmt.Errorf("%w: no link between %q and %q", ErrRPC, address, peerAddress)
		}
	}
	return true, nil
}

// GetLinkInfo returns the name and description stored for a direct link.
func (r *RPCFunctions) GetLinkInfo(senderAddress, receiverAddress string) (map[string]any, error) {
	lk := linkKey{sender: strings.ToUpper(senderAddress), receiver: strings.ToUpper(receiverAddress)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.linkParamsets[lk]; !ok {
		return nil, fmt.Errorf("%w: no link between %q and %q", ErrRPC, senderAddress, receiverAddress)
	}
	info := r.linkInfo[lk]
	return map[string]any{
		hmconst.AttrName:        info.name,
		hmconst.AttrDescription: info.description,
	}, nil
}

// SetLinkInfo stores the name and description of a direct link.
func (r *RPCFunctions) SetLinkInfo(senderAddress, receiverAddress, name, description string) (bool, error) {
	lk := linkKey{sender: strings.ToUpper(senderAddress), receiver: strings.ToUpper(receiverAddress)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.linkParamsets[lk]; !ok {
		return false, fmt.Errorf("%w: no link between %q and %q", ErrRPC, senderAddress, receiverAddress)
	}
	r.linkInfo[lk] = linkDetails{name: name, description: description}
	return true, nil
}

// RssiInfo reports the receive field strengths between the central and
// every known device: device address → partner → [rssi_device,
// rssi_partner]. The simulator has no radio, so it reports a constant
// healthy value per known root device — enough for a client to exercise
// the nested struct-of-struct parsing this method is known for.
func (r *RPCFunctions) RssiInfo() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]any, len(r.devices))
	for _, d := range r.devices {
		addr, _ := d[hmconst.AttrAddress].(string)
		if addr == "" || strings.Contains(addr, ":") {
			continue // channels carry no RSSI of their own
		}
		out[addr] = map[string]any{
			hmconst.CentralAddress: []any{simulatedRssi, simulatedRssi},
		}
	}
	return out
}

// simulatedRssi is the constant field strength the simulator reports;
// a plausible "good reception" value on a real installation.
const simulatedRssi = -65

// AddLink records a link between sender and receiver and allocates a default
// LINK paramset for the pair (built from the sender channel's LINK description,
// if available). Calling AddLink for an already-registered pair is idempotent
// and does not overwrite existing paramset values.
func (r *RPCFunctions) AddLink(senderAddress, receiverAddress, _, _ string) bool {
	lk := linkKey{
		sender:   strings.ToUpper(senderAddress),
		receiver: strings.ToUpper(receiverAddress),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, already := r.linkParamsets[lk]; already {
		// Idempotent: already present, nothing to do.
		return true
	}
	// Build default LINK paramset from the sender channel's LINK description.
	defaults := make(map[string]any)
	if desc, ok := r.paramsetDescByAddr[lk.sender]; ok {
		if linkDesc, ok := desc[hmconst.ParamsetAttrLink].(map[string]any); ok {
			defaults = buildDefaults(linkDesc)
		}
	}
	r.linkParamsets[lk] = defaults
	return true
}

// RemoveLink drops the link paramset for the (sender, receiver) pair.
// Calling RemoveLink for an unknown pair is a no-error no-op (idempotent).
func (r *RPCFunctions) RemoveLink(senderAddress, receiverAddress string) bool {
	lk := linkKey{
		sender:   strings.ToUpper(senderAddress),
		receiver: strings.ToUpper(receiverAddress),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.linkParamsets, lk)
	return true
}

// GetLinkPeers returns the list of peer addresses for all links whose sender
// matches the given channel address.
func (r *RPCFunctions) GetLinkPeers(channelAddress string) []string {
	addrUp := strings.ToUpper(channelAddress)
	r.mu.Lock()
	defer r.mu.Unlock()
	peers := make([]string, 0)
	for lk := range r.linkParamsets {
		if lk.sender == addrUp {
			peers = append(peers, lk.receiver)
		}
	}
	return peers
}

// GetLinks returns a list of link descriptor maps for the given channel address.
// When channelAddress is empty all links are returned. Flags are accepted but
// currently ignored (mirrors the real CCU behaviour for the simulator context).
func (r *RPCFunctions) GetLinks(channelAddress string, _ int) []any {
	addrUp := strings.ToUpper(channelAddress)
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]any, 0)
	for lk, vals := range r.linkParamsets {
		if addrUp != "" && lk.sender != addrUp {
			continue
		}
		desc := map[string]any{
			"SENDER":      lk.sender,
			"RECEIVER":    lk.receiver,
			"NAME":        "",
			"DESCRIPTION": "",
		}
		for k, v := range vals {
			desc[k] = v
		}
		out = append(out, desc)
	}
	return out
}
func (r *RPCFunctions) GetInstallMode() int { return 0 }
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
