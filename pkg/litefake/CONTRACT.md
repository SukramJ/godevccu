# The occulited wire contract (condensed)

This document is the wire contract `litefake` is implemented from: our
own condensation of the occulited HTTP API, re-derived from occulited's
published documentation and from reading its behaviour (occulited is
GPL-3.0 — no source, fixture or algorithm was copied or translated;
litefake is written from this document alone). It originated as
Appendix A of OpenCCU-Loom's openccu-lite implementation plan and is
reproduced verbatim below, including that project's name where the
appendix mentions it.

## Appendix A — the occulited wire contract (condensed)

Re-derived from reading occulited's code and documentation at occulited commit `24127692918f`
(2026-09-26) and openccu-lite `250775b0bafd` (`LITE-VERSION`: `VERSION=1.0.0-dev`,
`BASE=3.89.11.20260919`). **Pre-1.0.** Facts are from code unless marked *(doc)* or *(inferred)*.
Use this appendix as the contract; do not consult occulited source.

### A.1 Topology and general HTTP rules

- occulited listens on loopback only (default `127.0.0.1:8183`); **lighttpd** faces the LAN on 80/443
  and proxies `/api/`, `/addons/` and everything else to occulited. WebSocket upgrade is proxied.
- The CCU XML-RPC ports (2001/2010/9292/2000) are **not** exposed by default (only under "Classic
  RPC", not used by Loom). ReGa ports and the WebUI JSON-RPC **do not exist**; `POST
  /api/homematic.cgi` exists only as a loopback stub (404 from the LAN).
- While occulited is down, lighttpd answers every `/api/*` with `503`
  `{"error":"starting","message":"occulited is not answering yet"}` and `Retry-After: 5`.
- Requests with a body over lighttpd need `Content-Length` (else `411`): send `{}` on
  POST/PUT/PATCH/DELETE when there is nothing to send *(doc)*.
- JSON answers: `Content-Type: application/json; charset=utf-8`, `Cache-Control: no-store`.
- Error body: `{"error": <code>, "message": <text>, "detail"?: …}`.
- occulited's web shell (SPA) answers **any** non-API path without a static-file extension
  (`.png .jpg .jpeg .gif .webp .svg .ico .css .js .mjs .map .json .xml .txt .woff .woff2 .ttf
  .webmanifest`) with `200` and its HTML shell; a missing static-extension file is `404`. So
  `/ise/checkrega.cgi` and `/VERSION` answer `200 text/html`, never `OK`.

### A.2 Authentication, tokens, scopes

- Credentials (first valid wins): cookie session; `Authorization: Bearer <x>` (API token
  `olt_<32 lowercase hex>` or a 26-char base32 session id); **Basic auth whose password is an API
  token** (user name ignored); `?sid=` (refused on lite-rpc: `400 bad-request "credentials are not
  accepted in the query string here: use the Authorization header"`). The 10-char legacy alias is never
  accepted.
- Token name pattern `^[a-z0-9][a-z0-9_.-]{1,31}$`. Only the SHA-256 is stored; the secret is shown once.
  Optional expiry and IP ranges (a token with IP ranges needs a known remote). Rotation:
  `POST /api/auth/v1/tokens/self/rotate` (old secret valid 60 s). Narrowing only via `PATCH`.
- Scopes: `meta:read`, `meta:write`, `system:read`, `logs:read`, `system:write`, `addons:write`,
  `power`, `backup`, `led`, `radio:keys`, `auth:admin`, `rpc:read`, `rpc:operate`, `rpc:configure`,
  `rpc:admin`, `*`, (`self`: accounts only). Implications: `meta:write`⊃`meta:read`;
  `system:write`⊃`system:read`,`led`; `addons:write`⊃`system:read`; `auth:admin`⊃`self`;
  `rpc:operate`⊃`rpc:read`; `rpc:configure`⊃`rpc:operate`,`rpc:read`; `rpc:admin`⊃ all rpc tiers;
  `*`⊃ everything.
- `GET /api/auth/v1/state` (open): always `{setup_required, authenticated}`; with a credential adds
  `user` (`"token:<name>"` for a token), `scopes` (the **stored** list, implied scopes **not**
  expanded), `must_change_password`; accounts add `role`, `level`, `account_id`, `sid`, `method`. Auth
  mode `off`: fixed object with `auth_off: true`.
- `POST /api/auth/v1/login {username, password}` (open) → `{sid, user, role, level, account_id,
  must_change_password}`; `level` ∈ `read`, `operate`, `configure`, `administer`.
  `POST /api/auth/v1/logout` needs the session (`Authorization: Bearer <sid>`).
- `401 {"error":"unauthenticated","message":"login required"}`;
  `403 {"error":"forbidden","message":"the scope <s> is required","scope":"<s>"}` (the route's first
  scope). `403 cross-site` applies only to cookie sessions, never to tokens.
- Console alternative for a manual token (runs on the box):
  `occulited token <name> --scope <s> [--scope …] [--expires 2027-01-01|30d] [--ip RANGE]`;
  admin API: `POST /api/auth/v1/tokens {name, scopes, expires?, ips?}` (`auth:admin`) → `201 {…, token}`.

### A.3 lite-rpc XML-RPC proxy — `POST /api/rpc/v1/xmlrpc/{interface}`

- Route scope `rpc:read`; `501 unsupported` when the service is missing.
- `{interface}` matches the InterfacesList `<name>` **exactly, case-sensitive**; unknown →
  `404 {"error":"unknown-interface","message":"no such interface: <x>"}`.
- Body limit 4 MiB (over → `400 bad-request`); any declared request charset accepted; not a
  methodCall → `400 {"error":"bad-request","message":"not an XML-RPC call: …"}`.
- Forwarded re-encoded to ISO-8859-1 (non-Latin-1 → numeric character references) to the daemon URL
  with its path kept (`/groups` for VirtualDevices), 30 s timeout. The answer is re-encoded to
  **UTF-8** with `<?xml version="1.0" encoding="UTF-8"?>`, `Content-Type: text/xml; charset=utf-8`;
  types preserved; a void answer becomes an empty string value; 16 MiB read cap.
- Daemon fault → passed through (code + string); any other error → fault code `-1`.
- Interface process not answering (refused, timeout, non-2xx, undecodable) →
  **`503 {"error":"down","message":"the interface process does not answer: <iface>: …"}`** (JSON, not a fault).
- Refusals are **XML-RPC faults over HTTP 200**, code `-1`:
  - `init` anywhere, including inside `system.multicall`: faultString
    `init is not available remotely on openccu-lite: subscribe to /api/rpc/v1/events - see docs/rpc-remote.md`
    (the cited doc does not exist);
  - a tier the caller lacks: `not permitted: <method> needs rpc:<tier>`.
- `system.multicall` is allowed; each inner call is tier-checked; forwarded as one call. A multicall
  that is not an array of structs counts as the single method `system.multicall` (→ `rpc:admin`).
- `GET /api/rpc/v1/interfaces` → `[{"name", "protocol":"xmlrpc", "url_path":"/api/rpc/v1/xmlrpc/<name>",
  "running": bool}]` sorted by name. Included: every `xmlrpc://`, `http(s)://` entry plus `BidCos-RF`
  and `BidCos-Wired` on `xmlrpc_bin://`; **CUxD and other BIN-RPC entries excluded**. `running` = not
  `down` (`silent` counts as running). Shipped template: `BidCos-RF`, `VirtualDevices` (`…/groups`),
  `HmIP-RF`; `BidCos-Wired` only where hs485d runs.

### A.4 Method tiers (the data Loom's feature table and the fake's tier check are written from)

- **rpc:read**: `system.listMethods`, `system.methodHelp`, `system.methodSignature`, `listDevices`,
  `getDeviceDescription`, `getParamsetDescription`, `getParamset`, `getParamsetId`, `getValue`,
  `getLinks`, `getLinkInfo`, `getLinkPeers`, `getMetadata`, `getAllMetadata`, `listBidcosInterfaces`,
  `getInstallMode`, `getKeyMismatchDevice`, `getServiceMessages`, `listReplaceableDevices`,
  `getVersion`, `ping`, `rssiInfo`, `getLGWStatus`, `listTeams`, `getDeviceStatus`, `getMasterValue`,
  `clientServerInitialized`, `refreshDeployedDeviceFirmwareList`, `getRFLGWInfoLED`,
  `getParamsetsInfo`, `listAllDevices`, `getBackgroundBackupState`, `getCurrentDutyCycle`, `logLevel`
  (without params).
- **rpc:operate**: `setValue`; `putParamset` when param[1] equals `VALUES` (case-insensitive).
- **rpc:configure**: `putParamset` (MASTER, LINK, other), `setInstallMode`, `addLink`, `removeLink`,
  `setLinkInfo`, `setMetadata`, `deleteMetadata`, `setBidcosInterface`, `setTeam`, `addDevice`,
  `activateLinkParamset`, `reportValueUsage`, `abortDeleteDevice`, `logLevel` (with params),
  `setRFLGWInfoLED`, `setInterfaceClock`, `addVirtualDevice`, `setMasterValue`, `determineParameter`,
  `searchDevices`, `setTempKey`.
- **rpc:admin**: `deleteDevice`, `changeKey`, `restoreConfigToDevice`, `updateFirmware`,
  `installFirmware`, `changeDevice`, `replaceDevice`, `resetDevice`, and **every method not listed**
  (e.g. `setInstallModeWithWhitelist`, `getInstallModeWithWhitelist`, `getFirmwareInfo`,
  `suppressServiceMessages`, `getSuppressedServiceMessages`).
- `init`: refused before tiers, always.

### A.5 lite-rpc event stream — `GET /api/rpc/v1/events` (SSE); `/events/ws` (WebSocket)

- Scope `rpc:read`; `?sid=` → 400; a token passes. Limits **2 per subject** (`token:<name>`,
  `session:<user>`, `public:<remote>`) and **16 total**, SSE and WebSocket together; over →
  `429 {"error":"too-many-streams","message":"too many streams: 2 per token or session"|"… 16 in total"}`.
  The meta SSE, `/service-messages/stream`, `/pairing/stream`, `/log/stream` are **not** counted.
- Headers `Content-Type: text/event-stream; charset=utf-8`, `Cache-Control: no-store`,
  `X-Accel-Buffering: no`. `: connected\n\n` immediately. Frame: `id: <id>\n` (omitted when empty),
  `event: <type>\n`, `data: <one-line JSON>\n\n`. Heartbeat `: ping\n\n` every **15 s**; a client
  treats **45 s** of silence as dead. Write deadline 30 s. Close: SSE just ends.
- WebSocket: RFC 6455, subprotocol `openccu-lite.rpc-events.v1` echoed when offered; text frames
  `{"type", "data", "id"?}`; server pings every 15 s, no pong in 45 s closes; close codes 4001 overflow,
  4003 unauthorized, 4004 disabled/shutdown, 1000 normal; a plain GET → `426 upgrade-required`.
- **Ids** `<boot_id>-<seq>`: `boot_id` 16 hex, random per occulited process; `seq` uint64. Messages
  **without** id: `resync`, and the `?devices=1` snapshots.
- **Types** (`data` fields):
  - `hello` (always first, before filters, has an id): `boot_id`, `seq`, `interfaces: [{name, url,
    state: up|down|silent, registered, last_activity?, last_init?, last_error?, events, calls,
    restored?, stalled?, stall_kind?: delivery|calls}]`, `buffer: {seconds: 300, events: 5000}`.
  - `event`: `interface`, `address`, `key`, `value`, `ts` (RFC3339Nano UTC), `batch` (seq of the first
    event of the daemon's multicall); for the "chosen set": `lc`, `confirmed: true`, `previous_for_s?`.
  - `state`: `interface`, `address`, `datapoint`, `value`, `ts`, `lc`, `source: "sweep"`,
    `confirmed?`, `previous_for_s?` (occulited's own sweep, not a daemon event).
  - `interface`: `interface`, `state: up|down|restarted|added|removed`, `ts`.
  - `newDevices`, `deleteDevices`, `updateDevice`, `replaceDevice`, `readdedDevice`: `interface`,
    `addresses: [string]`, `ts` — **addresses only**; `updateDevice` carries one address and **no
    hint**; `replaceDevice` carries `[old, new]`.
  - `resync`: `reason: boot|gap|overflow` (no id; sent regardless of filters).
- **Value typing on the stream** (differs from the JSON-RPC path): `i4`/`int` → int (**parse failure →
  0 silently**); `boolean` → bool (`"1"`/`"true"`); `double` → float64 (**JSON writes `1.0` as `1`**);
  `dateTime.iso8601` → the **raw string** (`20260926T12:00:00`); `base64` → the **raw base64 string**;
  struct → object; array → list; string/empty → string (`<value></value>` → `""`); missing → `null`.
- **PONG**: no special handling; every daemon `event` is published, so `CENTRAL`/`PONG` events (value =
  caller id) reach clients unless filtered — including those caused by occulited's own liveness pings
  (`occulited_<iface>`) and other clients'. Whether a daemon broadcasts PONG for an unregistered caller
  id rests on an occulited comment (UNVERIFIED, §4.5).
- **`?devices=1`**: after hello and replay, one `newDevices` per interface with `{"interface",
  "devices": <listDevices converted with the JSON path's typing: base64 → {"base64":…}, dateTime →
  RFC3339>}` or `{"interface","error"}`; no id, no ts. A client must branch on `devices` vs `addresses`.
- **Filters** (query, repeatable, comma-separable; AND across kinds, OR within): `interface=`,
  `address=` (matches `X` and `X:*`), `key=` (alias `datapoint=`), `type=`. `key`/`address` apply only to
  `event` and `state`.
- **Resume**: `Last-Event-ID: <boot>-<seq>` header (or `?last_event_id=`). Unparsable or other boot →
  `resync{boot}`, no replay. Ring no longer covers `since+1` (or ring empty while `since < next`) →
  `resync{gap}`, no replay. Else every ring message with seq > since is replayed with its id.
  `since >= next` → nothing, no resync. Ring: ≤ 5000 messages or 5 minutes; all message kinds occupy it.
- **Overflow**: per-reader queue of 1024; a full queue drops and counts; detected at the next heartbeat
  tick (≤ 15 s) → `resync{overflow}`, then the stream ends (WS close 4001).
- **Revocation**: every heartbeat re-validates the credential; revoked/expired → SSE ends (WS 4003).
- `resync` means: re-seed values and devices, continue from the new position *(inferred semantics)*.

#### A.5.6 `GET /api/rpc/v1/state` and `/history`

- `/state` → `{entries: [{interface, address, datapoint, value, ts, lc, confirmed, confirmed_at?,
  source: event|sweep|restored, previous?, previous_for_s?}], total, unconfirmed, next?, event_id,
  sweeps, datapoints? (first page only)}`; paging `limit` (1–5000, default 1000), `after=<next>`;
  filters `interface`, `address`, `datapoint`/`key`. `event_id` is taken **before** the read (use it
  as `Last-Event-ID` for a gap-free seed). **Only a chosen datapoint set** is kept (STATE, LEVEL,
  LEVEL_2, temperatures, humidity, POWER, ENERGY_COUNTER, …, UNREACH, STICKY_UNREACH, LOWBAT,
  CONFIG_PENDING, UPDATE_PENDING, SABOTAGE, DUTY_CYCLE, …, `ERROR_*`) — not a full value source.
  `confirmed:false` / `source:"restored"` entries come from disk after a restart and must not be acted on.
- `/history?interface&address&datapoint` → `[[ts_ms, value], …]`; `404 not-recorded` outside its list.

### A.6 Metadata API — `/api/meta/v1` (normative together with its fixtures)

- Scopes: reads `meta:read`, mutations/imports `meta:write`, `/version` open.
- `GET /version` → `{"api":"meta","version":1,"format":1,"revision":N,"implementation":"occulited
  <build>","hmip":{keyserver_mode, device_keys, offline_pairing},"capabilities":{"pairing":bool,
  "state":true,"history":true,"apis":{"meta":1,"rpc":1,"system":1,"auth":1},
  "transports":["sse","websocket"],"limits":{"streams_per_token":2,"streams_total":16,
  "buffer_seconds":300,"buffer_events":5000},"json_double":true}}`. Rule: anything that is not a JSON
  body with `"api":"meta"` is a CCU; a client refuses a higher major in `apis`; a box without `hmip` /
  `capabilities` is older — treat as absent.
- `GET /snapshot` → `{"format":1, "revision":N, "objects":{"<iface>.<address>": {"name", "enums":
  [<full paths>], "meta": {ns: any}, "orphaned"?: true}}, "enums": {"<id>": {"name": {"en":…,"de":…},
  "tree": [{"id","name","icon"?,"children"?:[…]}]}}}`. Full paths include the enum id:
  `"room/eg/wohnzimmer"`. Default enums: `room` (Räume/Rooms), `function` (Gewerke/Functions); the
  system maintains `favorite` (one node per account); older stores may carry `floor`.
- `GET /objects?enum=<path>&orphaned=true|false` → `{revision, objects}`; `GET /objects/{ref}` →
  `{revision, ref, object}` or `404 unknown-object`. Refs `<interface>.<address>`, split at the first
  `.`, case-sensitive; **percent-encode `:`** in the path (`BidCos-RF.JEQ0230153%3A1`).
- **The store never invents objects**: only named devices/channels exist (imports drop objects still
  named `<type> <address>` unless they are in a room/function).
- `GET /enums` → `{revision, enums}`; `GET /enums/{enum}/tree` → `{revision, enum, name, tree}`;
  unknown → `404 unknown-enum`. `GET /export?format=json|yaml`.
- Writes (all accept `If-Match: <revision>`; mismatch → `409 revision-conflict`):
  - `PUT /objects/{ref} {name, enums?, meta?}` replaces (missing optionals reset);
  - `PATCH /objects/{ref}` with any of `name`, `enums` (replaces the whole list), `meta` (merged per
    namespace; `null` deletes a namespace); **PATCH creates the object when it does not exist and the
    body carries a `name`** (without a name → invalid-name error); `orphaned` in a body → `403`;
    unknown fields → `422`;
  - `DELETE /objects/{ref}`; `POST /objects:bulk {set: {ref: patch}, delete: [ref]}` (one revision);
  - `POST /enums {id, name}` (201); `PATCH /enums/{e} {name}`; `DELETE /enums/{e}[?members=detach]`;
  - `POST /enums/{e}/nodes {parent: <full path>|null, id, name, icon?, position?}` (201) — `parent`
    `null`, `""` or the enum id means root; a non-root parent must start with `<e>/`;
  - `PATCH /enums/{e}/nodes/{path…} {name?, icon?, parent?|null, position?}` (move = new parent);
  - `DELETE /enums/{e}/nodes/{path…}[?members=detach]` — removes the node and its subtree; **refused
    with `has-members` (detail `refs`) unless `members=detach`**, which removes the subtree's paths from
    every object;
  - `PUT /import?mode=replace|merge`, `POST /import/ccu`, `POST /import/regadom` (not used by Loom).
- Mutation answers: `200`/`201 {"revision": N}` + `ETag: N`; **unchanged → `304`, empty body, revision
  only in `ETag`**.
- Error codes are stable (messages are not): `unknown-object`, `unknown-enum`, `unknown-path` (**422**,
  also on reads with a bad `enum=` filter), `revision-conflict` (409), `has-members`, `invalid-name`,
  `invalid-id`, `invalid-body`, `forbidden`, `format-unsupported`, …
- Validation: names trimmed, non-empty, no control characters, ≤ 255 bytes; node/enum/namespace ids
  `^[a-z0-9][a-z0-9-]*$` ≤ 32; tree depth ≤ 8; all `meta` namespaces of one object ≤ 16 KiB.
- `orphaned` is set only by occulited (every 10 minutes, from each answering interface's
  `listDevices`); a change emits `object.updated`.
- **Change stream `GET /events/sse`**: `Content-Type: text/event-stream` (no charset), `: connected`,
  then frames that are **only `data: <json>\n\n`** — no `event:`, no `id:`. Event JSON `{revision,
  kind, ref?, enum?, path?, from?, to?, value?, objects?, enums?}`; kinds `object.updated`,
  `object.deleted`, `enum.created|updated|deleted`, `node.created|updated|deleted|moved`, `import`
  (re-snapshot); one mutation may emit several events with the **same** revision. `?since=<rev>`
  replays events with revision > rev; `{"kind":"resync","revision":N}` when since < 0, since > current,
  not an integer, or older than the retained log (≤ 1000 events, **memory only** — after an occulited
  restart any `since` below the current revision resyncs). Heartbeat `: ping` every **30 s**. Slow
  subscriber: 64-slot queue, **overflow drops silently** → detect a revision gap. No stream limit. No
  WebSocket variant (`/api/meta/v1/events` is 404).

### A.7 System API endpoints Loom uses — `/api/system/v1`

| Endpoint | Scope | Shape |
|---|---|---|
| `GET /health` | open | `{ok, version, release, base, uptime_s, meta: {revision, recovered}}` |
| `GET /status` | system:read | hostname, `/VERSION` record, `occulited_version`, uptime, load, memory, disks, time, `timezone` (zone name), `tz`?, `hm_mode`, `meta_recovered`, `unclean_shutdown?`, `container?` |
| `GET /time` | system:read | `{tz, zone, ntp_servers, has_ntp, now}` |
| `GET /system-update` | system:read | `{running: <VERSION record; running.lite = openccu-lite version>, staged: null\|{file,size,modified,kind,version,board,warning,recovery_armed,way_back}, feed: null\|{enabled, feed_url, checked, error, downloading, available: null\|{version, tag, name, url, size, sha256_url, published, notes_url, newer}}, container: ""\|"lxc"}` |
| `POST /system-update/check` | power | asks the release feed now |
| `POST /system-update/download` | power | long-running; downloads, verifies, stages; answers the staged record or `422 no-space` |
| `POST /system-update/install` | power | arms recovery and reboots; `409` when nothing is staged |
| `DELETE /system-update`, `PUT /system-update/settings {enabled}`, `POST /system-update/upload` | power | not used by Loom |
| `POST /reboot {"confirm":true}` | power | `200 {ok, message:"rebooting"}` |
| `POST /halt {"confirm":true}` | power | halt |
| `POST /reboot/recovery {"confirm":true}` | power | reboot into recovery |
| `GET /backup` | backup | streams a CCU-compatible `.sbk` (`Content-Disposition` with the CCU's file name); with backup encryption on: `<name>.sbk.age`, no `Content-Length`; `?encrypted=false` → plain (a token passes without a confirm ticket) |
| `POST /backup/run {target?}` | backup | `202 {started, instance}`; `409 busy` |
| `GET /backup/targets` | system:read | `{nightly, container, kinds, encryption, hostname, needed_bytes, targets: [{id, name, kind, enabled, …, state: {state, detail?, …}, last_backup?: {at, ok, state, step?, error?, name?, size?, encrypted, …}}]}`; `state.state` ∈ `unsupported`, `idle`, `connecting`, `writable`, `read-only`, `full`, `unreachable`, `auth-failed`, `host-key-unknown`, `host-key-changed`, `no-sftp`, `stale`, `no-medium`, `update-room`, `running`, `error` (`running` while a create/deliver run for it is active) |
| `POST /restore/check` | power | multipart `file` (or raw body, or JSON `{target,name}`) → `{file, check: {ok, output, backup_version, running_version, needs_key, has_rega}, encryption: {encrypted, needs_recovery_key, created_here, …}}`; `422 corrupt`; one upload at a time |
| `POST /restore/apply {file, key?, force?}` | power | `200 {ok, output, rebooting: true}` then reboot; `422 restore-failed`; `rebooting:false`+`message` when the reboot did not start; an `.sbk.age` is refused (400) until `/restore/decrypt` |
| `GET /service-messages` | system:read | `{count, messages: [{interface, address (device), channel ("0"), key, value, since (RFC3339), seen: event\|start, type?}], swept, errors, feed?}`; read-only (no acknowledge); a message = a service-flagged datapoint active on channel 0; `/service-messages/stream` SSE `event: messages`, `: ping` every 30 s |
| `GET /groups` | system:read | `{groups: [{id (**JSON number**, observed live 2026-09-28), name, type, type_label, device ("INT0000001"), ref ("VirtualDevices.INT0000001")}], devices_to_configure: [{id, serial, type}]}` |
| `GET /groups/types` | system:read | `{types: [{id, label, assignable: [Member], leftover: [Member]}]}` (types e.g. `HomeMatic.heating`, `hmip.heating.group`); observed live: `Member` = `{id: <channel address>, serial: <the same>, type: <channel type>}` |
| `GET /groups/{id}` | system:read | `{id, name, type, device, ref, device_name, forbid_single_operation, members, assignable, leftover, types}`; `404 unknown-group` |
| `POST /groups {name, type, members: [id], forbid_single_operation?}` | system:write | the group + `devices_to_configure`; name 1–64 chars one line; `422 invalid` names the field. Observed live 2026-09-28: `502 {"error":"hmipserver",…context deadline exceeded}` after 30 s **while the group was created anyway, without its member** — a failed create must be followed by a re-read |
| `PUT /groups/{id} {name?, members?, forbid_single_operation?}` | system:write | members replace as a whole list; observed live: the same 30 s `502 hmipserver` timeout, members unchanged |
| `DELETE /groups/{id}` | system:write | `{deleted, former_members}`; observed live: `deleted` is the deleted group's **id as a JSON number** (`{"deleted":6,"former_members":[]}`) |
| `GET /radio/health` | system:read | `{polled, interfaces: [{interface, address, type, connected, default, firmware, duty_cycle, carrier_sense?}], answering, errors, history, busy, busy_interface}` (sampled once a minute) |
| `GET /radio` | system:read | inventory incl. per-interface `subscribers` |
| `GET/PUT/DELETE /radio/hmip/local-key` | system:write (even GET) | not used; the key-mode summary comes from `/api/meta/v1/version` `hmip` |
| `/radio/hmip/device-keys*` | radio:keys | not used (pairing can never grant it) |
| `GET /firmware` | system:read | device firmware index status (Loom keeps deriving device firmware state from descriptions) |
| `GET /upnp/basic_dev.cgi` (root path, not under `/api`) | open | UPnP Basic:1: `manufacturer`=`modelName`=`openccu-lite`, `modelDescription` `openccu-lite <serial>`, `serialNumber`, `UDN uuid:upnp-BasicDevice-1_0-<serial>`, `friendlyName` (hostname); serial = `/var/board_sgtin` else `/var/board_serial` else hostname |

**No REST install-mode endpoint** exists: use XML-RPC `setInstallMode` (rpc:configure) /
`getInstallMode` (rpc:read); `setInstallModeWithWhitelist` needs rpc:admin. Device firmware updates are
XML-RPC `installFirmware`/`updateFirmware` (rpc:admin).

### A.8 Client pairing — `/api/auth/v1/pairing/request`

1. `POST /api/auth/v1/pairing/request` (open) with body (strict decoding, unknown fields → 422, 1 MiB):
   `{"app": ^[A-Za-z0-9][A-Za-z0-9._-]{0,47}$, "app_version": ≤32, "instance": ≤64, "name": ≤80
   (default "<app> on <instance>"), "access": {"devices": read|operate|configure|administer,
   "names": read|configure, "system": read|configure}, "purpose": {<area>: ≤200 chars}, "commit": <64
   hex = sha256(client_nonce)>}`. At least one area. Level → scopes: devices `read`=rpc:read,
   `operate`=+rpc:operate, `configure`=+rpc:configure, `administer`=+rpc:admin; names `read`=meta:read,
   `configure`=+meta:write; system `read`=system:read+logs:read, `configure`=+system:write. **Never
   granted**: `*`, `auth:admin`, `radio:keys`, `power`, `backup`; `addons:write`, `led` are in no level.
2. Answer `202 {id: 16 hex, poll: 48 hex secret, nonce: 32 hex, expires_in: 300, interval: 2,
   fingerprint: <hex sha256 of the served certificate DER, "" over plain HTTP>}`. Refusals:
   `403 pairing-off`, `403 not-local` (caller outside loopback / the firewall's local networks),
   `429 limit` + `Retry-After: 60` (one pending per address+app, 5 pending total, 10/h per address,
   10-minute mute after a reject or a wrong code), `422 invalid`, `501 unsupported`.
3. **Code** (both sides): `code = fmt.Sprintf("%06d", BigEndianUint32(SHA256(nonce_bytes ‖
   client_nonce_bytes ‖ fingerprint_bytes)[0:4]) % 1_000_000)`, `nonce_bytes` = hex-decoded `nonce`,
   `client_nonce` ≥ 16 bytes, `fingerprint_bytes` = SHA-256 of the certificate DER the **client saw**
   (empty over HTTP). The client should compare the answer's `fingerprint` with its own.
4. Poll `GET /api/auth/v1/pairing/request/{id}?client_nonce=<hex>&wait=<s ≤ 30>` with header
   `Authorization: Pairing <poll>`. The first poll must reveal `client_nonce`; the admin sees the
   request only after that. `200` (`Cache-Control: no-store`) `{"state":"pending|approved|rejected|expired"}`;
   `approved` adds `token`, `name`, `scopes`, `access` **once**; afterwards `404 not_found`. Without
   `wait`, faster than 2 s → `429 slow_down` + `Retry-After: 2`. Wrong poll secret → `403 forbidden`;
   nonce not matching the commit → `422 invalid`. Lifetime 5 min, kept 1 more minute for the last poll.
5. `DELETE /api/auth/v1/pairing/request/{id}` with the `Pairing` header → `204`.
6. Admin side (for the fake): the admin approves with `POST /api/auth/v1/pairing/{id}/approve {code}`
   (wrong code → `409 wrong-code`, request rejected and muted; `409 not-ready` before the reveal).
   The paired token has no expiry and no IP ranges; its name is a slug of `app-instance` ≤ 28 chars,
   `-2`, `-3` on collision.

### A.9 What occulited does not offer

System variables, programs, HM-Script, ReGa ids, ReGa alarm messages, the CCU inbox, the WebUI
JSON-RPC (`Session.*`, `Device.*`, `Interface.*`, `SysVar.*`, `Program.*`, `Room.*`), service-message
acknowledgement, `init` with a client callback via lite-rpc, BIN-RPC and CUxD, REST install mode /
inclusion / links, a full value snapshot (`/state` is a chosen set), device descriptions in live
device-list events, a metadata stream over WebSocket, pairing tokens with `backup`/`power`/`radio:keys`/
`addons:write`, rooms and functions as objects with integer ids (replaced by enum node paths that form
trees).

### A.10 Versioning and hermetic testing facts

- API majors in `/version` `capabilities.apis` and in URL prefixes (`/api/meta/v1`, `/api/rpc/v1`,
  `/api/system/v1`, `/api/auth/v1`). The metadata API is normative with its fixtures; system and auth
  APIs are "what exists, kept in step with the code"; scope names freeze at 1.0 (not released). There is
  no changelog in either repository.
- occulited dev mode (`--root <dir>`) does **not** run lite-rpc (the subscriber starts only with root
  `/`): `/interfaces` is `[]`, `/xmlrpc/*` 404, `/events` only `hello` + heartbeats. A real occulited in
  CI would need a container with `--root /` and a real `InterfacesList.xml` — not planned (GPL binary,
  separate decision).

### A.11 occulited docs-vs-code disagreements (the code wins; do not "fix" Loom towards the docs)

1. Docs say the `rpc:*` tiers are used by no route — lite-rpc routes need `rpc:read` and tiers are
   enforced per method.
2. "Tokens never expire" — optional expiry, IP ranges and rotation exist.
3. "Local token has role user" — the local token holds `meta:read` only; tokens carry scopes.
4. `?sid=<10-char alias>` "works" — never accepted on `/api`.
5. "Service messages: interface-level state only" — `/api/system/v1/service-messages` (+ `/stream`) exist.
6. "Off-system integrations must use the XML-RPC proxy on the LAN port" — that proxy is off by default;
   lite-rpc is the intended path.
7. Default listen `127.0.0.1:2121` — it is `127.0.0.1:8183`.
8. `unknown-path` is 404 on read — it is always 422.
9. Unchanged mutation "returns 304 with the unchanged revision" — the body is empty; the revision is
   only in `ETag`.
10. `/health` lists `{ok, version, uptime_s, meta}` — it also sends `release` and `base`.
11. "Every SSE message has `id:`" — `resync` and `?devices=1` snapshots have none.
12. `docs/rpc-remote.md` (cited by the `init` fault) does not exist; the stream's value typing is
    undocumented and differs from the JSON path (A.5).
13. Meta heartbeat described as "empty comment / WebSocket ping" — it is `: ping`, SSE only.
14. "A client that does not read gets `resync overflow` and is disconnected" — detected only at the next
    15 s heartbeat tick after the 1024-slot queue overflowed.

---

