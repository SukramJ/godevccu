# godevccu — Detailed Documentation

`godevccu` is a Go port of [pydevccu](https://github.com/sukramj/pydevccu).
The public API in `pkg/godevccu` covers the same use cases as the
Python original — from a pure XML-RPC server (Homegear mode) to a full
CCU/OpenCCU simulation with the JSON-RPC web API.

---

## Contents

1. [Backend modes](#backend-modes)
2. [VirtualCCU](#virtualccu)
3. [State manager](#state-manager)
4. [Session management](#session-management)
5. [XML-RPC layer](#xml-rpc-layer)
6. [JSON-RPC layer](#json-rpc-layer)
7. [ReGa script engine](#rega-script-engine)
8. [Device definitions](#device-definitions)
9. [Device behaviour simulators](#device-behaviour-simulators)
10. [Configuration](#configuration)
11. [Persistence](#persistence)
12. [Example workflows](#example-workflows)

---

## Backend modes

| Mode      | XML-RPC | JSON-RPC | Authentication | ReGa | Description |
|-----------|---------|----------|----------------|------|-------------|
| `HOMEGEAR`| yes     | no       | no             | no   | Slim mode, XML-RPC only. The version string becomes `godevccu-<VERSION>`. |
| `CCU`     | yes     | yes      | yes            | yes  | Classic CCU2/CCU3 simulation. |
| `OPENCCU` | yes     | yes      | yes            | yes  | OpenCCU/RaspberryMatic. Identical behaviour to `CCU`, but `Product=OpenCCU`. |

The mode is selected through `Config.Mode`
(`godevccu.BackendModeHomegear`, `BackendModeCCU`,
`BackendModeOpenCCU`).

In `CCU`/`OPENCCU` mode `getVersion` returns the real CCU firmware
version `3.87.1.20250130`; in Homegear mode it returns the `godevccu`
version string.

---

## VirtualCCU

`VirtualCCU` is the orchestrator. It bundles:

- the XML-RPC server (`internal/ccu`),
- the JSON-RPC server (`internal/jsonrpc`, only in CCU/OpenCCU mode),
- the ReGa engine (`internal/rega`),
- the state manager and session manager.

```go
v, err := godevccu.New(godevccu.Config{
    Mode:        godevccu.BackendModeOpenCCU,
    Host:        "127.0.0.1",
    XMLRPCPort:  2001,
    JSONRPCPort: 8080,
    Username:    "Admin",
    Password:    "secret",
    AuthEnabled: true,
    Devices:     []string{"HmIP-SWSD"},
    SetupDefaults: true,
    Persistence: false,
    Serial:      "GODEVCCU0001",
})
if err != nil { panic(err) }
if err := v.Start(); err != nil { panic(err) }
defer v.Stop()
```

Important methods:

- `Start() / Stop()` — idempotent lifecycle control.
- `IsRunning() bool`
- `XMLRPCAddr() net.Addr` / `JSONRPCAddr() net.Addr` — useful when
  `Port:0` is requested for ephemeral ports (tests).
- `RPC() *ccu.RPCFunctions` — direct access to the XML-RPC methods.
- `State() *state.Manager`, `Session() *session.Manager`.

Unlike pydevccu, the Go implementation does not use `async with` —
lifecycle management is handled with `Start`/`Stop` plus `defer`.

---

## State manager

`state.Manager` (re-exported as `godevccu.State`) owns:

- **Programs** (`Program`)
- **System variables** (`SystemVariable`)
- **Rooms** (`Room`)
- **Functions / Gewerke** (`Function`)
- **Service messages** (`ServiceMessage`)
- **Inbox devices** (`InboxDevice`)
- **Backup status** (`BackupStatus`)
- **Firmware update info** (`UpdateInfo`)
- **Device value cache** (for `fetch_all_device_data.fn`)
- **Custom device names** (for JSON-RPC `Device.setName` /
  `Channel.setName`)

Examples:

```go
st := v.State()

st.AddProgram("Heating Morning", "Start heating at 6:00", true, 0)
st.AddSystemVariable("Presence", "BOOL", true, godevccu.AddSystemVariableOpts{
    Description: "Someone is home",
})
st.AddRoom("Living Room", "Main living area", []string{"VCU2822385:1"}, 0)

st.RegisterSysVarCallback(func(name string, value any) {
    log.Printf("sysvar %s = %v", name, value)
})
```

All write methods on the manager are goroutine-safe.

---

## Session management

`session.Manager` implements token-based authentication. The session
ID is 32 hex characters (16 bytes from `crypto/rand`). The default
inactivity timeout is 30 minutes.

```go
m := v.Session()
id := m.Login("Admin", "secret")          // "" on failed login
ok := m.Validate(id)                      // touch + valid
new := m.Renew(id)                        // returns a fresh id
m.Logout(id)
```

When `Config.AuthEnabled = false`, `Validate(...)` always returns
`true`.

Authentication is required for every JSON-RPC method except:

- `Session.login`
- `CCU.getAuthEnabled`
- `CCU.getHttpsRedirectEnabled`
- `system.listMethods`

---

## XML-RPC layer

`internal/xmlrpc` contains:

- A **`Value` sum type** with the HomeMatic-relevant concretisations
  (`IntValue`, `BoolValue`, `StringValue`, `DoubleValue`,
  `DateTimeValue`, `Base64Value`, `StructValue`, `ArrayValue`,
  `NilValue`).
- **Encoder/decoder** in `decode.go` and `message.go`.
- **`Mux`** for method dispatch including `system.listMethods`,
  `system.methodHelp`, `system.multicall`.
- **HTTP `Handler`** as an `http.Handler` adapter.
- **`Client`** for outgoing
  `event`/`newDevices`/`deleteDevices`/`listDevices` calls to
  registered remotes.
- **`FromAny` / `ToAny`** as a bridge to native Go structures
  (`map[string]any`, slices, primitives).

Implemented XML-RPC methods:

`listDevices`, `getServiceMessages`, `ping`, `getVersion`,
`getAllSystemVariables`, `getSystemVariable`, `setSystemVariable`,
`deleteSystemVariable`, `getValue`, `setValue`, `getDeviceDescription`,
`getParamsetDescription`, `getParamset`, `putParamset`, `init`,
`getMetadata`, `setMetadata`, `addLink`, `removeLink`, `getLinkPeers`,
`getLinks`, `getInstallMode`, `setInstallMode`, `reportValueUsage`,
`installFirmware`, `updateFirmware`, `clientServerInitialized`.

Plus the system methods `system.listMethods`, `system.methodHelp`,
`system.multicall`.

---

## JSON-RPC layer

Endpoint: **`POST /api/homematic.cgi`**.

Implemented namespaces / methods:

| Namespace   | Methods                                                                 |
|-------------|-------------------------------------------------------------------------|
| `Session`   | `login`, `logout`, `renew`                                              |
| `CCU`       | `getAuthEnabled`, `getHttpsRedirectEnabled`                             |
| `system`    | `listMethods`                                                           |
| `Interface` | `listInterfaces`, `listDevices`, `getDeviceDescription`, `getParamset`, `getParamsetDescription`, `getValue`, `setValue`, `putParamset`, `isPresent`, `getInstallMode`, `setInstallMode`, `setInstallModeHMIP`, `getMasterValue`, `ping`, `init` |
| `Device`    | `listAllDetail`, `get`, `setName`                                       |
| `Channel`   | `setName`, `hasProgramIds`                                              |
| `Program`   | `getAll`, `execute`, `setActive`                                        |
| `SysVar`    | `getAll`, `getValueByName`, `setBool`, `setFloat`, `setString`, `deleteSysVarByName` |
| `Room`      | `getAll`, `listAll`                                                     |
| `Subsection`| `getAll`                                                                |
| `ReGa`      | `runScript`                                                             |

Additional HTTP endpoints:

- `GET  /VERSION` — `VERSION=…\nPRODUCT=…\n`
- `GET  /config/cp_security.cgi?sid=…` — backup download
- `POST /config/cp_maintenance.cgi?sid=…` — `checkUpdate` /
  `triggerUpdate`

The response envelope follows the CCU convention (`jsonrpc:"1.1"`,
both `result` *and* `error` always present).

---

## ReGa script engine

Instead of a full ReGa interpreter, the engine recognises the patterns
that `aiohomematic/gohomematic` produces and returns the expected
JSON payload:

- `get_backend_info.fn`
- `get_serial.fn`
- `fetch_all_device_data.fn`
- `get_program_descriptions.fn`
- `get_system_variable_descriptions.fn`
- `get_service_messages.fn`
- `get_inbox_devices.fn`
- `set_program_state.fn`
- `set_system_variable.fn`
- `create_backup_start.fn` / `create_backup_status.fn`
- `get_system_update_info.fn` / `trigger_firmware_update.fn`
- `get_rooms.fn` / `get_functions.fn`
- generic `Write("…")`

Unknown scripts return empty `Output` strings (with `Success=true`).
This keeps the Go implementation behaviourally identical to pydevccu.

---

## Device definitions

The JSON files from
`pydevccu/pydevccu/{device_descriptions,paramset_descriptions}/` are
copied via `script/copy_data.sh` into `internal/embed/data/` and
embedded into the binary at build time via `//go:embed all:data/...`.

```bash
# import data from ../pydevccu
./script/copy_data.sh

# or with an explicit path
./script/copy_data.sh /path/to/pydevccu/pydevccu

# alternatively via Make
make data PYDEVCCU=/path/to/pydevccu
```

397 device types are currently available (HM Wired, HM Wireless,
HmIP).

---

## Device behaviour simulators

Optionally enabled via `Config.EnableLogic = true`. For each
supported device type a goroutine is started that produces a steady
stream of value updates:

| Device type         | Address        | Behaviour                                               |
|---------------------|----------------|---------------------------------------------------------|
| `HM-Sec-SC-2`       | `VCU0000240:1` | Toggles `STATE`; flips `LOWBAT` every 5 iterations.     |
| `HM-Sen-MDIR-WM55`  | `VCU0000274:*` | Toggles `MOTION`, randomises `BRIGHTNESS [60..90]`, fires `PRESS_SHORT` on channel 1; `LOWBAT` every 5 iterations. |

Configurable through `Config.LogicConfig{StartupDelay, Interval}`.
Defaults are 5 s startup delay and 60 s interval.

---

## Configuration

Full config schema (every field is optional, sensible defaults via
`godevccu.Defaults()`):

```go
type Config struct {
    Mode          BackendMode      // Default: BackendModeOpenCCU
    Host          string           // Default: "127.0.0.1"
    XMLRPCPort    int              // Default: 2001
    JSONRPCPort   int              // Default: 80
    Username      string           // Default: "Admin"
    Password      string
    AuthEnabled   bool
    Devices       []string         // nil = all 397 device types
    Persistence   bool
    Serial        string           // Default: "GODEVCCU0001"
    SetupDefaults bool             // pre-populate programs/sysvars/rooms
    EnableLogic   bool             // enable the device behaviour simulators
    LogicConfig   LogicConfig      // Defaults: 5 s / 60 s
    Logger        *slog.Logger     // Default: slog.Default()
}
```

---

## Persistence

When `Persistence=true`, all `paramsets` values are written to a
JSON file (`paramsets_db.json` in the working directory) and reloaded
on startup. The file is created automatically on first start.

`PersistencePath` can currently only be set directly via
`ccu.Options.PersistencePath` — the public-API field can be added
later if required.

---

## Example workflows

### Test against the public API (Go)

```go
v, _ := godevccu.New(godevccu.Config{
    Mode:        godevccu.BackendModeOpenCCU,
    XMLRPCPort:  0,        // ephemeral port
    JSONRPCPort: 0,
    Devices:     []string{"HmIP-SWSD"},
})
_ = v.Start()
defer v.Stop()

xmlAddr := v.XMLRPCAddr().String()  // "127.0.0.1:NNNN"
jsonAddr := v.JSONRPCAddr().String()
```

### XML-RPC call via a standard client

`godevccu` ships `internal/xmlrpc.Client`, which is used internally
for callback pushes. External test clients (such as
aiohomematic/gohomematic) can talk to the server using any XML-RPC
library.

### Refresh data from pydevccu and rebuild

```bash
git -C ../pydevccu pull
make data
make test
make build
```

### Docker / OCI

Because no CGo is involved, a single-binary image is trivial:

```dockerfile
FROM scratch
COPY bin/godevccu /godevccu
ENTRYPOINT ["/godevccu"]
```

---

## Versioning

`godevccu` follows SemVer for the `pkg/godevccu` API. Everything
under `internal/` is explicitly excluded from the stability promise.
The version string is exposed as `godevccu.Version`.
