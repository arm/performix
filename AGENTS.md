<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# AGENTS.md

## Scope

This file applies to repository-root and cross-component work in the Arm Performix monorepo.

When working inside a component, also read and follow its guidance:

- [`gui/AGENTS.md`](gui/AGENTS.md) for GUI work.
- [`core/AGENTS.md`](core/AGENTS.md) for core, CLI, engine, agent, client, and Robot work.

If root and component guidance conflict, follow the more specific component guidance for files under that component and call out the conflict in your response.

## Repository structure

- `gui/` contains the Electron desktop application, React renderer, GUI tests, packaging, and GUI release workflows.
- `core/` contains the `apx` CLI, engine daemon, target-side agent, generated clients, Robot tests, and core release workflows.
- `plugin-system/` contains the host-independent visualisation and
  state-interaction framework consumed by the GUI and future hosts.
- `.github/` contains CI workflows, reusable actions, repository automation, and CI documentation.
- `Taskfile.yml` is the root entry point for shared developer workflows and delegates to the component Taskfiles.

## Documentation ownership

Keep information in the document whose role matches it:

- [`README.md`](README.md) provides repository orientation and the documentation map.
- [`DEVELOPMENT.md`](DEVELOPMENT.md) is the source of truth for setup, build, run, generation, test, message, and feature-flag workflows across the components.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) defines pull-request, testing, labelling, and team conventions.
- [`gui/README.md`](gui/README.md), [`core/README.md`](core/README.md), and
  [`plugin-system/README.md`](plugin-system/README.md) explain component
  architecture and source structure; the GUI and core READMEs also explain
  their release boundaries and component-specific testing strategies.
- [`.github/docs/README.md`](.github/docs/README.md) describes CI workflows and reusable actions.
- `AGENTS.md` files contain durable instructions for AI coding agents rather than user-facing development guidance.
- `mise.toml` and `mise.lock` define the optional pinned local development toolchain.

## Local Toolchain

- Use [`DEVELOPMENT.md`](DEVELOPMENT.md) for local setup guidance.
- When a repository command fails because `task`, `node`, `npm`, `go`,
  `python`, `protoc`, or `golangci-lint` is missing or has the wrong version,
  prefer the pinned `mise` toolchain before suggesting ad hoc installs.
- On macOS or Linux, run `./bootstrap`. On Windows, run `./bootstrap.ps1`.
- If `mise` is installed but shell activation is not active, run repository
  commands as `mise exec -- task <task>`.
- `mise` supplies tool versions; `Taskfile.yml` remains the workflow entrypoint.

Do not duplicate repository development procedures in component READMEs. Link to the root guide when a component overview needs to direct readers to a workflow. Keep the root README concise and navigational.

## Working rules

- Read this file and the relevant component guidance before changing files.
- Use the workflows documented in [`DEVELOPMENT.md`](DEVELOPMENT.md). Run `task --list` to discover tasks and prefer root aggregate tasks unless a narrower component task is appropriate.
- Preserve component boundaries. Put shared guidance at the root and component-specific architecture or testing strategy in the relevant component README.
- Keep documentation aligned with the nearest source of truth: Taskfiles for commands, source and configuration for behaviour, and CI workflows for automation.
- `plugin-system/` is work-in-progress guidance only. Do not add or migrate
  plugin code, or integrate it with any host, until boundary enforcement is
  complete and the WIP notice in [`gui/README.md`](gui/README.md#plugin-system-integration)
  is removed.
- Treat generated protobuf clients and engine message codes as derived output. Shared protobuf changes can affect both core and GUI generated bindings; use the documented generation workflow and review the complete generated diff.
- Consider feature flags for risky or incomplete functional changes. Follow the root development guide before changing flag names, defaults, or loading behaviour, then apply the component-specific implementation rules.
- Do not move detailed GUI guidance into core documentation or detailed core guidance into GUI documentation unless the change describes a shared boundary between them.

## Contribution workflow

Follow [`CONTRIBUTING.md`](CONTRIBUTING.md) for pull requests, testing expectations, component and change-type labels, and release metadata. Run the narrowest relevant verification first, then broaden it in proportion to the change's risk.

When creating a GitHub pull request, preserve and complete the pull request body from [`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md). If using `gh pr create`, pass `--template .github/PULL_REQUEST_TEMPLATE.md` or construct the body from that file. Do not replace the template body with `--fill` output.

For pull request titles, follow the Conventional Commits requirement in `CONTRIBUTING.md`. If using `gh pr create`, pass `--title` explicitly unless the autofilled title already satisfies that requirement.
