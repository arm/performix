<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Plugin System

The plugin system is a host-independent framework for visualisations and
state-driven interactivity. The GUI is its first consumer, but the component
does not depend on GUI implementation details so that other hosts, such as an
MCP server, can integrate it in future.

## Source structure

| Path                                   | Purpose                                                                 |
| -------------------------------------- | ----------------------------------------------------------------------- |
| [`plugin-api/`](plugin-api/)           | Public contracts, schemas, bindings, and host capability interfaces.    |
| [`plugins/`](plugins/)                 | Concrete, in-repository visualisation and interaction implementations.  |
| [`plugin-registry/`](plugin-registry/) | Composition boundary that assembles the available plugin entry modules. |

## Dependency policy

Dependencies point towards the public Plugin API. Plugins use only that API
and approved third-party dependencies. The Plugin Registry imports each
plugin's public entry module and exposes the assembled result to a host:

```text
the GUI ───────> plugin-registry ───────> plugins
    │                  │                    │
    └──────────────────┴───> plugin-api <───┘
```

The GUI may import the public Plugin API and the assembled Plugin Registry, but
not concrete plugin implementations. Plugin system code must not import GUI
implementation code. When a plugin needs a host capability, define a narrow
contract in the Plugin API and inject its implementation from the host.

The Plugin Registry is the only production source area permitted to import
concrete plugin entry modules. Each subdirectory README describes what belongs
inside its boundary.
