// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
)

// GetSupportedPlatforms determines the supported platforms for the given recipe deployments
// It does this by checking each of the known platform combinations against the recipe deployments
// and the available tool integrations in the package manager
func GetSupportedPlatforms(pm *packages.PackageManager, recipe *Recipe) ([]deploymentsupport.PlatformSupport, error) {
	var err error
	tr, err := pm.FindToolIntegrations()
	if err != nil {
		return nil, err
	}
	depResolver := func(name string, version string) ([]deploymentsupport.DeploymentDeclaration, error) {
		factory := tr.FindTool(name, version)
		if factory == nil {
			metadata := map[string]string{}
			metadata["toolIntegration"] = name
			metadata["version"] = version
			metadata["recipe"] = recipe.Name
			return nil, message.New(message.EngineRecipeFindToolIntegration).WithMetadata(metadata)
		}
		return factory.Deployments(), nil
	}
	supportedPlatforms := []deploymentsupport.PlatformSupport{}
	for _, platformConfig := range conductor.PlatformCombinations {
		platformSupport, err2 := deploymentsupport.GetDeploymentSupportForPlatform(depResolver, deploymentsupport.MatchAll, recipe.Deployments, platformConfig)
		if err2 != nil {
			return nil, err2
		}
		supportedPlatforms = append(supportedPlatforms, platformSupport)
	}
	if err != nil {
		return nil, err
	}
	return supportedPlatforms, nil
}
