# CLAUDE.md

This document targets AI assistants (Claude Code & friends) working on
`godevccu`. It is intentionally compact. godevccu is the reference
implementation: behaviour is decided here. It started as a port of
[`pydevccu`](https://github.com/sukramj/pydevccu), which is archived and
no longer a source of behaviour or data.

---

## Project overview

`godevccu` is a virtual HomeMatic CCU in Go.
Goals:

1. **Stable default wire-level behaviour** over both XML-RPC and
   JSON-RPC. Clients (aiohomematic, gohomematic, openccu-loom) test
   against the default run; change it only deliberately and say so in
   the CHANGELOG. `internal/binrpc` is one deliberate extension: it
   models real CUxD over BIN-RPC — including its `system.multicall`
   callback envelope. Keep it opt-in (`Config.BINRPCPort`) so a default
   run is unchanged.
   `pkg/litefake` is the second deliberate exception: a fake
   openccu-lite box (occulited's HTTP API — token auth, XML-RPC proxy
   with init refusal and method tiers, SSE event stream, metadata,
   system, pairing) composed on top of per-interface simulator
   listeners. It is written from the
   condensed wire contract in `pkg/litefake/CONTRACT.md` and from
   nothing else — occulited is GPL-3.0, so no source, fixture or
   algorithm is ever copied or translated from it. Keep it opt-in too
   (`litefake.Start`, CLI `-mode lite`): a default run is unchanged.
2. **Single static binary** (`CGO_ENABLED=0`). No platform-specific
   build steps.
3. **Embedded device definitions** (via `//go:embed`) — no runtime
   filesystem dependencies.

## Hard rules (non-negotiable)

- **License header (MIT)** in every new source file, naming the
  copyright holder (existing `godevccu authors` headers stay as they
  are):
  ```
  // SPDX-License-Identifier: MIT
  // Copyright (C) 2026 SukramJ.
  ```
- **No CGo dependencies** (`CGO_ENABLED=0` is set globally).
- **Method names** at the XML-RPC layer stay **camelCase** (HomeMatic
  specification).
- **Addresses** are processed **case-insensitively** but stored in
  upper case.
- **Device descriptions** live in `internal/embed/data/` and are
  maintained here. Add a device with `device_descriptions/<TYPE>.json`
  and `paramset_descriptions/<TYPE>.json` (the format of Homematic(IP)
  Local's `export_device_definition` ZIP). Catalogue-wide corrections
  belong in the load-time normalisation (`internal/ccu/normalize.go`),
  not in individual files.
- **Public API** lives in `pkg/godevccu/` and `pkg/litefake/`.
  Everything else lives under `internal/` and is excluded from the API
  stability promise. `pkg/litefake` mirrors a pre-1.0 wire contract
  (`pkg/litefake/CONTRACT.md`) and may change in minor releases.

## Build & test

```bash
make build      # builds bin/godevccu
make test       # go test -race -cover ./...
make lint       # golangci-lint
make cover      # HTML coverage report
```

Run a single test file:

```bash
go test ./internal/state/ -run TestPrograms -v
```

## Architecture

The packages originate from these pydevccu modules (archived; the
mapping only explains where existing behaviour came from):

| godevccu                       | origin in pydevccu                |
|--------------------------------|-----------------------------------|
| `internal/hmconst`             | `pydevccu/const.py`               |
| `internal/xmlrpc`              | `xmlrpc.server` / `xmlrpc.client` |
| `internal/binrpc`              | — (no pydevccu counterpart)       |
| `internal/ccu`                 | `pydevccu/ccu.py`                 |
| `internal/state`               | `pydevccu/state/`                 |
| `internal/session`             | `pydevccu/session.py`             |
| `internal/jsonrpc`             | `pydevccu/json_rpc/`              |
| `internal/rega`                | `pydevccu/rega/`                  |
| `internal/converter`           | `pydevccu/converter.py`           |
| `internal/deviceresponses`     | `pydevccu/device_responses.py`    |
| `internal/devicelogic`         | `pydevccu/device_logic/`          |
| `internal/virtualccu`          | `pydevccu/server.py`              |
| `pkg/godevccu`                 | `pydevccu/__init__.py`            |
| `internal/embed/data/...`      | `device_descriptions/`, `paramset_descriptions/` |

## Conventions

- **Package layout**: anything not part of the public API lives under
  `internal/`. The `pkg/godevccu` package re-exports types — no logic
  of its own.
- **Logging**: `log/slog`. Configure via `slog.SetDefault`.
- **Errors**: `fmt.Errorf("…: %w", err)` for wrapping. Sentinels
  (`ccu.ErrRPC`, `xmlrpc.Fault`) instead of string comparisons.
- **Tests**: every new piece of functionality needs a test. End-to-end
  tests belong in `internal/virtualccu/virtualccu_test.go`.
- **Goroutines**: every goroutine must be cancellable through a
  `context.Context` or a stop channel. No `for { … time.Sleep(…) }`
  loops without a stop path.
- **Format**: `gofmt`-compliant; `goimports` ordering is enforced by
  the linter.

## Implementation policy

- **Do not change deterministic values** that clients rely on.
  For example, `getServiceMessages` returns the
  hard-coded `[["VCU0000001:1","ERROR",7]]` because existing
  integration tests expect that exact shape.
- **JSON-RPC response envelope** is `1.1` (not `2.0`) —
  aiohomematic/gohomematic check both `result` and `error` fields even
  on success.
- **Session IDs** are extracted from the top level, from `params` or
  from a stringified dict.
- **Device behaviour simulators** are opt-in (`Config.EnableLogic`).
  They exist purely for deterministic test scenarios and are not meant
  to emulate realistic device behaviour.

## Common tasks

### Add a new XML-RPC method handler

1. Implement the method in `internal/ccu/rpcfunctions.go`.
2. Wire it up in `internal/ccu/server.go:registerMethods`.
3. Add a test in `internal/ccu/rpcfunctions_test.go`, plus an
   end-to-end test in `internal/virtualccu/virtualccu_test.go` if it
   is reachable from the network surface.

### Add a new JSON-RPC handler

1. Implement the method in `internal/jsonrpc/handlers.go`.
2. Register it in `Methods()`.
3. If the method must bypass auth, add it to `PublicMethods`.

### Add a new device behaviour simulator

1. Create `internal/devicelogic/<NAME>.go` embedding the `runner`
   helper.
2. Add an entry to `Registry` in `devicelogic.go`.
3. Add a test in `internal/devicelogic/`.

## Working with hm-simulator

- [hm-simulator](https://github.com/hobbyquaker/hm-simulator) (Sebastian
  Raff, MIT; reference checkout under `../hm-simulator`) is the source of
  the scenario API and of the interface-process behaviours behind
  `Realism.InitSemantics`, `MasterModel`, `InterfaceQuirks` and
  `ServiceMessagesFault`. Its README says per behaviour whether it was
  measured on a CCU or is a model — carry that distinction over, and
  credit hm-simulator in the doc comment of anything taken from it.
- These behaviours stay opt-in: the default run is unchanged.
