<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Plugin API

This directory is the boundary between plugins and their host applications. It
owns the contracts that plugin implementations use without depending on host
implementation details.

## Allowed here

- Boundary-owned types, interfaces, and schemas.
- Plugin and contribution descriptor helpers.
- Visualisation and state-interaction contracts and bindings intended for
  plugin authors.
- Small, deliberately curated adapters or reusable components that form part of
  the supported plugin surface.

## Not allowed here

- Imports from the GUI implementation in `../../gui/src/main/`,
  `../../gui/src/preload/`, `../../gui/src/renderer/`, or
  `../../gui/src/common/`.
- Generated GUI types from `../../gui/src/generated/` as part of the public
  plugin contract.
- Concrete plugin implementations from `../plugins/`.
- Plugin Registry implementation from `../plugin-registry/`.
- Large host implementations moved here only to make them accessible to
  plugins.

When a plugin needs live behaviour, define a narrow interface here and inject
its implementation from the host, such as the GUI or a future MCP integration.
Keep the public surface small and expose it through intentional entry points
rather than deep imports.
