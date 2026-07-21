// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package deploymentsupport_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	ds "github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
)

var linuxOS = conductor.Linux
var windowsOS = conductor.Win
var aarch64 = conductor.AArch64
var x86 = conductor.X86_64

func getDepResolverFunc(toolIntegrations []tool.ToolIntegration) ds.DependencyResolver {
	return func(name string, version string) ([]ds.DeploymentDeclaration, error) {
		ti := tool.FindToolIntegration(toolIntegrations, name, version)
		if ti == nil {
			return nil, fmt.Errorf("tool integration %s v%s not found", name, version)
		}
		return ti.Properties().Deployments, nil
	}
}

func TestGetDeploymentSupportForPlatform(t *testing.T) {
	recipeDeployments := []ds.DeploymentDeclaration{}
	deployment := ds.DeploymentDeclaration{
		AppliesTo: []ds.PlatformConfigurationFilter{
			{OS: &linuxOS, Architecture: &aarch64},
			{OS: &windowsOS, Architecture: &aarch64},
		},
		// Dependencies will be set inside tests
	}
	recipeDeployments = append(recipeDeployments, deployment)

	t.Run("target platform is supported when there's no deployments in recipe, as it defaults to supported", func(t *testing.T) {
		recipeDeployments := []ds.DeploymentDeclaration{}
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.AArch64,
		}
		depResolver := func(name string, version string) ([]ds.DeploymentDeclaration, error) {
			return nil, nil
		}

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			Result:   ds.PlatformIsSupported,
		}
		require.Equal(t, expectedSupport, support)
	})
	t.Run("target platform is supported when there's no recipe dependencies", func(t *testing.T) {
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.AArch64,
		}
		depResolver := func(name string, version string) ([]ds.DeploymentDeclaration, error) {
			return nil, nil
		}

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			Result:   ds.PlatformIsSupported,
		}
		require.Equal(t, expectedSupport, support)
	})
	t.Run("target platform is conditionally supported with negated condition when a 2nd tool is conditionally supported on another platform", func(t *testing.T) {
		toolDeployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{},
		}
		fakeTool := &tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		toolDeployment = ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &windowsOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{},
		}
		toolIntegrations := []tool.ToolIntegration{fakeTool}
		fakeTool = &tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool2",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.AArch64,
		}
		toolIntegrations = append(toolIntegrations, fakeTool)
		depResolver := getDepResolverFunc(toolIntegrations)
		deploymentDeps := []ds.Dependency{
			{Type: ds.DependencyTypeTool, Name: "fake-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			{Type: ds.DependencyTypeTool, Name: "fake-tool2", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "value1"}}},
		}
		// overwrite dependencies set in previous tests
		recipeDeployments[0].Dependencies = deploymentDeps

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform:      conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			Result:        ds.PlatformSupportConditional,
			ConditionList: []ds.RequirementSpec{{Type: ds.RequirementTypeIfParamIsNotSet, Parameters: map[string]interface{}{"param1": "value1"}}},
		}
		require.Equal(t, expectedSupport, support)
	})
	t.Run("target platform is fully supported with one tool supported and a 2nd tool conditionally supported, both with the same platform filter", func(t *testing.T) {
		// Here target platform matches the platform filter for both tools. One tool is always required,
		// whilst the other tool is conditionally required.
		toolIntegrations := []tool.ToolIntegration{}
		toolDeployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{},
		}
		fakeTool := &tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		toolDeployment = ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{},
		}
		toolIntegrations = append(toolIntegrations, fakeTool)
		fakeTool = &tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool2",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.AArch64,
		}
		toolIntegrations = append(toolIntegrations, fakeTool)
		depResolver := getDepResolverFunc(toolIntegrations)
		deploymentDeps := []ds.Dependency{
			{Type: ds.DependencyTypeTool, Name: "fake-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			{Type: ds.DependencyTypeTool, Name: "fake-tool2", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "value1"}}},
		}
		// overwrite dependencies set in previous tests
		recipeDeployments[0].Dependencies = deploymentDeps

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			Result:   ds.PlatformIsSupported,
		}
		require.Equal(t, expectedSupport, support)
	})
	t.Run("target platform is unsupported when one tool is always required and unsupported on target, and the other tool is conditionally supported", func(t *testing.T) {
		toolIntegrations := []tool.ToolIntegration{}
		toolDeployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{},
		}
		fakeTool := &tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		toolDeployment = ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &windowsOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{},
		}
		toolIntegrations = append(toolIntegrations, fakeTool)
		fakeTool = &tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool2",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Win,
			Architecture: conductor.AArch64,
		}
		toolIntegrations = append(toolIntegrations, fakeTool)
		depResolver := getDepResolverFunc(toolIntegrations)
		deploymentDeps := []ds.Dependency{
			{Type: ds.DependencyTypeTool, Name: "fake-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			{Type: ds.DependencyTypeTool, Name: "fake-tool2", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "value1"}}},
		}
		// overwrite dependencies set in previous tests
		recipeDeployments[0].Dependencies = deploymentDeps

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Win, Architecture: conductor.AArch64},
			Result:   ds.PlatformNotSupported,
		}
		require.Equal(t, expectedSupport, support)
	})
	t.Run("target platform is fully supported when recipe and tool matches, but with a condition", func(t *testing.T) {
		// A single tool ds.Dependency, matching the target platform configuration. But regardless if it's
		// conditionally required, the result is still "unconditionally supported". If the condition is not
		// met, then essentially we have no tools to deploy, and that is intended.
		toolIntegrations := []tool.ToolIntegration{}
		toolDeployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{},
		}
		fakeTool := tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		toolIntegrations = append(toolIntegrations, &fakeTool)
		depResolver := getDepResolverFunc(toolIntegrations)
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.AArch64,
		}

		deploymentDeps := []ds.Dependency{
			{Type: ds.DependencyTypeTool, Name: "fake-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "value1"}}},
		}
		// overwrite dependencies set in previous tests
		recipeDeployments[0].Dependencies = deploymentDeps

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			Result:   ds.PlatformIsSupported,
		}
		require.Equal(t, expectedSupport, support)
	})

	t.Run("target platform falls back to a supported deployment when first deployment is not supported", func(t *testing.T) {
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Win,
			Architecture: conductor.AArch64,
		}

		recipeDeployments := []ds.DeploymentDeclaration{
			{
				AppliesTo: []ds.PlatformConfigurationFilter{
					{OS: &windowsOS, Architecture: &aarch64},
				},
				Dependencies: []ds.Dependency{
					{Type: ds.DependencyTypeTool, Name: "unsupported-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
				},
			},
			{
				AppliesTo: []ds.PlatformConfigurationFilter{
					{OS: &windowsOS, Architecture: &aarch64},
				},
				Dependencies: []ds.Dependency{
					{Type: ds.DependencyTypeTool, Name: "supported-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
				},
			},
		}

		unsupportedTool := &tool_mocks.ToolIntegrationMock{}
		unsupportedTool.On("Properties").Return(tool.IntegrationProperties{
			Name:        "unsupported-tool",
			Version:     "1.0",
			Deployments: []ds.DeploymentDeclaration{{AppliesTo: []ds.PlatformConfigurationFilter{{OS: &linuxOS, Architecture: &aarch64}}}},
		})
		supportedTool := &tool_mocks.ToolIntegrationMock{}
		supportedTool.On("Properties").Return(tool.IntegrationProperties{
			Name:        "supported-tool",
			Version:     "1.0",
			Deployments: []ds.DeploymentDeclaration{{AppliesTo: []ds.PlatformConfigurationFilter{{OS: &windowsOS, Architecture: &aarch64}}}},
		})

		toolIntegrations := []tool.ToolIntegration{unsupportedTool, supportedTool}
		depResolver := getDepResolverFunc(toolIntegrations)

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: targetPlatform,
			Result:   ds.PlatformIsSupported,
		}
		require.Equal(t, expectedSupport, support)
	})

	t.Run("target platform support errors when tool does not exist", func(t *testing.T) {
		toolIntegrations := []tool.ToolIntegration{}
		toolDeployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &aarch64},
			},
			Dependencies: []ds.Dependency{},
		}
		fakeTool := tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "non-existant-tool",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		toolIntegrations = append(toolIntegrations, &fakeTool)
		depResolver := getDepResolverFunc(toolIntegrations)
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.AArch64,
		}

		deploymentDeps := []ds.Dependency{
			{Type: ds.DependencyTypeTool, Name: "fake-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "value1"}}},
		}
		// overwrite dependencies set in previous tests
		recipeDeployments[0].Dependencies = deploymentDeps

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.EqualError(t, err, "tool integration fake-tool v1.0 not found")
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			Result:   ds.PlatformNotSupported,
		}
		require.Equal(t, expectedSupport, support)
	})
	t.Run("target platform is unsupported when recipe filters and tool filters do not intersect (aarch64 vs x86)", func(t *testing.T) {
		toolDeployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &x86},
			},
			Dependencies: []ds.Dependency{},
		}
		fakeTool := tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		toolIntegrations := []tool.ToolIntegration{&fakeTool}
		depResolver := getDepResolverFunc(toolIntegrations)
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.AArch64,
		}

		deploymentDeps := []ds.Dependency{
			{Type: ds.DependencyTypeTool, Name: "fake-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
		}
		// overwrite dependencies set in previous tests
		recipeDeployments[0].Dependencies = deploymentDeps

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			Result:   ds.PlatformNotSupported,
		}
		require.Equal(t, expectedSupport, support)
	})
	t.Run("target platform not supported when recipe filters are empty but recipe dependencies are not supported", func(t *testing.T) {
		// declare recipeDeployments locally to this test
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "fake-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		toolDeployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &x86},
			},
			Dependencies: []ds.Dependency{},
		}
		fakeTool := tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		toolIntegrations := []tool.ToolIntegration{&fakeTool}
		depResolver := getDepResolverFunc(toolIntegrations)
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.AArch64,
		}

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64},
			Result:   ds.PlatformNotSupported,
		}
		require.Equal(t, expectedSupport, support)
	})
	t.Run("target platform is supported when recipe filters are empty but recipe dependencies are supported", func(t *testing.T) {
		// declare recipeDeployments locally to this test
		recipeDeployments := []ds.DeploymentDeclaration{}
		deployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{},
			Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeTool, Name: "fake-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
			},
		}
		recipeDeployments = append(recipeDeployments, deployment)
		toolDeployment := ds.DeploymentDeclaration{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{OS: &linuxOS, Architecture: &x86},
			},
			Dependencies: []ds.Dependency{},
		}
		fakeTool := tool_mocks.ToolIntegrationMock{}
		fakeTool.On("Properties").Return(tool.IntegrationProperties{
			Name:             "fake-tool",
			Deployments:      []ds.DeploymentDeclaration{toolDeployment},
			Version:          "1.0",
			ShortDescription: "Fake tool for testing",
			LongDescription:  "This tool is a fake implementation for test purposes.",
		})
		toolIntegrations := []tool.ToolIntegration{&fakeTool}
		depResolver := getDepResolverFunc(toolIntegrations)
		targetPlatform := conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.X86_64,
		}

		support, err := ds.GetDeploymentSupportForPlatform(depResolver, ds.MatchAll, recipeDeployments, targetPlatform)
		assert.NoError(t, err)
		expectedSupport := ds.PlatformSupport{
			Platform: conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.X86_64},
			Result:   ds.PlatformIsSupported,
		}
		require.Equal(t, expectedSupport, support)
	})
}

