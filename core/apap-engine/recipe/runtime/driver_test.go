// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func createRunCollection(t *testing.T) (*run.RunCollection, run.RunID) {
	runs, err := run.NewRunCollection(t.TempDir())
	assert.NoError(t, err)
	meta := recipe.RecipeMetadata{Name: "DummyC", Version: "0.1.2"}
	rc := recipe.RecipeCtx{RecipeMetadata: meta, Target: &target.SSHTarget{Jumps: []target.SSHHostConfig{
		{Host: "1.2.3.4", Port: 345, Username: "Blobby", PrivateKeyFilename: "/a/real/file"},
	}}}
	builder, err := runs.RunBuilder()
	builder.AddEntity("foo")
	builder.AddToolOutput("test_tool", "0.0.1", 42)
	assert.NoError(t, err)
	metadata, err := rc.CreateMetadata(&builder, parameters.BoundParameters{})
	assert.NoError(t, err)
	runID, err := runs.CreateRun(builder, metadata)
	assert.NoError(t, err)
	return runs, runID
}

func TestDriver(t *testing.T) {
	state := &cmdsync.CommandState{}
	state.RecipeCommand.Store(uint32(0))

	t.Run("DriveRecipeExecutionStage skips expected stages", func(t *testing.T) {
		expectedError := errors.New("rekt")
		expectedRunResult := "FakeStage1Failed"
		fakeStage2Executed := false

		fakeStageMock1 := fakeStageMock{}
		fakeStageMock1.On("Execute", mock.Anything).Return(func() {}, expectedError)
		fakeStageMock1.On("Name").Return("FabulousStage")
		fakeStageMock1.On("ErrorType").Return(run.RunResult(expectedRunResult))
		fakeStageMock1.On("AlwaysExecute").Return(false)

		// fake stage 2 should never execute if prior stage fails
		fakeStageMock2 := fakeStageMock{}
		fakeStageMock2.On("Execute", mock.Anything).Return(func() {}, nil).Run(func(args mock.Arguments) { fakeStage2Executed = true })
		fakeStageMock2.On("Name").Return("FabulousStage")
		fakeStageMock2.On("AlwaysExecute").Return(false)

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }), expectedError)

		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: &cmdsync.CommandState{}}
		recipeStage := []recipe.Stage{&fakeStageMock1, &fakeStageMock2}

		res, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.Error(t, err)                                   // We should return an error
		assert.Equal(t, run.RunResult(expectedRunResult), res) // The RunResult should be the expected run result
		assert.False(t, fakeStage2Executed)                    // Stage 2 should not have executed
	})

	t.Run("DriveRecipeExecutionStage executed expected stages", func(t *testing.T) {
		expectedError := errors.New("rekt")
		expectedRunResult := "FakeStage1Failed"
		fakeStage2Executed := false

		fakeStageMock1 := fakeStageMock{}
		fakeStageMock1.On("Execute", mock.Anything).Return(func() {}, expectedError)
		fakeStageMock1.On("Name").Return("FabulousStage")
		fakeStageMock1.On("ErrorType").Return(run.RunResult(expectedRunResult))

		// fake stage 2 should execute evwen if prior stage fails
		fakeStageMock2 := fakeStageMock{}
		fakeStageMock2.On("Execute", mock.Anything).Return(func() {}, nil).Run(func(args mock.Arguments) { fakeStage2Executed = true })
		fakeStageMock2.On("Name").Return("FabulousStage")
		fakeStageMock2.On("AlwaysExecute").Return(true)

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.AnythingOfType("notifiers.StageInfo"))
		mockNotifier.On("OnStageEnd", mock.AnythingOfType("notifiers.StageInfo"), mock.Anything)

		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: &cmdsync.CommandState{}}
		recipeStage := []recipe.Stage{&fakeStageMock1, &fakeStageMock2}

		res, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.Error(t, err)                                   // We should return an error
		assert.Equal(t, run.RunResult(expectedRunResult), res) // The RunResult should be the expected run result
		assert.True(t, fakeStage2Executed)                     // Stage 2 should have executed
	})

	t.Run("DriveRecipeExecutionStage cancels when it sees cancel message", func(t *testing.T) {
		fakeStageMock := fakeStageMock{}
		fakeStageMock.On("Execute", mock.Anything).Return(func() {}, nil)
		fakeStageMock.On("Name").Return("FabulousStage")
		fakeStageMock.On("ErrorType").Return(run.RunResult("success"))

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }), nil)
		mockNotifier.On("OnStageCancelled", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))

		state.RecipeCommand.Store(uint32(cmdsync.CommandCancel))
		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: state}
		recipeStage := []recipe.Stage{&fakeStageMock}

		_, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		expectedErr := message.New(message.EngineCommonUserCanceled)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("DriveRecipeExecutionStage returns the error when set", func(t *testing.T) {
		fakeStageMock := fakeStageMock{}
		fakeStageMock.On("Execute", mock.Anything).Return(func() {}, nil)
		fakeStageMock.On("Name").Return("FabulousStage")
		fakeStageMock.On("ErrorType").Return(run.RunResult("success"))

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }), nil)
		mockNotifier.On("OnStageCancelled", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))

		cancelErr := errors.New("agent connection lost")
		cancelState := &cmdsync.CommandState{}
		cancelState.Set(cmdsync.CommandCancel)
		cancelState.SetCancelError(cancelErr)
		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: cancelState}
		recipeStage := []recipe.Stage{&fakeStageMock}

		res, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.Equal(t, run.RunResult("success"), res)
		assert.Equal(t, cancelErr, err)
	})

	t.Run("DriveRecipeExecutionStage ignore stop message", func(t *testing.T) {
		fakeStageMock := fakeStageMock{}
		fakeStageMock.On("Execute", mock.Anything).Return(func() {}, nil)
		fakeStageMock.On("Name").Return("FabulousStage")
		fakeStageMock.On("ErrorType").Return(run.RunResult("success"))

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }), nil)

		state.RecipeCommand.Store(uint32(cmdsync.CommandStop))
		recipeStage := []recipe.Stage{&fakeStageMock}
		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: state}

		_, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.NoError(t, err)
	})

	t.Run("DriveRecipeExecutionStages succeeds with empty Stage array", func(t *testing.T) {
		testRunCollection, runID := createRunCollection(t)

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }), nil)

		stageContext := recipe.StageContext{StageNotifier: mockNotifier}
		emptyStages := []recipe.Stage{}

		runResult, err := DriveRecipeExecutionStages(emptyStages, &stageContext)
		assert.NoError(t, err)
		err = testRunCollection.UpdateRunResult(context.Background(), runID, runResult, err)
		assert.NoError(t, err)

		desc, err := testRunCollection.RunDescription(context.Background(), runID)
		assert.NoError(t, err)
		assert.Equal(t, run.RecipeSuccess, run.RunResult(desc.RunResult))
	})

	t.Run("DriveRecipeExecutionStages succeeds with valid Stage array", func(t *testing.T) {
		fakeStageMock := fakeStageMock{}
		fakeStageMock.On("Execute", mock.Anything).Return(func() {}, nil)
		fakeStageMock.On("Name").Return("FabulousStage")

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }), nil)

		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: &cmdsync.CommandState{}}
		recipeStage := []recipe.Stage{&fakeStageMock}

		_, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.NoError(t, err)
	})

	t.Run("DriveRecipeExecutionStages fails when an Execute() fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		fakeStage := fakeStageMock{}
		fakeStage.On("Execute", mock.Anything).Return(func() {}, expectedError)
		fakeStage.On("Name").Return("FabulousStage")
		fakeStage.On("ErrorType").Return(run.RunResult("FabFailed"))

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }), expectedError)

		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: &cmdsync.CommandState{}}

		recipeStage := []recipe.Stage{&fakeStage}
		_, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.Error(t, expectedError, err)
	})

	t.Run("DriveRecipeExecutionStages cleans up after all pass", func(t *testing.T) {
		fakeStage := fakeStageMock{}
		fakeStage.On("Execute", mock.Anything).Return(func() {}, nil)
		fakeStage.On("Name").Return("FabulousStage")
		fakeStage.On("ErrorType").Return(run.RunResult("FabFailed"))

		stage3Executed := false
		deferCalledAfterStage3 := false
		fakeStage2 := fakeStageMock{}
		fakeStage2.On("Execute", mock.Anything).Return(func() {
			if stage3Executed {
				deferCalledAfterStage3 = true
			}
		}, nil)
		fakeStage2.On("Name").Return("FabulousStage")

		fakeStage3 := fakeStageMock{}
		fakeStage3.On("Execute", mock.Anything).Return(func() {}, nil).Run(func(args mock.Arguments) { stage3Executed = true })
		fakeStage3.On("Name").Return("FabulousStage")

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.AnythingOfType("notifiers.StageInfo"))
		mockNotifier.On("OnStageEnd", mock.AnythingOfType("notifiers.StageInfo"), nil)

		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: &cmdsync.CommandState{}}

		recipeStage := []recipe.Stage{&fakeStage, &fakeStage2, &fakeStage3}
		_, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.NoError(t, err)
		assert.True(t, deferCalledAfterStage3)
	})

	t.Run("DriveRecipeExecutionStages doesn't clean up if stage failes", func(t *testing.T) {
		expectedError := errors.New("rekt")
		fakeStage := fakeStageMock{}
		fakeStage.On("Execute", mock.Anything).Return(func() {}, nil)
		fakeStage.On("Name").Return("Fabulous")
		fakeStage.On("ErrorType").Return(run.RunResult("FabFailed"))
		fakeStage.On("AlwaysExecute").Return(false)

		stage3Executed := false
		deferCalledAfterStage3 := false
		fakeStage2 := fakeStageMock{}
		fakeStage2.On("Execute", mock.Anything).Return(func() {
			if stage3Executed {
				deferCalledAfterStage3 = true
			}
		}, expectedError)
		fakeStage2.On("Name").Return("Fabulous2")
		fakeStage2.On("ErrorType").Return(run.RunResult("FabFailed"))
		fakeStage2.On("AlwaysExecute").Return(false)

		fakeStage3 := fakeStageMock{}
		fakeStage3.On("Execute", mock.Anything).Return(func() {}, nil).Run(func(args mock.Arguments) { stage3Executed = true })
		fakeStage3.On("Name").Return("Fabulous")
		fakeStage3.On("ErrorType").Return(run.RunResult("FabFailed"))
		fakeStage3.On("AlwaysExecute").Return(false)

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "Fabulous" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "Fabulous" }), nil)
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "Fabulous2" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "Fabulous2" }), expectedError)

		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: &cmdsync.CommandState{}}

		recipeStage := []recipe.Stage{&fakeStage, &fakeStage2, &fakeStage3}
		_, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.ErrorIs(t, err, expectedError)
		assert.False(t, deferCalledAfterStage3)
	})

	t.Run("Run collection metadata reports correct failure when DriveRecipeExecutionStages fails", func(t *testing.T) {
		testRunCollection, runID := createRunCollection(t)

		expectedError := errors.New("rekt")
		fakeStage := fakeStageMock{}
		fakeStage.On("Execute", mock.Anything).Return(func() {}, expectedError)
		fakeStage.On("Name").Return("FabulousStage")
		fakeStage.On("ErrorType").Return(run.RunResult("FabFailed"))

		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }))
		mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "FabulousStage" }), expectedError)

		stageContext := recipe.StageContext{StageNotifier: mockNotifier, CommandState: &cmdsync.CommandState{}}

		recipeStage := []recipe.Stage{&fakeStage}
		runResult, err := DriveRecipeExecutionStages(recipeStage, &stageContext)
		assert.Error(t, expectedError, err)
		err = testRunCollection.UpdateRunResult(context.Background(), runID, runResult, err)
		assert.NoError(t, err)

		desc, err := testRunCollection.RunDescription(context.Background(), runID)
		assert.NoError(t, err)
		assert.Equal(t, run.RunResult("FabFailed"), run.RunResult(desc.RunResult))
		assert.Equal(t, "rekt", desc.RunError)
	})

	t.Run("Run collection metadata reports the start and end time accurately", func(t *testing.T) {
		startTime := util.CurrentTime().ToFormattedString()

		testRunCollection, runID := createRunCollection(t)

		stageContext := recipe.StageContext{StageNotifier: &mocks.MockStageNotifier{}, CommandState: &cmdsync.CommandState{}}
		_, err := DriveRecipeExecutionStages([]recipe.Stage{}, &stageContext)
		assert.NoError(t, err)

		err = testRunCollection.SetRunEndTime(context.Background(), runID)
		assert.NoError(t, err)

		desc, err := testRunCollection.RunDescription(context.Background(), runID)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, desc.StartTime, startTime)
		assert.GreaterOrEqual(t, util.CurrentTime().ToFormattedString(), desc.StartTime)
		assert.GreaterOrEqual(t, desc.EndTime, desc.StartTime)
	})

	t.Run("Run description contains tools used", func(t *testing.T) {
		testRunCollection, runID := createRunCollection(t)

		stageContext := recipe.StageContext{StageNotifier: &mocks.MockStageNotifier{}, CommandState: &cmdsync.CommandState{}}
		runResult, err := DriveRecipeExecutionStages([]recipe.Stage{}, &stageContext)
		assert.NoError(t, err)

		err = testRunCollection.UpdateRunResult(context.Background(), runID, runResult, err)
		assert.NoError(t, err)

		desc, err := testRunCollection.RunDescription(context.Background(), runID)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(desc.ToolsUsed))
		assert.Equal(t, "test_tool", desc.ToolsUsed[0].Tool)
		assert.Equal(t, "0.0.1", desc.ToolsUsed[0].Version)
		assert.Equal(t, 42, desc.ToolsUsed[0].Invocation)
	})
}
