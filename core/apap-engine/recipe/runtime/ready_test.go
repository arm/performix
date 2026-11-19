// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	agentmocks "github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	ds "github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/stages"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	sessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	apimocks "github.com/Arm-Debug/apap-cli/api/mocks"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

type scriptedReadyStageMock struct {
	mock.Mock
	requiresTools bool
}

func (s *scriptedReadyStageMock) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	args := s.Called(ctx, stageContext)
	cleanUp, _ := args.Get(0).(func())
	return cleanUp, args.Error(1)
}

func (s *scriptedReadyStageMock) Name() string {
	return s.Called().String(0)
}

func (s *scriptedReadyStageMock) RequiresTools() bool { return s.requiresTools }

func newScriptedReadyStageMock(name string) *scriptedReadyStageMock {
	stage := &scriptedReadyStageMock{}
	stage.On("Name").Return(name).Maybe()
	stage.On("Execute", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	return stage
}

func newReadyStageConfiguration(recipeDef *recipe.Recipe) *StageConfiguration {
	return &StageConfiguration{
		Recipe: recipeDef,
		Ctx: &recipe.RecipeCtx{
			Target:         &target.LocalTarget{},
			RecipePath:     "dummy-recipe.yaml",
			RecipeMetadata: recipe.RecipeMetadata{Name: "test", Version: "1.0", APIVersion: "1.0,0"},
		},
	}
}

func TestAndroidActivityCheckedOnlyForAndroidLaunch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		workload tool.Workload
		want     bool
	}{
		{name: "android launch", workload: &tool.WorkloadAndroidLaunch{}, want: true},
		{name: "ordinary launch", workload: &tool.WorkloadLaunch{}},
		{name: "system-wide", workload: &tool.WorkloadSystemWide{}},
		{name: "no workload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := newReadyStageConfiguration(&recipe.Recipe{})
			config.Ctx.OrigWorkload = tc.workload

			stageList := ConfigureRecipeReadyStages(config, nil)

			if tc.want {
				require.IsType(t, &stages.TargetLockStage{}, stageList[7])
				require.IsType(t, &stages.ToolDeploymentStage{}, stageList[8])
				require.IsType(t, &stages.ToolDeploymentStage{}, stageList[9])
				require.IsType(t, &stages.ValidateAndroidActivityStage{}, stageList[10])
				require.IsType(t, &stages.CollectTargetInfoStage{}, stageList[11])
			} else {
				for _, stage := range stageList {
					_, isActivityCheck := stage.(*stages.ValidateAndroidActivityStage)
					assert.False(t, isActivityCheck)
				}
			}
		})
	}
}

func TestConfigureRecipeReadyStages(t *testing.T) {
	for _, validate := range []bool{false, true} {
		t.Run(fmt.Sprintf("parameter validation=%t", validate), func(t *testing.T) {
			// Given
			option := newScriptedReadyStageMock("option-stage")
			ready := newScriptedReadyStageMock("ready-stage")
			cfg := newReadyStageConfiguration(&recipe.Recipe{
				ParameterOptionsStages: []recipe.ParameterOptionsStage{option},
				ReadyStages:            []recipe.ScriptedStage{ready},
			})
			if validate {
				cfg.Recipe.ParameterValidationStage = newScriptedReadyStageMock("validation")
			}

			// When
			stageList := ConfigureRecipeReadyStages(cfg, cfg.Recipe.ReadyStages)

			// Then
			expected := []recipe.Stage{
				&stages.TargetConnectStage{},
				&stages.TargetArchitectureStage{},
				&stages.TargetPlatformSupportStage{},
				&stages.WorkloadOptionsStage{},
				&stages.ToolBundleResolutionStage{},
				&stages.HostArchitectureStage{},
				&stages.ConnectingToTargetAgentStage{},
				&stages.TargetLockStage{},
				&stages.ToolDeploymentStage{},
				&stages.ToolDeploymentStage{},
				&stages.CollectTargetInfoStage{},
				&stages.ConnectingToHostAgentStage{},
			}
			if validate {
				expected = append(expected, &stages.CustomRecipeStage{}, &stages.CustomRecipeStage{})
			}
			expected = append(expected, &stages.CustomRecipeStage{}, &stages.ReleaseTargetLockStage{})
			require.Len(t, stageList, len(expected))
			for i, stage := range expected {
				assert.IsType(t, stage, stageList[i], "stage %d", i)
			}
			if validate {
				assert.Equal(t, "option-stage", stageList[12].Name())
				assert.Equal(t, "Validating recipe parameters", stageList[13].Name())
			}
			assert.Equal(t, "ready-stage", stageList[len(stageList)-2].Name())
		})
	}
}

