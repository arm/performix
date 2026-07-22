// Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

type ToolBundleResolutionStage struct {
	TargetPlatformConfigurationSupplier recipe.PlatformConfigurationSupplier
	PackageManager                      *packages.PackageManager
	RecipeInvocationParams              *parameters.BoundParameters
	Recipe                              *recipe.Recipe
	ToolBundles                         []deploymentsupport.ToolBundleInfo
	ToolBundlesSupplier                 recipe.ToolBundlesSupplier
}

func NewToolBundleResolutionStage(
	packageManager *packages.PackageManager,
	targetPlatformSupplier recipe.PlatformConfigurationSupplier,
	recipeInvocationParams *parameters.BoundParameters,
	recipeDecl *recipe.Recipe,
) *ToolBundleResolutionStage {
	stage := &ToolBundleResolutionStage{
		TargetPlatformConfigurationSupplier: targetPlatformSupplier,
		PackageManager:                      packageManager,
		RecipeInvocationParams:              recipeInvocationParams,
		Recipe:                              recipeDecl,
	}
	stage.ToolBundlesSupplier = func() []deploymentsupport.ToolBundleInfo {
		return stage.ToolBundles
	}
	return stage
}

func (t *ToolBundleResolutionStage) Name() string {
	return "Resolving tool bundles"
}

func (t *ToolBundleResolutionStage) ErrorType() run.RunResult {
	return run.RecipeFailureDeploy
}

func (t *ToolBundleResolutionStage) AlwaysExecute() bool {
	return false
}

func (t *ToolBundleResolutionStage) Execute(ctx *recipe.StageContext) (func(), error) {
	toolRegistry, err := t.PackageManager.FindToolIntegrations()
	if err != nil {
		return nil, err
	}

	depResolver := func(name, version string) ([]deploymentsupport.DeploymentDeclaration, error) {
		factory := toolRegistry.FindTool(name, version)
		if factory == nil {
			metadata := map[string]string{
				"toolIntegration": name,
				"version":         version,
			}
			if t.Recipe != nil {
				metadata["recipe"] = t.Recipe.Name
			}
			return nil, message.New(message.EngineRecipeFindToolIntegration).
				WithMetadata(metadata)
		}

		return factory.Deployments(), nil
	}

	t.ToolBundles, err = deploymentsupport.ResolveToolBundles(
		ctx.Context,
		t.TargetPlatformConfigurationSupplier(),
		t.RecipeInvocationParams,
		t.Recipe.Deployments,
		depResolver,
	)
	if err != nil {
		return nil, err
	}

	return nil, nil
}
