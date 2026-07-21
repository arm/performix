// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
)

type MockReadinessNotifier struct{ mock.Mock }

func (m *MockReadinessNotifier) OnReadinessProbed(out recipe.ReadyOutput) {
	m.Called(out)
}

type setup struct {
	t        *testing.T
	ctx      context.Context
	rn       *MockReadinessNotifier
	tgt      target.Target
	session  *targetsessionmocks.MockTargetSession
	stage    *ConnectingToTargetAgentStage
	stageCtx *recipe.StageContext
}

// newLocalSetup initializes and returns a setup struct with a local target, calling t.Cleanup to assert mock expectations.
func newLocalSetup(t *testing.T) *setup {
	return newSetupWithTarget(t, &target.LocalTarget{})
}

// newSSHSetup initializes and returns a setup struct with an SSH target, calling t.Cleanup to assert mock expectations.
func newSSHSetup(t *testing.T) *setup {
	return newSetupWithTarget(t, &target.SSHTarget{})
}

// newSetupWithTarget initializes and returns a setup struct using the provided target, calling t.Cleanup to assert mock expectations.
func newSetupWithTarget(t *testing.T, tgt target.Target) *setup {
	s := &setup{
		t:   t,
		ctx: context.Background(),
		rn:  &MockReadinessNotifier{},
		tgt: tgt,
	}
	s.session = &targetsessionmocks.MockTargetSession{}

	s.stage = NewConnectingToTargetAgentStage(s.tgt, func() targetsession.TargetSession { return s.session })
	s.stageCtx = &recipe.StageContext{
		Context:           s.ctx,
		ReadinessNotifier: s.rn,
	}

	s.t.Cleanup(func() {
		s.session.AssertExpectations(t)
		s.rn.AssertExpectations(t)
	})
	return s
}

func TestConnectingToTargetAgentStage_LocalhostSuccess(t *testing.T) {
	s := newLocalSetup(t)

	ac := &agent.AgentConn{}
	s.session.On("TargetAgent", s.ctx).Return(ac, nil).Once()

	cleanup, err := s.stage.Execute(s.stageCtx)
	assert.NoError(t, err)
	assert.Nil(t, cleanup)
	assert.Equal(t, ac, s.stage.Agent)
	assert.Equal(t, ac, s.stage.AgentConnSupplier())

	s.rn.AssertNotCalled(t, "OnReadinessProbed", mock.Anything)
}

func TestConnectingToTargetAgentStage_SSHSuccess(t *testing.T) {
	s := newSSHSetup(t)

	ac := &agent.AgentConn{}
	s.session.On("TargetAgent", s.ctx).Return(ac, nil).Once()

	cleanup, err := s.stage.Execute(s.stageCtx)
	assert.NoError(t, err)
	assert.Nil(t, cleanup)
	assert.Equal(t, ac, s.stage.Agent)
	assert.Equal(t, ac, s.stage.AgentConnSupplier())

	s.rn.AssertNotCalled(t, "OnReadinessProbed", mock.Anything)
}

func TestConnectingToTargetAgentStage_FailureWithMessageCode(t *testing.T) {
	s := newSSHSetup(t)

	msgErr := message.New(message.EngineAgentConnectionCreatorRunLaunchCommand)

	s.session.On("TargetAgent", s.ctx).Return((*agent.AgentConn)(nil), msgErr).Once()

	readinessOutput := recipe.ReadyOutput{}
	s.rn.On("OnReadinessProbed", mock.Anything).Run(func(args mock.Arguments) {
		readinessOutput = args.Get(0).(recipe.ReadyOutput)
	}).Once()

	cleanup, err := s.stage.Execute(s.stageCtx)
	assert.Error(t, err)
	assert.Nil(t, cleanup)
	assert.Equal(t, msgErr, err)
	assert.Nil(t, s.stage.Agent)

	assert.Equal(t, recipe.ReadyStatusError, readinessOutput.Status)
	assert.Equal(t, 1, len(readinessOutput.Advice))
	assert.Equal(t, msgErr, readinessOutput.Advice[0].AdviceMessage)
}

func TestConnectingToTargetAgentStage_FailureGenericError(t *testing.T) {
	s := newSSHSetup(t)

	genErr := errors.New("unknown error")

	s.session.On("TargetAgent", s.ctx).Return((*agent.AgentConn)(nil), genErr).Once()

	readinessOutput := recipe.ReadyOutput{}
	s.rn.On("OnReadinessProbed", mock.Anything).Run(func(args mock.Arguments) {
		readinessOutput = args.Get(0).(recipe.ReadyOutput)
	}).Once()

	cleanup, err := s.stage.Execute(s.stageCtx)
	assert.Error(t, err)
	assert.Nil(t, cleanup)
	assert.Equal(t, genErr, err)
	assert.Nil(t, s.stage.Agent)

	assert.Equal(t, recipe.ReadyStatusError, readinessOutput.Status)
	assert.Equal(t, 1, len(readinessOutput.Advice))
	assert.Equal(t, message.EngineRecipeparserJsRecipeStageReadinessMessage, readinessOutput.Advice[0].AdviceMessage.Code())
	assert.Equal(t, "The target agent connection failed due to an internal error.", readinessOutput.Advice[0].AdviceMessage.Metadata()["message"])
}

func TestConnectingToTargetAgentStage_API(t *testing.T) {
	s := newSSHSetup(t)
	assert.Equal(t, "Connecting to target agent", s.stage.Name())
	assert.Equal(t, run.RecipeFailureConnectAgent, s.stage.ErrorType())
	assert.False(t, s.stage.AlwaysExecute())
}

func TestConnectingToTargetAgentStage_OnConnectionLostSetsCommandCancel(t *testing.T) {
	s := newSSHSetup(t)
	cmdState := &cmdsync.CommandState{}
	s.stageCtx.CommandState = cmdState

	ac := &agent.AgentConn{}
	s.session.On("TargetAgent", s.ctx).Return(ac, nil).Once()

	cleanup, err := s.stage.Execute(s.stageCtx)
	assert.NoError(t, err)
	assert.Nil(t, cleanup)

	ac.AbortWithCause(errors.New("boom"))

	assert.Eventually(t, func() bool {
		return cmdState.Read() == cmdsync.CommandCancel
	}, 500*time.Millisecond, 10*time.Millisecond)
	assert.Error(t, cmdState.CancelError())
	assert.ErrorIs(t, cmdState.CancelError(), message.New(message.EngineAgentConnectionTransportError))
}

func TestConnectingToTargetAgentStage_OnConnectionLostNilCommandStateSafe(t *testing.T) {
	s := newSSHSetup(t)
	s.stageCtx.CommandState = nil

	ac := &agent.AgentConn{}
	s.session.On("TargetAgent", s.ctx).Return(ac, nil).Once()

	cleanup, err := s.stage.Execute(s.stageCtx)
	assert.NoError(t, err)
	assert.Nil(t, cleanup)

	// Should not panic when disconnect callback runs with nil CommandState.
	ac.AbortWithCause(errors.New("boom"))
}
