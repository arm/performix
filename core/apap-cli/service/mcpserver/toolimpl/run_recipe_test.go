// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	targetservice "github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

// mcpRunRecipeResult decodes the run_recipe tool's JSON text output.
type mcpRunRecipeResult struct {
	RunID        string     `json:"run_id"`
	Status       string     `json:"run_status"`
	TargetStatus string     `json:"target_status"`
	Error        *toolError `json:"error"`
}

// decodeRunRecipeResult extracts the structured result carried in the tool's text content. Both
// successful and operational-error results carry the JSON document, so this works for either.
func decodeRunRecipeResult(t *testing.T, result *mcp.CallToolResult) mcpRunRecipeResult {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var decoded mcpRunRecipeResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &decoded))
	return decoded
}

// fakeRecipeStream is a scripted Apap_RecipeIssueCommandClient. Recv returns each queued response
// in turn, then io.EOF once they are exhausted. The embedded nil grpc.ClientStream satisfies the
// rest of the interface; only Recv is exercised by the tool.
type fakeRecipeStream struct {
	grpc.ClientStream
	responses []*apapproto.RecipeResponse
	idx       int
}

func (s *fakeRecipeStream) Recv() (*apapproto.RecipeResponse, error) {
	if s.idx < len(s.responses) {
		response := s.responses[s.idx]
		s.idx++
		return response, nil
	}
	return nil, io.EOF
}

func recipeStream(responses ...*apapproto.RecipeResponse) *fakeRecipeStream {
	return &fakeRecipeStream{responses: responses}
}

func runStartResponse(runID string) *apapproto.RecipeResponse {
	return &apapproto.RecipeResponse{Id: &apapproto.RunId{Value: runID}}
}

func runFinishResponse(code apapproto.StatusCode, errChain *apapproto.ErrorChain) *apapproto.RecipeResponse {
	return &apapproto.RecipeResponse{
		SubMessage: &apapproto.RecipeResponse_RecipeFinish{
			RecipeFinish: &apapproto.RecipeFinish{ReturnCode: code, Error: errChain},
		},
	}
}

func recipeListing(names ...string) *apapproto.RecipeNameListing {
	entries := make([]*apapproto.RecipeNameEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, &apapproto.RecipeNameEntry{
			Identifier: &apapproto.RecipeNameEntry_Name{Name: name},
		})
	}
	return &apapproto.RecipeNameListing{RecipeNames: entries}
}

func expectAvailableRecipes(engine *apapprotomocks.ApapClient, names ...string) {
	engine.On("ListRecipes", mock.Anything, &emptypb.Empty{}).Return(recipeListing(names...), nil).Once()
}

var expectedBundledRecipeNames = []string{
	"asct",
	"asct-new-ui",
	"cache_sharing",
	"cmn_analysis",
	"code_hotspots",
	"cpu_microarchitecture",
	"instruction_mix",
	"memory_access",
	"syscall_trace_summary",
	"system_utilization",
}

func bundledRecipeNames(t *testing.T) []string {
	t.Helper()

	recipeFiles, err := filepath.Glob(filepath.Join("..", "..", "..", "recipes", "*.js"))
	require.NoError(t, err)
	require.NotEmpty(t, recipeFiles)

	parser := &recipeparser.RecipeParserJS{APIFactory: recipeparser.CreateConcreteAPI}
	names := make([]string, 0, len(recipeFiles))
	for _, recipeFile := range recipeFiles {
		contents, err := os.ReadFile(recipeFile)
		require.NoError(t, err)
		parsedRecipe, err := parser.ParseRecipe(recipeFile, string(contents))
		require.NoError(t, err, "parse %s", recipeFile)
		names = append(names, parsedRecipe.Name)
	}
	sort.Strings(names)
	require.Equal(t, expectedBundledRecipeNames, names)
	return names
}

// expectTarget stubs the manager so GetTarget(name) resolves to the given host.
func expectTarget(targets *targetservice.MockTargetManager, name, host string) {
	targets.On("GetTarget", name).Return(newTestTarget(host), nil).Once()
}

