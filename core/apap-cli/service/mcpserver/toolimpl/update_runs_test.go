// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

// TestUpdateRunsTool checks the advertised schema, engine requests and batch results.
func TestUpdateRunsTool(t *testing.T) {
	t.Run("advertises mutation annotations and property schemas", func(t *testing.T) {
		ctx := context.Background()
		client, server := connectTestServer(t, ctx, ToolDependencies{}, UpdateRunsTool{}.Register)
		defer client.Close()
		defer server.Close()

		tools, err := client.ListTools(ctx, nil)
		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		tool := tools.Tools[0]
		assert.Equal(t, "update_runs", tool.Name)
		annotations := tool.Annotations
		require.NotNil(t, annotations)
		assert.False(t, annotations.ReadOnlyHint)
		require.NotNil(t, annotations.DestructiveHint)
		assert.True(t, *annotations.DestructiveHint)
		assert.True(t, annotations.IdempotentHint)
		require.NotNil(t, annotations.OpenWorldHint)
		assert.False(t, *annotations.OpenWorldHint)

		// Verify that the MCP SDK advertises the input schema as intended.
		schemaJSON, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema jsonschema.Schema
		require.NoError(t, json.Unmarshal(schemaJSON, &schema))
		assert.ElementsMatch(t, []string{"run_ids", "updates"}, schema.Required)
		require.Len(t, schema.Properties, 2)
		ids := schema.Properties["run_ids"]
		require.NotNil(t, ids)
		assert.Equal(t, "array", ids.Type)
		assert.Empty(t, ids.Types)
		require.NotNil(t, ids.Items)
		assert.Equal(t, "string", ids.Items.Type)
		assert.Equal(t, jsonschema.Ptr(1), ids.MinItems)
		assert.True(t, ids.UniqueItems)
		updates := schema.Properties["updates"]
		require.NotNil(t, updates)
		assert.Equal(t, "object", updates.Type)
		assert.Equal(t, jsonschema.Ptr(1), updates.MinProperties)
		require.Len(t, updates.Properties, 2)
		for property, valueName := range map[string]string{"group": "value", "host_source_paths": "paths"} {
			propertySchema := updates.Properties[property]
			require.NotNil(t, propertySchema)
			assert.Equal(t, "object", propertySchema.Type)
			assert.Empty(t, propertySchema.Types)
			assert.Equal(t, []string{"operation"}, propertySchema.Required)
			require.Len(t, propertySchema.OneOf, 2)
			setVariant, clearVariant := propertySchema.OneOf[0], propertySchema.OneOf[1]
			assert.ElementsMatch(t, []string{"operation", valueName}, setVariant.Required)
			assert.Equal(t, []any{"set"}, setVariant.Properties["operation"].Enum)
			assert.Equal(t, []string{"operation"}, clearVariant.Required)
			assert.Equal(t, []any{"clear"}, clearVariant.Properties["operation"].Enum)
			require.NotNil(t, clearVariant.Not)
			assert.Equal(t, []string{valueName}, clearVariant.Not.Required)
			for _, variant := range propertySchema.OneOf {
				require.Len(t, variant.Properties, 2)
				assert.Equal(t, propertySchema.Properties[valueName], variant.Properties[valueName])
			}
		}
		assert.Equal(t, "string", updates.Properties["group"].Properties["value"].Type)
		paths := updates.Properties["host_source_paths"].Properties["paths"]
		assert.Equal(t, "array", paths.Type)
		assert.Empty(t, paths.Types)
		assert.Equal(t, jsonschema.Ptr(1), paths.MinItems)
		require.NotNil(t, paths.Items)
		assert.Equal(t, "string", paths.Items.Type)
	})

	for name, test := range map[string]struct {
		updates    map[string]any
		operations []*apapproto.RunUpdateOperation
	}{
		"sets a trimmed group and leaves source paths unchanged": {
			updates: map[string]any{"group": map[string]any{"operation": "set", "value": " compiler "}},
			operations: []*apapproto.RunUpdateOperation{{Operation: &apapproto.RunUpdateOperation_SetGroup{
				SetGroup: &apapproto.SetRunGroup{Value: "compiler"},
			}}},
		},
		"clears a group and leaves source paths unchanged": {
			updates: map[string]any{"group": map[string]any{"operation": "clear"}},
			operations: []*apapproto.RunUpdateOperation{{Operation: &apapproto.RunUpdateOperation_ClearGroup{
				ClearGroup: &apapproto.ClearRunGroup{},
			}}},
		},
		"sets source paths exactly as supplied and leaves group unchanged": {
			updates: map[string]any{"host_source_paths": map[string]any{"operation": "set", "paths": []string{"/work/project src", " /work/with spaces "}}},
			operations: []*apapproto.RunUpdateOperation{{Operation: &apapproto.RunUpdateOperation_SetHostSourceCodePaths{
				SetHostSourceCodePaths: &apapproto.SetHostSourceCodePaths{Value: &apapproto.HostSourceCodePaths{Paths: []string{"/work/project src", " /work/with spaces "}}},
			}}},
		},
		"clears source paths and leaves group unchanged": {
			updates: map[string]any{"host_source_paths": map[string]any{"operation": "clear"}},
			operations: []*apapproto.RunUpdateOperation{{Operation: &apapproto.RunUpdateOperation_ClearHostSourceCodePaths{
				ClearHostSourceCodePaths: &apapproto.ClearHostSourceCodePaths{},
			}}},
		},
		"sets both properties in one request": {
			updates: map[string]any{
				"group":             map[string]any{"operation": "set", "value": "baseline"},
				"host_source_paths": map[string]any{"operation": "set", "paths": []string{"/work/src"}},
			},
			operations: []*apapproto.RunUpdateOperation{
				{Operation: &apapproto.RunUpdateOperation_SetGroup{SetGroup: &apapproto.SetRunGroup{Value: "baseline"}}},
				{Operation: &apapproto.RunUpdateOperation_SetHostSourceCodePaths{SetHostSourceCodePaths: &apapproto.SetHostSourceCodePaths{Value: &apapproto.HostSourceCodePaths{Paths: []string{"/work/src"}}}}},
			},
		},
		"clears both properties in one request": {
			updates: map[string]any{
				"group":             map[string]any{"operation": "clear"},
				"host_source_paths": map[string]any{"operation": "clear"},
			},
			operations: []*apapproto.RunUpdateOperation{
				{Operation: &apapproto.RunUpdateOperation_ClearGroup{ClearGroup: &apapproto.ClearRunGroup{}}},
				{Operation: &apapproto.RunUpdateOperation_ClearHostSourceCodePaths{ClearHostSourceCodePaths: &apapproto.ClearHostSourceCodePaths{}}},
			},
		},
		"sets group and clears source paths": {
			updates: map[string]any{
				"group":             map[string]any{"operation": "set", "value": "baseline"},
				"host_source_paths": map[string]any{"operation": "clear"},
			},
			operations: []*apapproto.RunUpdateOperation{
				{Operation: &apapproto.RunUpdateOperation_SetGroup{SetGroup: &apapproto.SetRunGroup{Value: "baseline"}}},
				{Operation: &apapproto.RunUpdateOperation_ClearHostSourceCodePaths{ClearHostSourceCodePaths: &apapproto.ClearHostSourceCodePaths{}}},
			},
		},
		"clears group and sets source paths": {
			updates: map[string]any{
				"group":             map[string]any{"operation": "clear"},
				"host_source_paths": map[string]any{"operation": "set", "paths": []string{"/work/src"}},
			},
			operations: []*apapproto.RunUpdateOperation{
				{Operation: &apapproto.RunUpdateOperation_ClearGroup{ClearGroup: &apapproto.ClearRunGroup{}}},
				{Operation: &apapproto.RunUpdateOperation_SetHostSourceCodePaths{SetHostSourceCodePaths: &apapproto.SetHostSourceCodePaths{Value: &apapproto.HostSourceCodePaths{Paths: []string{"/work/src"}}}}},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			engine := apapprotomocks.NewApapClient(t)
			expected := &apapproto.UpdateRunsRequest{
				RunIds: []*apapproto.RunId{{Value: "run-a"}, {Value: "run-b"}},
				Patch:  &apapproto.RunUpdatePatch{Operations: test.operations},
			}
			engine.On("UpdateRuns", mock.Anything, mock.MatchedBy(func(request *apapproto.UpdateRunsRequest) bool {
				return proto.Equal(expected, request)
			})).Return(&apapproto.UpdateRunsResponse{Statuses: []*apapproto.RunUpdateStatus{{Id: "run-a"}, {Id: "run-b"}}}, nil).Once()
			client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, UpdateRunsTool{}.Register)
			defer client.Close()
			defer server.Close()

			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "update_runs", Arguments: map[string]any{
				"run_ids": []string{" run-a ", "run-b"}, "updates": test.updates,
			}})
			require.NoError(t, err)
			assert.False(t, result.IsError)
			statuses := result.StructuredContent.(map[string]any)["statuses"].([]any)
			require.Len(t, statuses, 2)
			assert.Equal(t, "run-a", statuses[0].(map[string]any)["run_id"])
			assert.Equal(t, "run-b", statuses[1].(map[string]any)["run_id"])
			for _, status := range statuses {
				assert.Equal(t, true, status.(map[string]any)["success"])
			}
		})
	}

	t.Run("returns every status on partial failure", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("UpdateRuns", mock.Anything, mock.Anything).Return(&apapproto.UpdateRunsResponse{Statuses: []*apapproto.RunUpdateStatus{
			{Id: "run-a"},
			{Id: "missing", Error: message.BuildErrorChain(message.New(message.EngineRunDoesNotExist))},
		}}, nil).Once()
		client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, UpdateRunsTool{}.Register)
		defer client.Close()
		defer server.Close()

		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "update_runs", Arguments: map[string]any{
			"run_ids": []string{"run-a", "missing"},
			"updates": map[string]any{
				"group":             map[string]any{"operation": "clear"},
				"host_source_paths": map[string]any{"operation": "clear"},
			},
		}})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		structured := result.StructuredContent.(map[string]any)
		statuses := structured["statuses"].([]any)
		require.Len(t, statuses, 2)
		assert.Equal(t, true, statuses[0].(map[string]any)["success"])
		assert.Equal(t, false, statuses[1].(map[string]any)["success"])
		assert.NotNil(t, statuses[1].(map[string]any)["error"])
	})

	t.Run("rejects invalid input without calling the engine", func(t *testing.T) {
		clearGroup := map[string]any{"group": map[string]any{"operation": "clear"}}
		for name, arguments := range map[string]map[string]any{
			"missing IDs":      {"updates": clearGroup},
			"null IDs":         {"run_ids": nil, "updates": clearGroup},
			"no IDs":           {"run_ids": []string{}, "updates": clearGroup},
			"blank ID":         {"run_ids": []string{" "}, "updates": clearGroup},
			"duplicate ID":     {"run_ids": []string{"run", " run "}, "updates": clearGroup},
			"missing updates":  {"run_ids": []string{"run"}},
			"null updates":     {"run_ids": []string{"run"}, "updates": nil},
			"empty updates":    {"run_ids": []string{"run"}, "updates": map[string]any{}},
			"unknown argument": {"run_ids": []string{"run"}, "updates": clearGroup, "unknown": true},
		} {
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				engine := apapprotomocks.NewApapClient(t)
				client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, UpdateRunsTool{}.Register)
				defer client.Close()
				defer server.Close()
				result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "update_runs", Arguments: arguments})
				require.NoError(t, err)
				assert.True(t, result.IsError)
				engine.AssertNotCalled(t, "UpdateRuns", mock.Anything, mock.Anything)
			})
		}

		for name, updates := range map[string]map[string]any{
			"null group":                   {"group": nil},
			"missing group operation":      {"group": map[string]any{"value": "compiler"}},
			"unknown group operation":      {"group": map[string]any{"operation": "add", "value": "compiler"}},
			"missing set group":            {"group": map[string]any{"operation": "set"}},
			"null set group":               {"group": map[string]any{"operation": "set", "value": nil}},
			"blank set group":              {"group": map[string]any{"operation": "set", "value": " "}},
			"clear with group value":       {"group": map[string]any{"operation": "clear", "value": "compiler"}},
			"clear with empty group value": {"group": map[string]any{"operation": "clear", "value": ""}},
			"unknown group argument":       {"group": map[string]any{"operation": "clear", "unknown": true}},
			"null source paths":            {"host_source_paths": nil},
			"missing paths operation":      {"host_source_paths": map[string]any{"paths": []string{"/work/src"}}},
			"unknown paths operation":      {"host_source_paths": map[string]any{"operation": "add", "paths": []string{"/work/src"}}},
			"missing set paths":            {"host_source_paths": map[string]any{"operation": "set"}},
			"null set paths":               {"host_source_paths": map[string]any{"operation": "set", "paths": nil}},
			"empty set paths":              {"host_source_paths": map[string]any{"operation": "set", "paths": []string{}}},
			"blank source path":            {"host_source_paths": map[string]any{"operation": "set", "paths": []string{"/work/src", " "}}},
			"clear with paths":             {"host_source_paths": map[string]any{"operation": "clear", "paths": []string{"/work/src"}}},
			"clear with empty paths":       {"host_source_paths": map[string]any{"operation": "clear", "paths": []string{}}},
			"unknown paths argument":       {"host_source_paths": map[string]any{"operation": "clear", "unknown": true}},
			"unsupported tags":             {"tags": map[string]any{"operation": "clear"}},
			"unsupported target paths":     {"target_source_paths": map[string]any{"operation": "clear"}},
			"valid group with invalid paths": {
				"group":             map[string]any{"operation": "set", "value": "compiler"},
				"host_source_paths": map[string]any{"operation": "set", "paths": []string{" "}},
			},
		} {
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				engine := apapprotomocks.NewApapClient(t)
				client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, UpdateRunsTool{}.Register)
				defer client.Close()
				defer server.Close()
				result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "update_runs", Arguments: map[string]any{
					"run_ids": []string{"run"}, "updates": updates,
				}})
				require.NoError(t, err)
				assert.True(t, result.IsError)
				engine.AssertNotCalled(t, "UpdateRuns", mock.Anything, mock.Anything)
			})
		}
	})

	t.Run("returns an RPC failure as a top-level error", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("UpdateRuns", mock.Anything, mock.Anything).Return((*apapproto.UpdateRunsResponse)(nil), errors.New("update failed")).Once()
		client, server := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, UpdateRunsTool{}.Register)
		defer client.Close()
		defer server.Close()
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "update_runs", Arguments: map[string]any{
			"run_ids": []string{"run"}, "updates": map[string]any{"group": map[string]any{"operation": "clear"}},
		}})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, requireToolText(t, result), "update failed")
		structured := result.StructuredContent.(map[string]any)
		assert.Empty(t, structured["statuses"])
		assert.NotNil(t, structured["error"])
	})
}