func TestResolveParameterSupport(t *testing.T) {
	ctx := context.Background()
	t.Run("returns supported when the platform is already supported", func(t *testing.T) {
		ps := ds.PlatformSupport{Result: ds.PlatformIsSupported}
		support, err := ds.ResolveParameterSupport(ctx, ps, parameters.BoundParameters{})
		assert.NoError(t, err)
		assert.Equal(t, support.Result, ds.PlatformIsSupported)
	})
	t.Run("returns unsupported when the given params are empty but a param is expected", func(t *testing.T) {
		ps := ds.PlatformSupport{Result: ds.PlatformSupportConditional, ConditionList: []ds.RequirementSpec{
			{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "value1"}},
		}}
		support, err := ds.ResolveParameterSupport(ctx, ps, parameters.BoundParameters{})
		assert.NoError(t, err)
		assert.Equal(t, support.Result, ds.PlatformNotSupported)
	})
	t.Run("returns unsupported when the params values don't match", func(t *testing.T) {
		ps := ds.PlatformSupport{Result: ds.PlatformSupportConditional, ConditionList: []ds.RequirementSpec{
			{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "value1"}},
		}}
		inputParams := []parameters.InputParameter{{Parameter: parameters.Parameter{ID: "param1"}}}
		bp, _ := parameters.BindRecipeParameters(map[string]interface{}{"param1": "value2"}, parameters.Parameters{Input: inputParams}, "test-recipe")
		support, err := ds.ResolveParameterSupport(ctx, ps, bp)
		assert.NoError(t, err)
		assert.Equal(t, support.Result, ds.PlatformNotSupported)
	})
	t.Run("returns unsupported when the params values match but condition is param_is_not_set", func(t *testing.T) {
		ps := ds.PlatformSupport{Result: ds.PlatformSupportConditional, ConditionList: []ds.RequirementSpec{
			{Type: ds.RequirementTypeIfParamIsNotSet, Parameters: map[string]interface{}{"param1": "value1"}},
		}}
		inputParams := []parameters.InputParameter{{Parameter: parameters.Parameter{ID: "param1"}}}
		bp, _ := parameters.BindRecipeParameters(map[string]interface{}{"param1": "value1"}, parameters.Parameters{Input: inputParams}, "test-recipe")
		support, err := ds.ResolveParameterSupport(ctx, ps, bp)
		assert.NoError(t, err)
		assert.Equal(t, support.Result, ds.PlatformNotSupported)
	})
	t.Run("returns supported when the params match", func(t *testing.T) {
		ps := ds.PlatformSupport{Result: ds.PlatformSupportConditional, ConditionList: []ds.RequirementSpec{
			{Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"param1": "value1", "param2": "value2"}},
		}}
		inputParams := []parameters.InputParameter{{Parameter: parameters.Parameter{ID: "param1"}}, {Parameter: parameters.Parameter{ID: "param2"}}}
		bp, _ := parameters.BindRecipeParameters(map[string]interface{}{"param1": "value1", "param2": "value2"}, parameters.Parameters{Input: inputParams}, "test-recipe")
		support, err := ds.ResolveParameterSupport(ctx, ps, bp)
		assert.NoError(t, err)
		assert.Equal(t, support.Result, ds.PlatformIsSupported)
	})
}

