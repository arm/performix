<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# API code generation

The protocol buffer compiler (`protoc`) along with language plugins is used to generate the
API for both the server part and Go client bindings.

From the repository root, `task core:generate` regenerates the Go protobuf outputs and mocks under `clients/go`.
CI also regenerates and commits generated protobuf artifacts for protobuf changes on pull requests.

## Go

The Go client library is distributed using GitHub, and GitHub tags are used to make a release:

* When an updated protobuf definition is submitted, the CI will regenerate the client code and commit it back to GitHub.
* When a release is made a tag is created in GitHub taking the form `clients/go/v<version>`.

For more information, see the [Go README](go/README.md).