func TestRunRecipeTool(t *testing.T) {
	t.Run("advertises false read-only hint", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		targets := &targetservice.MockTargetManager{}
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		assert.Equal(t, "run_recipe", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.False(t, tools.Tools[0].Annotations.ReadOnlyHint)
		require.NotNil(t, tools.Tools[0].InputSchema)
		schemaJSON, err := json.Marshal(tools.Tools[0].InputSchema)
		require.NoError(t, err)
		assert.NotContains(t, string(schemaJSON), `"enum"`)
		assert.Contains(t, string(schemaJSON), "list_recipes")
	})

	t.Run("runs a system-wide recipe to completion", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		var issuedCmd *apapproto.RecipeCommand
		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { issuedCmd = args.Get(1).(*apapproto.RecipeCommand) }).
			Return(recipeStream(
				runStartResponse("run-123"),
				runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "system": map[string]any{}},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "completed", decoded.Status)
		assert.Equal(t, "run-123", decoded.RunID)
		assert.Equal(t, "DEPLOYED", decoded.TargetStatus)
		assert.Nil(t, decoded.Error)

		require.NotNil(t, issuedCmd)
		startCommand := issuedCmd.GetStartCommand()
		require.NotNil(t, startCommand)
		assert.Equal(t, apapproto.ToolDeploy_AUTO, startCommand.GetDeploymentType())
		assert.NotNil(t, startCommand.GetWorkload().GetSystemWideWorkload())
		require.NotNil(t, startCommand.Timeout)
		assert.Equal(t, defaultRunTimeoutSeconds, startCommand.GetTimeout())

		structured, ok := result.StructuredContent.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "completed", structured["run_status"])

		targets.AssertExpectations(t)
	})

	t.Run("passes an explicit zero timeout through as no timeout", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		var issuedCmd *apapproto.RecipeCommand
		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "system_utilization")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { issuedCmd = args.Get(1).(*apapproto.RecipeCommand) }).
			Return(recipeStream(
				runStartResponse("run-system"),
				runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "system_utilization", "target": "myhost", "system": map[string]any{}, "timeout": 0},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		require.NotNil(t, issuedCmd)
		require.NotNil(t, issuedCmd.GetStartCommand().Timeout)
		assert.Equal(t, uint32(0), issuedCmd.GetStartCommand().GetTimeout())

		targets.AssertExpectations(t)
	})

	t.Run("uses a positive timeout when provided", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		var issuedCmd *apapproto.RecipeCommand
		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "system_utilization")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { issuedCmd = args.Get(1).(*apapproto.RecipeCommand) }).
			Return(recipeStream(
				runStartResponse("run-system"),
				runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "system_utilization", "target": "myhost", "system": map[string]any{}, "timeout": 30},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		require.NotNil(t, issuedCmd)
		assert.Equal(t, uint32(30), issuedCmd.GetStartCommand().GetTimeout())

		targets.AssertExpectations(t)
	})

	t.Run("runs every bundled recipe through the same MCP path", func(t *testing.T) {
		ctx := context.Background()
		recipeNames := bundledRecipeNames(t)

		for _, recipeName := range recipeNames {
			t.Run(recipeName, func(t *testing.T) {
				targets := &targetservice.MockTargetManager{}
				expectTarget(targets, "myhost", "10.0.0.1")

				var issuedCmd *apapproto.RecipeCommand
				engine := apapprotomocks.NewApapClient(t)
				expectAvailableRecipes(engine, recipeNames...)
				engine.On("TargetPrepare", mock.Anything, mock.Anything).
					Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
				engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) { issuedCmd = args.Get(1).(*apapproto.RecipeCommand) }).
					Return(recipeStream(
						runStartResponse("run-"+recipeName),
						runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
					), nil).Once()

				clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
				defer clientSession.Close()
				defer serverSession.Close()

				result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      "run_recipe",
					Arguments: map[string]any{"recipe": recipeName, "target": "myhost", "system": map[string]any{}},
				})

				require.NoError(t, err)
				require.False(t, result.IsError)
				decoded := decodeRunRecipeResult(t, result)
				assert.Equal(t, "completed", decoded.Status)
				assert.Equal(t, "run-"+recipeName, decoded.RunID)

				require.NotNil(t, issuedCmd)
				startCommand := issuedCmd.GetStartCommand()
				require.NotNil(t, startCommand)
				assert.Equal(t, recipeName, startCommand.GetName())
				assert.NotNil(t, startCommand.GetWorkload().GetSystemWideWorkload())
				assert.Equal(t, defaultRunTimeoutSeconds, startCommand.GetTimeout())

				targets.AssertExpectations(t)
			})
		}
	})

	t.Run("launches a workload on an explicitly named target", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("GetTarget", "remote").Return(newTestTarget("10.0.0.2"), nil).Once()

		var issuedCmd *apapproto.RecipeCommand
		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_NO_ACTION}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { issuedCmd = args.Get(1).(*apapproto.RecipeCommand) }).
			Return(recipeStream(
				runStartResponse("run-9"),
				runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_recipe",
			Arguments: map[string]any{
				"recipe":  "code_hotspots",
				"target":  "remote",
				"timeout": 30,
				"launch": map[string]any{
					"command":     "./bench --fast",
					"working_dir": "/tmp/work",
					"use_shell":   true,
					"env":         map[string]any{"MODE": "fast"},
				},
			},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "completed", decoded.Status)
		assert.Equal(t, "run-9", decoded.RunID)

		require.NotNil(t, issuedCmd)
		startCommand := issuedCmd.GetStartCommand()
		require.NotNil(t, startCommand)
		assert.Equal(t, apapproto.ToolDeploy_AUTO, startCommand.GetDeploymentType())
		assert.Equal(t, "code_hotspots", startCommand.GetName())
		assert.Equal(t, uint32(30), startCommand.GetTimeout())
		launch := startCommand.GetWorkload().GetLaunchWorkload()
		require.NotNil(t, launch)
		assert.Equal(t, "./bench --fast", launch.GetCommand())
		assert.Equal(t, "/tmp/work", launch.GetWorkingDir())
		assert.True(t, launch.GetUseShell())
		assert.Equal(t, map[string]string{"MODE": "fast"}, launch.GetEnvironment())

		targets.AssertExpectations(t)
	})

	t.Run("uses the MCP default timeout for a launched workload", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "remote", "10.0.0.2")

		var issuedCmd *apapproto.RecipeCommand
		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_NO_ACTION}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { issuedCmd = args.Get(1).(*apapproto.RecipeCommand) }).
			Return(recipeStream(
				runStartResponse("run-launch-default"),
				runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_recipe",
			Arguments: map[string]any{
				"recipe": "code_hotspots",
				"target": "remote",
				"launch": map[string]any{"command": "./bench --fast"},
			},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		require.NotNil(t, issuedCmd)
		assert.Equal(t, defaultRunTimeoutSeconds, issuedCmd.GetStartCommand().GetTimeout())

		targets.AssertExpectations(t)
	})

	t.Run("attaches to a running process by pid", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		var issuedCmd *apapproto.RecipeCommand
		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { issuedCmd = args.Get(1).(*apapproto.RecipeCommand) }).
			Return(recipeStream(
				runStartResponse("run-pid"),
				runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "attach_to_pid": map[string]any{"pid": 4321}},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "completed", decoded.Status)

		require.NotNil(t, issuedCmd)
		attach := issuedCmd.GetStartCommand().GetWorkload().GetAttachWorkload()
		require.NotNil(t, attach)
		assert.Equal(t, int32(4321), attach.GetPid())
		assert.Equal(t, defaultRunTimeoutSeconds, issuedCmd.GetStartCommand().GetTimeout())

		targets.AssertExpectations(t)
	})

	t.Run("reports a target preparation failure", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOY}, nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "system": map[string]any{}},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "error", decoded.Status)
		require.NotNil(t, decoded.Error)
		assert.Contains(t, decoded.Error.Message, "deployed")

		engine.AssertNotCalled(t, "RecipeIssueCommand", mock.Anything, mock.Anything)
		targets.AssertExpectations(t)
	})

	t.Run("rejects an unexpected target preparation result", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult(99)}, nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "system": map[string]any{}},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "error", decoded.Status)
		require.NotNil(t, decoded.Error)
		assert.Contains(t, decoded.Error.Message, "unexpected")

		engine.AssertNotCalled(t, "RecipeIssueCommand", mock.Anything, mock.Anything)
		targets.AssertExpectations(t)
	})

	t.Run("reports a failed recipe run", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Return(recipeStream(
				runStartResponse("run-7"),
				runFinishResponse(apapproto.StatusCode_ERROR, &apapproto.ErrorChain{
					Root: &apapproto.ErrorNode{Error: "recipe collection failed"},
				}),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "system": map[string]any{}},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "failed", decoded.Status)
		assert.Equal(t, "run-7", decoded.RunID)
		require.NotNil(t, decoded.Error)
		assert.Contains(t, decoded.Error.Message, "recipe collection failed")

		targets.AssertExpectations(t)
	})

	t.Run("reports an error when the stream ends without a finish", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		// The stream yields a run ID but is truncated before any RecipeFinish message.
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Return(recipeStream(
				runStartResponse("run-trunc"),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "system": map[string]any{}},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "error", decoded.Status)
		assert.Equal(t, "run-trunc", decoded.RunID)
		require.NotNil(t, decoded.Error)
		assert.NotEmpty(t, decoded.Error.Message)

		targets.AssertExpectations(t)
	})

	t.Run("rejects a call with no workload mode", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		targets := &targetservice.MockTargetManager{}

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, text.Text, "oneOf")

		targets.AssertNotCalled(t, "GetTarget", mock.Anything)
	})

	t.Run("rejects a call with multiple workload modes", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		targets := &targetservice.MockTargetManager{}

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "system": map[string]any{}, "attach_to_pid": map[string]any{"pid": 1234}},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, text.Text, "oneOf")
	})

	t.Run("rejects a recipe that is not available from the engine catalog", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		targets := &targetservice.MockTargetManager{}

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "dummy_recipe", "target": "myhost", "system": map[string]any{}},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "error", decoded.Status)
		require.NotNil(t, decoded.Error)
		assert.Equal(t, string(message.EngineRecipeDoesNotExist), decoded.Error.Code)
		assert.Contains(t, decoded.Error.Message, "dummy_recipe")

		targets.AssertNotCalled(t, "GetTarget", mock.Anything)
		engine.AssertNotCalled(t, "TargetPrepare", mock.Anything, mock.Anything)
	})

	t.Run("rejects a non-positive pid", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		targets := &targetservice.MockTargetManager{}

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		// 0 is not a real process and -1 is the CLI's system-wide sentinel; both must be rejected.
		for _, pid := range []int{0, -1} {
			result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
				Name:      "run_recipe",
				Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "attach_to_pid": map[string]any{"pid": pid}},
			})

			require.NoError(t, err)
			require.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			assert.Contains(t, text.Text, "pid")
		}
	})
}