func TestNegateRequirement(t *testing.T) {
	t.Run("negates IfParamIsSet to IfParamIsNotSet", func(t *testing.T) {
		req := ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsSet}
		negatedReq := req.Type.Negate()
		assert.Equal(t, ds.RequirementTypeIfParamIsNotSet, negatedReq)
	})
	t.Run("negates IfParamIsNotSet to IfParamIsSet", func(t *testing.T) {
		//nolint:unusedwrite
		req := ds.RequirementSpec{Type: ds.RequirementTypeIfParamIsNotSet}
		negatedReq := req.Type.Negate()
		assert.Equal(t, ds.RequirementTypeIfParamIsSet, negatedReq)
	})
}

func TestResolveToolBundles_ToolBundleType(t *testing.T) {
	platformConfig := conductor.PlatformConfiguration{
		Architecture: aarch64,
		OS:           linuxOS,
	}

	t.Run("successfully filters in/out deployments by target platform", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{
			{
				AppliesTo: []ds.PlatformConfigurationFilter{
					{Architecture: &aarch64},
				},
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeToolBundle,
						Name:    "filtered-in",
						Version: "1.0",
						RequiredWhen: ds.RequirementSpec{
							Type: ds.RequirementTypeAlways,
						},
					},
				},
			},
			{
				AppliesTo: []ds.PlatformConfigurationFilter{
					{Architecture: &x86},
				},
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeToolBundle,
						Name:    "filtered-out",
						Version: "2.0",
						RequiredWhen: ds.RequirementSpec{
							Type: ds.RequirementTypeAlways,
						},
					},
				},
			},
		}

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		require.NoError(t, err)
		assert.ElementsMatch(t, []ds.ToolBundleInfo{{Name: "filtered-in", Version: "1.0", Locality: ds.DeploymentLocalityTarget}}, bundles)
	})

	t.Run("skips unsupported deployment type", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyType("other"),
						Name:    "filtered-out",
						Version: "1.0",
						RequiredWhen: ds.RequirementSpec{
							Type: ds.RequirementTypeAlways,
						},
					},
				},
			},
		}

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		require.NoError(t, err)
		assert.Empty(t, bundles)
	})

	t.Run("returns error on invalid tool bundles (missing name/version)", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type: ds.DependencyTypeToolBundle,
						Name: "filtered-out",
						// no version
						RequiredWhen: ds.RequirementSpec{
							Type: ds.RequirementTypeAlways,
						},
					},
				},
			},
		}

		expectedErr := message.New(message.EngineDeploymentsupportToolBundleMissingFields)

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		assert.Equal(t, expectedErr, err)
		assert.Empty(t, bundles)
	})

	t.Run("successfully filters in/out requirement type param_is_set", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeToolBundle,
						Name:    "param-set-tool",
						Version: "1.0",
						RequiredWhen: ds.RequirementSpec{
							Type: ds.RequirementTypeIfParamIsSet,
							Parameters: map[string]any{
								"feature": "on",
							},
						},
					},
				},
			},
		}

		// Filters out with no parameters
		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		require.NoError(t, err)
		assert.Empty(t, bundles)

		// Filters in with matching parameter
		paramMatch := parameters.BoundParameters{
			Parameters: parameters.Parameters{
				Input: []parameters.InputParameter{
					{
						Parameter: parameters.Parameter{ID: "feature"},
					},
				},
			},
			Values: parameters.ParameterValues{
				Input: []string{"on"},
			},
		}

		bundles, err = ds.ResolveToolBundles(t.Context(), platformConfig, &paramMatch, deployments, nil)
		require.NoError(t, err)
		assert.Equal(t, []ds.ToolBundleInfo{{Name: "param-set-tool", Version: "1.0", Locality: ds.DeploymentLocalityTarget}}, bundles)

		// Filters out with non-matching parameter
		paramMismatch := parameters.BoundParameters{
			Parameters: parameters.Parameters{
				Input: []parameters.InputParameter{
					{
						Parameter: parameters.Parameter{ID: "feature"},
					},
				},
			},
			Values: parameters.ParameterValues{
				Input: []string{"off"},
			},
		}

		bundles, err = ds.ResolveToolBundles(t.Context(), platformConfig, &paramMismatch, deployments, nil)
		require.NoError(t, err)
		assert.Empty(t, bundles)

		// Filters out with empty parameters in requirement
		deployments = []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeToolBundle,
						Name:    "param-set-tool",
						Version: "1.0",
						RequiredWhen: ds.RequirementSpec{
							Type:       ds.RequirementTypeIfParamIsSet,
							Parameters: map[string]any{},
						},
					},
				},
			},
		}

		bundles, err = ds.ResolveToolBundles(t.Context(), platformConfig, &paramMatch, deployments, nil)
		assert.Error(t, err)
		assert.Empty(t, bundles)
	})

	t.Run("successfully filters in/out requirement type param_is_not_set", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeToolBundle,
						Name:    "param-not-set-tool",
						Version: "1.0",
						RequiredWhen: ds.RequirementSpec{
							Type: ds.RequirementTypeIfParamIsNotSet,
							Parameters: map[string]any{
								"feature": "off",
							},
						},
					},
				},
			},
		}

		// Filters in with no parameters
		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		require.NoError(t, err)
		assert.Equal(t, []ds.ToolBundleInfo{{Name: "param-not-set-tool", Version: "1.0", Locality: ds.DeploymentLocalityTarget}}, bundles)

		// Filters in with non-matching parameter
		paramMismatch := parameters.BoundParameters{
			Parameters: parameters.Parameters{
				Input: []parameters.InputParameter{
					{
						Parameter: parameters.Parameter{ID: "feature"},
					},
				},
			},
			Values: parameters.ParameterValues{
				Input: []string{"on"},
			},
		}
		bundles, err = ds.ResolveToolBundles(t.Context(), platformConfig, &paramMismatch, deployments, nil)
		require.NoError(t, err)
		assert.Equal(t, []ds.ToolBundleInfo{{Name: "param-not-set-tool", Version: "1.0", Locality: ds.DeploymentLocalityTarget}}, bundles)

		// Filters out with matching parameter
		paramMatch := parameters.BoundParameters{
			Parameters: parameters.Parameters{
				Input: []parameters.InputParameter{
					{
						Parameter: parameters.Parameter{ID: "feature"},
					},
				},
			},
			Values: parameters.ParameterValues{
				Input: []string{"off"},
			},
		}
		bundles, err = ds.ResolveToolBundles(t.Context(), platformConfig, &paramMatch, deployments, nil)
		require.NoError(t, err)
		assert.Empty(t, bundles)

		// Filters out with empty parameters in requirement
		deployments = []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeToolBundle,
						Name:    "param-not-set-tool",
						Version: "1.0",
						RequiredWhen: ds.RequirementSpec{
							Type:       ds.RequirementTypeIfParamIsNotSet,
							Parameters: map[string]any{},
						},
					},
				},
			},
		}

		bundles, err = ds.ResolveToolBundles(t.Context(), platformConfig, &paramMismatch, deployments, nil)
		assert.Error(t, err)
		assert.Empty(t, bundles)
	})

	t.Run("returns error on unsupported requirement types", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeToolBundle,
						Name:    "filtered-out",
						Version: "1.0",
						RequiredWhen: ds.RequirementSpec{
							Type: "unsupported",
						},
					},
				},
			},
		}

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		assert.Error(t, err)
		assert.Empty(t, bundles)
	})

	t.Run("defaults omitted locality to target", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{{
			Dependencies: []ds.Dependency{{
				Type:         ds.DependencyTypeToolBundle,
				Name:         "default-locality-tool",
				Version:      "1.0",
				RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
			}},
		}}

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		require.NoError(t, err)
		assert.Equal(t, []ds.ToolBundleInfo{{Name: "default-locality-tool", Version: "1.0", Locality: ds.DeploymentLocalityTarget}}, bundles)
	})

	t.Run("preserves explicit host locality", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{{
			Dependencies: []ds.Dependency{{
				Type:         ds.DependencyTypeToolBundle,
				Name:         "host-tool",
				Version:      "1.0",
				Locality:     ds.DeploymentLocalityHost,
				RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
			}},
		}}

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		require.NoError(t, err)
		assert.Equal(t, []ds.ToolBundleInfo{{Name: "host-tool", Version: "1.0", Locality: ds.DeploymentLocalityHost}}, bundles)
	})

	t.Run("returns error on invalid locality", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{{
			Dependencies: []ds.Dependency{{
				Type:         ds.DependencyTypeToolBundle,
				Name:         "bad-locality-tool",
				Version:      "1.0",
				Locality:     ds.DeploymentLocality("elsewhere"),
				RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
			}},
		}}

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		assert.Equal(t, message.New(message.EngineDeploymentsupportInvalidLocality).WithMetadata(map[string]string{
			"locality": "elsewhere",
		}), err)
		assert.Empty(t, bundles)
	})

	t.Run("keeps host and target bundles separate during dedupe", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{{
			Dependencies: []ds.Dependency{
				{
					Type:         ds.DependencyTypeToolBundle,
					Name:         "dual-locality-tool",
					Version:      "1.0",
					RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
				},
				{
					Type:         ds.DependencyTypeToolBundle,
					Name:         "dual-locality-tool",
					Version:      "1.0",
					Locality:     ds.DeploymentLocalityHost,
					RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
				},
			},
		}}

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, nil)
		require.NoError(t, err)
		assert.ElementsMatch(t, []ds.ToolBundleInfo{
			{Name: "dual-locality-tool", Version: "1.0", Locality: ds.DeploymentLocalityTarget},
			{Name: "dual-locality-tool", Version: "1.0", Locality: ds.DeploymentLocalityHost},
		}, bundles)
	})

	t.Run("rejects locality on tool dependencies", func(t *testing.T) {
		deployments := []ds.DeploymentDeclaration{{
			Dependencies: []ds.Dependency{{
				Type:         ds.DependencyTypeTool,
				Name:         "hosted-tool",
				Version:      "1.0.0",
				Locality:     ds.DeploymentLocalityHost,
				RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
			}},
		}}

		bundles, err := ds.ResolveToolBundles(t.Context(), platformConfig, nil, deployments, func(name, version string) ([]ds.DeploymentDeclaration, error) {
			return nil, nil
		})
		assert.Equal(t, message.New(message.EngineDeploymentsupportLocalityNotAllowedForToolDependency), err)
		assert.Empty(t, bundles)
	})
}

