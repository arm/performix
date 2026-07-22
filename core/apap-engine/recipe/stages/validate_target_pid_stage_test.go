// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestTargetPIDValidation_agent(t *testing.T) {
	t.Run("returns error when attached PID is missing", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		agentSupplier := func() *agent.AgentConn { return &agent.AgentConn{Client: client} }
		stage := NewValidateTargetPIDStage(agentSupplier, 999)

		ctx := &recipe.StageContext{
			Context: context.Background(),
			CachedAgentProcessList: &targetagentproto.ProcessList{
				Processes: []*targetagentproto.ProcessInfo{
					{Pid: 123, Name: "proc1"},
				},
			},
		}
		_, err := stage.Execute(ctx)
		if assert.Error(t, err) {
			var msgErr message.Message
			if assert.True(t, errors.As(err, &msgErr)) {
				assert.Equal(t, message.EngineRecipeStagesValidateTargetPidStagePidNotFound, msgErr.Code())
				assert.Equal(t, "999", msgErr.Metadata()["pid"])
			}
		}
	})

	t.Run("succeeds when attach PID found", func(t *testing.T) {
		client := &targetagentmocks.TargetAgentClient{}
		agentSupplier := func() *agent.AgentConn { return &agent.AgentConn{Client: client} }
		stage := NewValidateTargetPIDStage(agentSupplier, 123)

		ctx := &recipe.StageContext{
			Context: context.Background(),
			CachedAgentProcessList: &targetagentproto.ProcessList{
				Processes: []*targetagentproto.ProcessInfo{
					{Pid: 123, Name: "proc1"},
				},
			},
		}

		_, err := stage.Execute(ctx)
		assert.NoError(t, err)
	})
}
