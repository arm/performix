<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Development

This document describes the development workflow for Arm Performix. It is intended for developers who are setting up a local development environment, contributing to the repository, or debugging issues. For general contribution guidance, see [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Prerequisites

### Package access

- `GITHUB_TOKEN` for Arm-Debug packages and release assets. Create this from your GitHub account settings.
- `FONTAWESOME_PACKAGE_TOKEN` for licensed GUI packages.
- `ARTIFACTORY_API_TOKEN` for bundled tools. Create this from your Artifactory account settings.

Use Arm's npm mirror and private package registries.

Configure `~/.npmrc` with the following content:

```ini
registry=https://artifactory.arm.com/artifactory/api/npm/mirrors.npmjs_org
//npm.pkg.github.com/:_authToken=${GITHUB_TOKEN}
//npm.fontawesome.com/:_authToken=${FONTAWESOME_PACKAGE_TOKEN}
```

### (Manual version) Tool install

Install the following tools (alternatively, skip this and use the automated install via `mise` detailed in the next section):

- [`task`](https://taskfile.dev/installation/) for common developer workflows.
- [Node.js](https://nodejs.org/) and [npm](https://www.npmjs.com/)
- [Golang](https://golang.org/)
- [Protobuf](https://protobuf.dev/installation/)
- A C compiler toolchain such as GCC or Clang

Access to the Arm network or VPN is required for the configured internal npm mirror and some Artifactory dependencies.

### (Automated version) Automatic tool install via `mise`

Performix includes an optional [mise](https://mise.jdx.dev/) configuration for
setting up your local development environment, by installing the necessary
versions of required tools (task, node.js, golang, ...) in a user-specific
location (without interfering with system-wide tools).

On macOS / Linux run:

```shell
./bootstrap
```

On Windows, run:

```powershell
.\bootstrap.ps1
```

The bootstrap script uses an existing suitable `mise` if one is available. If
not, it installs a user-local `mise` from GitHub Releases.

It then runs `mise install` to install the required tool versions in a
user-specific location (default `.local/share/mise/installs`). The tools are
available through `mise exec`; if shell activation is enabled or already
configured, mise also adds them to your path when inside the `performix`
directory.

## First-time setup

From a new checkout, prepare both components from the repository root:

```shell
task install
```

This installs dependencies, regenerates derived sources, and builds the Core and GUI components.
Repository-wide Python development tools are installed from `requirements-dev.txt`
into the root `.venv` by `task deps`.

Once the checkout is prepared, run the unit-test aggregate with:

```shell
task test
```

## Task conventions

Run `task --list` from the repository root to discover tasks. Use `task <task> --summary` for a task's description and any supported arguments.

Tasks use a common vocabulary:

| Term       | Meaning                                                                                     |
| ---------- | ------------------------------------------------------------------------------------------- |
| `deps`     | Install dependencies required by a component.                                               |
| `generate` | Regenerate derived sources such as protobuf clients and engine message codes.               |
| `build`    | Build prepared sources without installing dependencies.                                     |
| `test`     | Run tests against a prepared component; more specific suites add another namespace segment. |
| `lint`     | Run the configured static-analysis and formatting workflow.                                 |
| `install`  | Run the full preparation sequence: dependencies, generation, then build.                    |

Task names use colon-delimited namespaces:

- root tasks use an action such as `task build` and operate across Performix;
- component tasks use `<component>:<action>`, such as `task gui:build` or `task core:build`; and
- focused tasks add a scope, such as `task gui:test:e2e` or `task core:build:apx`.

Tasks such as `deps`, `generate`, `build`, and `test` are intentionally atomic. For example, `build` expects dependencies to be installed and generated sources to be current. Use `install` when those prerequisites have not been prepared.

## Core workflow

Prepare the CLI, engine, target-side agent, and core dependencies with:

```shell
task core:install
```

Once prepared, use the narrower tasks as needed:

| Task                                        | Purpose                                                                             |
| ------------------------------------------- | ----------------------------------------------------------------------------------- |
| `task core:deps`                            | Prepare the Python environment, bundled tools, and Node dependencies.               |
| `task core:build`                           | Build `apx` and the target-side agent.                                              |
| `task core:build:apx`                       | Build only the `apx` executable.                                                    |
| `task core:build:agent`                     | Build only the target-side agent.                                                   |
| `task core:test:unit`                       | Run all core unit tests.                                                            |
| `task core:test:unit:engine`                | Run engine Go unit tests.                                                           |
| `task core:test:unit:apx`                   | Run CLI Go unit tests.                                                              |
| `task core:test:unit:scripts`               | Run core Python script unit tests.                                                  |
| `task core:test:unit:sysutil-timeline`      | Run sysutil-timeline Python unit tests.                                             |
| `task core:test:robot TARGET=<target-name>` | Run Robot functional tests against a configured target.                             |
| `task core:lint`                            | Run core linters and apply supported fixes.                                         |
| `task core:clean`                           | Remove local core build outputs.                                                    |

The supported local `apx` development build uses the `duckdb_arrow` and `confidential_telemetry` build tags. To build with the public-safe feature set used by default release builds, override the tags:

```shell
task core:build:apx APX_BUILD_TAGS=duckdb_arrow
```

Plain `go build` does not select the required build tags. When diagnosing outside Task, run the equivalent development command from `core/apap-cli/`:

```shell
go build -tags=duckdb_arrow,confidential_telemetry -o apx
```

Start and stop the locally built daemon with:

```shell
task core:start
task core:stop
```

Stop an existing daemon before testing a rebuilt executable so commands do not continue talking to the previous process.

Core testing layers and their ownership are described in [`core/README.md#testing-strategy`](core/README.md#testing-strategy). See the [Robot guide](core/robot/README.md) and [renderer regression guide](core/apap-engine/regressiontests/README.md) for suite-specific requirements.

## GUI workflow

Prepare the Electron application with:

```shell
task gui:install
```

Once prepared, use the narrower tasks as needed:

| Task                 | Purpose                                                   |
| -------------------- | --------------------------------------------------------- |
| `task gui:deps`      | Install npm dependencies.                                 |
| `task gui:build`     | Build the Electron main, preload, and renderer bundles.   |
| `task gui:watch`     | Run the application in development mode with the watcher. |
| `task gui:start`     | Start an already-built development application.           |
| `task gui:test:unit` | Run Vitest unit tests.                                    |
| `task gui:test:e2e`  | Build the GUI and run the default Playwright E2E project. |
| `task gui:lint`      | Run type, formatting, lint, and dependency checks.        |
| `task gui:clean`     | Remove GUI build artifacts.                               |

### Run the GUI with a local Core build

When the application runs from source, it looks for `core/apap-cli/apx` (`apx.exe` on Windows). Prepare core before starting the GUI:

```shell
task core:install
task gui:watch
```

If core dependencies and generated sources are already prepared, `task core:build:apx` is sufficient. Set `ARM_PERFORMIX_CLI_PATH` to test the GUI with another compatible `apx` executable; the value must be the executable path, not its containing directory.

GUI testing layers and their ownership are described in [`gui/README.md#testing-strategy`](gui/README.md#testing-strategy). See the [end-to-end testing guide](gui/docs/end-to-end-tests.md) for Playwright target, test-data, performance, soak, and stress details.

## Engine messages

User-facing engine messages are defined in [`core/apap-engine/message/catalog_en-US.json`](core/apap-engine/message/catalog_en-US.json) and raised through the engine `Message` type.

GUI Message overrides can be defined in [`gui/src/common/message-catalogues/en-US.json`](gui/src/common/message-catalogues/en-US.json)

After changing the catalogue, run `task core:generate` to update the generated message codes. Keep low-level diagnostics in logs or wrapped causes rather than hardcoding them into user-facing strings.

## Feature flags

Feature flags should be considered for large, risky, or incomplete functional changes. GUI and core flags have separate loading mechanisms and may need to be coordinated when a feature crosses the process boundary.

### GUI feature flags

GUI flags are defined in [`gui/src/common/feature-flag.ts`](gui/src/common/feature-flag.ts) and loaded once during application startup.

For local development, create `feature-flags.json` with only the overrides you need:

```json
{
  "aiChatbot": true
}
```

Set `ARM_PERFORMIX_FEATURE_FLAGS_PATH` to read overrides from another file. Packaged applications use `feature-flags.json` in Electron's user-data directory. Restart the application after changing a flag.

### Core feature flags

Core feature switches are registered with Viper in [`core/apap-cli/cmd/root.go`](core/apap-cli/cmd/root.go), with defaults under [`core/apap-cli/cmd/serverconfig/defaults.go`](core/apap-cli/cmd/serverconfig/defaults.go). Configuration can be supplied by a CLI flag, an `APXD_`-prefixed environment variable, or the CLI YAML configuration file.

For example, `enable-experimental-recipes: true` maps to `APXD_ENABLE_EXPERIMENTAL_RECIPES=true`. Restart the daemon after changing a value read during startup. Use `apx config print` to inspect supported values and their effective configuration.

## Further reading

- [`core/README.md`](core/README.md) — core runtime, released components, and testing strategy.
- [`gui/README.md`](gui/README.md) — GUI architecture, source layout, and testing strategy.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — pull-request and testing expectations.
- [`.github/docs/README.md`](.github/docs/README.md) — CI workflow and PR-check behaviour.
