// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package virtualccu

// Realism groups the behaviours where a real CCU differs from pydevccu.
// pydevccu parity is a contract (see CLAUDE.md), so every one of these
// is opt-in and the zero value reproduces the established behaviour bit
// for bit — the same rule [Config.BINRPCPort] follows.
//
// Switch on what a test needs, or take the full set with [RealismCCU].
type Realism struct {
	// JSONSchema makes the JSON-RPC payloads match the CCU's own field
	// names and types: SysVar.getAll reports LOGIC/NUMBER/LIST with
	// stringified values and the conditional fields, Room.listAll
	// returns plain id strings, Program.getAll formats lastExecuteTime
	// as a date, and Device.listAllDetail carries the full channel
	// record including "paramsets".
	JSONSchema bool

	// RegaIDs assigns numeric ReGa object ids to devices and channels
	// and reports them where a CCU reports ids — Device.listAllDetail's
	// "id", and the channel ids of rooms and functions, which are empty
	// without it, so every room/function assignment a client reads
	// points at nothing.
	RegaIDs bool

	// ErrorModel switches the JSON-RPC envelope and error objects to
	// the CCU's 1.1 form ({name: "JSONRPCError", code: 400/401/…})
	// and enforces the per-method privilege levels. Clients map those
	// codes onto their own exception types; the JSON-RPC 2.0 codes the
	// simulator sends by default never trigger the auth-failure paths.
	ErrorModel bool

	// ServiceMessages derives service messages from the maintenance
	// channel (UNREACH, STICKY_UNREACH, LOWBAT, CONFIG_PENDING,
	// ERROR≠0) instead of reporting the hard-coded pydevccu entry, and
	// enables the suppression store behind
	// getSuppressedServiceMessages/suppressServiceMessages.
	//
	// The XML-RPC getServiceMessages default stays hard-coded even
	// here — CLAUDE.md pins that shape and integration tests in both
	// ecosystems assert it.
	ServiceMessages bool

	// Reachability enables the device state machines a CCU runs:
	// UNREACH/STICKY_UNREACH via [VirtualCCU.SetDeviceUnreachable],
	// and a CONFIG_PENDING pulse after a MASTER paramset write.
	Reachability bool

	// BatchEvents delivers events asynchronously from a per-remote
	// dispatcher, bundled into one system.multicall, the way a CCU
	// does. Without it every event is a separate synchronous call on
	// the caller's goroutine, so a slow callback receiver blocks
	// setValue.
	BatchEvents bool

	// PersistInit writes the init() callback registrations alongside
	// the paramsets, so a restarted simulator reconnects to the clients
	// that were registered before — a CCU remembers them across a
	// reboot. Requires [Config.Persistence].
	PersistInit bool

	// Discovery answers SSDP M-SEARCH on UDP 1900 and serves
	// /upnp/basic_dev.cgi, which is how clients find a CCU on the
	// network without being told its address.
	Discovery bool

	// BasicAuth enforces HTTP basic authentication on the XML-RPC
	// surface (realm "theRealm") for non-loopback callers, as a CCU
	// with authentication enabled does — its reverse proxy applies the
	// auth block to the remote API ports and switches it back off for
	// the web API, which authenticates by session instead. Loopback is
	// exempt, so an add-on on the central itself needs no credentials.
	//
	// Requires [Config.AuthEnabled]; without it the field is inert,
	// matching a CCU that only includes its auth configuration when
	// authentication is switched on.
	BasicAuth bool

	// BackupAPI serves /api/backup/{login,version,run-script,tarfile}
	// and completes the backup lifecycle (start → running → completed →
	// download) rather than leaving every backup "running" forever.
	BackupAPI bool

	// Lifecycle runs the pairing and firmware automata: setInstallMode
	// starts a countdown getInstallMode reports, and a firmware update
	// walks FIRMWARE_UPDATE_STATE through the states a CCU reports.
	// Without it both are the constants they have always been.
	Lifecycle bool

	// Ramps stretches an actuator move over its travel time: a LEVEL
	// write is answered with a moving ACTIVITY_STATE in the reported
	// direction, and the idle state follows. The value itself still
	// lands immediately, so a test never waits to read it back.
	Ramps bool

	// FaultCodes reports the HomeMatic fault catalogue (-2 unknown
	// device, -3 unknown paramset, -5 unknown parameter or invalid
	// value; see internal/ccu/faults.go) instead of answering every
	// failure with -1. Clients decide from the code whether a call is
	// worth retrying, and -1 lands in the retryable bucket.
	FaultCodes bool

	// NormalizeData completes the embedded device descriptions while
	// loading: the missing parameter IDs, the UNIT: null that
	// serialises as <nil/>, mistyped BOOL defaults and the firmware
	// fields. The fixtures stay untouched — they are import targets.
	NormalizeData bool

	// InitSemantics handles init() the way the interface processes do,
	// as hm-simulator documents them: a registration is keyed by its
	// url exactly (pydevccu keys by interface id and removes by a
	// substring match, so init("http://h:1", "") also removes
	// "http://h:10"), deleteDevices goes out before newDevices, a
	// device whose VERSION differs is sent again, and HmIP devices are
	// sent again on every init. See internal/ccu/initsemantics.go.
	InitSemantics bool

	// MasterModel answers MASTER writes with the models hm-simulator
	// documents as measured on rfd and hmipserver: hmipserver stores
	// everything, poisons a channel with an unknown parameter and
	// raises a sticky CONFIG_PENDING on a wrong type; rfd never faults,
	// drops, clamps and coerces, and raises CONFIG_PENDING while the
	// change is queued. Chosen per device by its protocol family. See
	// internal/ccu/mastermodel.go.
	MasterModel bool

	// InterfaceQuirks reproduces the answers hm-simulator documents for
	// the individual interface processes: no PONG from hmipserver,
	// getServiceMessages derived from the maintenance channels with
	// rfd's empty string for "nothing pending", SENDER_BROKEN on rfd's
	// unfiltered getLinks, and the metadata faults of rfd. Only
	// meaningful with [Config.InterfacePorts]; see internal/ccu/quirks.go.
	InterfaceQuirks bool

	// ServiceMessagesFault makes getServiceMessages on HmIP-RF and
	// VirtualDevices answer the unknown-method fault
	// ("Invalid XML-RPC message"), as hm-simulator records those
	// processes were seen to on firmware 3.89.x. Not part of
	// [RealismCCU]: the observation is not consistent across versions.
	ServiceMessagesFault bool
}

// RealismCCU returns a [Realism] with every behaviour enabled — the
// closest the simulator gets to a real CCU. Use it when the client
// under test talks to real hardware in production; pick individual
// fields when a test needs one behaviour without the others.
func RealismCCU() Realism {
	return Realism{
		JSONSchema:      true,
		RegaIDs:         true,
		ErrorModel:      true,
		ServiceMessages: true,
		Reachability:    true,
		BatchEvents:     true,
		PersistInit:     true,
		Discovery:       true,
		BasicAuth:       true,
		BackupAPI:       true,
		Lifecycle:       true,
		Ramps:           true,
		FaultCodes:      true,
		NormalizeData:   true,
		InitSemantics:   true,
		MasterModel:     true,
		InterfaceQuirks: true,
	}
}

// Any reports whether at least one behaviour is enabled.
func (r Realism) Any() bool {
	return r != Realism{}
}
