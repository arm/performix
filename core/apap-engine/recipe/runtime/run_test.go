// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
	"github.com/Arm-Debug/apap-cli/apap-engine/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
)

func newRunCollectionWithLogHook(t *testing.T) (*log.Logger, *logging.DeferredFileOpenLogHook, *run.RunCollection, *run.RunID) {
	t.Helper()

	tmpDir := t.TempDir()
	runCollectionPath := filepath.Join(tmpDir, "runs")

	rc, err := run.NewRunCollection(runCollectionPath)
	require.NoError(t, err)

	logger := log.New()
	logHook := logging.NewDeferredFileOpenLogHook(&log.JSONFormatter{})
	logger.AddHook(logHook)

	runIDPtr := new(run.RunID)

	return logger, logHook, rc, runIDPtr
}

type fakeStageMock struct {
	mock.Mock
}

func (e *fakeStageMock) Execute(ctx *recipe.StageContext) (func(), error) {
	mockArgs := e.Called(ctx)
	return mockArgs.Get(0).(func()), mockArgs.Error(1)
}

func (e *fakeStageMock) Name() string {
	return e.Called().String(0)
}

func (e *fakeStageMock) ErrorType() run.RunResult {
	return e.Called().Get(0).(run.RunResult)
}

func (e *fakeStageMock) AlwaysExecute() bool {
	return e.Called().Get(0).(bool)
}

// stageRecordingStub lets us prove DriveRecipeExecutionStages really ran.
type stageRecordingStub struct {
	name     string
	executed bool
	err      error
	res      run.RunResult
}

func (s *stageRecordingStub) Name() string        { return s.name }
func (s *stageRecordingStub) AlwaysExecute() bool { return true }
func (s *stageRecordingStub) Execute(*recipe.StageContext) (func(), error) {
	s.executed = true
	return nil, s.err
}
func (s *stageRecordingStub) ErrorType() run.RunResult { return s.res }

// StageFactory stub that just hands the slice back.
type factoryStub struct{ stages []recipe.Stage }

func (f *factoryStub) BuildStages(cfg *StageConfiguration, _ notifiers.StageNotifier) ([]recipe.Stage, recipe.ExecutionContext) {
	execCtx := cfg.NewRunExecutionContext(afero.NewOsFs())
	execCtx.Collector = &recipe.Collector{CollectionState: cfg.CollectionState}
	return f.stages, execCtx
}

type cmdMapMock struct{ mock.Mock }

func (m *cmdMapMock) CreateCommandState(id run.RunID) *cmdsync.CommandState {
	args := m.Called(id)
	return args.Get(0).(*cmdsync.CommandState)
}

func (m *cmdMapMock) Write(id run.RunID, flag cmdsync.CommandStateType) error {
	args := m.Called(id, flag)
	return args.Error(0)
}

func (m *cmdMapMock) Remove(id run.RunID) error {
	return m.Called(id).Error(0)
}

type logEntry map[string]interface{}

func checkLogEntries(t *testing.T, rc *run.RunCollection, runID run.RunID, expectations ...logEntry) {
	t.Helper()
	model, err := rc.LoadRun(runID)
	require.NoError(t, err)
	logComponent, err := model.ResolveComponent("log.json")
	require.NoError(t, err)
	assert.Equal(t, cdf.ComponentType{Name: cdf.TypeLogJSON, SchemaVersion: "0.1"}, logComponent.Type)
	content, err := os.ReadFile(logComponent.AbsolutePath)
	require.NoError(t, err)

	var entries []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(line), &entry), "failed to parse log line as JSON: %s", line)
		entries = append(entries, entry)
	}

	for _, exp := range expectations {
		found := false
		for _, entry := range entries {
			if logEntryMatches(entry, exp) {
				found = true
				break
			}
		}
		assert.True(t, found, "no log entry matched expectation: %v", exp)
	}
}

// logEntryMatches checks whether a parsed log entry contains all expected fields.
func logEntryMatches(entry map[string]interface{}, expectation logEntry) bool {
	for key, expected := range expectation {
		actual, ok := entry[key]
		if !ok || !valuesMatch(actual, expected) {
			return false
		}
	}
	return true
}

