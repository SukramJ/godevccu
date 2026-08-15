# godevccu — Detailed Documentation

`godevccu` is a Go port of [pydevccu](https://github.com/sukramj/pydevccu).
The public API in `pkg/godevccu` covers the same use cases as the
Python original — from a pure XML-RPC server (Homegear mode) to a full
CCU/OpenCCU simulation with the JSON-RPC web API.

---

## Contents

1. [Realism (opt-in)](#realism-opt-in)
2. [Backend modes](#backend-modes)
3. [VirtualCCU](#virtualccu)
4. [State manager](#state-manager)
5. [Session management](#session-management)
6. [XML-RPC layer](#xml-rpc-layer)
7. [BIN-RPC layer (CUxD)](#bin-rpc-layer-cuxd)
8. [JSON-RPC layer](#json-rpc-layer)
9. [ReGa script engine](#rega-script-engine)
10. [Device definitions](#device-definitions)
11. [Device behaviour simulators](#device-behaviour-simulators)
12. [Configuration](#configuration)
13. [Persistence](#persistence)
14. [Example workflows](#example-workflows)

---

## Realism (opt-in)

pydevccu parity is a contract, so every behaviour where a real CCU
differs from pydevccu sits behind `Config.Realism`. The zero value
reproduces the established behaviour bit for bit; `godevccu.RealismCCU()`
switches everything on.

| Field | What it changes |
|-------|-----------------|
| `JSONSchema` | CCU field names and types: `SysVar.getAll` reports LOGIC/NUMBER/LIST with stringified values and conditional fields, `Room.listAll` returns plain ids, `Program.getAll` formats `lastExecuteTime` as a date, `Device.listAllDetail` carries the full channel record including `paramsets` and the firmware fields. |
| `RegaIDs` | Numeric ReGa object ids for devices and channels. Without them `Room.getAll.channelIds` is empty, so every room and function assignment a client reads points at nothing. |
| `ErrorModel` | The CCU's 1.1 envelope (`version`, not `jsonrpc`), its error objects (`{name: "JSONRPCError", code: 400/401/402/…}`) and per-method privilege levels. |
| `ServiceMessages` | Service messages derived from the maintenance channel, plus the suppression store behind `getSuppressedServiceMessages`/`suppressServiceMessages`. |
| `Reachability` | `SetDeviceUnreachable` (UNREACH + latching STICKY_UNREACH) and a CONFIG_PENDING pulse after a MASTER write. |
| `BatchEvents` | Asynchronous delivery from a per-remote dispatcher, bundled into one `system.multicall`. |
| `PersistInit` | Callback registrations survive a restart (requires `Persistence`). |
| `Discovery` | SSDP on UDP 1900 plus `/upnp/basic_dev.cgi`. |
| `BasicAuth` | HTTP basic authentication (realm `theRealm`) on the XML-RPC surface for non-loopback callers. The web API is deliberately exempt — it authenticates by session, as on a CCU. Requires `AuthEnabled`. |
| `BackupAPI` | `/api/backup/*` and a backup that actually reaches "completed". |
| `Lifecycle` | Pairing counts down (`getInstallMode` reports the remainder) and a firmware update walks `FIRMWARE_UPDATE_STATE` through its progression. |
| `Ramps` | An actuator move reports a travelling `ACTIVITY_STATE` first and the idle state after the travel time. The value itself still lands immediately. |
| `FaultCodes` | The HomeMatic fault catalogue (−2/−4/−5/−6) instead of answering everything with −1, which clients read as "retryable". |
| `NormalizeData` | Completes the embedded descriptions while loading: missing parameter `ID`s, `UNIT: null` (which serialises as `<nil/>`), mistyped BOOL defaults, empty firmware fields. The fixtures stay untouched. |

### Separate interface listeners

`Config.InterfacePorts` models the CCU's interface processes. Each entry
gets its own listener, its own callback registry and only the devices of
that protocol family, classified by type prefix:

```go
v, _ := godevccu.New(godevccu.Config{
    InterfacePorts: godevccu.DefaultInterfacePorts(), // 2001/2010/2000/9292
    Realism:        godevccu.RealismCCU(),
})
```

`VirtualCCU.InterfaceAddr(name)` and `InterfaceRPC(name)` reach an
individual listener. A nil map keeps the single-endpoint behaviour,
where `XMLRPCPort` serves every device.

### ReGa script endpoint

`Config.RegaScriptPort` serves HomeMatic Script under `/tclrega.exe`
(8181 on a CCU) — the endpoint ccu-jack attaches through, and the only
way a client can run a script the pattern matcher has never seen.

Scripts run through the interpreter in `internal/regavm`, which covers
the language the shipped scripts actually use: the seven declaration
keywords, `if`/`elseif`/`else`, `foreach`, `while`, the `#`
concatenation operator, the string methods and the `dom`/`system`/
`interfaces` namespaces. Anything outside that surfaces as an error
rather than a wrong answer.

```go
v, _ := godevccu.New(godevccu.Config{
    RegaScriptPort: godevccu.PortRegaScript,
})
```

### TLS

`Config.TLS` adds the HTTPS twins a CCU serves alongside its plaintext
ports (2001/42001, 80/443). Without a supplied certificate a self-signed
one is generated at startup. `TLS.Redirect` makes the plaintext web API
answer 302 and `CCU.getHttpsRedirectEnabled` report true.

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
`getParamsetDescription`, `getParamset`, `getParamsetId`, `putParamset`,
`determineParameter`, `init`, `getMetadata`, `setMetadata`,
`getAllMetadata`, `addLink`, `removeLink`, `getLinkPeers`, `getLinks`,
`getLinkInfo`, `setLinkInfo`, `activateLinkParamset`,
`listBidcosInterfaces`, `rssiInfo`, `getInstallMode`, `setInstallMode`,
`reportValueUsage`, `installFirmware`, `updateFirmware`,
`clientServerInitialized`.

Plus the system methods `system.listMethods`, `system.methodHelp`,
`system.multicall`.

`ping` answers `true` and then delivers a `CENTRAL`/`PONG` event
carrying the caller id back to the registered client, the way a real
CCU lets a client match its own ping. Callers that pass no caller id
get the bare `true` and no event.

A callback receiver is only deregistered on a *transport* error. A
fault is the client answering, so it stays registered and keeps
receiving events — matching both the CCU and pydevccu.

While `Config.StartNotReady` is in effect the XML-RPC surface answers
`503 CCU not ready yet`, not just the JSON-RPC web API: a booting CCU
refuses every remote API port.

---

## BIN-RPC layer (CUxD)

`internal/binrpc` implements the HomeMatic BIN-RPC wire protocol —
`xmlrpc_bin://`, the binary sibling of XML-RPC that CUxD speaks
exclusively. It reuses `xmlrpc.Value`, since BIN-RPC carries the same
value set and differs only in framing.

**This is a deliberate extension beyond pydevccu parity.** pydevccu has
neither BIN-RPC nor CUxD. The transport exists because the CUxD callback
direction is otherwise untestable without real hardware.

Enable it with `Config.BINRPCPort` (0 disables it, `EphemeralPort` binds
an OS-assigned port and writes the resolved number back into the config).
`VirtualCCU.BINRPCAddr()` reports the bound address. The listener serves
the same `Mux` as the XML-RPC surface, so the whole method set answers
over BIN-RPC too.

### Callbacks are wrapped in `system.multicall`

The behaviour worth knowing: **every** callback pushed to a
`xmlrpc_bin://` receiver is wrapped in a `system.multicall` envelope,
exactly as real CUxD does — a single value change included.

```
system.multicall([
  {methodName: "event",
   params: ["<interface_id>", "<address>", "<parameter>", <value>]}
])
```

The interface id therefore lives **inside** the sub-call, not in the
envelope's `params[0]`. A consumer that reads `params[0]` as the
interface id sees a string for a bare call and an array for an envelope.
That distinction is the reason this transport is modelled at all: a
simulator pushing bare calls would let such a consumer pass every test
while dropping every real CUxD event.

Wire encoding notes:

- Big-endian throughout; 8-byte header `'B' 'i' 'n' <msgType> <size:u32>`.
- Strings are ISO-8859-1. A rune above U+00FF is refused rather than
  silently substituted — a mangled device name is harder to trace than a
  refused encode.
- Doubles use BIN-RPC's `mantissa * 2^exp / 2^30` representation.
- Decoding bounds message size, nesting depth, and element/member counts
  against the remaining payload, so a crafted frame cannot drive an
  oversized allocation or unbounded recursion.

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
    BINRPCPort    int              // Default: 0 = BIN-RPC/CUxD disabled
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
