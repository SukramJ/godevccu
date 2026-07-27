# Changelog

All notable changes to `godevccu` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the project follows [Semantic Versioning](https://semver.org/).

The public API surface is `pkg/godevccu`. Anything under `internal/`
is excluded from the stability promise.

## [Unreleased]

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

[Unreleased]: https://github.com/SukramJ/godevccu/compare/v0.1.9...HEAD
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
