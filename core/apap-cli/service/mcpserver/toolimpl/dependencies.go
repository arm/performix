// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// ToolDependencies bundles the collaborators that MCP tools may use when handling
// requests. Tools receive these through their Register method so the set of available
// collaborators can grow without changing every tool's signature.
type ToolDependencies struct {
	// Engine is the gRPC client for the Performix engine daemon.
	Engine apapproto.ApapClient
	// Targets provides access to the CLI-side target configuration (targets.json).
	Targets target.TargetManagerService
}