func TestConfigureRecipeInfoStages(t *testing.T) {
	t.Run("stages are in the correct order", func(t *testing.T) {
		infoStage := newScriptedReadyStageMock("info-stage")
		cfg := newReadyStageConfiguration(&recipe.Recipe{})

		stageList := ConfigureRecipeInfoStages(cfg, []recipe.ParameterOptionsStage{infoStage})

		require.Len(t, stageList, 9)
		assert.IsType(t, &stages.TargetConnectStage{}, stageList[0])
		assert.IsType(t, &stages.TargetArchitectureStage{}, stageList[1])
		assert.IsType(t, &stages.TargetPlatformSupportStage{}, stageList[2])
		assert.IsType(t, &stages.WorkloadOptionsStage{}, stageList[3])
		assert.IsType(t, &stages.ConnectingToTargetAgentStage{}, stageList[4])
		assert.IsType(t, &stages.TargetLockStage{}, stageList[5])
		assert.IsType(t, &stages.CollectTargetInfoStage{}, stageList[6])
		require.IsType(t, &stages.CustomRecipeStage{}, stageList[7])
		assert.Equal(t, "info-stage", stageList[7].Name())
		assert.IsType(t, &stages.ReleaseTargetLockStage{}, stageList[8])
	})

	t.Run("only target support stages are required without dynamic options", func(t *testing.T) {
		cfg := newReadyStageConfiguration(&recipe.Recipe{})

		stageList := ConfigureRecipeInfoStages(cfg, nil)

		require.Len(t, stageList, 3)
		assert.IsType(t, &stages.TargetConnectStage{}, stageList[0])
		assert.IsType(t, &stages.TargetArchitectureStage{}, stageList[1])
		assert.IsType(t, &stages.TargetPlatformSupportStage{}, stageList[2])
	})
}

