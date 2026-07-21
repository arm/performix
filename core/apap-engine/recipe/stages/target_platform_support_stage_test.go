// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	ds "github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

const toolDescr1 = `
	let tool = {
		name: "core-tool-1",
		version: "0.1",
	    description: {
		  short: "Short description",
		  long: "Long description",
	    },
	    deployments: [{
		  appliesTo: [
		    {architecture: "x86_64", os: "Linux"}
		  ],
	    }],
	};`

const toolDescr2 = `
			let tool = {
				name: "core-tool-other",
				version: "0.1",
				description: {
				  short: "Short description",
				  long: "Long description",
				},
				deployments: [{
				  appliesTo: [
					{architecture: "x86_64", os: "Linux"}
				  ],
				  dependencies: [
					{type: "tool", name: "core-tool-1", version: "0.1", requiredWhen: {type: "always"}}
				  ],
				}],
			};`

var linuxOS = conductor.Linux
var aarch64 = conductor.AArch64
var x86 = conductor.X86_64

func TestTargetPlatformSupportStage(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "performix-test-")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(tempDir)) }()

	// Create a single tool bundle file
	executableDir := filepath.Join(tempDir, terminology.GetProductBinaryName())
	toolBundlePath := filepath.Join(executableDir, "tools", "core-tool-1", "1.1", "core-tool-1.tar.gz")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolBundlePath), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(toolBundlePath, nil, perms.LocalFilePerm))

	// Create first tool integration file
	toolIntPath := filepath.Join(executableDir, "tool-integrations", "core-tool-integration-1.js")
	toolIntData := []byte(toolDescr1)
	require.NoError(t, os.MkdirAll(filepath.Dir(toolIntPath), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(toolIntPath, toolIntData, perms.LocalFilePerm))

	// Create a second tool integration file that has a tool dependency
	toolIntPath = filepath.Join(executableDir, "tool-integrations", "core-tool-integration-2.js")
	toolIntData = []byte(toolDescr2)
	require.NoError(t, os.WriteFile(toolIntPath, toolIntData, perms.LocalFilePerm))

	mockMachineTypeSupplier := func() conductor.PlatformConfiguration {
		return conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.X86_64}
	}
	radioParams := []parameters.RadioParameter{
		{Parameter: parameters.Parameter{ID: "param1"}, DefaultValue: "value1"},
	}
	recipeInvocationParams, _ := parameters.BindRecipeParameters(map[string]any{}, parameters.Parameters{Radio: radioParams}, "test-recipe")

	t.Run("stage executes successfully with failIfUnsupported set to true and supported platform", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &x86},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "core-tool-1", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		pm := packages.NewPackageManager(executableDir, "")
		stage := stages.NewTargetPlatformSupportStage(pm, mockMachineTypeSupplier, recipeInvocationParams, &recipe.Recipe{Deployments: recipeDeployments}, true)
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		assert.NoError(t, err)
	})
	t.Run("stage fails with failIfUnsupported set to true and unsupported platform", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "core-tool-1", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		pm := packages.NewPackageManager(executableDir, "")
		stage := stages.NewTargetPlatformSupportStage(pm, mockMachineTypeSupplier, recipeInvocationParams, &recipe.Recipe{Deployments: recipeDeployments, Name: "my-recipe"}, true)

		readinessOutput := &runtime.ReadinessCollector{}
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background(), ReadinessNotifier: readinessOutput})
		metadata := map[string]string{}
		metadata["recipe"] = "my-recipe"
		metadata["OS"] = "Linux"
		metadata["Architecture"] = "x86_64"
		expectedErrMsg := message.New(message.EngineRecipeUnsupportedPlatformForRecipe).WithMetadata(metadata)
		assert.Equal(t, err, expectedErrMsg)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		// Validate readiness output
		expectedReadinessOutput := recipe.ReadyOutput{
			Status: recipe.ReadyStatusError,
			Advice: []recipe.ReadyAdvice{
				{
					AdviceSeverity: recipe.AdviceSeverityError,
					AdviceMessage:  expectedErrMsg,
				},
			},
		}
		assert.Len(t, readinessOutput.ReadinessOutput, 1)
		assert.Equal(t, readinessOutput.ReadinessOutput[0], expectedReadinessOutput)
		assert.Equal(t, stage.ErrorType(), run.RecipeFailureUnsupportedPlatform)
	})
	t.Run("stage fails when cannot find tool integration", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &x86},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "invalid-tool", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		pm := packages.NewPackageManager(executableDir, "")
		stage := stages.NewTargetPlatformSupportStage(pm, mockMachineTypeSupplier, recipeInvocationParams, &recipe.Recipe{Deployments: recipeDeployments, Name: "my-recipe"}, true)

		readinessOutput := &runtime.ReadinessCollector{}
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background(), ReadinessNotifier: readinessOutput})
		metadata := map[string]string{}
		metadata["toolIntegration"] = "invalid-tool"
		metadata["version"] = "0.1"
		metadata["recipe"] = "my-recipe"
		expectedErrMsg := message.New(message.EngineRecipeFindToolIntegration).WithMetadata(metadata)
		assert.Equal(t, err, expectedErrMsg)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.Len(t, readinessOutput.ReadinessOutput, 0)
		assert.Equal(t, stage.ErrorType(), run.RecipeFailureCheckPlatformSupport)
	})
	t.Run("stage fails with failIfUnsupported set to true and invalid use of tool dependency", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &x86},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "core-tool-other", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		pm := packages.NewPackageManager(executableDir, "")
		stage := stages.NewTargetPlatformSupportStage(pm, mockMachineTypeSupplier, recipeInvocationParams, &recipe.Recipe{Deployments: recipeDeployments, Name: "my-recipe"}, true)

		readinessOutput := &runtime.ReadinessCollector{}
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background(), ReadinessNotifier: readinessOutput})
		expectedErrMsg := message.New(message.EngineDeploymentsupportDependencyNotAllowed)
		assert.Equal(t, err, expectedErrMsg)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.Len(t, readinessOutput.ReadinessOutput, 0)
		assert.Equal(t, stage.ErrorType(), run.RecipeFailureCheckPlatformSupport)
	})
	t.Run("stage correctly records in the notifier when target is not supported", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "core-tool-1", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		targetSupportCollector := &runtime.TargetSupportCollector{}
		pm := packages.NewPackageManager(executableDir, "")
		stage := stages.NewTargetPlatformSupportStage(pm, mockMachineTypeSupplier, recipeInvocationParams, &recipe.Recipe{Deployments: recipeDeployments}, false)
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background(), TargetSupportNotifier: targetSupportCollector})
		assert.NoError(t, err)
		// Check that the unsupported platform was recorded
		assert.True(t, targetSupportCollector.PlatformSupport.Result == ds.PlatformNotSupported)
	})
	t.Run("stage executes without errors with failIfUnsupported set to true, when the platform is conditionally supported and the condition is resolved as supported", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "core-tool-1", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "other-value"}}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)

		// Target platform now is aarch64 and does not match with the core-tool-1's deployment (x86_64).
		// The tool is only required when param1 is set to "other-value". Our invocation parameters set param1 to "value1",
		// so this should be resolved as not required, and the stage should complete without errors.
		mockMachineTypeSupplier := func() conductor.PlatformConfiguration {
			return conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64}
		}
		targetSupportCollector := &runtime.TargetSupportCollector{}
		pm := packages.NewPackageManager(executableDir, "")
		stage := stages.NewTargetPlatformSupportStage(pm, mockMachineTypeSupplier, recipeInvocationParams, &recipe.Recipe{Deployments: recipeDeployments}, true)
		_, err := stage.Execute(&recipe.StageContext{Context: context.Background(), TargetSupportNotifier: targetSupportCollector})
		assert.NoError(t, err)
	})
	t.Run("stage errors with failIfUnsupported set to true, when the platform is conditionally supported and the condition is resolved as unsupported", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "core-tool-1", Version: "0.1", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsNotSet, Parameters: map[string]interface{}{"param1": "other-value"}}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)

		// Target platform now is aarch64 and does not match with the core-tool-1's deployment (x86_64).
		// The tool is only required when param1 is NOT set to "other-value". Our invocation parameters set param1 to "value1",
		// so this should be resolved as a required deployment, and the stage should complete with errors.
		mockMachineTypeSupplier := func() conductor.PlatformConfiguration {
			return conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64}
		}
		targetSupportCollector := &runtime.TargetSupportCollector{}
		pm := packages.NewPackageManager(executableDir, "")
		stage := stages.NewTargetPlatformSupportStage(pm, mockMachineTypeSupplier, recipeInvocationParams, &recipe.Recipe{Deployments: recipeDeployments, Name: "my-recipe"}, true)
		_, err = stage.Execute(&recipe.StageContext{Context: context.Background(), TargetSupportNotifier: targetSupportCollector, ReadinessNotifier: &recipe.NullReadinessNotifier{}})
		metadata := map[string]string{}
		metadata["recipe"] = "my-recipe"
		metadata["OS"] = "Linux"
		metadata["Architecture"] = "aarch64"
		expectedErrMsg := message.New(message.EngineRecipeUnsupportedPlatformForRecipe).WithMetadata(metadata)
		assert.Equal(t, err, expectedErrMsg)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.True(t, stage.Name() == "Checking target platform support")
		assert.Equal(t, stage.ErrorType(), run.RecipeFailureUnsupportedPlatform)
	})

	t.Run("ReadinessCollector escalates known severity", func(t *testing.T) {
		collector := &runtime.ReadinessCollector{}

		in := recipe.ReadyOutput{
			Status: recipe.ReadyStatusReady,
			Advice: []recipe.ReadyAdvice{{AdviceSeverity: recipe.AdviceSeverityError}},
		}

		collector.OnReadinessProbed(in)

		out := collector.ReadinessOutput[0]
		assert.Equal(t, recipe.ReadyStatusError, out.Status)
	})

	t.Run("ReadinessCollector unknown severity becomes unknown", func(t *testing.T) {
		collector := &runtime.ReadinessCollector{}

		in := recipe.ReadyOutput{
			Status: recipe.ReadyStatusReady,
			Advice: []recipe.ReadyAdvice{{AdviceSeverity: "this-is-not-valid"}},
		}

		collector.OnReadinessProbed(in)

		out := collector.ReadinessOutput[0]
		assert.Equal(t, recipe.ReadyStatusUnknown, out.Status)
	})
}
