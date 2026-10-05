<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Plugins

This directory owns concrete, in-repository visualisation and state-interaction
plugins. Each plugin is self-contained and consumes the supported surface
exported by `../plugin-api/`.

## Allowed here

- Concrete plugin descriptors and contribution implementations.
- Plugin-local components, helpers, assets, schemas, and tests.
- Imports from the public Plugin API and approved third-party dependencies.

## Not allowed here

- Imports from the GUI implementation in `../../gui/src/main/`,
  `../../gui/src/preload/`, `../../gui/src/renderer/`,
  `../../gui/src/common/`, or `../../gui/src/generated/`.
- Imports from `../plugin-registry/`.
- Registration through global state or other import-time side effects.
- Shared contracts that belong in `../plugin-api/`.

Concrete plugin entry modules are assembled by `../plugin-registry/`; the GUI
must not import them directly.
