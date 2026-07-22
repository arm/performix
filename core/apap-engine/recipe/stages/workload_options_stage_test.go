// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks_test "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func TestWorkloadOptionsStageUpdateWorkingDir(t *testing.T) {
	testCases := []struct {
		name         string
		origWl       tool.Workload
		resolvedWl   tool.Workload
		homeDir      string
		shouldUpdate bool
	}{
		{
			name:         "updates launch workload with empty workingDir",
			origWl:       &tool.WorkloadLaunch{WorkingDir: ""},
			resolvedWl:   nil,
			homeDir:      "abc",
			shouldUpdate: true,
		},
		{
			name:         "doesn't update launch workload with non-empty workingDir",
			origWl:       &tool.WorkloadLaunch{WorkingDir: "something"},
			resolvedWl:   nil,
			homeDir:      "abc",
			shouldUpdate: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockSession := targetsessionmocks.MockTargetSession{}
			targetPlatform := conductor.TargetPlatform{
				PlatformConfiguration: conductor.PlatformConfiguration{
					OS: conductor.Linux,
				},
			}
			mockSession.On("TargetPlatform").Return(&targetPlatform, nil)
			sessionSupplier := func() targetsession.TargetSession {
				return &mockSession
			}

			mockRunner := conductormocks_test.MockCommandRunner{}
			mockRunner.On("RunCommand", "printenv HOME").Return(tc.homeDir, "", nil)
			runnerSupplier := func() conductor.CommandRunner {
				return &mockRunner
			}

			wlStage := NewWorkloadOptionsStage(tc.origWl, &tc.resolvedWl, sessionSupplier, runnerSupplier)
			originalWorkingDir := tc.origWl.(*tool.WorkloadLaunch).WorkingDir
			_, err := wlStage.Execute(&recipe.StageContext{})
			assert.NoError(t, err)

			resolvedWlLaunch := tc.resolvedWl.(*tool.WorkloadLaunch)
			if tc.shouldUpdate {
				assert.Equal(t, tc.homeDir, resolvedWlLaunch.WorkingDir, "workload working dir should have been updated, but was not")
				assert.Equal(t, originalWorkingDir, tc.origWl.(*tool.WorkloadLaunch).WorkingDir, "original workload should not have been mutated")
			} else {
				assert.Equal(t, tc.resolvedWl, tc.origWl, "workload working dir should not have been updated, but was")
				assert.NotEqual(t, tc.homeDir, resolvedWlLaunch.WorkingDir, "workload working dir should not have been updated, but was")
			}
		})
	}
	t.Run("doesn't resolve non-command workloads", func(t *testing.T) {
		wls := []tool.Workload{
			&tool.WorkloadAndroidLaunch{PackageName: "com.example.app", ActivityName: ".MainActivity"},
			&tool.WorkloadAttach{PID: 1234},
			&tool.WorkloadSystemWide{},
		}
		for _, w := range wls {
			var resolvedWl tool.Workload
			wlStage := NewWorkloadOptionsStage(w, &resolvedWl, nil, nil)
			_, err := wlStage.Execute(&recipe.StageContext{})

			assert.NoError(t, err)
			assert.Equal(t, resolvedWl, w)
		}
	})
	t.Run("persists resolved working dir in run metadata", func(t *testing.T) {
		mockSession := targetsessionmocks.MockTargetSession{}
		targetPlatform := conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{
				OS: conductor.Linux,
			},
		}
		mockSession.On("TargetPlatform").Return(&targetPlatform, nil)
		sessionSupplier := func() targetsession.TargetSession {
			return &mockSession
		}

		mockRunner := conductormocks_test.MockCommandRunner{}
		mockRunner.On("RunCommand", "printenv HOME").Return("/home/someone", "", nil)
		runnerSupplier := func() conductor.CommandRunner {
			return &mockRunner
		}

		runCollection, err := run.NewRunCollection(t.TempDir())
		require.NoError(t, err)
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		builder.AddEntity("fakeEntity")
		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{WorkloadType: "Launch"})
		require.NoError(t, err)

		origWl := &tool.WorkloadLaunch{WorkingDir: ""}
		var resolvedWl tool.Workload
		wlStage := NewWorkloadOptionsStage(origWl, &resolvedWl, sessionSupplier, runnerSupplier)
		_, err = wlStage.Execute(&recipe.StageContext{
			Context:       context.Background(),
			RunID:         runID,
			RunCollection: runCollection,
		})
		require.NoError(t, err)

		desc, err := runCollection.RunDescription(context.Background(), runID)
		require.NoError(t, err)
		assert.Equal(t, "/home/someone", desc.WorkingDir)
	})
}

