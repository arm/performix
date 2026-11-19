// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
)

type activityCheckClient struct {
	targetagentproto.TargetAgentClient
	request *targetagentproto.ExecCommandRequest
	result  *targetagentproto.CommandResult
	err     error
}

func (c *activityCheckClient) ExecCommand(_ context.Context, req *targetagentproto.ExecCommandRequest, _ ...grpc.CallOption) (*targetagentproto.CommandResult, error) {
	c.request = req
	return c.result, c.err
}

type activityReadinessCollector struct{ outputs []recipe.ReadyOutput }

func (c *activityReadinessCollector) OnReadinessProbed(output recipe.ReadyOutput) {
	c.outputs = append(c.outputs, output)
}

func TestAndroidActivityReadiness(t *testing.T) {
	commandErr := errors.New("connection lost")
	tests := []struct {
		name      string
		activity  string
		result    *targetagentproto.CommandResult
		err       error
		wantCode  message.MessageCode
		wantCause error
	}{
		{name: "relative activity resolves to full name", activity: ".MainActivity", result: &targetagentproto.CommandResult{Stdout: "priority=0 preferredOrder=0\ncom.example.app/com.example.app.MainActivity\n"}},
		{name: "full activity resolves to relative name", activity: "com.example.app.MainActivity", result: &targetagentproto.CommandResult{Stdout: "priority=0\ncom.example.app/.MainActivity\n"}},
		{name: "unresolved activity", activity: ".Missing", result: &targetagentproto.CommandResult{Stdout: "No activity found\n"}, wantCode: message.EngineRecipeStagesValidateAndroidActivityStageActivityUnresolved},
		{name: "command failed", activity: ".MainActivity", result: &targetagentproto.CommandResult{Rc: 1, Stderr: "permission denied"}, wantCode: message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed},
		{name: "connection failed", activity: ".MainActivity", err: commandErr, wantCode: message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed, wantCause: commandErr},
		{name: "unexpected output", activity: ".MainActivity", result: &targetagentproto.CommandResult{Stdout: "unexpected\n"}, wantCode: message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed},
		{name: "unexpected extra output", activity: ".MainActivity", result: &targetagentproto.CommandResult{Stdout: "unexpected\ncom.example.app/.MainActivity\n"}, wantCode: message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed},
		{name: "different component", activity: ".MainActivity", result: &targetagentproto.CommandResult{Stdout: "com.example.app/.Other\n"}, wantCode: message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed},
		{name: "different package", activity: ".MainActivity", result: &targetagentproto.CommandResult{Stdout: "com.example.other/.MainActivity\n"}, wantCode: message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed},
		{name: "checks exact activity", activity: " .MainActivity ", result: &targetagentproto.CommandResult{Stdout: "com.example.app/.MainActivity\n"}, wantCode: message.EngineRecipeStagesValidateAndroidActivityStageCheckFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			client := &activityCheckClient{result: tc.result, err: tc.err}
			collector := &activityReadinessCollector{}
			var workload tool.Workload = &tool.WorkloadAndroidLaunch{PackageName: "com.example.app", ActivityName: tc.activity}
			stage := NewValidateAndroidActivityStage(func() *agent.AgentConn { return agent.NewAgentConn(client) }, &workload)
			stageCtx := &recipe.StageContext{Context: context.Background(), ReadinessNotifier: collector}

			// When
			cleanup, err := stage.Execute(stageCtx)

			// Then
			require.NoError(t, err)
			assert.Nil(t, cleanup)
			require.NotNil(t, client.request)
			assert.Equal(t, []string{"cmd", "package", "resolve-activity", "--brief", "-n", "com.example.app/" + tc.activity}, client.request.Command)
			assert.False(t, client.request.AsPrivileged)
			if tc.wantCode == "" {
				assert.Empty(t, collector.outputs)
				return
			}
			require.Len(t, collector.outputs, 1)
			assert.Equal(t, recipe.ReadyStatusError, collector.outputs[0].Status)
			require.Len(t, collector.outputs[0].Advice, 1)
			advice := collector.outputs[0].Advice[0]
			assert.Equal(t, recipe.AdviceSeverityError, advice.AdviceSeverity)
			assert.Equal(t, tc.wantCode, advice.AdviceMessage.Code())
			assert.Equal(t, map[string]string{"package": "com.example.app", "activity": tc.activity}, advice.AdviceMessage.Metadata())
			if tc.wantCause != nil {
				assert.ErrorIs(t, advice.AdviceMessage, tc.wantCause)
			}
		})
	}
}
