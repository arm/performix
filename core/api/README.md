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

[`dependencies.go`](dependencies.go) retains the dependencies used by generated Go
bindings and mocks so Dependabot can update this module before source generation.
Each generated package also has a `dependencies.go` file so Dependabot can
resolve imports from other modules before source generation. These files mirror
the imports in the generated sources; update them when generated imports change.
Their `dependabot` build tag excludes them from normal builds. `go mod tidy`
considers all build tags except `ignore`, so it can discover the packages without
requiring generated type definitions.

See the [development guide](../../DEVELOPMENT.md#core-workflow) for generation
commands.
