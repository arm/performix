// Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// requiredToolsForLocality is the source of truth for deployments required on
// a locality. It includes mandatory tools and matching recipe tool bundles.
func requiredToolsForLocality(
	bundles []deploymentsupport.ToolBundleInfo,
	locality deploymentsupport.DeploymentLocality,
) []tool.ToolInfo {
	required := []tool.ToolInfo{{
		Name:    terminology.GetAgentBinaryName(),
		Version: versions.GetVersion(),
	}}

	for _, bundle := range bundles {
		if bundle.Locality != locality {
			continue
		}
		required = appendUniqueTools(required, tool.ToolInfo{
			Name:    bundle.Name,
			Version: bundle.Version,
		})
	}

	return required
}

func appendUniqueTools(tools []tool.ToolInfo, additions ...tool.ToolInfo) []tool.ToolInfo {
	type toolIdentity struct {
		name    string
		version string
	}
	identity := func(toolInfo tool.ToolInfo) toolIdentity {
		return toolIdentity{name: toolInfo.Name, version: toolInfo.Version}
	}

	seen := make(map[toolIdentity]struct{}, len(tools)+len(additions))
	for _, toolInfo := range tools {
		seen[identity(toolInfo)] = struct{}{}
	}

	for _, toolInfo := range additions {
		key := identity(toolInfo)
		if _, exists := seen[key]; exists {
			continue
		}
		tools = append(tools, toolInfo)
		seen[key] = struct{}{}
	}

	return tools
}
