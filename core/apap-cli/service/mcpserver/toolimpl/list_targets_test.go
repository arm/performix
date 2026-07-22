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
	"github.com/stretchr/testify/require"

	targetservice "github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

// mcpTargetsResult decodes the tool's JSON text output into the stable MCP contract.
type mcpTargetsResult struct {
	Targets      []mcpListedTarget `json:"targets"`
	TotalTargets int               `json:"total_targets"`
	Offset       int               `json:"offset"`
	Limit        int               `json:"limit"`
	Error        *toolError        `json:"error,omitempty"`
}

// mcpListedTarget mirrors the JSON contract a client sees for each listed target.
type mcpListedTarget struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	IsDefault   bool   `json:"is_default"`
	DisplayHost string `json:"display_host"`
}

func newTestTarget(host string) engine_target.Target {
	return &engine_target.SSHTarget{
		Jumps: []engine_target.SSHHostConfig{{Host: host, Username: "user", Port: 22}},
	}
}

func TestListTargetsTool(t *testing.T) {
	t.Run("advertises read-only hint", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, ListTargetsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		assert.Equal(t, "list_targets", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.True(t, tools.Tools[0].Annotations.ReadOnlyHint)
	})

	t.Run("returns target listing", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Default: "alpha",
			Targets: map[string]engine_target.Target{
				"alpha":     newTestTarget("10.0.0.1"),
				"localhost": &engine_target.LocalTarget{},
			},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, ListTargetsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_targets",
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		var content mcpTargetsResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, 2, content.TotalTargets)
		assert.Equal(t, 0, content.Offset)
		assert.Equal(t, defaultListTargetsLimit, content.Limit)
		require.Len(t, content.Targets, 2)

		// Targets are sorted case-sensitively by name, so "alpha" precedes "localhost".
		assert.Equal(t, mcpListedTarget{
			Name:        "alpha",
			Type:        "ssh",
			IsDefault:   true,
			DisplayHost: "user@10.0.0.1",
		}, content.Targets[0])
		assert.Equal(t, mcpListedTarget{
			Name:        "localhost",
			Type:        "local",
			IsDefault:   false,
			DisplayHost: "localhost",
		}, content.Targets[1])
	})

	t.Run("limits and offsets targets", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Targets: map[string]engine_target.Target{
				"alpha": newTestTarget("10.0.0.1"),
				"beta":  newTestTarget("10.0.0.2"),
				"gamma": newTestTarget("10.0.0.3"),
			},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, ListTargetsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_targets",
			Arguments: map[string]any{"limit": 1, "offset": 1},
		})

		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		var content mcpTargetsResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, 3, content.TotalTargets)
		assert.Equal(t, 1, content.Offset)
		assert.Equal(t, 1, content.Limit)
		require.Len(t, content.Targets, 1)
	})

	t.Run("rejects negative target limit", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, ListTargetsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_targets",
			Arguments: map[string]any{"limit": -1},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
	})

	t.Run("rejects negative offset", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, ListTargetsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_targets",
			Arguments: map[string]any{"offset": -1},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
	})

	t.Run("returns empty target array when no targets exist", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("ReadTargetConfig").Return(&engine_target.TargetConfig{
			Targets: map[string]engine_target.Target{},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, ListTargetsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_targets",
		})

		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		var content mcpTargetsResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.Equal(t, 0, content.TotalTargets)
		assert.Equal(t, 0, content.Offset)
		assert.Equal(t, defaultListTargetsLimit, content.Limit)
		assert.NotNil(t, content.Targets)
		assert.Empty(t, content.Targets)
	})

	t.Run("returns target config read error", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("ReadTargetConfig").Return((*engine_target.TargetConfig)(nil), errors.New("read failed")).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Targets: targets}, ListTargetsTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_targets",
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, text.Text, "read failed")
		var content mcpTargetsResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		require.NotNil(t, content.Error)
		assert.NotEmpty(t, content.Error.Message)
	})
}

