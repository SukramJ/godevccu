# CLAUDE.md

This document targets AI assistants (Claude Code & friends) working on
`godevccu`. It is intentionally compact — the source of design truth is
the comparison with [`pydevccu`](https://github.com/sukramj/pydevccu)
(reference repository under `../pydevccu`).

---

## Project overview

`godevccu` is a port of the virtual HomeMatic CCU (`pydevccu`) to Go.
Goals:

1. **Identical wire-level behaviour** to `pydevccu` over both XML-RPC
   and JSON-RPC. Tests in either repo should produce the same answers.
2. **Single static binary** (`CGO_ENABLED=0`). No platform-specific
   build steps.
3. **Embedded device definitions** (via `//go:embed`) — no runtime
   filesystem dependencies.

## Hard rules (non-negotiable)

- **License header (MIT)** in every new source file:
  ```
  // SPDX-License-Identifier: MIT
  // Copyright (C) 2026 godevccu authors.
  ```
- **No CGo dependencies** (`CGO_ENABLED=0` is set globally).
- **Method names** at the XML-RPC layer stay **camelCase** (HomeMatic
  specification).
- **Addresses** are processed **case-insensitively** but stored in
  upper case.
- **Device descriptions** must not be modified directly in the repo —
  they are imported via `script/copy_data.sh` from `pydevccu/pydevccu/`.
- **Public API** lives in `pkg/godevccu/`. Everything else lives under
  `internal/` and is excluded from the API stability promise.

## Build & test

```bash
make build      # builds bin/godevccu
make test       # go test -race -cover ./...
make lint       # golangci-lint
make data       # copies device definitions from ../pydevccu
make cover      # HTML coverage report
```

Run a single test file:

```bash
go test ./internal/state/ -run TestPrograms -v
```

## Architecture

The packages mirror the pydevccu modules:

| godevccu                       | pydevccu                          |
|--------------------------------|-----------------------------------|
| `internal/hmconst`             | `pydevccu/const.py`               |
| `internal/xmlrpc`              | `xmlrpc.server` / `xmlrpc.client` |
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

- **Do not improvise** where pydevccu returns deterministic values.
  For example, `getServiceMessages` in the Go port also returns the
  hard-coded `[["VCU0000001:1","ERROR",7]]` because existing
  integration tests expect that exact shape.
- **JSON-RPC response envelope** is `1.1` (not `2.0`) —
  aiohomematic/gohomematic check both `result` and `error` fields even
  on success.
- **Session IDs** are extracted using the same rules as pydevccu
  (top-level, in `params`, stringified dict).
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

## Working with pydevccu

- pydevccu lives under `../pydevccu`. When in doubt about how a
  behaviour should manifest, look there — the Python code is the
  ground truth.
- `pydevccu/CLAUDE.md` contains a compact pydevccu architecture
  overview — read it first before diving into individual Python
  modules.