func TestResolveToolBundles_ToolType(t *testing.T) {
	targetAarch64 := conductor.PlatformConfiguration{
		OS:           conductor.Linux,
		Architecture: conductor.AArch64,
	}
	targetX86 := conductor.PlatformConfiguration{
		OS:           conductor.Linux,
		Architecture: conductor.X86_64,
	}

	// Sample tool (mimics neoprof.js)
	streamlineDeployments := []ds.DeploymentDeclaration{
		{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{Architecture: &aarch64, OS: &linuxOS},
				{Architecture: &x86, OS: &linuxOS},
			},
			Dependencies: []ds.Dependency{
				{
					Type:         ds.DependencyTypeToolBundle,
					Name:         "sl-record",
					Version:      "1.9.1-build-4",
					RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
				},
				{
					Type:         ds.DependencyTypeToolBundle,
					Name:         "sl-analyze",
					Version:      "1.9.1-build-4",
					RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
				},
			},
		},
		{
			AppliesTo: []ds.PlatformConfigurationFilter{
				{Architecture: &aarch64, OS: &linuxOS},
			},
			Dependencies: []ds.Dependency{
				{
					Type:         ds.DependencyTypeToolBundle,
					Name:         "jitdump-jvm",
					Version:      "0.1.1",
					RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
				},
			},
		},
	}

	// Register mock tool integration
	streamline := &tool_mocks.ToolIntegrationMock{}
	streamline.On("Properties").Return(tool.IntegrationProperties{
		Name:        "streamline-cli",
		Version:     "1.0.0",
		Deployments: streamlineDeployments,
	})
	depResolver := getDepResolverFunc([]tool.ToolIntegration{streamline})

	// Sample recipe deployment
	recipeDeployments := []ds.DeploymentDeclaration{
		{
			Dependencies: []ds.Dependency{
				{
					Type:         ds.DependencyTypeTool,
					Name:         "streamline-cli",
					Version:      "1.0.0",
					RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
				},
			},
		},
	}

	t.Run("successfully returns tool bundles for supported platform", func(t *testing.T) {
		bundles, err := ds.ResolveToolBundles(t.Context(), targetAarch64, nil, recipeDeployments, depResolver)
		require.NoError(t, err)

		assert.ElementsMatch(t, []ds.ToolBundleInfo{
			{Name: "sl-record", Version: "1.9.1-build-4", Locality: ds.DeploymentLocalityTarget},
			{Name: "sl-analyze", Version: "1.9.1-build-4", Locality: ds.DeploymentLocalityTarget},
			{Name: "jitdump-jvm", Version: "0.1.1", Locality: ds.DeploymentLocalityTarget},
		}, bundles)
	})

	t.Run("successfully filters tools bundles for unsupported platform", func(t *testing.T) {
		bundles, err := ds.ResolveToolBundles(t.Context(), targetX86, nil, recipeDeployments, depResolver)
		require.NoError(t, err)

		assert.ElementsMatch(t, []ds.ToolBundleInfo{
			{Name: "sl-record", Version: "1.9.1-build-4", Locality: ds.DeploymentLocalityTarget},
			{Name: "sl-analyze", Version: "1.9.1-build-4", Locality: ds.DeploymentLocalityTarget},
		}, bundles)
	})

	t.Run("successfully filters in/out a tool when requirement not met", func(t *testing.T) {
		// Filtered out with no parameters
		deployments := []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeTool,
						Name:    "streamline-cli",
						Version: "1.0.0",
						RequiredWhen: ds.RequirementSpec{
							Type: ds.RequirementTypeIfParamIsSet,
							Parameters: map[string]any{
								"feature": "on",
							},
						},
					},
				},
			},
		}

		bundles, err := ds.ResolveToolBundles(t.Context(), targetAarch64, nil, deployments, depResolver)
		require.NoError(t, err)
		assert.Empty(t, bundles)

		// Filtered in with a matching parameter
		paramValues := parameters.BoundParameters{
			Parameters: parameters.Parameters{
				Radio: []parameters.RadioParameter{
					{Parameter: parameters.Parameter{ID: "feature"}},
				},
			},
			Values: parameters.ParameterValues{
				Radio: []string{"on"},
			},
		}

		bundles, err = ds.ResolveToolBundles(t.Context(), targetAarch64, &paramValues, deployments, depResolver)
		require.NoError(t, err)
		assert.ElementsMatch(t, []ds.ToolBundleInfo{
			{Name: "sl-record", Version: "1.9.1-build-4", Locality: ds.DeploymentLocalityTarget},
			{Name: "sl-analyze", Version: "1.9.1-build-4", Locality: ds.DeploymentLocalityTarget},
			{Name: "jitdump-jvm", Version: "0.1.1", Locality: ds.DeploymentLocalityTarget},
		}, bundles)
	})

	t.Run("fails when a tool depends on a tool (depth > 1)", func(t *testing.T) {
		// Create a deployment that depends on a tool which itself depends on another tool
		deployments := []ds.DeploymentDeclaration{
			{
				Dependencies: []ds.Dependency{
					{
						Type:    ds.DependencyTypeTool,
						Name:    "upstream-tool",
						Version: "1.0.0",
						RequiredWhen: ds.RequirementSpec{
							Type: ds.RequirementTypeAlways,
						},
					},
				},
			},
		}

		// Mock a tool that depends on another tool
		streamlineWithDep := &tool_mocks.ToolIntegrationMock{}
		streamlineWithDep.On("Properties").Return(tool.IntegrationProperties{
			Name:    "upstream-tool",
			Version: "1.0.0",
			Deployments: []ds.DeploymentDeclaration{
				{
					Dependencies: []ds.Dependency{
						{
							Type:    ds.DependencyTypeTool,
							Name:    "downstream-tool",
							Version: "2.0.0",
							RequiredWhen: ds.RequirementSpec{
								Type: ds.RequirementTypeAlways,
							},
						},
					},
				},
			},
		})

		depResolverWithDep := getDepResolverFunc([]tool.ToolIntegration{streamlineWithDep})

		_, err := ds.ResolveToolBundles(t.Context(), targetAarch64, nil, deployments, depResolverWithDep)
		assert.Equal(t, message.New(message.EngineDeploymentsupportDependencyNotAllowed), err)
	})

	streamline.AssertExpectations(t)
}

