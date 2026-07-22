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
| [`api/`](api/) | Protobuf definitions shared by the engine, agents, generated clients, and GUI. |
| [`clients/`](clients/) | Generated client libraries and their generation configuration. |

### Command-line interface

[`apap-cli/`](apap-cli/) is released as the `apx` executable. It is both the user-facing command-line interface and the process manager for the engine daemon.

Its main areas are:

- `cmd/` — Cobra commands for daemon, target, recipe, run, render, support, configuration, and MCP operations.
- `service/` — application services that adapt commands to generated clients and engine operations.
- `recipes/`, `data/`, `tools/`, and `tool-integrations/` — runtime content packaged with release archives.
- `main.go` — executable entry point.

### Engine

[`apap-engine/`](apap-engine/) is the shared Go implementation shipped by the core release. It is compiled into the daemon started by `apx`, rather than distributed as a second user-facing executable.

Its main responsibilities include:

- exposing the public API through `grpcserver/`;
- parsing and executing recipes through `recipe/`, `recipeparser/`, and `conductor/`;
- managing targets, sessions, tools, and collectors;
- storing, querying, importing, exporting, and rendering runs; and
- translating user-facing failures through the message catalogue.

The renderer regression suite under [`apap-engine/regressiontests/`](apap-engine/regressiontests/) protects the engine's analysis output.

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

Core testing follows the boundaries between reusable packages, released components, and target-backed product workflows:

- Go and Python unit tests exercise packages close to their implementation, with focused suites for the CLI and engine.
- Renderer regression tests protect stable engine analysis output using recorded run data.
- Robot Framework tests validate CLI and engine behaviour against real Linux, Windows, and Android targets.
- Evaluation and benchmarking suites measure accuracy, overhead, workload compatibility, and AI Insights quality outside the functional test layers.

Commands belong in the [repository development guide](../DEVELOPMENT.md#core-workflow). Suite-specific requirements live in the [Robot guide](robot/README.md), the [renderer regression guide](apap-engine/regressiontests/README.md), and the [AI Insights evaluation guide](scripts/ai-insights-evaluation/README.md).

## Release boundary

The CLI, engine, target-side agent, runtime content, and Go client form the core release surface. The engine implementation is distributed through `apx`, while `apx-agent` and collection tools are packaged as deployable target assets. The GUI records its compatible core version in the `engineVersion` field of [`../gui/package.json`](../gui/package.json).

Engine versions follow `MAJOR.MINOR.PATCH` semantics. Between product releases, `feature` increments the minor version and `bugfix` increments the patch version for pull requests labelled `core`; other change-type labels do not affect the engine version. The highest core impact wins. See the [labelling guidance](../CONTRIBUTING.md#labels) for choosing pull-request labels.

Release preparation records the calculated engine version in the GUI metadata. CLI release builds package the generated GitHub release notes as `CHANGELOG.md` for compatibility with existing distributions.

Release artifacts are published from the [Arm-Debug/performix releases](https://github.com/Arm-Debug/performix/releases) page. Core licensing is described in [`LICENSE`](LICENSE), with distribution terms under [`license_terms/`](license_terms/).

## Documentation

| Guide | Use it for |
| --- | --- |
| [`clients/README.md`](clients/README.md) | Client generation and distribution. |
| [`robot/README.md`](robot/README.md) | Robot Framework functional tests. |
| [`apap-engine/regressiontests/README.md`](apap-engine/regressiontests/README.md) | Renderer regression tests. |