func TestWorkloadOptionsStageUseShell(t *testing.T) {
	t.Run("updates rawCommand and command of workload if UseShell is true", func(t *testing.T) {
		origWl := &tool.WorkloadLaunch{
			RawCommand:  `echo "a" && echo "b says \"c\""`,
			Command:     []string{"echo", "a", "&&", "echo", `b says "c"`},
			Environment: map[string]string{"FOO": "bar"},
			WorkingDir:  "/a/b/c",
			UseShell:    true,
		}
		var updatedWl tool.Workload

		mockSession := targetsessionmocks.MockTargetSession{}
		targetPlatform := conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{
				OS: conductor.Linux,
			},
		}
		mockSession.On("TargetPlatform").Return(&targetPlatform, nil)
		sessionSupplier := func() targetsession.TargetSession {
			return &mockSession
		}

		mockRunner := conductormocks_test.MockCommandRunner{}
		mockRunner.On("RunCommand", "echo $SHELL").Return("bash", "", nil)
		mockRunner.On("RunCommand", "bash -c true").Return("", "", nil)
		runnerSupplier := func() conductor.CommandRunner {
			return &mockRunner
		}

		wlStage := NewWorkloadOptionsStage(origWl, &updatedWl, sessionSupplier, runnerSupplier)
		_, err := wlStage.Execute(&recipe.StageContext{})
		assert.NoError(t, err)

		expectedWl := &tool.WorkloadLaunch{
			RawCommand:  `bash -c "echo \"a\" && echo \"b says \\\"c\\\"\""`,
			Command:     []string{"bash", "-c", `echo "a" && echo "b says \"c\""`},
			Environment: origWl.Environment,
			WorkingDir:  origWl.WorkingDir,
			UseShell:    origWl.UseShell,
		}

		assert.Equal(t, expectedWl, updatedWl)
		mockRunner.AssertExpectations(t)
	})
	t.Run("noop if UseShell is false", func(t *testing.T) {
		origWl := &tool.WorkloadLaunch{
			RawCommand:  `echo "A" && echo "b says \"c\""`,
			Command:     []string{"echo", "A", "&&", "echo", `b says "c"`},
			Environment: map[string]string{"FOO": "bar"},
			WorkingDir:  "/a/b/c",
			UseShell:    false,
		}
		var updatedWl tool.Workload
		wlStage := NewWorkloadOptionsStage(origWl, &updatedWl, nil, nil)
		_, err := wlStage.Execute(&recipe.StageContext{})
		assert.NoError(t, err)
		assert.Equal(t, origWl, updatedWl)
	})
	t.Run("notifies readiness notifier and updates stage error if no shell available", func(t *testing.T) {
		origWl := &tool.WorkloadLaunch{
			RawCommand:  `echo "A" && echo "b says \"c\""`,
			Command:     []string{"echo", "A", "&&", "echo", `b says "c"`},
			Environment: map[string]string{"FOO": "bar"},
			WorkingDir:  "/a/b/c",
			UseShell:    true,
		}
		var updatedWl tool.Workload

		mockSession := targetsessionmocks.MockTargetSession{}
		targetPlatform := conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{
				OS: conductor.Linux,
			},
		}
		mockSession.On("TargetPlatform").Return(&targetPlatform, nil)
		sessionSupplier := func() targetsession.TargetSession {
			return &mockSession
		}

		mockRunner := conductormocks_test.MockCommandRunner{}
		mockRunner.On("RunCommand", mock.Anything).Return("", "", errors.New("doesn't exist!"))
		runnerSupplier := func() conductor.CommandRunner {
			return &mockRunner
		}

		wlStage := NewWorkloadOptionsStage(origWl, &updatedWl, sessionSupplier, runnerSupplier)
		mockNotifier := MockReadinessNotifier{}
		mockNotifier.On("OnReadinessProbed", mock.Anything).Run(func(args mock.Arguments) {
			readyOutput := args.Get(0).(recipe.ReadyOutput)
			assert.Equal(t, message.EngineConductorShellresolutionNoShellFound, readyOutput.Advice[0].AdviceMessage.Code())
		})
		_, err := wlStage.Execute(&recipe.StageContext{ReadinessNotifier: &mockNotifier})
		assert.Error(t, err)
		assert.Equal(t, wlStage.ErrorType(), run.RecipeFailureNoShell)
		mockNotifier.AssertExpectations(t)
	})
	t.Run("propagates unexpected error", func(t *testing.T) {
		origWl := &tool.WorkloadLaunch{
			RawCommand:  `echo "A" && echo "b says \"c\""`,
			Command:     []string{"echo", "A", "&&", "echo", `b says "c"`},
			Environment: map[string]string{"FOO": "bar"},
			WorkingDir:  "/a/b/c",
			UseShell:    true,
		}
		var updatedWl tool.Workload

		mockSession := targetsessionmocks.MockTargetSession{}
		targetPlatform := conductor.TargetPlatform{
			PlatformConfiguration: conductor.PlatformConfiguration{
				OS: "abcd",
			},
		}
		mockSession.On("TargetPlatform").Return(&targetPlatform, nil)
		sessionSupplier := func() targetsession.TargetSession {
			return &mockSession
		}

		wlStage := NewWorkloadOptionsStage(origWl, &updatedWl, sessionSupplier, func() conductor.CommandRunner {
			return nil
		})
		_, err := wlStage.Execute(&recipe.StageContext{})
		assert.Error(t, err)
		assert.Equal(t, wlStage.ErrorType(), run.RecipeFailureWorkloadOptions)
	})
}