// valuesMatch compares an actual JSON-decoded value against an expected value.
// String values use strings.Contains for partial matching (e.g. log messages with variable suffixes).
// Numeric values handle JSON's float64 representation, allowing int expectations.
func valuesMatch(actual, expected interface{}) bool {
	switch ev := expected.(type) {
	case string:
		av, ok := actual.(string)
		return ok && strings.Contains(av, ev)
	case int:
		av, ok := actual.(float64)
		return ok && av == float64(ev)
	case float64:
		av, ok := actual.(float64)
		return ok && av == ev
	default:
		return actual == expected
	}
}

// stageLogExpectations returns the common set of log entry expectations for a
// stage lifecycle: run creation, stage start, and stage end.
func stageLogExpectations(stageName string, stageNum, totalStages int, endMsg string) []logEntry {
	return []logEntry{
		{"msg": "New run created at"},
		{
			"msg":              "Stage started",
			"event":            "stage_start",
			"stageName":        stageName,
			"stageNum":         stageNum,
			"totalStagesCount": totalStages,
		},
		{
			"msg":              endMsg,
			"event":            "stage_end",
			"stageName":        stageName,
			"stageNum":         stageNum,
			"totalStagesCount": totalStages,
		},
	}
}

func TestRunRecipe_HappyPath(t *testing.T) {
	logger, logHook, rc, runIDPtr := newRunCollectionWithLogHook(t)
	t.Cleanup(func() {
		assert.NoError(t, logHook.Close())
	})
	var runID run.RunID

	// Create a temporary recipe file
	tmpDir := t.TempDir()
	recipePath := filepath.Join(tmpDir, "recipe.yaml")
	err := os.WriteFile(recipePath, []byte("# dummy recipe"), 0644)
	require.NoError(t, err)

	stage := &fakeStageMock{}
	stage.On("Name").Return("compile")
	stage.On("Execute", mock.Anything).Return(func() {}, nil)
	stage.On("ErrorType").Return(run.RunResult("ok")).Maybe()
	stage.On("AlwaysExecute").Return(false).Maybe()

	cmdMap := &cmdMapMock{}
	cmdMap.On("CreateCommandState", mock.Anything).Return(&cmdsync.CommandState{})
	cmdMap.On("Remove", mock.Anything).Return(nil)

	mockNotifier := &mocks.MockStageNotifier{}
	mockNotifier.On("OnRunCreated", mock.Anything, mock.Anything).Once().Run(func(args mock.Arguments) {
		runID = args.Get(0).(run.RunID)
		*runIDPtr = runID
	})
	mockNotifier.On("OnStageStart", mock.AnythingOfType("notifiers.StageInfo")).Once()
	mockNotifier.On("OnStageEnd", mock.AnythingOfType("notifiers.StageInfo"), mock.Anything).Once()

	loggingNotifier := recipe.NewLoggingStageNotifier(logger)
	compositeNotifier := recipe.NewCompositeStageNotifier(mockNotifier, loggingNotifier)

	meta := recipe.RecipeMetadata{Name: "ok", Version: "1.0", APIVersion: "1.0,0"}
	cfg := &StageConfiguration{
		Ctx: &recipe.RecipeCtx{
			Target:         &target.LocalTarget{},
			RecipePath:     recipePath,
			RecipeMetadata: meta,
		},
		Recipe:             &recipe.Recipe{},
		RunCollection:      rc,
		ToolDeploymentType: deployer.ToolDeployNONE,
		UsrMessageWriter:   &run.ConcreteUserMessageWriter{},
		CollectionState:    &recipe.CollectionState{},
	}

	err = RunRecipe(context.Background(), logHook, cfg, &factoryStub{stages: []recipe.Stage{stage}}, compositeNotifier, cmdMap)
	require.NoError(t, err)

	checkLogEntries(t, rc, runID,
		stageLogExpectations("compile", 1, 1, "Stage completed successfully")...,
	)

	stage.AssertExpectations(t)
}

