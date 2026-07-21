<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

![Arm Performix](docs/images/arm-performix-header-dark.png#gh-dark-mode-only)
![Arm Performix](docs/images/arm-performix-header-light.png#gh-light-mode-only)

# Arm Performix

Arm Performix is a performance analysis toolkit for developers building on Arm-based infrastructure. It combines target-side data collection with guided analysis, function-level insights, and desktop visualisations.

## Repository structure

| Path                           | Purpose                                                                                          |
| ------------------------------ | ------------------------------------------------------------------------------------------------ |
| [`gui/`](gui/)                 | Electron desktop application, React user interface, packaging, and GUI tests.                    |
| [`core/`](core/)               | The `apx` CLI, engine daemon, target-side agent, shared APIs, generated clients, and core tests. |
| [`.github/`](.github/)         | CI workflows, reusable actions, release automation, and repository policy.                       |
| [`Taskfile.yml`](Taskfile.yml) | Shared entry point for repository and component development tasks.                               |

## Documentation Map

| Guide                                                                                                           | Use it for                                               |
| --------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| [Performix Repository Guide](https://confluence.arm.com/spaces/ITS/pages/2844177236/Performix+Repository+Guide) | A concise orientation to the repository.                 |
| [`DEVELOPMENT.md`](DEVELOPMENT.md)                                                                              | Setup and development workflows                          |
| [`CONTRIBUTING.md`](CONTRIBUTING.md)                                                                            | Pull requests, testing expectations and team conventions |
| [`gui/README.md`](gui/README.md)                                                                                | GUI architecture and directory map.                      |
| [`core/README.md`](core/README.md)                                                                              | Core architecture and directory map.                     |
| [`.github/docs/README.md`](.github/docs/README.md)                                                              | Automated CI workflows and actions                       |
| [`AGENTS.md`](AGENTS.md)                                                                                        | Repository guidance for AI coding agents.                |
