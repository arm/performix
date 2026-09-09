// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// This file contains a few demonstrative JS unit tests

func TestLinuxPerfProperties(t *testing.T) {
	t.Run("properties are correct", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "linux_perf")
		props := h.ToolProperties()
		assert.True(t, props.SupportsWorkloadLaunch)
		assert.Empty(t, props.Migrations)
	})
}

func TestLinuxPerfProbe(t *testing.T) {
	t.Run("probe", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "linux_perf")

		mockEngine := mocks.MockToolEngine{}
		mockEngine.On("ExecCommand", []string{"perf", "--version"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{}, nil))
		mockEngine.On("ExecCommand", []string{"python3", "--version"}, tool_goja.ExecOptions{}).
			Return(h.ToJSValPromise(t, process.CommandResult{Rc: 5}, nil))

		result, err := h.ToolProbe(t, &mockEngine, tool_goja.ToolContext{})
		assert.NoError(t, err)
		assert.False(t, result.Available)
		assert.Equal(t, 1, len(result.Advice))
		assert.Equal(t, message.EngineRecipeparserJsRecipeStageReadinessMessage, result.Advice[0].MessageCode)
		assert.Equal(t, "error", result.Advice[0].Level)
		assert.Contains(t, result.Advice[0].Metadata["message"], "Python3 is not available")
	})
}

func TestLinuxPerfRun(t *testing.T) {
	t.Run("fails if perfArgs param is not provided", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "linux_perf")

		mockEngine := mocks.MockToolEngineIgnoreLogs()
		mockEngine.On("CreateTempDir").Return(h.ToJSValPromise(t, "/tmp/dir", nil))

		ctx := jstest.EmptyToolContext()
		err := h.ToolRun(t, mockEngine, ctx)
		assert.ErrorContains(t, err, "perfArgs is required")
	})
	t.Run("builds process args correctly for launch workload", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "linux_perf")

		tmpDir := "/tmp/dir"
		mockEngine := mocks.MockToolEngineIgnoreLogs()
		mockEngine.On("CreateTempDir").Return(h.ToJSValPromise(t, tmpDir, nil))

		ctx := jstest.EmptyToolContext()
		ctx.Params["perfArgs"] = "-a -b -c"
		ctx.Workload = &tool_goja.WorkloadLaunch{
			Type:    "launch",
			Command: []string{"abc", "123"},
		}

		expectedCmd := []string{"perf", "-a", "-b", "-c", "-o", tmpDir + "/perf.data", "--", "abc", "123"}
		mockEngine.On("StartProcess", expectedCmd, mock.MatchedBy(func(received tool_goja.ProcessOptions) bool {
			return received.AsPrivileged
		})).Return(h.ToJSValPromise(t, nil, errors.New("an error!")))

		err := h.ToolRun(t, mockEngine, ctx)
		assert.ErrorContains(t, err, "an error!")
		mockEngine.AssertExpectations(t)
	})
}

func TestLinuxPerfOnCancel(t *testing.T) {
	t.Run("kills record handle", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "linux_perf")

		ctx := jstest.EmptyToolContext()
		handle := tool_mocks.MockProcessHandle{}
		// ToJSProcessHandle calls Stdout and Stderr to convert them to iterators
		handle.On("Stdout").Return(&bytes.Buffer{})
		handle.On("Stderr").Return(&bytes.Buffer{})

		killed := make(chan struct{})
		handle.On("Kill").Run(func(mock.Arguments) { close(killed) }).Return(nil)

		jsHandle := h.ToJSProcessHandle(t, &handle, tool_goja.ProcessHandleBindingOptions{})
		ctx.Metadata["recordHandle"] = jsHandle

		err := h.ToolCancel(t, nil, ctx)
		assert.NoError(t, err)

		// Needed as OnCancel() doesn't wait for `Kill` to return
		select {
		case <-killed:
		case <-time.After(time.Second):
			t.Fatal("Kill was not called")
		}

		handle.AssertExpectations(t)
	})
}