func TestRunRecipe_StageFailure(t *testing.T) {
	logger, logHook, rc, runIDPtr := newRunCollectionWithLogHook(t)
	t.Cleanup(func() {
		assert.NoError(t, logHook.Close())
	})
	var runID run.RunID

	// Create a temporary recipe file
	tmpDir := t.TempDir()
	recipePath := filepath.Join(tmpDir, "recipe.yaml")
	err := os.WriteFile(recipePath, []byte("# dummy recipe"), 0644)
	require.NoError(t, err)

	stageErr := errors.New("boom")
	stage := &fakeStageMock{}
	stage.On("Name").Return("explode")
	stage.On("Execute", mock.Anything).Return(func() {}, stageErr)
	stage.On("ErrorType").Return(run.RunResult("exploded")).Maybe()
	stage.On("AlwaysExecute").Return(false).Maybe()

	noopStage := &fakeStageMock{}
	noopStage.On("Name").Return("noop")
	noopStage.On("Execute", mock.Anything).Return(func() {}, nil).Maybe()
	noopStage.On("ErrorType").Return(run.RunResult("ok")).Maybe()
	noopStage.On("AlwaysExecute").Return(false).Maybe()

	cmdMap := &cmdMapMock{}
	cmdMap.On("CreateCommandState", mock.Anything).Return(&cmdsync.CommandState{})
	cmdMap.On("Remove", mock.Anything).Return(nil)

	mockNotifier := &mocks.MockStageNotifier{}
	mockNotifier.
		On("OnRunCreated", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			runID = args.Get(0).(run.RunID)
			*runIDPtr = runID
		})
	mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "noop" && info.Num == 1 && info.Count == 3 }))
	mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "noop" && info.Num == 1 && info.Count == 3 }), nil)
	mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "explode" && info.Num == 2 && info.Count == 3 }))
	mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "explode" && info.Num == 2 && info.Count == 3 }), stageErr)

	loggingNotifier := recipe.NewLoggingStageNotifier(logger)
	compositeNotifier := recipe.NewCompositeStageNotifier(mockNotifier, loggingNotifier)

	meta := recipe.RecipeMetadata{Name: "fail", Version: "1.0", APIVersion: "1.0,0"}
	cfg := &StageConfiguration{
		Ctx: &recipe.RecipeCtx{
			Target:         &target.LocalTarget{},
			RecipePath:     recipePath,
			RecipeMetadata: meta,
		},
		Recipe:             &recipe.Recipe{},
		RunCollection:      rc,
		ToolDeploymentType: deployer.ToolDeployNONE,
		UsrMessageWriter:   &run.ConcreteUserMessageWriter{},
		CollectionState:    &recipe.CollectionState{},
	}

	stages := []recipe.Stage{noopStage, stage, noopStage}
	err = RunRecipe(context.Background(), logHook, cfg, &factoryStub{stages: stages}, compositeNotifier, cmdMap)
	require.Error(t, err)

	stage.AssertExpectations(t)

	checkLogEntries(t, rc, runID,
		stageLogExpectations("explode", 2, 3, "Stage failed")...,
	)
}

func TestRunRecipe_RemoveFromCommandStateMapFails(t *testing.T) {
	logger, logHook, rc, runIDPtr := newRunCollectionWithLogHook(t)
	t.Cleanup(func() {
		assert.NoError(t, logHook.Close())
	})
	var runID run.RunID

	// Create a temporary recipe file
	tmpDir := t.TempDir()
	recipePath := filepath.Join(tmpDir, "recipe.yaml")
	err := os.WriteFile(recipePath, []byte("# dummy recipe"), 0644)
	require.NoError(t, err)

	stage := &fakeStageMock{}
	stage.On("Name").Return("noop")
	stage.On("Execute", mock.Anything).Return(func() {}, nil)
	stage.On("ErrorType").Return(run.RunResult("ok")).Maybe()
	stage.On("AlwaysExecute").Return(false).Maybe()

	removeErr := errors.New("cannot remove")
	cmdMap := &cmdMapMock{}
	cmdMap.On("CreateCommandState", mock.Anything).Return(&cmdsync.CommandState{})
	cmdMap.On("Remove", mock.Anything).Return(removeErr)

	mockNotifier := &mocks.MockStageNotifier{}
	mockNotifier.
		On("OnRunCreated", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			runID = args.Get(0).(run.RunID)
			*runIDPtr = runID
		})
	mockNotifier.On("OnStageStart", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "noop" && info.Num == 1 && info.Count == 1 }))
	mockNotifier.On("OnStageEnd", mock.MatchedBy(func(info notifiers.StageInfo) bool { return info.Name == "noop" && info.Num == 1 && info.Count == 1 }), nil)

	loggingNotifier := recipe.NewLoggingStageNotifier(logger)
	compositeNotifier := recipe.NewCompositeStageNotifier(mockNotifier, loggingNotifier)

	meta := recipe.RecipeMetadata{Name: "remove-err", Version: "1.0", APIVersion: "1.0,0"}
	cfg := &StageConfiguration{
		Ctx: &recipe.RecipeCtx{
			Target:         &target.LocalTarget{},
			RecipePath:     recipePath,
			RecipeMetadata: meta,
		},
		Recipe:             &recipe.Recipe{},
		RunCollection:      rc,
		ToolDeploymentType: deployer.ToolDeployNONE,
		UsrMessageWriter:   &run.ConcreteUserMessageWriter{},
		CollectionState:    &recipe.CollectionState{},
	}

	err = RunRecipe(context.Background(), logHook, cfg, &factoryStub{stages: []recipe.Stage{stage}}, compositeNotifier, cmdMap)
	require.ErrorIs(t, err, removeErr)

	stage.AssertExpectations(t)

	// Verify the run was created and logged correctly, even though cleanup failed.
	_, err = rc.LoadRun(runID)
	assert.NoError(t, err, "run should exist in RunCollection even if CommandStateMap.Remove fails")

	checkLogEntries(t, rc, runID,
		logEntry{"msg": "New run created at"},
		logEntry{"msg": "Stage started"},
		logEntry{"msg": "Stage completed successfully"},
	)
}