func TestCheckRecipeReadyDeployment(t *testing.T) {
	for _, scenario := range []string{"first check", "optional tools", "tool-backed options", "missing host agent", "unprepared", "busy target", "deployment failure"} {
		t.Run(scenario, func(t *testing.T) {
			// Given
			platform := conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64}
			packageDir := t.TempDir()
			fs := afero.NewMemMapFs()
			agentName, agentVersion := terminology.GetAgentBinaryName(), versions.GetVersion()
			for _, bundle := range []struct{ name, version string }{
				{agentName, agentVersion}, {"probe-tool", "1.0"}, {"analysis-tool", "1.0"}, {"jit-tool", "1.0"},
			} {
				archive := fmt.Sprintf("%s-%s-%s.tar.gz", bundle.name, platform.OS, platform.Architecture)
				source := filepath.Join(packageDir, "tools", bundle.name, bundle.version, archive)
				require.NoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
				require.NoError(t, os.WriteFile(source, []byte{1}, 0o600))
				if bundle.name == agentName {
					for _, root := range []string{"/target-tools", "/host-tools"} {
						if root == "/host-tools" && scenario == "missing host agent" {
							continue
						}
						dir := filepath.Join(root, agentName, agentVersion)
						require.NoError(t, fs.MkdirAll(dir, 0o755))
						require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, archive), []byte{1}, 0o600))
						require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".extracted"), nil, 0o600))
					}
				}
			}

			runner := &conductormocks.MockCommandRunner{}
			var deploymentErr error
			if scenario == "deployment failure" {
				deploymentErr = errors.New("cannot extract bundle")
			}
			runner.On("RunCommand", mock.Anything).Return("", "", deploymentErr).Maybe()
			connection := &sessionmocks.MockTargetConnection{}
			connection.On("CommandRunner").Return(runner)
			connection.On("Filesystem").Return(conductor.NewAferoTargetFilesystemWithHostFS(fs, afero.NewOsFs()))
			client := &apimocks.TargetAgentClient{}
			stream := &agentmocks.MockHoldLockStream{}
			stream.On("Recv").Return(&targetagentproto.LockGranted{}, nil).Maybe()
			var lockErr error
			if scenario == "busy target" {
				lockErr = message.New(message.EngineRecipeTargetLockTimeout).WithMetadata(map[string]string{"timeout": "3s"})
			}
			client.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).Return(stream, lockErr).Maybe()
			client.On("GetTargetInfo", mock.Anything, mock.Anything, mock.Anything).Return(&targetagentproto.TargetInfo{
				CpuTopology: &targetagentproto.CPUTopology{},
			}, nil).Maybe()

			cfg := newReadyStageConfiguration(&recipe.Recipe{Name: "test-recipe"})
			provider := &sessionmocks.MockTargetSessionProvider{}
			targetSession := &sessionmocks.MockTargetSession{}
			hostSession := &sessionmocks.MockTargetSession{}
			for _, session := range []*sessionmocks.MockTargetSession{targetSession, hostSession} {
				session.On("Connect", mock.Anything).Return(connection, nil).Maybe()
				session.On("Connect", mock.Anything, mock.Anything).Return(connection, nil).Maybe()
				session.On("TargetPlatform").Return(&conductor.TargetPlatform{PlatformConfiguration: platform}, nil).Maybe()
			}
			var agentErr error
			if scenario == "unprepared" {
				agentErr = errors.New("target agent is not installed")
				require.NoError(t, fs.RemoveAll(filepath.Join("/target-tools", agentName)))
			}
			targetSession.On("TargetAgent", mock.Anything).Return(agent.NewAgentConn(client), agentErr).Maybe()
			hostSession.On("TargetAgent", mock.Anything).Run(func(mock.Arguments) {
				exists, err := afero.Exists(fs, filepath.Join("/host-tools", agentName, agentVersion, ".extracted"))
				require.NoError(t, err)
				require.True(t, exists, "host agent must be installed before connecting")
			}).Return(agent.NewAgentConn(client), nil).Maybe()
			if scenario == "missing host agent" {
				hostSession.On("CloseTargetAgent").Return(nil).Once()
			}
			targetSession.On("ResolveToolsDir").Return("/target-tools").Maybe()
			hostSession.On("ResolveToolsDir").Return("/host-tools").Maybe()
			provider.On("TargetSession", cfg.Ctx.Target).Return(targetSession, nil)
			provider.On("HostSession").Return(hostSession, nil).Maybe()
			cfg.TargetSessions = provider
			cfg.PackageManager = packages.NewPackageManager(packageDir, "")
			cfg.Recipe.Parameters.Checkbox = []parameters.CheckboxParameter{{Parameter: parameters.Parameter{ID: "collect_jit"}}}
			var err error
			cfg.Ctx.ParamValues, err = parameters.BindRecipeParameters(map[string]any{
				"collect_jit": scenario == "optional tools",
			}, cfg.Recipe.Parameters, cfg.Recipe.Name)
			require.NoError(t, err)
			cfg.Recipe.Deployments = []ds.DeploymentDeclaration{{Dependencies: []ds.Dependency{
				{Type: ds.DependencyTypeToolBundle, Name: "probe-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
				{Type: ds.DependencyTypeToolBundle, Name: "analysis-tool", Version: "1.0", Locality: ds.DeploymentLocalityHost, RequiredWhen: ds.RequirementSpec{Type: ds.RequirementTypeAlways}},
				{Type: ds.DependencyTypeToolBundle, Name: "jit-tool", Version: "1.0", RequiredWhen: ds.RequirementSpec{
					Type: ds.RequirementTypeIfParamIsSet, Parameters: map[string]interface{}{"collect_jit": true},
				}},
			}}}
			assertToolsDeployed := func() {
				t.Helper()
				for _, marker := range []string{"/target-tools/probe-tool/1.0/.extracted", "/host-tools/analysis-tool/1.0/.extracted"} {
					exists, err := afero.Exists(fs, marker)
					require.NoError(t, err)
					require.True(t, exists, "tools must be deployed before callbacks")
				}
			}
			options := &scriptedReadyStageMock{requiresTools: true}
			if scenario == "tool-backed options" {
				options.On("Name").Return("tool-backed options")
				options.On("Execute", mock.Anything, mock.Anything).Run(func(mock.Arguments) {
					assertToolsDeployed()
				}).Return(nil, nil)
				cfg.Recipe.ParameterOptionsStages = []recipe.ParameterOptionsStage{options}
				cfg.Recipe.ParameterValidationStage = newScriptedReadyStageMock("validation")
			}
			probeCount := 0
			probe := &scriptedReadyStageMock{}
			probe.On("Name").Return("probe")
			probe.On("Execute", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				probeCount++
				assertToolsDeployed()
				args.Get(1).(*recipe.StageContext).ReadinessNotifier.OnReadinessProbed(recipe.ReadyOutput{
					Status: recipe.ReadyStatusError,
					Advice: []recipe.ReadyAdvice{{AdviceSeverity: recipe.AdviceSeverityError,
						AdviceMessage: message.New(message.EngineRecipeparserJsRecipeStageReadinessMessage).WithMetadata(map[string]string{"message": "Insufficient PMU counters"}),
					}},
				})
			}).Return(nil, nil).Maybe()
			cfg.Recipe.ReadyStages = []recipe.ScriptedStage{probe}

			// When
			result, err := CheckRecipeReady(context.Background(), cfg)

			// Then
			switch scenario {
			case "unprepared":
				require.NoError(t, err)
				require.Equal(t, recipe.ReadyStatusError, result.Status)
				exists, err := afero.Exists(fs, filepath.Join("/target-tools", agentName))
				require.NoError(t, err)
				require.False(t, exists)
			case "busy target", "deployment failure":
				require.Error(t, err)
				require.Nil(t, result)
			default:
				require.NoError(t, err)
				require.Equal(t, recipe.ReadyStatusError, result.Status)
				require.Len(t, result.Advice, 1)
				require.Equal(t, "Insufficient PMU counters", result.Advice[0].AdviceMessage.Metadata()["message"])
				exists, err := afero.Exists(fs, "/target-tools/jit-tool/1.0/.extracted")
				require.NoError(t, err)
				require.Equal(t, scenario == "optional tools", exists)
				commands := len(runner.Calls)
				_, err = CheckRecipeReady(context.Background(), cfg)
				require.NoError(t, err)
				require.Equal(t, commands, len(runner.Calls), "repeat checks must not extract tools again")
				require.Equal(t, 2, probeCount)
				if scenario == "tool-backed options" {
					options.AssertNumberOfCalls(t, "Execute", 2)
				}
			}
			if scenario == "unprepared" || scenario == "busy target" || scenario == "deployment failure" {
				assert.Zero(t, probeCount)
				client.AssertNotCalled(t, "GetTargetInfo", mock.Anything, mock.Anything, mock.Anything)
			}
			if scenario == "unprepared" || scenario == "busy target" {
				runner.AssertNotCalled(t, "RunCommand", mock.Anything)
			}
			targetSession.AssertNotCalled(t, "CloseTargetAgent")
			if scenario == "missing host agent" {
				hostSession.AssertNumberOfCalls(t, "CloseTargetAgent", 1)
			} else {
				hostSession.AssertNotCalled(t, "CloseTargetAgent")
			}
		})
	}
}
