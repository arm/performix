// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	targetservice "github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

// mcpAddTargetResult decodes the add_target tool's JSON text output.
type mcpAddTargetResult struct {
	TargetName     string     `json:"target_name"`
	Host           string     `json:"host"`
	IsDefault      bool       `json:"is_default"`
	ConnectivityOK bool       `json:"connectivity_ok"`
	Error          *toolError `json:"error,omitempty"`
}

// decodeAddTargetResult extracts the structured result from a tool call.
func decodeAddTargetResult(t *testing.T, result *mcp.CallToolResult) mcpAddTargetResult {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var decoded mcpAddTargetResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &decoded))
	return decoded
}

// newTargetTestEngine returns an engine client mock that answers a single TargetTest call
// with the given connection status and no error chain.
func newTargetTestEngine(t *testing.T, status apapproto.ConnectionStatus) *apapprotomocks.ApapClient {
	t.Helper()
	engine := apapprotomocks.NewApapClient(t)
	engine.On("TargetTest", mock.Anything, mock.Anything).Return(&apapproto.TargetTestResponse{
		Connection: &apapproto.TargetTestConnection{Status: status},
	}, nil).Once()
	return engine
}

func TestAddTargetTool(t *testing.T) {
	t.Run("advertises false read-only hint", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		assert.Equal(t, "add_target", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.False(t, tools.Tools[0].Annotations.ReadOnlyHint)
	})

	t.Run("advertises added, added with connectivity error, or add error output schema", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		require.NotNil(t, tools.Tools[0].OutputSchema)

		schemaJSON, err := json.Marshal(tools.Tools[0].OutputSchema)
		require.NoError(t, err)
		schemaText := string(schemaJSON)
		assert.NotContains(t, schemaText, "Present only"+" when")
		assert.NotContains(t, schemaText, "omitted on"+" error")
		assert.Contains(t, schemaText, `"$defs"`)
		assert.Contains(t, schemaText, `"$ref"`)
		assert.Contains(t, schemaText, `"enum":["Info","Warning","Error"]`)

		var outputSchema struct {
			OneOf []struct {
				Required []string `json:"required"`
			} `json:"oneOf"`
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(schemaJSON, &outputSchema))
		require.Len(t, outputSchema.OneOf, 3)
		assert.ElementsMatch(t, []string{"target_name", "host", "is_default", "connectivity_ok"}, outputSchema.OneOf[0].Required)
		assert.ElementsMatch(t, []string{"target_name", "host", "is_default", "connectivity_ok", "error"}, outputSchema.OneOf[1].Required)
		assert.ElementsMatch(t, []string{"error"}, outputSchema.OneOf[2].Required)
		_, hasRemovedErrorField := outputSchema.Properties["connectivity"+"_"+"error"]
		assert.False(t, hasRemovedErrorField)
		assert.Equal(t, "object", outputSchema.Properties["error"].Type)

		resolved, err := addTargetOutputSchema.Resolve(nil)
		require.NoError(t, err)
		assert.NoError(t, resolved.Validate(map[string]any{
			"target_name":     "myhost",
			"host":            "10.0.0.1",
			"is_default":      true,
			"connectivity_ok": true,
		}))
		assert.NoError(t, resolved.Validate(map[string]any{
			"target_name":     "myhost",
			"host":            "10.0.0.1",
			"is_default":      true,
			"connectivity_ok": false,
			"error":           map[string]any{"severity": "Error", "message": "connection to target failed"},
		}))
		assert.NoError(t, resolved.Validate(map[string]any{
			"target_name": "myhost",
			"host":        "10.0.0.1",
			"error":       map[string]any{"severity": "Error", "message": "read failed"},
		}))
		assert.NoError(t, resolved.Validate(map[string]any{
			"error": map[string]any{"severity": "Error", "message": "write failed"},
		}))
		// Validate a nested child chain deeper is handled correctly.
		deepError := map[string]any{
			"severity": "Error",
			"message":  "level 0",
			"children": []any{map[string]any{
				"severity": "Error",
				"message":  "level 1",
				"children": []any{map[string]any{
					"severity": "Error",
					"message":  "level 2",
					"children": []any{map[string]any{
						"severity": "Error",
						"message":  "level 3",
						"children": []any{map[string]any{
							"severity": "Error",
							"message":  "level 4",
						}},
					}},
				}},
			}},
		}
		assert.NoError(t, resolved.Validate(map[string]any{"error": deepError}))
	})

	t.Run("adds an SSH target with defaults", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.MatchedBy(func(tgt engine_target.Target) bool {
			ssh, ok := tgt.(*engine_target.SSHTarget)
			if !ok || len(ssh.Jumps) != 1 {
				return false
			}
			hop := ssh.Jumps[0]
			return hop.Host == "10.0.0.1" &&
				hop.Port == defaultAddTargetPort &&
				hop.Username == "alice" &&
				hop.PrivateKeyFilename == "" &&
				hop.AuthMethod == engine_target.SSHAuthMethodKey &&
				hop.HostKeyPolicy == engine_target.RejectHostKeyIfMissing
		})).Return(nil).Once()
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "localhost",
			Targets: map[string]engine_target.Target{"myhost": newTestTarget("10.0.0.1")},
		}, nil).Once()

		engine := newTargetTestEngine(t, apapproto.ConnectionStatus_CONNECTION_STATUS_OK)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.Equal(t, "myhost", decoded.TargetName)
		assert.Equal(t, "10.0.0.1", decoded.Host)
		assert.False(t, decoded.IsDefault)
		assert.True(t, decoded.ConnectivityOK)
		assert.Nil(t, decoded.Error)
		targets.AssertExpectations(t)
		targets.AssertNotCalled(t, "SetDefaultTarget", mock.Anything)
	})

	t.Run("flows port and private key through", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.MatchedBy(func(tgt engine_target.Target) bool {
			ssh, ok := tgt.(*engine_target.SSHTarget)
			if !ok || len(ssh.Jumps) != 1 {
				return false
			}
			hop := ssh.Jumps[0]
			return hop.Port == 2222 &&
				hop.PrivateKeyFilename == "/home/alice/.ssh/id_ed25519" &&
				hop.HostKeyPolicy == engine_target.RejectHostKeyIfMissing
		})).Return(nil).Once()
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "myhost",
			Targets: map[string]engine_target.Target{"myhost": newTestTarget("10.0.0.1")},
		}, nil).Once()

		engine := newTargetTestEngine(t, apapproto.ConnectionStatus_CONNECTION_STATUS_OK)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "add_target",
			Arguments: map[string]any{
				"host":             "10.0.0.1",
				"user":             "alice",
				"name":             "myhost",
				"port":             2222,
				"private_key_path": "/home/alice/.ssh/id_ed25519",
			},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.True(t, decoded.ConnectivityOK)
		targets.AssertExpectations(t)
	})

	t.Run("adds a target through jump hosts in order", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.MatchedBy(func(tgt engine_target.Target) bool {
			ssh, ok := tgt.(*engine_target.SSHTarget)
			if !ok || len(ssh.Jumps) != 3 {
				return false
			}
			first, second, destination := ssh.Jumps[0], ssh.Jumps[1], ssh.Jumps[2]
			return first.Host == "jump1" &&
				first.Username == "jumper" &&
				first.Port == 2022 &&
				first.PrivateKeyFilename == "/keys/jump" &&
				first.HostKeyPolicy == engine_target.RejectHostKeyIfMissing &&
				first.AuthMethod == engine_target.SSHAuthMethodKey &&
				second.Host == "jump2" &&
				second.Port == defaultAddTargetPort &&
				second.HostKeyPolicy == engine_target.RejectHostKeyIfMissing &&
				destination.Host == "10.0.0.1" &&
				destination.Username == "alice" &&
				destination.Port == defaultAddTargetPort
		})).Return(nil).Once()
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "myhost",
			Targets: map[string]engine_target.Target{"myhost": newTestTarget("10.0.0.1")},
		}, nil).Once()

		engine := newTargetTestEngine(t, apapproto.ConnectionStatus_CONNECTION_STATUS_OK)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "add_target",
			Arguments: map[string]any{
				"host": "10.0.0.1",
				"user": "alice",
				"name": "myhost",
				"jumps": []any{
					map[string]any{"host": "jump1", "user": "jumper", "port": 2022, "private_key_path": "/keys/jump"},
					map[string]any{"host": "jump2"},
				},
			},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.Equal(t, "myhost", decoded.TargetName)
		assert.True(t, decoded.ConnectivityOK)
		targets.AssertExpectations(t)
	})

	t.Run("rejects an out-of-range jump port before adding", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "add_target",
			Arguments: map[string]any{
				"host":  "10.0.0.1",
				"user":  "alice",
				"name":  "myhost",
				"jumps": []any{map[string]any{"host": "jump1", "port": 70000}},
			},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, text.Text, "jumps")
		assert.Contains(t, text.Text, "maximum")
		targets.AssertNotCalled(t, "AddTarget", mock.Anything, mock.Anything)
	})

	t.Run("reports an internal error when persisting the target fails", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.Anything).Return(errors.New("write failed")).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		require.NotNil(t, decoded.Error)
		assert.NotEmpty(t, decoded.Error.Message)
		targets.AssertNotCalled(t, "ReadTargetConfig")
	})

	t.Run("omits success-only fields when it fails before probing connectivity", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.Anything).Return(errors.New("write failed")).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		// A failure before the connectivity probe must carry only the error, so a consumer
		// cannot mistake an unattempted probe for a real probe failure (connectivity_ok:false).
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(text.Text), &fields))
		assert.Contains(t, fields, "error")
		assert.NotContains(t, fields, "connectivity_ok")
		assert.NotContains(t, fields, "is_default")
		assert.NotContains(t, fields, "target_name")
		assert.NotContains(t, fields, "host")
	})

	t.Run("reports target details when reading config fails after adding", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.Anything).Return(nil).Once()
		targets.On("ReadTargetConfig").Return((*engine_target.TargetConfig)(nil), errors.New("read failed")).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.Equal(t, "myhost", decoded.TargetName)
		assert.Equal(t, "10.0.0.1", decoded.Host)
		assert.False(t, decoded.IsDefault)
		assert.False(t, decoded.ConnectivityOK)
		require.NotNil(t, decoded.Error)
		assert.Contains(t, decoded.Error.Message, "read failed")
		targets.AssertExpectations(t)
	})

	t.Run("reports target details when setting default fails after adding", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.Anything).Return(nil).Once()
		targets.On("SetDefaultTarget", "myhost").Return(errors.New("default failed")).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost", "set_default": true},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.Equal(t, "myhost", decoded.TargetName)
		assert.Equal(t, "10.0.0.1", decoded.Host)
		assert.False(t, decoded.IsDefault)
		assert.False(t, decoded.ConnectivityOK)
		require.NotNil(t, decoded.Error)
		assert.Contains(t, decoded.Error.Message, "default failed")
		targets.AssertExpectations(t)
		targets.AssertNotCalled(t, "ReadTargetConfig")
	})

	t.Run("generates a name when omitted and reports the one it persisted", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		// ReadTargetConfig is read once to pick a non-colliding name, then again to report the default.
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "localhost",
			Targets: map[string]engine_target.Target{},
		}, nil)

		var persistedName string
		targets.On("AddTarget", mock.AnythingOfType("string"), mock.Anything).
			Run(func(args mock.Arguments) { persistedName = args.String(0) }).
			Return(nil).Once()

		engine := newTargetTestEngine(t, apapproto.ConnectionStatus_CONNECTION_STATUS_OK)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.NotEmpty(t, persistedName)
		assert.Equal(t, persistedName, decoded.TargetName)
		assert.False(t, decoded.IsDefault)
		assert.True(t, decoded.ConnectivityOK)
		targets.AssertExpectations(t)
	})

	t.Run("sets the target as default when requested", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.Anything).Return(nil).Once()
		targets.On("SetDefaultTarget", "myhost").Return(nil).Once()
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "myhost",
			Targets: map[string]engine_target.Target{"myhost": newTestTarget("10.0.0.1")},
		}, nil).Once()

		engine := newTargetTestEngine(t, apapproto.ConnectionStatus_CONNECTION_STATUS_OK)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost", "set_default": true},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.True(t, decoded.IsDefault)
		assert.True(t, decoded.ConnectivityOK)
		targets.AssertExpectations(t)
	})

	t.Run("reports engine default promotion without set_default", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.Anything).Return(nil).Once()
		// The engine promotes the first real target over the reserved localhost default.
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "myhost",
			Targets: map[string]engine_target.Target{"myhost": newTestTarget("10.0.0.1")},
		}, nil).Once()

		engine := newTargetTestEngine(t, apapproto.ConnectionStatus_CONNECTION_STATUS_OK)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.True(t, decoded.IsDefault)
		targets.AssertExpectations(t)
		targets.AssertNotCalled(t, "SetDefaultTarget", mock.Anything)
	})

	t.Run("propagates add target error", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "dup", mock.Anything).Return(errors.New("target already exists")).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "dup"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, text.Text, "target already exists")
		targets.AssertNotCalled(t, "ReadTargetConfig")
	})

	t.Run("rejects an out-of-range port before adding", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost", "port": 70000},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, text.Text, "maximum")
		targets.AssertNotCalled(t, "AddTarget", mock.Anything, mock.Anything)
	})

	t.Run("rejects a request missing the host", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"user": "alice"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		targets.AssertNotCalled(t, "AddTarget", mock.Anything, mock.Anything)
	})

	t.Run("rejects a request missing the user", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		targets.AssertNotCalled(t, "AddTarget", mock.Anything, mock.Anything)
	})

	t.Run("reports failed connectivity without rolling back the add", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.Anything).Return(nil).Once()
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "myhost",
			Targets: map[string]engine_target.Target{"myhost": newTestTarget("10.0.0.1")},
		}, nil).Once()
		engine := newTargetTestEngine(t, apapproto.ConnectionStatus_CONNECTION_STATUS_ERROR)

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost"},
		})

		require.NoError(t, err)
		// The target is added, but the failed connectivity probe is reported as a tool error.
		require.True(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.Equal(t, "myhost", decoded.TargetName)
		assert.False(t, decoded.ConnectivityOK)
		require.NotNil(t, decoded.Error)
		assert.NotEmpty(t, decoded.Error.Message)
		targets.AssertExpectations(t)
	})

	t.Run("reports connectivity failure when the test rpc errors", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("AddTarget", "myhost", mock.Anything).Return(nil).Once()
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "myhost",
			Targets: map[string]engine_target.Target{"myhost": newTestTarget("10.0.0.1")},
		}, nil).Once()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("TargetTest", mock.Anything, mock.Anything).
			Return((*apapproto.TargetTestResponse)(nil), errors.New("engine unavailable")).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, AddTargetTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_target",
			Arguments: map[string]any{"host": "10.0.0.1", "user": "alice", "name": "myhost"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		decoded := decodeAddTargetResult(t, result)
		assert.False(t, decoded.ConnectivityOK)
		require.NotNil(t, decoded.Error)
		assert.Contains(t, decoded.Error.Message, "engine unavailable")
		targets.AssertExpectations(t)
	})
}

