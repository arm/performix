// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver"
)

type fakeRunner struct {
	called bool
	err    error
}

func (f *fakeRunner) Run(ctx context.Context, in io.ReadCloser, out io.Writer, errOut io.Writer) error {
	f.called = true
	return f.err
}

func newTestMCPStartCmd(runner MCPRunner) *cobra.Command {
	return newMCPStartCmd(func(mcpserver.MCPServerOptions) MCPRunner {
		return runner
	})
}

func TestRootCommand(t *testing.T) {
	t.Run("creates visible MCP root command", func(t *testing.T) {
		cmd := newMCPCommand()

		assert.Equal(t, "mcp", cmd.Use)
		assert.Contains(t, cmd.Short, "Set up")
		assert.NotEmpty(t, cmd.Long)
		assert.False(t, cmd.Hidden)
		assert.Equal(t, grouping.GroupMCP, cmd.Annotations[grouping.GroupAnnotation])
		require.Len(t, cmd.Commands(), 5)
		for _, subcommand := range cmd.Commands() {
			assert.False(t, subcommand.Hidden)
		}
	})

	t.Run("shows help when help flag used", func(t *testing.T) {
		cmd := newMCPCommand()

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"--help"})

		_, err := cmd.ExecuteC()

		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Set up")
	})

	t.Run("rejects unexpected arguments", func(t *testing.T) {
		cmd := newMCPCommand()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"unexpected arguments go here"})

		_, err := cmd.ExecuteC()

		require.Error(t, err)
	})

	t.Run("shows help without arguments", func(t *testing.T) {
		cmd := newMCPCommand()

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{})

		_, err := cmd.ExecuteC()

		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Set up")
	})
}

func TestMCPStartSourceSharingFlag(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		acknowledged bool
	}{
		{"default", []string{}, false},
		{"enabled", []string{"--acknowledge-source-sharing"}, true},
		{"disabled", []string{"--acknowledge-source-sharing=false"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			var gotOptions mcpserver.MCPServerOptions
			cmd := newMCPStartCmd(func(options mcpserver.MCPServerOptions) MCPRunner {
				gotOptions = options
				return runner
			})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)

			_, err := cmd.ExecuteC()

			require.NoError(t, err)
			require.True(t, runner.called)
			assert.Equal(t, tc.acknowledged, gotOptions.SourceSharingAcknowledged)
		})
	}
}

func TestRun(t *testing.T) {
	t.Run("create new MCP start command and call injectable fake runner", func(t *testing.T) {
		f := &fakeRunner{}
		cmd := newTestMCPStartCmd(f)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{})

		_, err := cmd.ExecuteC()

		assert.NoError(t, err)
		assert.True(t, f.called)
		assert.Equal(t, "start", cmd.Use)
		assert.Contains(t, cmd.Short, "Start")
		assert.NotEmpty(t, cmd.Long)
		assert.Equal(t, grouping.GroupMCPSub, cmd.Annotations[grouping.GroupAnnotation])
		require.NotNil(t, cmd.RunE)
	})

	t.Run("MCP start command writes no non-protocol output", func(t *testing.T) {
		cmd := newTestMCPStartCmd(&fakeRunner{})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{})

		_, err := cmd.ExecuteC()

		require.NoError(t, err)
		require.Empty(t, buf.String())
	})

	t.Run("MCP start command accepts no arguments", func(t *testing.T) {
		cmd := newTestMCPStartCmd(&fakeRunner{})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{})

		_, err := cmd.ExecuteC()

		require.NoError(t, err)
	})

	t.Run("MCP start command rejects unexpected arguments", func(t *testing.T) {
		cmd := newTestMCPStartCmd(&fakeRunner{})
		cmd.SetArgs([]string{"unexpected"})

		_, err := cmd.ExecuteC()

		require.Error(t, err)
	})

	t.Run("MCP start command shows help without running server", func(t *testing.T) {
		f := &fakeRunner{}
		cmd := newTestMCPStartCmd(f)

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"--help"})

		_, err := cmd.ExecuteC()

		require.NoError(t, err)
		assert.False(t, f.called)
		assert.Contains(t, buf.String(), "Start")
		assert.Contains(t, buf.String(), "--acknowledge-source-sharing")
	})

	t.Run("MCP start command wraps runner error", func(t *testing.T) {
		expectedErr := errors.New("runner failed")
		cmd := newTestMCPStartCmd(&fakeRunner{err: expectedErr})
		var outBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		var errBuf bytes.Buffer
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{})

		_, err := cmd.ExecuteC()

		require.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)
		assert.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
		assert.Contains(t, err.Error(), "failed to start MCP server")
		assert.Empty(t, outBuf.String())
		assert.Contains(t, errBuf.String(), "failed to start MCP server")
	})
}