// Test for conditional inclusion of ValidateTargetPIDStage
func TestBuildStages_ValidatePIDStageInclusion(t *testing.T) {
	factory := &RunStageFactory{}
	meta := recipe.RecipeMetadata{Name: "ok", Version: "1.0", APIVersion: "1.0,0"}

	newCfg := func(w tool.Workload) *StageConfiguration {
		return &StageConfiguration{
			Ctx: &recipe.RecipeCtx{
				Target:         &target.LocalTarget{},
				RecipePath:     "dummy",
				RecipeMetadata: meta,
				OrigWorkload:   w,
			},
			Recipe:             &recipe.Recipe{},
			ToolDeploymentType: deployer.ToolDeployNONE,
			CollectionState:    &recipe.CollectionState{},
		}
	}

	hasValidate := func(ss []recipe.Stage) bool {
		for _, st := range ss {
			if _, ok := st.(*stages.ValidateTargetPIDStage); ok {
				return true
			}
		}
		return false
	}

	t.Run("attach with non-zero PID includes ValidateTargetPIDStage", func(t *testing.T) {
		cfg := newCfg(&tool.WorkloadAttach{PID: 4321})
		ss, _ := factory.BuildStages(cfg, nil)
		assert.True(t, hasValidate(ss))
	})

	t.Run("attach with zero PID does not include ValidateTargetPIDStage", func(t *testing.T) {
		cfg := newCfg(&tool.WorkloadAttach{PID: 0})
		ss, _ := factory.BuildStages(cfg, nil)
		assert.False(t, hasValidate(ss))
	})

	t.Run("attach with default PID does not include ValidateTargetPIDStage", func(t *testing.T) {
		cfg := newCfg(&tool.WorkloadAttach{PID: -2})
		ss, _ := factory.BuildStages(cfg, nil)
		assert.False(t, hasValidate(ss))
	})

	t.Run("non-attach workloads do not include ValidateTargetPIDStage", func(t *testing.T) {
		cfg := newCfg(&tool.WorkloadSystemWide{})
		ss, _ := factory.BuildStages(cfg, nil)
		assert.False(t, hasValidate(ss))

		cfg = newCfg(&tool.WorkloadLaunch{Command: []string{"/bin/true"}})
		ss, _ = factory.BuildStages(cfg, nil)
		assert.False(t, hasValidate(ss))
	})
}

func TestBuildStages(t *testing.T) {
	factory := &RunStageFactory{}

	t.Run("stages are in the expected default order", func(t *testing.T) {
		cfg := &StageConfiguration{
			Ctx:             &recipe.RecipeCtx{},
			Recipe:          &recipe.Recipe{},
			CollectionState: &recipe.CollectionState{},
		}
		ss, _ := factory.BuildStages(cfg, nil)

		require.Len(t, ss, 10)
		assert.IsType(t, &stages.TargetConnectStage{}, ss[0])
		assert.IsType(t, &stages.TargetArchitectureStage{}, ss[1])
		assert.IsType(t, &stages.TargetPlatformSupportStage{}, ss[2])
		assert.IsType(t, &stages.WorkloadOptionsStage{}, ss[3])
		assert.IsType(t, &stages.ConnectingToTargetAgentStage{}, ss[4])
		assert.IsType(t, &stages.TargetLockStage{}, ss[5])
		assert.IsType(t, &stages.CollectTargetInfoStage{}, ss[6])
		assert.IsType(t, &stages.CollectTargetPIDStage{}, ss[7])
		assert.IsType(t, &stages.ReleaseTargetLockStage{}, ss[8])
		assert.IsType(t, &stages.RetrieveAgentFilesStage{}, ss[9])
	})

	t.Run("includes transfer manager stages if feature flag is enabled", func(t *testing.T) {
		cfg := &StageConfiguration{
			Ctx:                    &recipe.RecipeCtx{},
			Recipe:                 &recipe.Recipe{},
			TransferManagerEnabled: true,
			CollectionState:        &recipe.CollectionState{},
		}
		ss, _ := factory.BuildStages(cfg, nil)
		startStage, ok := ss[len(ss)-3].(*stages.StartTransferManagerStage)
		assert.True(t, ok)
		assert.NotNil(t, startStage.TransferManager)
		_, ok = ss[len(ss)-1].(*stages.WaitForTransfersStage)
		assert.True(t, ok)
	})
}

