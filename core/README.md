<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Arm Performix Core

The core component contains the command-line, engine, and target-side software that powers Arm Performix. It also owns the shared gRPC API, generated clients, compatibility data, and functional test infrastructure used by the GUI and other clients.

## Runtime model

The core runtime is split across the host and the profiling target:

1. A user or application invokes `apx` or connects with a generated gRPC client.
2. `apx` starts the local engine daemon when required. The CLI and other clients then communicate with that daemon over gRPC.
3. The engine validates and executes recipes, coordinates target access, manages run data, and prepares analysis results.
4. When a recipe needs target-side services, the engine deploys and controls `apx-agent` and the required collection tools on the target.

The GUI follows the same model: its Electron main process invokes the CLI for command-oriented operations and uses generated TypeScript bindings to call the engine API directly.

## Source structure

| Path | Purpose |
| --- | --- |
| [`apap-cli/`](apap-cli/) | `apx` command-line interface, engine-daemon process management, and packaged runtime content. |
| [`apap-engine/`](apap-engine/) | Shared engine implementation, gRPC server, recipe execution, run storage, and rendering. |
| [`atperf-agent/`](atperf-agent/) | Target-side `apx-agent` implementation for managed target operations. |
| [`api/`](api/) | Shared protobuf definitions, Go bindings, generated test mocks, and their generation configuration. |

### Command-line interface

[`apap-cli/`](apap-cli/) is released as the `apx` executable. It is both the user-facing command-line interface and the process manager for the engine daemon.

Its main areas are:

- `cmd/` — Cobra commands for daemon, target, recipe, run, render, support, configuration, and MCP operations.
- `service/` — application services that adapt commands to generated clients and engine operations.
- `recipes/`, `data/`, `tools/`, and `tool-integrations/` — runtime content packaged with release archives.
- `main.go` — executable entry point.

NeoProf timelines in Code Hotspots and System Utilization share bin-catalogue,
capture-domain and viewport-range helpers in `apap-cli/recipes/lib/`. Sources
retain compressed counter intervals; chart queries select a bin level and expand
only the requested range. Each recipe owns its metric calculations and chart
groups. System Utilization keeps its Summary and embedded overview on a separate
100 ms table; its Python/CSV collector uses the existing timeline path.

### Engine

[`apap-engine/`](apap-engine/) is the shared Go implementation shipped by the core release. It is compiled into the daemon started by `apx`, rather than distributed as a second user-facing executable.

Its main responsibilities include:

- exposing the public API through `grpcserver/`;
- parsing and executing recipes through `recipe/`, `recipeparser/`, and `conductor/`;
- managing targets, sessions, tools, and collectors;
- storing, querying, importing, exporting, and rendering runs; and
- translating user-facing failures through the message catalogue.

Logical counter capabilities are defined in
[`apap-engine/capabilities/definitions.json`](apap-engine/capabilities/definitions.json).
The engine embeds this catalogue and uses its IDs, component types, titles, and
units for request, discovery, and publication validation. A new CPU counter using
the existing schema needs a catalogue entry; a new component schema also needs a
validator. CPU state and series-binding rules remain in `capabilities/cpu.go`.

The renderer regression suite under [`apap-engine/regressiontests/`](apap-engine/regressiontests/) protects the engine's analysis output.

#### Structured execution helpers

`conductor.ExecutionRunner` provides `RunProcess` to launch a program with literal
arguments and `RunScript` to run a script with an explicitly selected shell.
Local runners and `tool.AgentEngine` implement these helpers and return captured
stdout, stderr and exit status. Existing `CommandRunner.RunCommand(string)` callers
remain unchanged. Windows scripts support PowerShell 5.1 and cmd.
Cmd scripts deliberately reject nonempty `ScriptRequest.Arguments` with
`ExecutionUnsupported`: batch-file expansion does not guarantee literal values.
Use PowerShell when a Windows script needs literal arguments.
The embedded [`execution_windows.ps1`](apap-engine/conductor/execution_windows.ps1)
wrapper starts the selected Windows shell, preserves literal PowerShell arguments
and exit status, and writes cmd source into a process-owned temporary directory.
Windows PowerShell 5.1 (`powershell.exe`) is the supported interpreter; PowerShell 7
(`pwsh.exe`) has different execution behaviour and is not selected by this API.
See the [execution API](apap-engine/conductor/execution.go) for the request and
error contracts.

