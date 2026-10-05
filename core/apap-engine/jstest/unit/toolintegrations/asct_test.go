// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestASCTProbe(t *testing.T) {
	t.Run("reports error when Python venv creation fails", func(t *testing.T) {
		harness := jstest.LoadToolIntegration(t, "asct")
		engine := mocks.MockToolEngine{}

		engine.On("CreateTempDir").
			Return(harness.ToJSValPromise(t, "/tmp/asct-probe", nil)).
			Once()
		engine.On("ExecCommand", []string{"python3", "-m", "venv", "/tmp/asct-probe/probe-venv"}, tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
		engine.On("ExecCommand", []string{"gcc", "-dumpfullversion", "-dumpversion"}, tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{Stdout: "11.2.0"}, nil))
		engine.On("ExecCommand", mock.Anything, tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{}, nil))

		engine.On("GetPlatform").
			Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil)

		context := jstest.EmptyToolContext()
		context.ToolsRoot = "/target/tools"

		result, err := harness.ToolProbe(t, &engine, context)

		require.NoError(t, err)
		require.False(t, result.Available)
		require.Len(t, result.Advice, 1)
		require.Equal(t, "error", result.Advice[0].Level)
		require.Equal(t, message.EngineRecipeparserJsRecipeStageReadinessMessage, result.Advice[0].MessageCode)
		require.Contains(t, result.Advice[0].Metadata["message"], "python3-venv")
		engine.AssertExpectations(t)
	})
}