func TestBuildStages_ToolDeploymentProgressCallback(t *testing.T) {
	factory := &RunStageFactory{}
	meta := recipe.RecipeMetadata{Name: "deploy-test", Version: "1.0", APIVersion: "1.0,0"}

	t.Run("progress callback is set with correct stage number and count", func(t *testing.T) {
		mockNotifier := &mocks.MockStageNotifier{}

		cfg := &StageConfiguration{
			Ctx: &recipe.RecipeCtx{
				Target:         &target.LocalTarget{},
				RecipePath:     "dummy",
				RecipeMetadata: meta,
				OrigWorkload:   &tool.WorkloadSystemWide{},
			},
			Recipe:             &recipe.Recipe{},
			ToolDeploymentType: deployer.ToolDeployAUTO,
			CollectionState:    &recipe.CollectionState{},
		}

		ss, _ := factory.BuildStages(cfg, mockNotifier)

		// Find the indexes of the ToolDeploymentStages
		deployStages := map[string]*stages.ToolDeploymentStage{}
		deployIndices := map[string]int{}
		for i, st := range ss {
			if td, ok := st.(*stages.ToolDeploymentStage); ok {
				deployStages[td.Name()] = td
				deployIndices[td.Name()] = i
			}
		}

		require.Len(t, deployStages, 2, "Both target and host ToolDeploymentStages should be present when ToolDeploymentType != NONE")
		expectedStageCount := len(ss)

		for stageName, deployStage := range deployStages {
			require.NotNil(t, deployStage.ProgressCallback, "ProgressCallback should be set after BuildStages")

			// Set up the expectation for OnStageProgress with the correct arguments
			mockNotifier.On("OnStageProgress",
				notifiers.StageInfo{
					Name:  stageName,
					Num:   deployIndices[stageName] + 1,
					Count: expectedStageCount,
				},
				notifiers.StageProgress{
					Sent: int64(512),
					Max:  int64(1024),
					Unit: notifiers.UnitBytes,
				},
			).Once()

			// Invoke the callback and verify it calls the notifier correctly
			deployStage.ProgressCallback(512, 1024, time.Time{})
		}

		mockNotifier.AssertExpectations(t)
	})

	t.Run("progress callback not set when tool deployment is NONE", func(t *testing.T) {
		cfg := &StageConfiguration{
			Ctx: &recipe.RecipeCtx{
				Target:         &target.LocalTarget{},
				RecipePath:     "dummy",
				RecipeMetadata: meta,
				OrigWorkload:   &tool.WorkloadSystemWide{},
			},
			Recipe:             &recipe.Recipe{},
			ToolDeploymentType: deployer.ToolDeployNONE,
			CollectionState:    &recipe.CollectionState{},
		}

		ss, _ := factory.BuildStages(cfg, nil)

		for _, st := range ss {
			_, isToolDeploy := st.(*stages.ToolDeploymentStage)
			assert.False(t, isToolDeploy, "ToolDeploymentStage should not be present when ToolDeploymentType is NONE")
		}
	})

	t.Run("progress callback reports different byte values correctly", func(t *testing.T) {
		mockNotifier := &mocks.MockStageNotifier{}

		cfg := &StageConfiguration{
			Ctx: &recipe.RecipeCtx{
				Target:         &target.LocalTarget{},
				RecipePath:     "dummy",
				RecipeMetadata: meta,
				OrigWorkload:   &tool.WorkloadSystemWide{},
			},
			Recipe:             &recipe.Recipe{},
			ToolDeploymentType: deployer.ToolDeployAUTO,
			CollectionState:    &recipe.CollectionState{},
		}

		ss, _ := factory.BuildStages(cfg, mockNotifier)

		deployStages := map[string]*stages.ToolDeploymentStage{}
		deployIndices := map[string]int{}
		for i, st := range ss {
			if td, ok := st.(*stages.ToolDeploymentStage); ok {
				deployStages[td.Name()] = td
				deployIndices[td.Name()] = i
			}
		}
		require.Len(t, deployStages, 2)

		expectedStageCount := len(ss)

		for stageName, deployStage := range deployStages {
			// Test with zero bytes (start of transfer)
			mockNotifier.On("OnStageProgress",
				notifiers.StageInfo{
					Name:  stageName,
					Num:   deployIndices[stageName] + 1,
					Count: expectedStageCount,
				},
				notifiers.StageProgress{
					Sent: int64(0),
					Max:  int64(2048),
					Unit: notifiers.UnitBytes,
				},
			).Once()

			deployStage.ProgressCallback(0, 2048, time.Time{})

			// Test with completed transfer
			mockNotifier.On("OnStageProgress",
				notifiers.StageInfo{
					Name:  stageName,
					Num:   deployIndices[stageName] + 1,
					Count: expectedStageCount,
				},
				notifiers.StageProgress{
					Sent: int64(2048),
					Max:  int64(2048),
					Unit: notifiers.UnitBytes,
				},
			).Once()

			deployStage.ProgressCallback(2048, 2048, time.Time{})
		}

		mockNotifier.AssertExpectations(t)
	})
}