func TestBuildSSHTarget(t *testing.T) {
	t.Run("defaults the port to 22", func(t *testing.T) {
		tgt, err := buildSSHTarget(addTargetInput{Host: "10.0.0.1", User: "alice"})

		require.NoError(t, err)
		require.Len(t, tgt.Jumps, 1)
		assert.Equal(t, int32(defaultAddTargetPort), tgt.Jumps[0].Port)
	})

	t.Run("hardcodes a strict host key policy and key auth", func(t *testing.T) {
		tgt, err := buildSSHTarget(addTargetInput{Host: "10.0.0.1", User: "alice"})

		require.NoError(t, err)
		require.Len(t, tgt.Jumps, 1)
		assert.Equal(t, engine_target.RejectHostKeyIfMissing, tgt.Jumps[0].HostKeyPolicy)
		assert.Equal(t, engine_target.SSHAuthMethodKey, tgt.Jumps[0].AuthMethod)
	})

	t.Run("carries the supplied port and key path", func(t *testing.T) {
		tgt, err := buildSSHTarget(addTargetInput{
			Host:           "10.0.0.1",
			User:           "alice",
			Port:           2222,
			PrivateKeyPath: "/home/alice/.ssh/id_ed25519",
		})

		require.NoError(t, err)
		require.Len(t, tgt.Jumps, 1)
		assert.Equal(t, int32(2222), tgt.Jumps[0].Port)
		assert.Equal(t, "/home/alice/.ssh/id_ed25519", tgt.Jumps[0].PrivateKeyFilename)
	})

	t.Run("rejects an out-of-range port", func(t *testing.T) {
		_, err := buildSSHTarget(addTargetInput{Host: "10.0.0.1", User: "alice", Port: 70000})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid port")
	})

	t.Run("rejects a negative port", func(t *testing.T) {
		_, err := buildSSHTarget(addTargetInput{Host: "10.0.0.1", User: "alice", Port: -1})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid port")
	})

	t.Run("prepends jump hosts before the destination", func(t *testing.T) {
		tgt, err := buildSSHTarget(addTargetInput{
			Host: "10.0.0.1",
			User: "alice",
			Jumps: []addTargetJumpInput{
				{Host: "jump1", User: "jumper", Port: 2022, PrivateKeyPath: "/keys/jump"},
				{Host: "jump2"},
			},
		})

		require.NoError(t, err)
		require.Len(t, tgt.Jumps, 3)
		assert.Equal(t, "jump1", tgt.Jumps[0].Host)
		assert.Equal(t, "jumper", tgt.Jumps[0].Username)
		assert.Equal(t, int32(2022), tgt.Jumps[0].Port)
		assert.Equal(t, "/keys/jump", tgt.Jumps[0].PrivateKeyFilename)
		assert.Equal(t, "jump2", tgt.Jumps[1].Host)
		assert.Equal(t, int32(defaultAddTargetPort), tgt.Jumps[1].Port)
		// The destination host is always the final jump.
		assert.Equal(t, "10.0.0.1", tgt.Jumps[2].Host)
		assert.Equal(t, "alice", tgt.Jumps[2].Username)
	})

	t.Run("applies a strict host key policy and key auth to jump hosts", func(t *testing.T) {
		tgt, err := buildSSHTarget(addTargetInput{
			Host:  "10.0.0.1",
			User:  "alice",
			Jumps: []addTargetJumpInput{{Host: "jump1"}},
		})

		require.NoError(t, err)
		require.Len(t, tgt.Jumps, 2)
		assert.Equal(t, engine_target.RejectHostKeyIfMissing, tgt.Jumps[0].HostKeyPolicy)
		assert.Equal(t, engine_target.SSHAuthMethodKey, tgt.Jumps[0].AuthMethod)
	})

	t.Run("rejects an out-of-range jump port", func(t *testing.T) {
		_, err := buildSSHTarget(addTargetInput{
			Host:  "10.0.0.1",
			User:  "alice",
			Jumps: []addTargetJumpInput{{Host: "jump1", Port: 70000}},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "jump host 1")
		assert.Contains(t, err.Error(), "invalid port")
	})
}
