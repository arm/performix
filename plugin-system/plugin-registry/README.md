<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Plugin Registry

This directory is the composition boundary between plugins and their host
applications. It assembles known plugins and exposes registration data to the
GUI or another host integration.

## Allowed here

- The explicit list of in-repository plugin entry modules.
- Registry construction and validation using contracts from `../plugin-api/`.
- Composition code that validates and exposes registered contributions.
- Tests for registry assembly and validation.

## Not allowed here

- Plugin implementations that belong in `../plugins/`.
- Public plugin contracts that belong in `../plugin-api/`.
- Imports from the GUI implementation in `../../gui/src/main/`,
  `../../gui/src/preload/`, `../../gui/src/renderer/`,
  `../../gui/src/common/`, or `../../gui/src/generated/`.
- Registration through global state or other import-time side effects.
- External plugin discovery or loading unless that capability is explicitly
  designed and approved later.

This is the only production source area allowed to import concrete entry
modules from `../plugins/`. The GUI imports the assembled registry rather than
individual plugins.
