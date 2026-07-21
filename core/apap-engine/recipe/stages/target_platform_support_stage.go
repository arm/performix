// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
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

// TargetPlatformSupportStage
type TargetPlatformSupportStage struct {
	PlatformConfigurationSupplier recipe.PlatformConfigurationSupplier
	PackageManager                *packages.PackageManager
	RecipeInvocationParams        parameters.BoundParameters
	Recipe                        *recipe.Recipe
	FailIfUnsuported              bool
	StageError                    run.RunResult
}

func (t *TargetPlatformSupportStage) Name() string {
	return "Checking target platform support"
}

func (t *TargetPlatformSupportStage) ErrorType() run.RunResult {
	return t.StageError
}

func (t *TargetPlatformSupportStage) Execute(ctx *recipe.StageContext) (func(), error) {
	var err error
	tr, err := t.PackageManager.FindToolIntegrations()
	if err != nil {
		return nil, err
	}

	// depResolver is a function that takes a tool name and version and returns the deployments it supports.
	depResolver := func(name string, version string) ([]deploymentsupport.DeploymentDeclaration, error) {
		factory := tr.FindTool(name, version)
		if factory == nil {
			metadata := map[string]string{}
			metadata["toolIntegration"] = name
			metadata["version"] = version
			metadata["recipe"] = t.Recipe.Name
			return nil, message.New(message.EngineRecipeFindToolIntegration).WithMetadata(metadata)
		}
		return factory.Deployments(), nil
	}

	// Get Target Platform support. The support may be conditional on params.
	platformSupport, err := deploymentsupport.GetDeploymentSupportForPlatform(depResolver, deploymentsupport.MatchAll, t.Recipe.Deployments, t.PlatformConfigurationSupplier())
	if err != nil {
		return nil, err
	}

	if !t.FailIfUnsuported {
		// We don't fail if unsupported, instead write result to notifier and return.
		// This path is generally followed when doing "atperf recipe info --target <target>"
		ctx.TargetSupportNotifier.OnTargetSupportChecked(platformSupport)
		return nil, nil
	}

	metadata := map[string]string{
		"recipe":       t.Recipe.Name,
		"OS":           string(t.PlatformConfigurationSupplier().OS),
		"Architecture": string(t.PlatformConfigurationSupplier().Architecture),
	}
	// The following logic is to determine if we should fail the recipe due to unsupported platform.
	// This logic is relevant when running a recipe.
	switch platformSupport.Result {
	case deploymentsupport.PlatformNotSupported:
		// If we're in the recipe readiness workflow, we return a readiness message
		errorMsg := message.New(message.EngineRecipeUnsupportedPlatformForRecipe).WithMetadata(metadata)
		readyOutput := recipe.ConvertMessageToReadyOutput(errorMsg)
		ctx.ReadinessNotifier.OnReadinessProbed(readyOutput)
		t.StageError = run.RecipeFailureUnsupportedPlatform
		return nil, errorMsg
	case deploymentsupport.PlatformSupportConditional:
		// Support is conditional on params. We know the given params on a recipe run, so continue to resolve the platform support.
		platformSupport, err = deploymentsupport.ResolveParameterSupport(ctx.Context, platformSupport, t.RecipeInvocationParams)
		if err != nil {
			return nil, err
		}
	}

	// After resolving params, we now have a definitive answer.
	if platformSupport.Result == deploymentsupport.PlatformIsSupported {
		return nil, nil
	} else {
		// Unsupported platform
		// If we're in the recipe readiness workflow, we return a readiness message
		errorMsg := message.New(message.EngineRecipeUnsupportedPlatformForRecipe).WithMetadata(metadata)
		readyOutput := recipe.ConvertMessageToReadyOutput(errorMsg)
		ctx.ReadinessNotifier.OnReadinessProbed(readyOutput)
		t.StageError = run.RecipeFailureUnsupportedPlatform
		return nil, errorMsg
	}

}

func (t *TargetPlatformSupportStage) AlwaysExecute() bool {
	return false
}

func NewTargetPlatformSupportStage(packageManager *packages.PackageManager, supplier recipe.PlatformConfigurationSupplier, recipeInvocationParams parameters.BoundParameters, recipe *recipe.Recipe, failIfUnsupported bool) *TargetPlatformSupportStage {
	return &TargetPlatformSupportStage{
		PlatformConfigurationSupplier: supplier,
		PackageManager:                packageManager,
		RecipeInvocationParams:        recipeInvocationParams,
		Recipe:                        recipe,
		FailIfUnsuported:              failIfUnsupported,
		StageError:                    run.RecipeFailureCheckPlatformSupport, // default error type
	}
}