func TestRunRecipe(t *testing.T) {
	t.Run("sets run result and end time", func(t *testing.T) {
		_, logHook, rc, _ := newRunCollectionWithLogHook(t)
		t.Cleanup(func() {
			assert.NoError(t, logHook.Close())
		})

		tmpDir := t.TempDir()
		recipePath := filepath.Join(tmpDir, "recipe.yaml")
		err := os.WriteFile(recipePath, []byte("# dummy recipe"), 0644)
		require.NoError(t, err)

		stage := &fakeStageMock{}
		stage.On("Name").Return("complete")
		stage.On("Execute", mock.Anything).Return(func() {}, nil)
		stage.On("ErrorType").Return(run.RunResult("not-used")).Maybe()
		stage.On("AlwaysExecute").Return(false).Maybe()

		cmdMap := &cmdMapMock{}
		cmdMap.On("CreateCommandState", mock.Anything).Return(&cmdsync.CommandState{})
		cmdMap.On("Remove", mock.Anything).Return(nil)

		var runID run.RunID
		mockNotifier := &mocks.MockStageNotifier{}
		mockNotifier.On("OnRunCreated", mock.Anything, mock.Anything).Once().Run(func(args mock.Arguments) {
			runID = args.Get(0).(run.RunID)
		})
		mockNotifier.On("OnStageStart", mock.AnythingOfType("notifiers.StageInfo")).Once()
		mockNotifier.On("OnStageEnd", mock.AnythingOfType("notifiers.StageInfo"), mock.Anything).Once()

		cfg := &StageConfiguration{
			Ctx: &recipe.RecipeCtx{
				Target:         &target.LocalTarget{},
				RecipePath:     recipePath,
				RecipeMetadata: recipe.RecipeMetadata{Name: "complete", Version: "1.0", APIVersion: "1.0,0"},
			},
			Recipe:             &recipe.Recipe{},
			RunCollection:      rc,
			ToolDeploymentType: deployer.ToolDeployNONE,
			UsrMessageWriter:   &run.ConcreteUserMessageWriter{},
			CollectionState:    &recipe.CollectionState{},
		}

		err = RunRecipe(context.Background(), logHook, cfg, &factoryStub{stages: []recipe.Stage{stage}}, mockNotifier, cmdMap)
		require.NoError(t, err)

		desc, err := rc.RunDescription(context.Background(), runID)
		require.NoError(t, err)
		require.Equal(t, string(run.RecipeSuccess), desc.RunResult)
		require.NotEmpty(t, desc.EndTime)
		require.GreaterOrEqual(t, desc.EndTime, desc.StartTime)

		stage.AssertExpectations(t)
		mockNotifier.AssertExpectations(t)
		cmdMap.AssertExpectations(t)
	})
}
