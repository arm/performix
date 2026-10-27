<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Shared protobuf API

This directory contains the protobuf definitions shared by the CLI, engine,
target-side agent, and GUI. The Go module provides protobuf messages and gRPC
client and server interfaces. Local modules reference it through `go.mod`
replacement directives.

Go bindings are generated beside their `.proto` sources, with test mocks generated
under `mocks/` using [`.mockery.yaml`](.mockery.yaml). These generated Go files are
ignored by Git. GUI TypeScript bindings are generated under
`gui/src/generated/engine-api/` instead.

See the [development guide](../../DEVELOPMENT.md#core-workflow) for generation
commands.
