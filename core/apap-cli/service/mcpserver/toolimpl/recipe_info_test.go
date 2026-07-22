// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	recipejson "github.com/Arm-Debug/apap-cli/apap-cli/service/clijson/recipe"
	targetservice "github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func decodeRecipeInfoResult(t *testing.T, result *mcp.CallToolResult) recipeInfoResult {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var decoded recipeInfoResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &decoded))
	return decoded
}

func optionDescription(description string) *string {
	return &description
}

func TestRecipeInfoTool(t *testing.T) {
	t.Run("advertises read-only hint and schemas", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RecipeInfoTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		assert.Equal(t, "recipe_info", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.True(t, tools.Tools[0].Annotations.ReadOnlyHint)
		require.NotNil(t, tools.Tools[0].InputSchema)
		require.NotNil(t, tools.Tools[0].OutputSchema)
		schemaJSON, err := json.Marshal(tools.Tools[0].InputSchema)
		require.NoError(t, err)
		assert.Contains(t, string(schemaJSON), "recipe")
		schemaJSON, err = json.Marshal(tools.Tools[0].OutputSchema)
		require.NoError(t, err)
		assert.Contains(t, string(schemaJSON), "mcp_guidance")
	})

	t.Run("returns recipe metadata without target", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ParseRecipe", mock.Anything, &apapproto.ParseRecipeMessage{Name: "asct"}).Return(&apapproto.ParseRecipeResponse{
			Name:        "asct",
			Title:       "Arm System Characterisation Tool",
			Description: "Collects system characterisation benchmarks.",
			McpGuidance: proto.String("Use timeout 600 for default benchmark runs."),
			Version:     "1.0",
			Status:      apapproto.RecipeStatus_RECIPE_STATUS_STABLE,
			Parameters: []*apapproto.ParameterList{
				{Parameter: &apapproto.ParameterList_SingleSelect{SingleSelect: &apapproto.SingleSelectParameter{
					Base:         &apapproto.RecipeParameterBase{ID: "benchmark_set", Label: "Benchmark set", Description: "Select benchmark set."},
					DefaultValue: "quick",
					Options: []*apapproto.ParameterOption{
						{Value: "quick", Label: "Quick", Description: optionDescription("Short smoke test")},
						{Value: "all", Label: "All"},
					},
				}}},
				{Parameter: &apapproto.ParameterList_Checkbox{Checkbox: &apapproto.CheckboxParameter{
					Base:         &apapproto.RecipeParameterBase{ID: "include_memory", Label: "Include memory", Description: "Run memory benchmarks."},
					DefaultValue: true,
				}}},
			},
			RenderParameters: []*apapproto.RenderParameter{
				{Id: "thread_id", Type: apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_NUMBER, IsArray: true},
			},
			PlatformSupport: []*apapproto.PlatformSupport{
				{
					Platform: &apapproto.PlatformConfiguration{Arch: "aarch64", Os: "Linux"},
					Result:   apapproto.PlatformSupportResult_PLATFORM_SUPPORTED,
				},
				{
					Platform: &apapproto.PlatformConfiguration{Arch: "x86_64", Os: "Windows"},
					Result:   apapproto.PlatformSupportResult_PLATFORM_UNSUPPORTED,
				},
			},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RecipeInfoTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "recipe_info",
			Arguments: map[string]any{"recipe": "asct"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.NotContains(t, text.Text, `"options"`)
		decoded := decodeRecipeInfoResult(t, result)
		assert.Equal(t, "asct", decoded.Name)
		assert.Equal(t, "Arm System Characterisation Tool", decoded.Title)
		assert.Equal(t, "Use timeout 600 for default benchmark runs.", decoded.MCPGuidance)
		assert.Equal(t, "stable", decoded.Status)
		require.Len(t, decoded.Parameters, 2)
		assert.Equal(t, "benchmark_set", decoded.Parameters[0].ID)
		assert.Equal(t, "single_select", decoded.Parameters[0].Config.Type)
		assert.Equal(t, "quick", decoded.Parameters[0].Config.DefaultValue)
		require.Len(t, decoded.Parameters[0].Config.OptionItems, 2)
		assert.Equal(t, "Short smoke test", decoded.Parameters[0].Config.OptionItems[0].Description)
		assert.Equal(t, "checkbox", decoded.Parameters[1].Config.Type)
		assert.Equal(t, true, decoded.Parameters[1].Config.DefaultValue)
		require.Len(t, decoded.RenderParameters, 1)
		assert.Equal(t, recipejson.RenderParameterJSON{ID: "thread_id", Type: "number", IsArray: true}, decoded.RenderParameters[0])
		require.Len(t, decoded.SupportedPlatforms, 1)
		assert.Equal(t, recipejson.PlatformConfigurationJSON{Architecture: "aarch64", OS: "Linux"}, decoded.SupportedPlatforms[0].Platform)
		assert.Nil(t, decoded.TargetSupport)
	})

	t.Run("returns target-specific support", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		engine := apapprotomocks.NewApapClient(t)
		engine.On("ParseRecipe", mock.Anything, mock.MatchedBy(func(req *apapproto.ParseRecipeMessage) bool {
			return req.GetName() == "memory_access" && req.GetTarget() != nil
		})).Return(&apapproto.ParseRecipeResponse{
			Name: "memory_access",
			Parameters: []*apapproto.ParameterList{
				{Parameter: &apapproto.ParameterList_Radio{Radio: &apapproto.RadioParameter{
					Base:         &apapproto.RecipeParameterBase{ID: "sampling_freq", Label: "Sampling Frequency"},
					DefaultValue: "normal",
					Options: []*apapproto.ParameterOption{
						{Value: "normal", Label: "Normal"},
						{Value: "high", Label: "High"},
					},
				}}},
			},
			PlatformSupport: []*apapproto.PlatformSupport{
				{
					Result: apapproto.PlatformSupportResult_PLATFORM_CONDITIONALLY_SUPPORTED,
					ConditionList: []*apapproto.PlatformSupportRequirementSpec{
						{Type: "kernel_driver", Parameters: map[string]string{"driver": "arm_spe_pmu"}},
					},
				},
			},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RecipeInfoTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "recipe_info",
			Arguments: map[string]any{"recipe": "memory_access", "target": "myhost"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeRecipeInfoResult(t, result)
		assert.Equal(t, "memory_access", decoded.Name)
		assert.Empty(t, decoded.SupportedPlatforms)
		require.NotNil(t, decoded.TargetSupport)
		assert.Equal(t, "conditionally_supported", decoded.TargetSupport.Result)
		require.Len(t, decoded.TargetSupport.Conditions, 1)
		assert.Equal(t, "kernel_driver", decoded.TargetSupport.Conditions[0].Type)
		assert.Equal(t, "arm_spe_pmu", decoded.TargetSupport.Conditions[0].Parameters["driver"])
	})

	t.Run("returns structured error payload", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ParseRecipe", mock.Anything, &apapproto.ParseRecipeMessage{Name: "missing"}).
			Return((*apapproto.ParseRecipeResponse)(nil), message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": "missing"})).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, RecipeInfoTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "recipe_info",
			Arguments: map[string]any{"recipe": "missing"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeRecipeInfoResult(t, result)
		require.NotNil(t, decoded.Error)
		assert.Equal(t, string(message.EngineRecipeDoesNotExist), decoded.Error.Code)
		assert.Equal(t, string(message.SeverityError), decoded.Error.Severity)
		assert.Contains(t, decoded.Error.Message, "missing")
		assert.NotEmpty(t, decoded.Error.Explanation)
		assert.NotEmpty(t, decoded.Error.Advice)
		assert.Equal(t, "missing", decoded.Error.Metadata["recipe"])
	})
}