func TestNewListTargetsResult(t *testing.T) {
	t.Run("includes every target name and value", func(t *testing.T) {
		targetsMap := map[string]engine_target.Target{
			"alpha":     newTestTarget("10.0.0.1"),
			"beta":      newTestTarget("10.0.0.2"),
			"localhost": &engine_target.LocalTarget{},
		}

		result := newListTargetsResult(targetsMap, "beta", listTargetsInput{Limit: defaultListTargetsLimit})

		assert.Equal(t, 3, result.TotalTargets)
		require.Len(t, result.Targets, 3)
		names := make([]string, 0, len(result.Targets))
		for _, listed := range result.Targets {
			names = append(names, listed.Name)
			assert.NotEmpty(t, listed.Name)
			assert.NotEmpty(t, listed.Type)
			assert.NotEmpty(t, listed.DisplayHost)
			assert.Equal(t, listed.Name == "beta", listed.IsDefault)
		}
		assert.ElementsMatch(t, []string{"alpha", "beta", "localhost"}, names)
	})

	t.Run("applies limit and offset", func(t *testing.T) {
		targetsMap := map[string]engine_target.Target{
			"alpha": newTestTarget("10.0.0.1"),
			"beta":  newTestTarget("10.0.0.2"),
			"gamma": newTestTarget("10.0.0.3"),
		}

		result := newListTargetsResult(targetsMap, "", listTargetsInput{Limit: 1, Offset: 1})

		assert.Equal(t, 3, result.TotalTargets)
		assert.Equal(t, 1, result.Offset)
		assert.Equal(t, 1, result.Limit)
		require.Len(t, result.Targets, 1)
	})

	t.Run("returns all targets when limit exceeds total", func(t *testing.T) {
		targetsMap := map[string]engine_target.Target{
			"alpha": newTestTarget("10.0.0.1"),
			"beta":  newTestTarget("10.0.0.2"),
		}

		result := newListTargetsResult(targetsMap, "", listTargetsInput{Limit: defaultListTargetsLimit})

		require.Len(t, result.Targets, 2)
	})

	t.Run("returns empty slice when offset exceeds total", func(t *testing.T) {
		targetsMap := map[string]engine_target.Target{
			"alpha": newTestTarget("10.0.0.1"),
			"beta":  newTestTarget("10.0.0.2"),
		}

		result := newListTargetsResult(targetsMap, "", listTargetsInput{Limit: defaultListTargetsLimit, Offset: 5})

		assert.Equal(t, 2, result.TotalTargets)
		assert.NotNil(t, result.Targets)
		assert.Empty(t, result.Targets)
	})

	t.Run("handles an empty target map", func(t *testing.T) {
		result := newListTargetsResult(map[string]engine_target.Target{}, "", listTargetsInput{Limit: defaultListTargetsLimit})

		assert.Equal(t, 0, result.TotalTargets)
		assert.NotNil(t, result.Targets)
		assert.Empty(t, result.Targets)
	})

	t.Run("orders targets alphabetically with case sensitivity", func(t *testing.T) {
		targetsMap := map[string]engine_target.Target{
			"Bravo":   newTestTarget("10.0.0.1"),
			"alpha":   newTestTarget("10.0.0.2"),
			"Delta":   newTestTarget("10.0.0.3"),
			"charlie": newTestTarget("10.0.0.4"),
			"bravo":   newTestTarget("10.0.0.5"),
		}

		result := newListTargetsResult(targetsMap, "", listTargetsInput{Limit: defaultListTargetsLimit})

		require.Len(t, result.Targets, 5)
		names := targetNames(result.Targets)
		// Case-sensitive byte-wise order: uppercase names ("Bravo", "Delta") sort
		// before lowercase ones, and the "Bravo"/"bravo" tie is deterministic.
		assert.Equal(t, []string{"Bravo", "Delta", "alpha", "bravo", "charlie"}, names)
	})

	t.Run("paginates over the case-sensitive order", func(t *testing.T) {
		targetsMap := map[string]engine_target.Target{
			"Bravo":   newTestTarget("10.0.0.1"),
			"alpha":   newTestTarget("10.0.0.2"),
			"charlie": newTestTarget("10.0.0.3"),
			"bravo":   newTestTarget("10.0.0.4"),
		}

		result := newListTargetsResult(targetsMap, "", listTargetsInput{Limit: 1, Offset: 1})

		require.Len(t, result.Targets, 1)
		assert.Equal(t, "alpha", result.Targets[0].Name)
	})
}

func targetNames(targets []listedTarget) []string {
	names := make([]string, 0, len(targets))
	for _, listed := range targets {
		names = append(names, listed.Name)
	}
	return names
}