func TestBuildRecipeWorkloadAndroidLaunch(t *testing.T) {
	workload, err := buildRecipeWorkload(runRecipeInput{
		AndroidLaunch: &androidLaunchOpts{
			PackageName:  "com.example.app",
			ActivityName: ".MainActivity",
		},
	})
	require.NoError(t, err)

	android := workload.GetAndroidLaunchWorkload()
	require.NotNil(t, android)
	assert.Equal(t, "com.example.app", android.PackageName)
	assert.Equal(t, ".MainActivity", android.ActivityName)
}

// TestRunRecipeToolParameters checks how run_recipe passes recipe-specific parameters to
// the engine. The MCP server does not interpret parameter names, and existing
// recipe-specific tests cover testing the specific parameter each recipe supports.
// As such, these tests cover JSON-to-protobuf conversion (MCP to Apap gRPC service),
// when validation is called, and how validation errors are reported.
func TestRunRecipeToolParameters(t *testing.T) {
	assertJSONParameterValues := func(params map[string]*structpb.Value) {
		t.Helper()

		require.Contains(t, params, "string_param")
		assert.Equal(t, "normal", params["string_param"].GetStringValue())
		require.Contains(t, params, "bool_param")
		assert.True(t, params["bool_param"].GetBoolValue())
		require.Contains(t, params, "number_param")
		assert.InEpsilon(t, 0.25, params["number_param"].GetNumberValue(), 0.0001)

		require.Contains(t, params, "array_param")
		array := params["array_param"].GetListValue()
		require.NotNil(t, array)
		require.Len(t, array.Values, 2)
		assert.Equal(t, "frontend_bound", array.Values[0].GetStringValue())
		assert.Equal(t, "backend_bound", array.Values[1].GetStringValue())

		require.Contains(t, params, "object_param")
		object := params["object_param"].GetStructValue()
		require.NotNil(t, object)
		require.Contains(t, object.Fields, "nested")
		assert.Equal(t, "value", object.Fields["nested"].GetStringValue())

		require.Contains(t, params, "null_param")
		assert.True(t, proto.Equal(structpb.NewNullValue(), params["null_param"]))
	}

	t.Run("forwards JSON values as protobuf values", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		target := newTestTarget("10.0.0.1")
		targets.On("GetTarget", "myhost").Return(target, nil).Once()
		expectedProtoTarget := grpcserver.TargetToProto(target)

		var issuedCmd *apapproto.RecipeCommand
		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("RecipeValidateParameters", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) {
				req := args.Get(1).(*apapproto.RecipeValidateParametersRequest)
				assert.Equal(t, "code_hotspots", req.GetRecipeName())
				require.NotNil(t, req.TargetName)
				assert.Equal(t, "myhost", req.GetTargetName())
				assert.True(t, proto.Equal(expectedProtoTarget, req.GetTarget()))
				assert.NotNil(t, req.GetWorkload().GetSystemWideWorkload())
				assertJSONParameterValues(req.GetParameters())
			}).
			Return(&apapproto.RecipeValidateParametersResponse{}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Run(func(args mock.Arguments) { issuedCmd = args.Get(1).(*apapproto.RecipeCommand) }).
			Return(recipeStream(
				runStartResponse("run-params"),
				runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_recipe",
			Arguments: map[string]any{
				"recipe": "code_hotspots",
				"target": "myhost",
				"system": map[string]any{},
				"parameters": map[string]any{
					"array_param":  []any{"frontend_bound", "backend_bound"},
					"bool_param":   true,
					"null_param":   nil,
					"number_param": 0.25,
					"object_param": map[string]any{"nested": "value"},
					"string_param": "normal",
				},
			},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)

		require.NotNil(t, issuedCmd)
		startCommand := issuedCmd.GetStartCommand()
		require.NotNil(t, startCommand)
		assert.Equal(t, "code_hotspots", startCommand.GetName())
		require.NotNil(t, startCommand.TargetName)
		assert.Equal(t, "myhost", startCommand.GetTargetName())
		assert.True(t, proto.Equal(expectedProtoTarget, startCommand.GetTarget()))
		assert.NotNil(t, startCommand.GetWorkload().GetSystemWideWorkload())
		assertJSONParameterValues(startCommand.GetParameters())

		targets.AssertExpectations(t)
	})

	t.Run("skips validation when parameters are omitted", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("RecipeIssueCommand", mock.Anything, mock.Anything).
			Return(recipeStream(
				runStartResponse("run-no-params"),
				runFinishResponse(apapproto.StatusCode_SUCCESS, nil),
			), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "run_recipe",
			Arguments: map[string]any{"recipe": "code_hotspots", "target": "myhost", "system": map[string]any{}},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		engine.AssertNotCalled(t, "RecipeValidateParameters", mock.Anything, mock.Anything)
		targets.AssertExpectations(t)
	})

	t.Run("rejects values that cannot be represented as protobuf values", func(t *testing.T) {
		_, err := recipeParameters(map[string]any{"unsupported": complex(1, 2)})

		require.Error(t, err)
		assert.Contains(t, err.Error(), `invalid value for recipe parameter "unsupported"`)
	})

	t.Run("reports invalid recipe parameters", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("RecipeValidateParameters", mock.Anything, mock.Anything).
			Return(&apapproto.RecipeValidateParametersResponse{
				Messages: []*apapproto.ParameterValidationResult{
					{
						ParameterId: "collect_java_stacks",
						Message:     &apapproto.ErrorChain{Root: &apapproto.ErrorNode{Error: "must be a boolean"}},
					},
				},
			}, nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_recipe",
			Arguments: map[string]any{
				"recipe":     "code_hotspots",
				"target":     "myhost",
				"system":     map[string]any{},
				"parameters": map[string]any{"collect_java_stacks": "notabool"},
			},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "error", decoded.Status)
		require.NotNil(t, decoded.Error)
		assert.Contains(t, decoded.Error.Message, "collect_java_stacks")
		assert.Contains(t, decoded.Error.Message, "must be a boolean")

		engine.AssertNotCalled(t, "RecipeIssueCommand", mock.Anything, mock.Anything)
		targets.AssertExpectations(t)
	})

	t.Run("surfaces catalog detail for an unknown recipe parameter", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		expectTarget(targets, "myhost", "10.0.0.1")

		engine := apapprotomocks.NewApapClient(t)
		expectAvailableRecipes(engine, "code_hotspots")
		engine.On("TargetPrepare", mock.Anything, mock.Anything).
			Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		// The engine rejects an unknown parameter name as a catalog-coded error rather
		// than as a per-parameter validation message. The MCP result should retain
		// the catalog fields so the client can reason from the code, explanation and
		// advice without a separate MCP-specific kind.
		engine.On("RecipeValidateParameters", mock.Anything, mock.Anything).
			Return(nil, message.New(message.EngineParametersInvalidParam)).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, RunRecipeTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "run_recipe",
			Arguments: map[string]any{
				"recipe":     "code_hotspots",
				"target":     "myhost",
				"system":     map[string]any{},
				"parameters": map[string]any{"bogus_param": true},
			},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeRunRecipeResult(t, result)
		assert.Equal(t, "error", decoded.Status)
		require.NotNil(t, decoded.Error)
		assert.Equal(t, message.EngineParametersInvalidParam, decoded.Error.Code)
		assert.Equal(t, string(message.SeverityError), decoded.Error.Severity)
		assert.NotEmpty(t, decoded.Error.Message)
		assert.NotEmpty(t, decoded.Error.Explanation)
		assert.NotEmpty(t, decoded.Error.Advice)

		engine.AssertNotCalled(t, "RecipeIssueCommand", mock.Anything, mock.Anything)
		targets.AssertExpectations(t)
	})
}
