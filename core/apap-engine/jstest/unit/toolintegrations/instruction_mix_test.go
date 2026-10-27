// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestInstructionMixWorkingDirectoryValidation(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "instruction_mix")
	engine := &mocks.MockToolEngine{}

	engine.On(
		"ExecCommand",
		[]string{"python3", "--version"},
		tool_goja.ExecOptions{},
	).Return(
		harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil),
	).Once()
	engine.On("GetPlatform").Return(
		conductor.PlatformConfiguration{OS: conductor.Linux},
		nil,
	).Once()
	engine.On(
		"ExecCommand",
		[]string{
			"stat",
			"/target/tools/instruction_mix-0.4.5-py3-none-any.whl/0.4.5/instruction_mix-0.4.5-py3-none-any.whl",
		},
		tool_goja.ExecOptions{},
	).Return(
		harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil),
	).Once()
	engine.On("GetLocality").Return("target", nil).Once()
	engine.On(
		"ExecCommand",
		[]string{"test", "-d", "/foo/bar"},
		tool_goja.ExecOptions{},
	).Return(
		harness.ToJSValPromise(t, process.CommandResult{}, nil),
	).Once()
	engine.On(
		"ExecCommand",
		[]string{"test", "-x", "/foo/bar"},
		tool_goja.ExecOptions{},
	).Return(
		harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil),
	).Once()

	ctx := jstest.EmptyToolContext()
	ctx.ToolsRoot = "/target/tools"
	ctx.Workload = tool_goja.NewWorkloadLaunch(
		"sleep 5",
		[]string{"sleep", "5"},
		nil,
		"/foo/bar",
		false,
	)

	_, err := harness.ToolProbe(t, engine, ctx)

	var messageError *message.MessageImpl
	require.ErrorAs(t, err, &messageError)
	assert.Equal(
		t,
		message.MessageCode("tool_integrations.common.WORKING_DIR_NOT_ACCESSIBLE"),
		messageError.Code(),
	)
	assert.Equal(t, "/foo/bar", messageError.Metadata()["path"])
	engine.AssertExpectations(t)
}
