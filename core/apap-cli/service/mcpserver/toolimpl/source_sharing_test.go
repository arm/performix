// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connectSourceSharingTestClient(t *testing.T, options *mcp.ClientOptions) (*mcp.ClientSession, *mcp.CallToolParams) {
	t.Helper()
	cache, err := newAIInsightsBundleCache(1)
	require.NoError(t, err)
	cache.store("bundle-1", []aiInsightsPayload{{name: "payload", payload: "source evidence"}})
	client, server := connectTestServerWithClientOptions(t, t.Context(), ToolDependencies{}, options,
		func(s *mcp.Server, _ ToolDependencies) {
			GenerateAIInsightsTool{}.registerReadAIInsightsPayloadDetailsTool(s, cache)
		})
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return client, &mcp.CallToolParams{
		Name: "read_ai_insights_payload_details", Arguments: map[string]any{"bundle_id": "bundle-1", "name": "payload"},
	}
}

func TestSourceSharingPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	t.Setenv("APXD_CONFIG_DIR", dir)
	prompts := 0
	options := &mcp.ClientOptions{ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		prompts++
		assert.Equal(t, "form", req.Params.Mode)
		assert.Equal(t, sourceSharingMessage, req.Params.Message)
		assert.NoDirExists(t, dir, "acknowledgement must not be saved before the user responds")
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"acknowledged": true}}, nil
	}}
	client, call := connectSourceSharingTestClient(t, options)
	result, err := client.CallTool(t.Context(), call)
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, "source evidence")
	assert.Equal(t, 1, prompts)
	filename := filepath.Join(dir, mcpAcknowledgmentsDirname, sourceSharingAcknowledgementFilename)
	assert.FileExists(t, filename)

	// A new session reuses the saved acknowledgement without prompting.
	options = &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		prompts++
		return &mcp.ElicitResult{Action: "decline"}, nil
	}}
	client, call = connectSourceSharingTestClient(t, options)
	result, err = client.CallTool(t.Context(), call)
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Equal(t, 1, prompts)

	// Deleting the acknowledgement prompts again in the existing session. Declining stops the call.
	require.NoError(t, os.Remove(filename))
	result, err = client.CallTool(t.Context(), call)
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Equal(t, 2, prompts)
	assert.NoFileExists(t, filename)
}

func TestSourceSharingWithoutFormElicitation(t *testing.T) {
	for _, tc := range []struct {
		name string
		caps *mcp.ElicitationCapabilities
	}{
		{"unsupported", nil},
		{"url only", &mcp.ElicitationCapabilities{URL: &mcp.URLElicitationCapabilities{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "config")
			t.Setenv("APXD_CONFIG_DIR", dir)
			client, call := connectSourceSharingTestClient(t, &mcp.ClientOptions{
				Capabilities: &mcp.ClientCapabilities{Elicitation: tc.caps},
			})
			result, err := client.CallTool(t.Context(), call)
			require.NoError(t, err)
			require.False(t, result.IsError)
			assert.NoDirExists(t, dir)
		})
	}
}

func TestSourceSharingElicitationRejections(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *mcp.ElicitResult
	}{
		{"unchecked", &mcp.ElicitResult{Action: "accept", Content: map[string]any{"acknowledged": false}}},
		{"missing content", &mcp.ElicitResult{Action: "accept"}},
		{"decline", &mcp.ElicitResult{Action: "decline"}},
		{"cancel", &mcp.ElicitResult{Action: "cancel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "config")
			t.Setenv("APXD_CONFIG_DIR", dir)
			prompts := 0
			client, call := connectSourceSharingTestClient(t, &mcp.ClientOptions{
				// The legacy empty capability must also trigger form elicitation.
				Capabilities: &mcp.ClientCapabilities{Elicitation: &mcp.ElicitationCapabilities{}},
				ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					prompts++
					return tc.result, nil
				},
			})
			result, err := client.CallTool(t.Context(), call)
			assert.Equal(t, 1, prompts)
			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.Nil(t, result.StructuredContent)
			assert.NotContains(t, result.Content[0].(*mcp.TextContent).Text, "source evidence")
			assert.NoDirExists(t, dir)
		})
	}
}

