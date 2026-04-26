# Changelog

All notable changes to `godevccu` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the project follows [Semantic Versioning](https://semver.org/).

The public API surface is `pkg/godevccu`. Anything under `internal/`
is excluded from the stability promise.

## [Unreleased]

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

[Unreleased]: https://github.com/SukramJ/godevccu/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/SukramJ/godevccu/releases/tag/v0.1.0
