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

## Which model does which work

Set by the owner for the go-fabric family on 2026-10-10 (go-fabric's
`CLAUDE.md`); the review split of 2026-11-08 (go-fabric #456) likewise.
Adopted here on 2026-11-08; re-cut by the owner on 2026-11-15, mirrored
from go-fabric #529 and openccu-loom #1108 (in the owner's words: "Wichtig
ist, dass die Qualität hoch ist, aber Fable gezielt für die Dinge
eingesetzt wird, die es wirklich besser kann"). The bar is unchanged: a
statement about the CCU's wire behaviour that lands in this repo is cited
later as fact — a comment that names a caller or a consumer, a docstring
that states what the other side does, a `CHANGELOG.md` line — so it comes
from the strongest model. But the strongest model does only the part that
needs it: reading the reference and judging. The writing, the mechanics
and the code review are Opus. The agents in
[Sub-Agent Delegation](#sub-agent-delegation) carry a default model each;
the rows below decide when a call overrides it (`model:` on the Agent
tool). Implementation defaults to Opus.

| Work | Model |
| --- | --- |
| **Coordination** of a slice: starting agents, integrating, guards, pull requests, status | **Opus**, in a fresh session per slice (the state is in the notes and the memory, never in a long context) |
| **Planning and decisions:** the slice plan; the decision briefs for the owner; *which* guard gets built and its bite line; the verdict where the CCU firmware (OpenCCU), a client's recorded expectation (aiohomematic, gohomematic, openccu-loom), hm-simulator and `pkg/litefake/CONTRACT.md` contradict each other | **Fable**, as a targeted agent |
| **Reading the reference and judging:** what the CCU firmware (OpenCCU) does on XML-RPC, JSON-RPC or BIN-RPC, what a client expects (aiohomematic, gohomematic, openccu-loom), what hm-simulator does for the scenario API, and what `pkg/litefake/CONTRACT.md` states — for `DOCUMENTATION.md`, `pkg/litefake/CONTRACT.md`, `CHANGELOG.md` and every comment or docstring that names a CCU wire behaviour, a client's expectation, a caller or a consumer; the adversarial read of the `hunt` agent | **Fable** gives the verdict with the lines as a short fact note; **Opus** writes the entry or the comment in the file's form; **Fable** reads the finished text once against its note. Every reference statement comes from Fable |
| **Implementation**, also on the paths the next section keeps out of an agent's hands (the XML-RPC, JSON-RPC and BIN-RPC envelopes, `internal/session`, `pkg/litefake/` and its GPL-3.0 boundary — nothing copied from occulited); a device behaviour simulator, a device definition in `internal/embed/data/`; tests; `CHANGELOG.md` and docs; rebases and merge conflicts | **Opus** (the `impl` default; also what fast mode runs on) — with the Fable read of the next row before it merges where it touches one of those paths or mirrors a reference |
| **Review** of a slice's pull request | **Opus** does the code review — correctness, concurrency, error paths, tests, the fixed report shape. **Fable** reads on top, bounded: behaviour against the CCU firmware (OpenCCU), a client's recorded expectation, hm-simulator and `pkg/litefake/CONTRACT.md`, the paths of the row above, and every finding of high or medium severity for confirmation before it reaches an implementer |
| **Citations** | **Opus** checks every range (the file, the lines, the construct the sentence names). **Fable** reads every citation that is the reason for behaviour ("as the CCU does", "aiohomematic expects", "taken from hm-simulator"), every doubtful one and a sample of the rest |
| Work a test or generator checks immediately: `make fmt`, a table case; search and first reading of a tree whose result a stronger model or the owner reads again | **Sonnet** (the `guard` default; `impl` with `model: "sonnet"`) |
| Mechanical inventories whose answer is a list the caller can re-grep: which files reference X, which links are dead, which entries lack a counterpart | **Haiku** (`sweep` default) |

Sonnet never writes a claim about a CCU XML-RPC, JSON-RPC or BIN-RPC
behaviour, or a client's expectation into the repo. Opus does not either:
it writes what Fable's fact note states. Haiku returns lists, never
verdicts. When in doubt whether a statement is a reference fact, it is one.

---

## Sub-Agent Delegation

As go-fabric's and openccu-loom's rule (owner, 2026-10-10). The main
conversation coordinates: integration, guards, sub-agent steering, final
verification, and push / pull request / tag. Planning, contract and wire
decisions and guard specifications come from Fable (the planning row of
[Which model does which work](#which-model-does-which-work)) and reach the
coordinator as a plan or a fact note. It still does not own typing.

**Delegate what a gate can prove; keep what only a reader can accept.**
Delegate freely: table cases, fakes, scaffolding, fixture updates, doc and
link cleanups, version bumps across the changelog set, inventories and grep
sweeps. Never delegate to an agent: the wire envelopes, the litefake
contract and its licence boundary, session handling — those are
implemented in the coordinating session, and the stop conditions of `impl`
(`.claude/agents/impl.md`) keep them out; *which* guard gets built is
Fable's. Locating is delegable; reading is not.

| Agent | Default model | Use for |
|---|---|---|
| `impl` | Opus | scoped implementation with a stated acceptance command |
| `guard` | Sonnet | tests from a caller-written guard spec, plus the bite proof |
| `sweep` | Haiku | read-only inventories and grep sweeps, high fan-out |
| `hunt` | Fable | adversarial read-only defect hunt, ranked candidates |

Each default is one row of
[Which model does which work](#which-model-does-which-work); a call that
falls into another row passes `model:` on the Agent tool — `impl` with
`model: "sonnet"` for work a generator or formatter checks immediately.

**Size CPU-bound fan-out from the host, never from a constant** — the owner
works from a 4-core box and a 14-core box:

```sh
cores=$(nproc); agents=$(( (cores - 1) / share ))   # share = cores per agent
```

Check the 1-minute load first; if it is at or above the core count, run one
CPU-bound agent and queue the rest. Eight concurrent agents is a practical
ceiling whatever the core count. Every CPU-bound agent must **pin** its share
in the command it runs (`GOMAXPROCS=<share> go test -p <share> ./internal/x/...`, `golangci-lint run --concurrency=<share>`) — handing out a number without
pinning it changes nothing. Read-only agents cost no core worth counting;
6–8 is a practical ceiling there.

Two hard rules: **no sub-agent runs the full gate (`make test` (race, cover), `make lint`) or a
repo-wide lint** — the full gate runs once, in the main conversation, at the
end of the slice — and **no two writing agents in the same package**
(disjoint file sets, or `isolation: "worktree"`).

Every brief carries five things: the files and public surface plus one style
reference; the acceptance command the agent runs itself; the invariants it
touches; **stop conditions** (new dependency, API or wire change, credential
handling, an ADR-shaped decision — a stop is a successful outcome); and a
report format ≤ 250 words, large artefacts to the scratchpad as a path.

**A report is a claim.** Read the diff, run the gate yourself. For `guard`,
the acceptance artefact is not a green test but the **bite proof**: the named
production line removed, the test observed red with its message, the line
restored, green again.

---

## Token use

Set by the owner on 2026-10-14 ("Wichtig ist mir eine effiziente Nutzung
der Token"), as go-fabric's rule of the same date. A slice costs on
the order of half a million to a million tokens per implementing agent
before the owner's review; the rules below are what keeps that from
doubling.

- **Read slices, not files.** `sed -n a,bp`, `grep -n` with a tight
  pattern and `head`; a whole file only when the task is the file. Never
  re-read what is already in context; cite the earlier read.
- **Delegate first reads.** A source tree's inventory (the CCU firmware, aiohomematic, openccu-loom, hm-simulator, this repo)
  is a `sweep` or Sonnet read written to the scratchpad with `file:line`
  for every fact; the owner spot-checks the load-bearing lines and verifies
  every fact about the CCU's wire behaviour before it enters a brief or
  `DOCUMENTATION.md`, `pkg/litefake/CONTRACT.md` or `CHANGELOG.md` — the agent's result is a map, never a citation.
- **Logs by their summary.** The gate (`make test` (race, cover), `make lint`) runs in the
  background with a summary file (one line per gate, `FAIL`/`ok` lines
  only); read the summary, and open a log only on failure, with `grep` for
  the failure markers and the error strings — never `cat`. The same for CI:
  the job log is fetched once to a file and grepped.
- **One turn, every independent call.** Reads, checks and background
  starts that do not depend on each other go out together; a wait is a
  `Monitor` or an `until` loop in the background, not polling by hand.
- **Short artefacts.** A brief names files, decisions and the acceptance
  command, not prose; a report follows the fixed shape of
  [Sub-Agent Delegation](#sub-agent-delegation) (≤ 250 words, large
  artefacts as a scratchpad path); status messages to the owner are short,
  outcome first, in German.
- **Skip what cannot have changed.** No regeneration for a comment change;
  no second gate run for a notes-only commit — say what was skipped and
  why.
- **Edit by script.** Several replacements in several files are one
  scripted call with asserted anchors, not a chain of interactive edits;
  a generated or formatted artefact is regenerated, never patched.
- **The strongest model for the part that needs it.** Set by the owner on
  2026-11-15. Fable reads references, judges and plans (the table above);
  it does not coordinate, format, write prose or run mechanics. A Fable
  agent's brief is bounded: the lines to read, the question to answer, a
  fact note back.
- **Few turns in a long session.** A coordinator waits for several agents
  at once and integrates them together; a status message carries new
  information or is not sent; a slice starts in a fresh session.
- **The assistant says when to clear the context.** Set by the owner on
  2026-11-15. It does not wait to be asked: it proposes a fresh session
  when a slice or a pull request is merged; when the context has been
  summarised once and the work that remains is of another kind; or when
  what it still needs fits in a short hand-over and the turns are mostly
  waiting for agents or pipelines. It does NOT propose one while agents of
  the session are running or a decision of the owner is open. The proposal
  comes with the hand-over already written — the state, the open points
  with where each goes, the next step, the lessons — into the memory file
  and the notes (never only into the chat), and with the exact way to go
  on: `/clear`, the model to switch to (`/model`), and the first message
  to send, which names the files to read.
- **The pipeline's guards before the pull request, not after.** The local
  guard set is the commands CI runs (`.github/workflows/ci.yml`):
  `gofmt -l .` (no `make` target checks it; `make fmt` rewrites),
  `make vet`, `make lint`, `make test` and `make build`. A red pipeline
  costs a round of coordinator turns.
- **Agents read what the brief names.** An implementing agent gets the
  file list and the pinned sources in the brief, reads those, and reports
  in the fixed shape; it does not survey the tree. One agent per brief;
  two agents only on disjoint paths, at most one Opus agent at a time.
- **Push, pull request and tag are the session's own calls.** An
  implementing agent works locally — branch, commits, guards — and
  reports; the owner's session reads the diff, runs the gate, pushes,
  opens the pull request and tags the merge commit; the owner merges. A
  delegated push, pull request or tag is refused by the auto-mode
  classifier ("Create Public Surface", 2026-11-07), and the review in
  [Which model does which work](#which-model-does-which-work) sits
  between the agent's report and the push anyway.

---

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
