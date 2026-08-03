// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func newRecipeJobTestConfig(t *testing.T, runCollection *run.RunCollection) *StageConfiguration {
	t.Helper()
	recipePath := filepath.Join(t.TempDir(), "recipe.yaml")
	require.NoError(t, os.WriteFile(recipePath, []byte("# test recipe"), 0o600))

	return &StageConfiguration{
		Ctx: &recipe.RecipeCtx{
			Target:         &target.LocalTarget{},
			RecipePath:     recipePath,
			RecipeMetadata: recipe.RecipeMetadata{Name: "test", Version: "1.0", APIVersion: "1.0.0"},
		},
		Recipe:             &recipe.Recipe{},
		RunCollection:      runCollection,
		ToolDeploymentType: deployer.ToolDeployNONE,
		UsrMessageWriter:   &run.ConcreteUserMessageWriter{},
		CollectionState:    &recipe.CollectionState{},
	}
}

func TestRecipeJob(t *testing.T) {
	t.Run("signals phase 1 and buffers final completion", func(t *testing.T) {
		wantErr := errors.New("background transfer failed")
		tests := []struct {
			name           string
			stageErr       error
			finalRunResult run.RunResult
		}{
			{name: "background success", finalRunResult: run.RecipeSuccess},
			{name: "background failure", stageErr: wantErr, finalRunResult: run.RecipeFailureRetrievePhase1Complete},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, _, runCollection, _ := newRunCollectionWithLogHook(t)
				release := make(chan struct{})
				released := false
				config := newRecipeJobTestConfig(t, runCollection)
				stage := &fakeStageMock{}
				stage.On("Name").Return("waiting for background work")
				stage.On("AlwaysExecute").Return(true).Maybe()
				stage.On("ErrorType").Return(run.RecipeFailureRetrievePhase1Complete).Maybe()
				stage.On("Execute", mock.Anything).Run(func(mock.Arguments) {
					config.OnPhase1TransferComplete(true)
					<-release
				}).Return(func() {}, tt.stageErr).Once()
				job := StartRecipeJob(
					context.Background(),
					config,
					&factoryStub{stages: []recipe.Stage{stage}},
					&recipe.NullStageNotifier{},
					cmdsync.NewCommandStateMap(),
					true,
				)
				t.Cleanup(func() {
					if !released {
						close(release)
					}
					job.Cancel()
				})

				require.Equal(t, 1, cap(job.done))
				select {
				case <-job.Phase1Done():
				case <-time.After(time.Second):
					t.Fatal("job did not signal phase-1 completion")
				}
				select {
				case <-job.Done():
					t.Fatal("job completed while background work was blocked")
				default:
				}

				close(release)
				released = true
				require.Eventually(t, func() bool { return len(job.done) == 1 }, time.Second, time.Millisecond)
				jobErr := <-job.Done()
				if tt.stageErr == nil {
					require.NoError(t, jobErr)
				} else {
					require.ErrorIs(t, jobErr, tt.stageErr)
				}

				runIDs, err := runCollection.ListRuns(context.Background())
				require.NoError(t, err)
				require.Len(t, runIDs, 1)
				description, err := runCollection.RunDescription(context.Background(), runIDs[0])
				require.NoError(t, err)
				require.Equal(t, string(tt.finalRunResult), description.RunResult)
				require.NotEqual(t, util.InvalidTime().ToFormattedString(), description.EndTime)
				stage.AssertExpectations(t)
			})
		}
	})

	t.Run("does not signal phase 1 without pending background transfers", func(t *testing.T) {
		_, _, runCollection, _ := newRunCollectionWithLogHook(t)
		config := newRecipeJobTestConfig(t, runCollection)
		stage := &fakeStageMock{}
		stage.On("Name").Return("finishing required transfers")
		stage.On("AlwaysExecute").Return(true).Maybe()
		stage.On("ErrorType").Return(run.RecipeFailureRetrieve).Maybe()
		stage.On("Execute", mock.Anything).Run(func(mock.Arguments) {
			config.OnPhase1TransferComplete(false)
		}).Return(func() {}, nil).Once()

		job := StartRecipeJob(
			context.Background(),
			config,
			&factoryStub{stages: []recipe.Stage{stage}},
			&recipe.NullStageNotifier{},
			cmdsync.NewCommandStateMap(),
			true,
		)
		t.Cleanup(job.Cancel)

		select {
		case err := <-job.Done():
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("job did not complete")
		}
		select {
		case <-job.Phase1Done():
			t.Fatal("phase 1 was signalled without pending background transfers")
		default:
		}
		stage.AssertExpectations(t)
	})

	t.Run("cancellation before phase 1 correctly cancels the run", func(t *testing.T) {
		_, _, runCollection, _ := newRunCollectionWithLogHook(t)
		config := newRecipeJobTestConfig(t, runCollection)
		started := make(chan struct{})
		stage := &fakeStageMock{}
		stage.On("Name").Return("waiting for cancellation")
		stage.On("AlwaysExecute").Return(true).Maybe()
		stage.On("ErrorType").Return(run.RecipeFailureRetrievePhase1Complete).Maybe()
		stage.On("Execute", mock.Anything).Run(func(args mock.Arguments) {
			stageCtx := args.Get(0).(*recipe.StageContext)
			close(started)
			<-stageCtx.Context.Done()
		}).Return(func() {}, context.Canceled).Once()

		job := StartRecipeJob(
			context.Background(),
			config,
			&factoryStub{stages: []recipe.Stage{stage}},
			&recipe.NullStageNotifier{},
			cmdsync.NewCommandStateMap(),
			false,
		)
		t.Cleanup(job.Cancel)

		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("job did not begin its cancellable stage")
		}
		job.Cancel()

		select {
		case err := <-job.Done():
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("job did not complete after cancellation")
		}
		select {
		case <-job.Phase1Done():
			t.Fatal("phase 1 was signalled for a cancelled job")
		default:
		}
		stage.AssertExpectations(t)
	})
}
