# Changelog

All notable changes to `godevccu` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the project follows [Semantic Versioning](https://semver.org/).

The public API surface is `pkg/godevccu`. Anything under `internal/`
is excluded from the stability promise.

## [Unreleased]

### Added — CCU realism (opt-in)

Every behaviour below sits behind `Config.Realism` or an explicit
configuration field. The zero value reproduces the established
pydevccu-shaped behaviour bit for bit, so an existing run is unaffected;
`godevccu.RealismCCU()` switches the whole set on.

- **Separate interface listeners** (`Config.InterfacePorts`). A CCU is
  not one process serving every device: rfd serves BidCos-RF on 2001,
  the HMIPServer serves HomeMatic IP on 2010, hs485d the wired bus on
  2000 and the group interface answers on 9292. Each configured entry
  gets its own listener, its own callback registry and only the devices
  of that protocol family, classified by type prefix. Reach them with
  `InterfaceAddr(name)` / `InterfaceRPC(name)`;
  `Interface.listInterfaces` then reports each on its real port, in the
  CCU's own field set (`name`/`port`/`info`).

- **TLS** (`Config.TLS`). The HTTPS twins of the API ports —
  2001/42001, 2010/42010, 80/443 — with a self-signed certificate
  generated at startup when none is supplied. `TLS.Redirect` makes the
  plaintext web API answer 302 and `CCU.getHttpsRedirectEnabled` report
  true instead of a hard-coded false.

- **The CCU's error model** (`Realism.ErrorModel`). The 1.1 envelope
  (`version`, not `jsonrpc`), the error object
  `{name: "JSONRPCError", code, message}` with the firmware's codes
  (100/102/103/400/401/402/500), and per-method privilege levels taken
  from the CCU's own `methods.conf`. Without it a client's
  authentication-failure path never triggers, because JSON-RPC 2.0
  codes mean nothing to it.

- **ReGa object ids** (`Realism.RegaIDs`). Devices and channels get the
  numeric ids a CCU assigns, reported as `Device.listAllDetail.id` and
  as the `channelIds` of rooms and functions. Without them a client
  stores a textual address where it expects an ise_id, so every room and
  function assignment it reads points at nothing.

- **CCU-shaped JSON payloads** (`Realism.JSONSchema`). `SysVar.getAll`
  reports LOGIC/NUMBER/LIST/ALARM with stringified values and only the
  type-dependent fields that apply; `Room.listAll` returns plain id
  strings; `Program.getAll` formats `lastExecuteTime` as a timestamp;
  `Device.listAllDetail` carries `paramsets`, the firmware fields and
  the full channel record.

- **Device state machines** (`Realism.Reachability`).
  `SetDeviceUnreachable` sets UNREACH and latches STICKY_UNREACH until a
  client acknowledges it, and a MASTER paramset write raises a
  CONFIG_PENDING pulse with proper edge detection — a second write while
  one is in flight extends it instead of emitting another rising edge.

- **Service messages derived from the maintenance channel**
  (`Realism.ServiceMessages`), plus the suppression store behind
  `getSuppressedServiceMessages` / `suppressServiceMessages` (an empty
  parameter id silences the whole channel). The XML-RPC
  `getServiceMessages` default stays hard-coded even here — CLAUDE.md
  pins that shape.

- **Batched, asynchronous event delivery** (`Realism.BatchEvents`).
  Events queue into a per-remote dispatcher and travel as one
  `system.multicall`, so a callback receiver that is slow to answer no
  longer stalls the `setValue` that produced the event.

- **Callback registrations survive a restart** (`Realism.PersistInit`,
  requires `Persistence`). A rebooting CCU re-establishes them and
  resumes pushing; the simulator forgot every client.

- **HTTP basic authentication** (`Realism.BasicAuth`, requires
  `AuthEnabled`). A CCU protects its remote API ports with basic auth in
  realm `theRealm` and exempts loopback callers, while the web API stays
  session-authenticated — the simulator left XML-RPC open regardless of
  `AuthEnabled`.

- **SSDP/UPnP discovery** (`Realism.Discovery`): M-SEARCH answers and
  periodic `ssdp:alive` on UDP 1900 from the new `internal/ssdp`
  package, plus `/upnp/basic_dev.cgi`. This is how a client finds a
  central without being told its address.

- **Backup lifecycle** (`Realism.BackupAPI`). A started backup now
  reaches "completed" with a `<host>-<version>-<date>.sbk` name and a
  downloadable artefact, instead of staying "running" forever behind a
  permanent 404. Adds `/api/backup/{login,version,run-script,tarfile}.cgi`.

- **`Interface.getSuppressedServiceMessages` and
  `Interface.suppressServiceMessages`** on both transports, and the
  built-in system variables a factory CCU ships
  (`state.SetupBuiltinSysvars`: `${sysVarAlarmZone1}`,
  `${sysVarPresence}` and the ids 40/41 clients special-case).

### Fixed

- **ReGa scripts are dispatched by their `!# name:` header.** The
  engine matched script *content* in registration order, so the generic
  patterns shadowed the specific scripts. Six scripts reached the wrong
  handler: `set_program_state.fn` and `set_system_variable.fn` were
  answered with listings and silently changed nothing while reporting
  success, `accept_device_in_inbox.fn` and `acknowledge_message.fn`
  returned the inbox/service-message *listing* instead of
  `{"success":…}`, `create_backup_status.fn` was answered with backend
  info, and `get_program_descriptions.fn` came back as `Program.getAll`.
  Content patterns remain as a fallback for clients that send scripts
  without the header.

- **`fetch_all_device_data.fn` returns a JSON object**, keyed
  `"<interface>.<channel_address>.<parameter>"` as the script emits it,
  instead of an array of `{address,param,value}` records. Clients
  iterate the result as a mapping, so the bulk fetch was unusable.

- **ReGa response keys match the real scripts**: `is_ha_app` (was
  `is_ha_addon`), `id`/`type` in the inbox listing (were
  `deviceId`/`deviceType`), snake_case firmware keys
  (`current_firmware`, `available_firmware`, `update_available`,
  `check_script_available`), `file` instead of `filepath` in the backup
  status — which now reports only `status` unless the backup completed —
  and `script_available`/`message` on the update trigger.

- **Names and descriptions are percent-encoded**, so a space arrives as
  `%20`. `+` survived into client-side decoded names because clients
  decode with `unquote()`, not `unquote_plus()`.

- **A callback client is no longer deregistered when it answers with a
  fault.** Only a transport error means the client is gone; the previous
  behaviour silently ended event delivery for the rest of the session
  and was stricter than both the CCU and pydevccu.

- **`setMetadata` stores its value.** Writes were discarded, so a
  `setMetadata` → `getMetadata` round trip was impossible. Keys that
  were never written still fall back to the device description.

### Added

- **`ping` fires a `CENTRAL`/`PONG` event** carrying the caller id back
  to the client that pinged. Without it a client's connection
  monitoring reports a permanent ping/pong mismatch.

- **XML-RPC methods that real clients call**: `determineParameter`
  (accepts the two- and three-argument form), `getParamsetId`,
  `activateLinkParamset`, `getLinkInfo`/`setLinkInfo`, `getAllMetadata`
  and `rssiInfo`.

- **JSON-RPC methods**: `SysVar.createBool`, `SysVar.createFloat`,
  `SysVar.createEnum`, `SysVar.setEnum`, `SysVar.get` (CCU type
  nomenclature LOGIC/NUMBER/LIST with the conditional fields),
  `CCU.getSerial`, `CCU.getVersion`, `Interface.rssiInfo`,
  `Interface.listBidcosInterfaces`, `Interface.getLinkInfo`,
  `Interface.setLinkInfo`, `Interface.determineParameter`,
  `Interface.getParamsetId`, `system.methodHelp` and `system.describe`.
  The system-variable lifecycle (create → set → read → delete) is
  testable against the simulator for the first time.

- **`listBidcosInterfaces` reports the built-in radio module** instead
  of an empty list. Clients read `DUTY_CYCLE` and `CONNECTED` off that
  entry; a real CCU always lists at least one gateway.

- **`system.listMethods` (JSON-RPC) reports `name`, `level` and `info`**
  sorted by name, with the privilege levels and descriptions the CCU
  publishes in its own `methods.conf`.

### Changed

- **`Config.StartNotReady` also gates the XML-RPC surface** with
  `503 CCU not ready yet`. A booting CCU refuses every remote API port,
  not only the web API.

- The ReGa engine is covered by the *unmodified* client scripts in
  `internal/rega/testdata/scripts/`. The previous tests used
  hand-written fragments, which is why every one of the routing defects
  above passed unnoticed.

## [0.1.10] — 2026-08-05

### Added

- **BIN-RPC transport (`xmlrpc_bin://`), the protocol CUxD speaks
  exclusively.** Opt in with `Config.BINRPCPort` (`EphemeralPort` is
  supported and the resolved port is written back, as for the other
  listeners); left at 0 nothing changes. The listener serves the same
  method set as the XML-RPC surface, so `listDevices`, `getValue`,
  `setValue`, `ping` and `init` all answer over it.

  This is a deliberate extension beyond pydevccu parity: pydevccu has
  neither BIN-RPC nor CUxD. It exists because the CUxD callback direction
  was otherwise untestable without real hardware, and that gap is not
  theoretical — it hid a defect in a downstream consumer for the
  consumer's entire lifetime.

  The behaviour that matters is the callback direction: **every callback
  to a `xmlrpc_bin://` receiver is wrapped in a `system.multicall`
  envelope**, as real CUxD does, even for a single event. A consumer that
  reads the interface id out of `params[0]` finds a string for a bare call
  and the sub-call array for an envelope — so a simulator that pushed bare
  calls would let a consumer that cannot parse the envelope pass every
  test while dropping every real event.

  `VirtualCCU.BINRPCAddr()` reports the bound address.

## [0.1.9] — 2026-07-27

### Fixed

- **Dedicated ReGa handler for `get_alarm_messages.fn`.** aiohomematic's
  alarm-message script walks `ID_SYSTEM_VARIABLES` and calls `.DPInfo()`,
  so it was misrouted to the sysvar-description handlers, whose entries
  lack the `name` key the alarm parser requires — a `KeyError` that failed
  the consumer's entire entry setup. The script is now matched by its
  `name:` header and answered with the empty active-alarm list (godevccu's
  state does not model `ALARMDP` variables), matching a real CCU without
  pending alarms.

## [0.1.8] — 2026-07-12

### Added

- **System-variable channel assignment.** `SystemVariable` (and
  `AddSystemVariableOpts`) carry a `ChannelAddress` — the channel a
  variable is explicitly assigned to in the CCU WebUI
  ("Kanalzuordnung").
- **Dedicated ReGa handler for the sysvar-description script family.**
  Scripts that walk `ID_SYSTEM_VARIABLES` and call `.DPInfo()` per
  variable are now answered in the real script's wire shape —
  string-framed ids, URL-encoded `description`, plus the new
  `channel_address` field (empty when unassigned). Previously these
  scripts fell through to the generic sysvar handler, whose integer ids
  failed consumers that decode the description shape.


## [0.1.7] — 2026-07-09

### Added

- **`VirtualCCU.SimulateDeviceEvent(address, valueKey, value)`** — emulate the
  CCU RF/HmIP layer delivering an unsolicited device-originated value change to
  subscribers. A thin `PutParamset(..., force=true)` wrapper that runs the
  device's `ComputeEvents` follow-ups and fires them to registered callbacks, so
  a consumer's test can drive the receive direction (CCU → controller) for a
  read-only (`ops=RE`) telemetry parameter without tripping the operator-write
  permission gate.

### Fixed

- **Case-insensitive fleet loading.** The device loader dropped fixtures whose
  restrict-list spelling differed in case from the embedded filename (e.g.
  `HmIP-PS` vs `HMIP-PS.json`), silently returning fewer devices than requested;
  both sides are now upper-cased before matching (`internal/ccu/loader.go`).
- **Case-insensitive device-response mapping.** `deviceresponses.Mapping` /
  `startsWith` now compare case-insensitively, so `stateWithWorking` /
  `windowState` follow-up events apply to the all-caps `HMIP-*` device types.

### Changed

- Explicit `ComputeEvents` telemetry entries for the read-only params a consumer
  send/receive matrix exercises (BWTH temperature/humidity, BSM
  power/energy/voltage/current/frequency, smoke-alarm status, motion/illuminance,
  contact state, CO₂ concentration, low-battery), so receive coverage no longer
  relies only on the generic echo fallback. `blindLevel` now synthesizes
  `ACTIVITY_STATE` alongside `LEVEL`, symmetric with `levelWithActivity`.

## [0.1.6] — 2026-06-23

### Changed

- **Refreshed `HmIP-FWI` device and paramset descriptions** from upstream
  `pydevccu` (`internal/embed/data`). The fixture was re-imported via
  `script/copy_data.sh`, which re-addressed the device (`VCU1851882` →
  `VCU4820995`) and normalized its paramset descriptions to the current
  upstream shape. No behavioural change to the simulator — only the embedded
  catalogue data was updated.

## [0.1.5] — 2026-06-18

### Added

- **Simulated CCU boot window (readiness gate)** (`internal/virtualccu`,
  `internal/jsonrpc`). A new `Config.StartNotReady` boots the virtual CCU in
  its "still warming up" state, and `VirtualCCU.SetReady(bool)` / `Ready()`
  flip it at runtime. While not ready the JSON-RPC web API
  (`/api/homematic.cgi`, e.g. `Device.listAllDetail`) answers http 503
  ("internal backend exception") and the new `/ise/checkrega.cgi` probe
  returns a body other than the literal `OK`; once ready, checkrega returns
  `OK` and the API serves normally. This models an add-on co-started with a
  (re)booting CCU — XML-RPC `listDevices` (devices) can succeed while
  JSON-RPC (names) still 503s — so a readiness-gated client can be exercised
  end-to-end against the boot race. Defaults are unchanged: every existing
  fixture boots immediately ready.

## [0.1.4] — 2026-06-13

### Fixed

- **Rooms and functions serialize `channelIds` as an empty array (`[]`)
  instead of `null`** (`internal/state`). Rooms/functions without any
  assigned channels previously emitted `null` over both JSON-RPC
  (`Room.getAll`, `Subsection.getAll`) and the ReGa engine, because the
  accessor copies used `append([]string(nil), ...)`, which yields `nil`
  for an empty input. The real CCU contract returns `[]`; clients that
  iterate `channelIds` directly crashed during room/function
  enumeration. A new `cloneChannelIDs` helper guarantees a non-nil copy
  at every room/function accessor and creation site. Surfaced by a
  three-way godevccu parity e2e harness in the reference Home Assistant
  integration.

## [0.1.3] — 2026-06-05

### Added

- **`listBidcosInterfaces` XML-RPC method** (`internal/ccu`): returns
  an empty interface inventory. The simulator models no physical
  BidCoS radio gateways, but exposing the method lets clients probe it
  during interface detection without provoking a `methodNotFound`
  fault.
- **`replaceDevice` / `readdedDevice` callback push helpers**
  (`RPCFunctions.ReplaceDevice`, `RPCFunctions.ReaddedDevice`): push
  the corresponding system events to every registered callback
  receiver, mirroring the real CCU's wire shape — `replaceDevice`
  carries `(interfaceID, oldDeviceAddress, newDeviceAddress)`,
  `readdedDevice` carries `(interfaceID, addresses[])`. These let
  consumers exercise device-replacement and re-pair callback paths
  end to end.

## [0.1.2] — 2026-05-21

### Added

- **`OnSetValue` hook** (`pkg/godevccu.Config.OnSetValue`, forwarded
  through `internal/virtualccu` to `internal/ccu`): a callback invoked
  synchronously after every successful `SetValue` / `PutParamset`
  paramset write. The hook receives the channel address, the value
  key and the written value, and runs on the writer's goroutine —
  long-running work must be dispatched separately. Combined-parameter
  writes (e.g. `COMBINED_PARAMETER`, `LEVEL_COMBINED`) surface their
  raw wire-shape *before* expansion so callers observe what
  gohomematic actually emitted; the per-parameter callbacks for the
  decomposed paramset still fire afterwards. Re-entering through
  `FireEvent` / `SetValue` from inside the hook is allowed, which
  lets tests script CCU-side echo events for ACTION DPs (such as the
  `AUTO_MODE` → `CONTROL_MODE` pair on RF thermostats).
- **Nested-array XML-RPC encoding** (`internal/xmlrpc/convert.go`):
  `FromAny` now recognises `[][]any` and emits properly nested
  `<array>` elements instead of falling back to a `fmt.Sprintf`
  string. This fixes the wire shape of responses like
  `getServiceMessages`, which return a list of `[address, key,
  value]` triples.

### Changed

- **ReGa `get_serial.fn` handler** (`internal/rega/engine.go`): the
  pattern matcher now recognises `get_serial` references with or
  without the `.fn` suffix (`\bget_serial(\.fn)?\b`), matching
  aiohomematic / gohomematic script variants that strip the
  extension. The handler also returns the serial wrapped in a
  `{"serial": …}` object rather than the bare string, matching the
  shape pydevccu emits.

### Tests

- Extensive new unit and integration coverage across `internal/ccu`,
  `internal/converter`, `internal/devicelogic`,
  `internal/deviceresponses`, `internal/hmconst`, `internal/jsonrpc`,
  `internal/rega`, `internal/session`, `internal/state`,
  `internal/virtualccu`, `internal/xmlrpc` and `pkg/godevccu`,
  exercising RPC/server, paramset conversion, persistence and
  callback paths.

## [0.1.1] — 2026-05-03

### Added

- **`deleteDevice` XML-RPC server-side handler** (`internal/ccu`): the XML-RPC
  mux now exposes `deleteDevice(address, flags)` as a server-received method.
  When called, the simulator removes the root device and all its channels from
  the catalogue and pushes a `deleteDevices` callback to every registered
  remote. The call is idempotent — an unknown address returns int `0` without
  error, matching pydevccu semantics. The `flags` parameter is accepted for
  wire compatibility (HomeMatic uses it for over-the-air deregistration bits)
  but is currently ignored. This unblocks `gohomematic`'s
  `DeviceCoordinator.UnpairDevice` integration tests.

## [0.1.0] — 2026-04-26

Initial release. A standalone Go port of
[`pydevccu`](https://github.com/sukramj/pydevccu).

### Added

- **XML-RPC server** with the full HomeMatic method set:
  `listDevices`, `getValue`, `setValue`, `putParamset`, `getParamset`,
  `getParamsetDescription`, `getDeviceDescription`, `init`,
  `getVersion`, `getServiceMessages`, `getAllSystemVariables`,
  `getSystemVariable`, `setSystemVariable`, `deleteSystemVariable`,
  `getMetadata`, `setMetadata`, `addLink`, `removeLink`,
  `getLinkPeers`, `getLinks`, `getInstallMode`, `setInstallMode`,
  `reportValueUsage`, `installFirmware`, `updateFirmware`,
  `clientServerInitialized`, `ping`, plus `system.listMethods`,
  `system.methodHelp` and `system.multicall`.
- **JSON-RPC server** (`POST /api/homematic.cgi`) compatible with
  the CCU/OpenCCU web API. Namespaces: `Session`, `CCU`, `Interface`,
  `Device`, `Channel`, `Program`, `SysVar`, `Room`, `Subsection`,
  `ReGa`, `system`. Plus the auxiliary `GET /VERSION`,
  `GET /config/cp_security.cgi` and `POST /config/cp_maintenance.cgi`
  endpoints.
- **VirtualCCU orchestrator** (`pkg/godevccu.VirtualCCU`) bundling
  the XML-RPC and JSON-RPC servers, the ReGa engine and state /
  session managers behind one `Start` / `Stop` lifecycle.
- **State manager** for programs, system variables, rooms, functions,
  service messages, inbox devices, backup status, firmware update
  info, device value cache and custom device names — all
  goroutine-safe.
- **Session manager** with token-based authentication, 30-minute
  inactivity timeout, renew/logout/cleanup APIs.
- **Pattern-based ReGa script engine** covering every script shipped
  by `aiohomematic` / `gohomematic`
  (`get_backend_info.fn`, `get_serial.fn`,
  `fetch_all_device_data.fn`, `get_program_descriptions.fn`,
  `get_system_variable_descriptions.fn`, `get_service_messages.fn`,
  `get_inbox_devices.fn`, `set_program_state.fn`,
  `set_system_variable.fn`, `create_backup_*.fn`,
  `get_system_update_info.fn`, `trigger_firmware_update.fn`,
  `get_rooms.fn`, `get_functions.fn`, generic `Write`).
- **Combined-parameter converter** (`COMBINED_PARAMETER`,
  `LEVEL_COMBINED`).
- **Device-response mappings** for switches, dimmers, blinds,
  thermostats, smoke detectors, window contacts and lock actuators.
- **Optional device behaviour simulators** for `HM-Sec-SC-2` and
  `HM-Sen-MDIR-WM55`.
- **397 device types** embedded via `//go:embed` from
  `pydevccu/pydevccu/{device_descriptions,paramset_descriptions}`.
- **CLI** `cmd/godevccu` with flags for mode, host, ports, auth and
  defaults.
- **Three backend modes**: `HOMEGEAR`, `CCU`, `OPENCCU`.
- **Persistence** of paramset values to `paramsets_db.json`
  (opt-in).
- **Build tooling**: `Makefile` targets (`build`, `test`, `cover`,
  `lint`, `data`, `run`, `clean`), `script/copy_data.sh` for
  refreshing the device catalogue from upstream `pydevccu`.
- **CI** workflow (`.github/workflows/ci.yml`) running gofmt, vet,
  golangci-lint and `go test -race -cover` on Linux, macOS and
  Windows.
- **Documentation**: `README.md`, `CLAUDE.md`, `DOCUMENTATION.md`.

### Notes

- Built and tested with Go 1.26.
- No CGo dependencies; ships as a single static binary.
- Public API lives under `pkg/godevccu`. Everything below
  `internal/` is implementation detail.
- `getVersion` reports `pydevccu-<PydevccuVersion>` in Homegear mode
  and `3.87.1.20250130` in CCU/OpenCCU mode — identical to upstream
  pydevccu so clients that branch on the prefix keep working.

[Unreleased]: https://github.com/SukramJ/godevccu/compare/v0.1.10...HEAD
[0.1.10]: https://github.com/SukramJ/godevccu/compare/v0.1.9...v0.1.10
[0.1.9]: https://github.com/SukramJ/godevccu/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/SukramJ/godevccu/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/SukramJ/godevccu/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/SukramJ/godevccu/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/SukramJ/godevccu/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/SukramJ/godevccu/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/SukramJ/godevccu/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/SukramJ/godevccu/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/SukramJ/godevccu/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/SukramJ/godevccu/releases/tag/v0.1.0