func TestContainsHostToolBundles(t *testing.T) {
	t.Run("returns false for empty bundles", func(t *testing.T) {
		assert.False(t, ds.ContainsHostToolBundles(nil))
		assert.False(t, ds.ContainsHostToolBundles([]ds.ToolBundleInfo{}))
	})

	t.Run("returns false when all bundles are target locality", func(t *testing.T) {
		bundles := []ds.ToolBundleInfo{
			{Name: "tool-a", Version: "1.0", Locality: ds.DeploymentLocalityTarget},
			{Name: "tool-b", Version: "2.0", Locality: ds.DeploymentLocalityTarget},
		}

		assert.False(t, ds.ContainsHostToolBundles(bundles))
	})

	t.Run("returns true when any bundle has host locality", func(t *testing.T) {
		bundles := []ds.ToolBundleInfo{
			{Name: "tool-a", Version: "1.0", Locality: ds.DeploymentLocalityTarget},
			{Name: "tool-b", Version: "2.0", Locality: ds.DeploymentLocalityHost},
		}

		assert.True(t, ds.ContainsHostToolBundles(bundles))
	})
}

func TestGetDeploymentSupportForPlatform_RejectsLocalityOnToolDependency(t *testing.T) {
	targetPlatform := conductor.PlatformConfiguration{
		OS:           conductor.Linux,
		Architecture: conductor.AArch64,
	}
	deployments := []ds.DeploymentDeclaration{{
		Dependencies: []ds.Dependency{{
			Type:         ds.DependencyTypeTool,
			Name:         "bad-tool",
			Version:      "1.0.0",
			Locality:     ds.DeploymentLocalityHost,
			RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
		}},
	}}

	support, err := ds.GetDeploymentSupportForPlatform(func(name, version string) ([]ds.DeploymentDeclaration, error) {
		return nil, nil
	}, ds.MatchAll, deployments, targetPlatform)

	assert.Equal(t, ds.PlatformSupport{
		Platform: targetPlatform,
		Result:   ds.PlatformNotSupported,
	}, support)
	assert.Equal(t, message.New(message.EngineDeploymentsupportLocalityNotAllowedForToolDependency), err)
}

func TestGetDeploymentSupportForPlatform_RejectsInvalidLocalityOnToolBundleDependency(t *testing.T) {
	targetPlatform := conductor.PlatformConfiguration{
		OS:           conductor.Linux,
		Architecture: conductor.AArch64,
	}
	deployments := []ds.DeploymentDeclaration{{
		Dependencies: []ds.Dependency{{
			Type:         ds.DependencyTypeToolBundle,
			Name:         "bad-bundle",
			Version:      "1.0.0",
			Locality:     ds.DeploymentLocality("elsewhere"),
			RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways},
		}},
	}}

	support, err := ds.GetDeploymentSupportForPlatform(func(name, version string) ([]ds.DeploymentDeclaration, error) {
		return nil, nil
	}, ds.MatchAll, deployments, targetPlatform)

	assert.Equal(t, ds.PlatformSupport{
		Platform: targetPlatform,
		Result:   ds.PlatformNotSupported,
	}, support)
	assert.Equal(t, message.New(message.EngineDeploymentsupportInvalidLocality).WithMetadata(map[string]string{
		"locality": "elsewhere",
	}), err)
}
