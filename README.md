# godevccu

`godevccu` is a virtual HomeMatic CCU exposed as XML-RPC and JSON-RPC servers, written in Go — a standalone port of [pydevccu](https://github.com/sukramj/pydevccu).

It is designed for development and automated testing of Home Assistant and aiohomematic/gohomematic integrations without requiring real hardware.

## Features

- **XML-RPC server** with the full HomeMatic method list (`listDevices`, `getValue`, `setValue`, `putParamset`, `getParamset`, `getDeviceDescription`, …).
- **JSON-RPC server** compatible with the CCU/OpenCCU web API (`/api/homematic.cgi`).
- **VirtualCCU orchestrator**: bundles XML-RPC, JSON-RPC, the ReGa engine, session management and the state manager.
- **ReGa script engine** (pattern-based) — compatible with the scripts shipped by `aiohomematic/gohomematic`.
- **Session authentication** in CCU/OpenCCU format.
- **397 device types** embedded from pydevccu (via `//go:embed`).
- **Three backend modes**: `HOMEGEAR`, `CCU`, `OPENCCU`.
- **Built-in device behaviour simulators** for HM-Sec-SC-2 and HM-Sen-MDIR-WM55.
- **Single static binary** — no CGo dependency.

## Quick start

### As a library

```go
package main

import (
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/SukramJ/godevccu/pkg/godevccu"
)

func main() {
    cfg := godevccu.Defaults()
    cfg.Mode = godevccu.BackendModeOpenCCU
    cfg.XMLRPCPort = 2001
    cfg.JSONRPCPort = 8080
    cfg.Password = "test"
    cfg.SetupDefaults = true
    cfg.Devices = []string{"HmIP-SWSD"}

    v, err := godevccu.New(cfg)
    if err != nil {
        log.Fatal(err)
    }
    if err := v.Start(); err != nil {
        log.Fatal(err)
    }
    defer v.Stop()

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    <-sig
}
```

### As a CLI

```bash
make build
./bin/godevccu -mode openccu -xml-rpc-port 2001 -json-rpc-port 8080 -defaults
```

## Backend modes

| Mode       | XML-RPC | JSON-RPC | Auth | ReGa | Description                       |
|------------|---------|----------|------|------|-----------------------------------|
| `HOMEGEAR` | yes     | no       | no   | no   | XML-RPC only, minimal simulation  |
| `CCU`      | yes     | yes      | yes  | yes  | CCU2/CCU3                         |
| `OPENCCU`  | yes     | yes      | yes  | yes  | OpenCCU/RaspberryMatic            |

## Architecture

```
pkg/godevccu/         Public API (façade over internal/)
internal/
  hmconst/            Protocol constants
  xmlrpc/             XML-RPC encoder/decoder/server/mux/client
  ccu/                RPCFunctions + ServerThread (core logic)
  state/              StateManager: programs, sysvars, rooms, functions, …
  session/            Token-based authentication
  jsonrpc/            CCU/OpenCCU JSON-RPC handlers + HTTP server
  rega/               Pattern-based ReGa script engine
  converter/          COMBINED_PARAMETER / LEVEL_COMBINED converters
  deviceresponses/    Device-specific event mappings
  devicelogic/        Optional device behaviour simulators
  virtualccu/         Orchestrator bundle
  embed/              //go:embed of the device definitions
cmd/godevccu/         CLI
```

The device definitions (`device_descriptions/*.json`, `paramset_descriptions/*.json`) live under `internal/embed/data/` and are refreshed by `script/copy_data.sh` from `pydevccu/pydevccu/`.

## Build

```bash
make build       # binary into bin/
make test        # all tests
make cover       # coverage report into coverage.html
make lint        # golangci-lint
make data        # copy device JSONs from ../pydevccu
```

The default path to the pydevccu source is `../pydevccu`. `PYDEVCCU=` overrides it:

```bash
make data PYDEVCCU=/path/to/pydevccu
```

## Tests

```bash
go test ./...                          # all tests
go test -race -cover ./...             # with race detector and coverage
go test ./internal/virtualccu/...      # end-to-end tests
```

The CI workflow (`.github/workflows/ci.yml`) runs lint, vet, test and build on Linux, macOS and Windows.

## License

MIT — see [`LICENSE`](LICENSE).

The embedded device and paramset descriptions originate from [pydevccu](https://github.com/sukramj/pydevccu) and were initially extracted from HomeMatic firmware XML. The eQ-3 license terms (non-commercial) apply to that data.