### Process lifetime ownership

Windows launches start suspended, join an owned Job Object, and resume after
assignment. Managed kills, forced shutdowns, and parent exit terminate remaining
descendants. Managed kills and forced shutdowns retain exit status `1`.
The shared launch wrapper releases the job when waiting completes or startup
fails. Unix managed launches keep their existing group-controller behavior.

This is process-tree ownership machinery for execution helpers. It does not
implement Windows Group Controller support or root-worker privilege escalation.
A future Windows Group Controller can build on the same Job Object ownership.

### Target-side agent

[`atperf-agent/`](atperf-agent/) is released as the target-side `apx-agent` binary. The engine deploys it as a managed tool when a profiling workflow needs richer target operations than the base remote connection provides.

Its main areas are:

- `cmd/` — agent process and worker entry points;
- `grpcserver/` — target-agent RPC implementation and transport;
- `process/`, `filetransfer/`, and `fsutil/` — controlled target operations;
- `privilege/` — privilege discovery and elevation support; and
- `systeminfo/` — target and CPU information.

Release packaging for `apx` and `apx-agent` is defined by [`core/.goreleaser.yml`](.goreleaser.yml) and [`core/.goreleaser-agent.yml`](.goreleaser-agent.yml).

## Supporting structure

| Path | Purpose |
| --- | --- |
| [`atperf-compatibility/`](atperf-compatibility/) | Compatibility rules for recipes, runs, and component versions. |
| [`atperf-version/`](atperf-version/) | Shared engine and bundled-tool version definitions. |
| [`robot/`](robot/) | Target-backed functional tests written with Robot Framework. |
| [`scripts/`](scripts/) | Build, generation, release, evaluation, and test utilities. |
| [`license_terms/`](license_terms/) | Product licence terms, redistributables, and third-party notices. |
| [`diagrams/`](diagrams/) | Source diagrams for core design documentation. |

## Testing strategy

See [TEST.md](../TEST.md) for the complete list of Performix test suites.

Go tests cover engine, CLI, and target-side agent packages alongside their implementation. Python tests cover built-in tools and supporting scripts. JavaScript recipes and tool integrations use the [Goja test harness](apap-engine/jstest/README.md).

Commands belong in the [repository development guide](../DEVELOPMENT.md#core-workflow). Suite-specific requirements live in the [Robot guide](robot/README.md), the [renderer regression guide](apap-engine/regressiontests/README.md), and the [AI Insights evaluation guide](scripts/ai-insights-evaluation/README.md).

Execution contract tests compare values reported by a native probe. Local tests
and an in-process gRPC server exercise real child processes, output, cancellation
and handle release. The in-process server excludes the Linux agent's self-reexec
group controller; its own tests cover that path. Windows process tests also check
batch-file rejection after executable lookup. Cross-compilation is not runtime
platform evidence. See the [execution test workflow](../DEVELOPMENT.md#execution-helper-tests).

## Release boundary

The CLI, engine, target-side agent, and runtime content form the core release surface. The engine implementation is distributed through `apx`, while `apx-agent` and collection tools are packaged as deployable target assets. The GUI records its compatible core version in the `engineVersion` field of [`../gui/package.json`](../gui/package.json).

Engine versions follow `MAJOR.MINOR.PATCH` semantics. Between product releases, `feature` increments the minor version and `bugfix` increments the patch version for pull requests labelled `core`; other change-type labels do not affect the engine version. The highest core impact wins. See the [labelling guidance](../CONTRIBUTING.md#labels) for choosing pull-request labels.

Release preparation records the calculated engine version in the GUI metadata. CLI release builds package the generated GitHub release notes as `CHANGELOG.md` for compatibility with existing distributions.

Release artifacts are published from the [Arm-Debug/performix releases](https://github.com/Arm-Debug/performix/releases) page. Core licensing is described in [`LICENSE`](LICENSE), with distribution terms under [`license_terms/`](license_terms/).

## Documentation

| Guide | Use it for |
| --- | --- |
| [`api/README.md`](api/README.md) | Shared protobuf definitions and Go bindings. |
| [`robot/README.md`](robot/README.md) | Robot Framework functional tests. |
| [`apap-engine/regressiontests/README.md`](apap-engine/regressiontests/README.md) | Renderer regression tests. |