func TestSourceSharingConcurrentRejection(t *testing.T) {
	for _, action := range []string{"decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "config")
			t.Setenv("APXD_CONFIG_DIR", dir)
			filename := filepath.Join(dir, mcpAcknowledgmentsDirname, sourceSharingAcknowledgementFilename)
			acceptedPrompts := 0
			acceptingClient, acceptingCall := connectSourceSharingTestClient(t, &mcp.ClientOptions{
				ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					acceptedPrompts++
					return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"acknowledged": true}}, nil
				},
			})
			var acceptedResult *mcp.CallToolResult
			var acceptedErr error
			rejectedPrompts := 0
			rejectingClient, rejectingCall := connectSourceSharingTestClient(t, &mcp.ClientOptions{
				ElicitationHandler: func(ctx context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					rejectedPrompts++
					// Save consent from another session while this call is awaiting a response.
					acceptedResult, acceptedErr = acceptingClient.CallTool(ctx, acceptingCall)
					assert.FileExists(t, filename)
					return &mcp.ElicitResult{Action: action}, nil
				},
			})

			result, err := rejectingClient.CallTool(t.Context(), rejectingCall)
			require.NoError(t, acceptedErr)
			require.NotNil(t, acceptedResult)
			require.False(t, acceptedResult.IsError)
			assert.Contains(t, acceptedResult.Content[0].(*mcp.TextContent).Text, "source evidence")
			assert.Equal(t, 1, acceptedPrompts)
			assert.Equal(t, 1, rejectedPrompts)
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.Nil(t, result.StructuredContent)
			assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, errSourceSharingPermissionDenied.Error())
			assert.NotContains(t, result.Content[0].(*mcp.TextContent).Text, "source evidence")
			assert.FileExists(t, filename)
		})
	}
}

func TestSourceSharingLegacyElicitation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	t.Setenv("APXD_CONFIG_DIR", dir)
	prompts := 0
	options := &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		prompts++
		return &mcp.ElicitResult{Action: "decline"}, nil
	}}
	client, server := connectTestServerWithClientOptions(t, t.Context(), ToolDependencies{}, options,
		func(s *mcp.Server, deps ToolDependencies) {
			// Reject discovery so the SDK negotiates the 2025-11-25 protocol.
			s.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
				return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
					if method == "server/discover" {
						return nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "unsupported"}
					}
					return next(ctx, method, req)
				}
			})
			RunQueryTool{}.Register(s, deps)
		})
	defer client.Close()
	defer server.Close()
	require.Equal(t, "2025-11-25", server.InitializeParams().ProtocolVersion)
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "run_query", Arguments: map[string]any{"session_id": "session-1", "sql": "SELECT 1"},
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Equal(t, 1, prompts)
	assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, errSourceSharingPermissionDenied.Error())
	assert.NoDirExists(t, dir)
}

func TestSourceSharingToolWiring(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"generate_ai_insights", map[string]any{"run_id": "run-1"}},
		{"run_query", map[string]any{"session_id": "session-1", "sql": "SELECT 1"}},
		{"read_ai_insights_payload_details", map[string]any{"bundle_id": "bundle-1", "name": "payload"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "config")
			t.Setenv("APXD_CONFIG_DIR", dir)
			options := &mcp.ClientOptions{ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
				return &mcp.ElicitResult{Action: "decline"}, nil
			}}
			client, server := connectTestServerWithClientOptions(t, t.Context(), ToolDependencies{}, options,
				RunQueryTool{}.Register, GenerateAIInsightsTool{}.Register)
			defer client.Close()
			defer server.Close()
			result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.Nil(t, result.StructuredContent)
			assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, errSourceSharingPermissionDenied.Error())
			assert.NoDirExists(t, dir)
		})
	}
}

func TestSourceSharingStorageFailureDoesNotBlockAcceptedCalls(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions without root privileges")
	}
	for _, tc := range []struct {
		name string
		mode os.FileMode
	}{
		{"cannot read marker", 0o000},
		{"cannot save marker", 0o500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("APXD_CONFIG_DIR", dir)
			acknowledgementsDir := filepath.Join(dir, mcpAcknowledgmentsDirname)
			filename := filepath.Join(acknowledgementsDir, sourceSharingAcknowledgementFilename)
			require.NoError(t, os.MkdirAll(acknowledgementsDir, 0o700))
			require.NoError(t, os.Chmod(acknowledgementsDir, tc.mode))
			t.Cleanup(func() { require.NoError(t, os.Chmod(acknowledgementsDir, 0o700)) })
			prompts := 0
			client, call := connectSourceSharingTestClient(t, &mcp.ClientOptions{
				ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					prompts++
					return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"acknowledged": true}}, nil
				},
			})
			for expectedPrompts := 1; expectedPrompts <= 2; expectedPrompts++ {
				result, err := client.CallTool(t.Context(), &mcp.CallToolParams{
					Name: call.Name, Arguments: call.Arguments,
				})
				require.NoError(t, err)
				require.False(t, result.IsError)
				assert.Contains(t, result.Content[0].(*mcp.TextContent).Text, "source evidence")
				assert.Equal(t, expectedPrompts, prompts)
			}
			require.NoError(t, os.Chmod(acknowledgementsDir, 0o700))
			assert.NoFileExists(t, filename)
		})
	}
}
