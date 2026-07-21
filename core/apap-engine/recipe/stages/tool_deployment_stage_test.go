// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package stages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// writeToolBundle creates a dummy tool bundle tarball
func writeToolBundle(t *testing.T, executableDir string, toolInfo tool.ToolInfo, platform conductor.PlatformConfiguration) {
	t.Helper()

	writeToolBundleWithContents(t, executableDir, toolInfo, platform, nil)
}

func writeToolBundleWithContents(t *testing.T, executableDir string, toolInfo tool.ToolInfo, platform conductor.PlatformConfiguration, contents []byte) {
	t.Helper()

	name := fmt.Sprintf("%s-%s-%s.tar.gz", toolInfo.Name, platform.OS, platform.Architecture)
	path := filepath.Join(executableDir, "tools", toolInfo.Name, toolInfo.Version, name)

	require.NoError(t, os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(path, contents, perms.LocalFilePerm))
}

func markToolDeployed(t *testing.T, fs afero.Fs, baseDir string, toolInfo tool.ToolInfo, platform conductor.PlatformConfiguration) {
	t.Helper()

	dir := filepath.Join(baseDir, toolInfo.Name, toolInfo.Version)
	bundle := filepath.Join(
		dir,
		fmt.Sprintf("%s-%s-%s.tar.gz", toolInfo.Name, platform.OS, platform.Architecture),
	)

	require.NoError(t, fs.MkdirAll(dir, perms.LocalDirPerm))
	require.NoError(t, afero.WriteFile(fs, bundle, []byte{1}, perms.LocalFilePerm))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".extracted"), []byte{}, perms.LocalFilePerm))
}

func TestUpdateStageResult(t *testing.T) {
	testCases := []struct {
		name           string
		results        []deployer.ReconcileResult
		expectedResult deployer.ReconcileResult
	}{
		{
			name:           "Deploy takes priority over all others",
			results:        []deployer.ReconcileResult{deployer.Deploy, deployer.Deployed, deployer.NoAction},
			expectedResult: deployer.Deploy,
		},
		{
			name:           "Deployed takes priority over NoAction",
			results:        []deployer.ReconcileResult{deployer.Deployed, deployer.NoAction},
			expectedResult: deployer.Deployed,
		},
		{
			name:           "NoAction set only if all results are NoAction",
			results:        []deployer.ReconcileResult{deployer.NoAction},
			expectedResult: deployer.NoAction,
		},
		{
			name:           "NoAction set if there are no results",
			results:        []deployer.ReconcileResult{},
			expectedResult: deployer.NoAction,
		},
		{
			name: "Mix test 1",
			results: []deployer.ReconcileResult{deployer.Deploy, deployer.NoAction, deployer.NoAction,
				deployer.Deploy, deployer.Deployed, deployer.Deployed, deployer.NoAction},
			expectedResult: deployer.Deploy,
		},
		{
			name: "Mix test 2",
			results: []deployer.ReconcileResult{deployer.NoAction, deployer.NoAction, deployer.Deployed,
				deployer.NoAction, deployer.NoAction, deployer.Deployed, deployer.NoAction},
			expectedResult: deployer.Deployed,
		},
		{
			name: "Mix test 3",
			results: []deployer.ReconcileResult{deployer.Deployed, deployer.NoAction, deployer.NoAction,
				deployer.NoAction, deployer.NoAction, deployer.NoAction, deployer.NoAction},
			expectedResult: deployer.Deployed,
		},
	}

	stage := ToolDeploymentStage{}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stage.updateStageResult(tc.results)
			assert.Equal(t, tc.expectedResult, stage.Result)
		})
	}
}

func TestToolDeploymentStage_AlwaysExecute(t *testing.T) {
	stage := ToolDeploymentStage{}
	assert.False(t, stage.AlwaysExecute())
}

func TestToolDeploymentStage_Execute(t *testing.T) {
	newTestToolDeploymentStage := func(t *testing.T, pm *packages.PackageManager, platform conductor.PlatformConfiguration, basePaths deployer.BaseToolDeploymentPaths) *ToolDeploymentStage {
		t.Helper()

		mockRunner := &conductormocks.MockCommandRunner{}
		mockRunner.On("RunCommand", mock.Anything).Return("", "", nil).Maybe()

		targetFS := afero.NewMemMapFs()
		session := &targetsessionmocks.MockTargetSession{}
		session.On("ResolveToolsDir").Return(basePaths.DeployedToolsDirectory).Maybe()
		targetSessionSupplier := func() targetsession.TargetSession { return session }

		return &ToolDeploymentStage{
			deploymentMode:        deployer.ToolDeployCHECK,
			machineTypeSupplier:   func() conductor.PlatformConfiguration { return platform },
			locality:              deploymentsupport.DeploymentLocalityTarget,
			commandRunnerSupplier: func() conductor.CommandRunner { return mockRunner },
			targetFilesystemSupplier: func() conductor.TargetFilesystem {
				return conductor.NewAferoTargetFilesystemWithHostFS(targetFS, afero.NewOsFs())
			},
			targetSessionSupplier: targetSessionSupplier,
			toolBundlesSupplier:   func() []deploymentsupport.ToolBundleInfo { return nil },
			packageManager:        pm,
			ProgressCallback:      func(int64, int64, time.Time) {},
		}
	}

	targetPlatform := conductor.PlatformConfiguration{
		Architecture: conductor.AArch64,
		OS:           conductor.Linux,
	}

	// Dummy package manager with tool integrations and tool bundles
	tempDir := t.TempDir()
	executableDir := filepath.Join(tempDir, "packages")

	toolBundles := []tool.ToolInfo{
		{Name: "mandatory-tool-bundle", Version: "1.0"},
		{Name: "recipe-tool-bundle", Version: "2.0"},
		{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
	}
	for _, tb := range toolBundles {
		writeToolBundle(t, executableDir, tb, targetPlatform)
	}

	pm := packages.NewPackageManager(executableDir, "")
	basePaths := deployer.BaseToolDeploymentPaths{
		DeployedToolsDirectory: filepath.Join(tempDir, "deployed-tool-bundles"),
	}

	t.Run("successfully deploys the mandatory tool bundles", func(t *testing.T) {
		stage := newTestToolDeploymentStage(t, pm, targetPlatform, basePaths)
		stage.ToolsToDeploy = []tool.ToolInfo{{Name: "mandatory-tool-bundle", Version: "1.0"}}

		ctx := &recipe.StageContext{Context: context.Background()}
		cleanup, err := stage.Execute(ctx)

		assert.NoError(t, err)
		assert.Nil(t, cleanup)
		assert.Equal(t, deployer.Deploy, stage.Result)
		assert.ElementsMatch(t, []tool.ToolInfo{
			{Name: "mandatory-tool-bundle", Version: "1.0"},
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}, stage.ToolsToDeploy)
	})

	t.Run("reports cumulative byte progress across multiple tool deployments", func(t *testing.T) {
		mandatoryTool := tool.ToolInfo{Name: "mandatory-tool-bundle", Version: "1.0"}
		agentTool := tool.ToolInfo{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()}
		mandatoryPayload := []byte("larger mandatory bundle")
		agentPayload := []byte("agent")
		writeToolBundleWithContents(t, executableDir, mandatoryTool, targetPlatform, mandatoryPayload)
		writeToolBundleWithContents(t, executableDir, agentTool, targetPlatform, agentPayload)

		stage := newTestToolDeploymentStage(t, pm, targetPlatform, basePaths)
		stage.deploymentMode = deployer.ToolDeployAUTO
		stage.ToolsToDeploy = []tool.ToolInfo{mandatoryTool}

		type progressUpdate struct {
			sent int64
			max  int64
		}
		var progress []progressUpdate
		stage.ProgressCallback = func(sent int64, max int64, _ time.Time) {
			progress = append(progress, progressUpdate{sent: sent, max: max})
		}

		session := &targetsessionmocks.MockTargetSession{}
		session.On("ResolveToolsDir").Return(basePaths.DeployedToolsDirectory).Maybe()
		session.On("CloseTargetAgent").Return(nil).Once()
		stage.targetSessionSupplier = func() targetsession.TargetSession { return session }

		ctx := &recipe.StageContext{Context: context.Background(), StageNotifier: &recipe.NullStageNotifier{}}
		cleanup, err := stage.Execute(ctx)
		require.NoError(t, err)
		require.Nil(t, cleanup)

		assert.Equal(t, deployer.Deployed, stage.Result)
		mandatorySize := int64(len(mandatoryPayload))
		totalSize := mandatorySize + int64(len(agentPayload))
		require.NotEmpty(t, progress)
		assert.Equal(t, progressUpdate{sent: 0, max: totalSize}, progress[0])
		assert.Contains(t, progress, progressUpdate{sent: mandatorySize, max: totalSize})
		assert.Equal(t, progressUpdate{sent: totalSize, max: totalSize}, progress[len(progress)-1])

		previousSent := int64(0)
		for _, update := range progress {
			assert.Equal(t, totalSize, update.max)
			assert.GreaterOrEqual(t, update.sent, previousSent)
			assert.LessOrEqual(t, update.sent, totalSize)
			previousSent = update.sent
		}
		session.AssertExpectations(t)
	})

	t.Run("successfully deploys the recipe tool bundles", func(t *testing.T) {
		stage := newTestToolDeploymentStage(t, pm, targetPlatform, basePaths)
		stage.locality = deploymentsupport.DeploymentLocalityTarget
		stage.ToolsToDeploy = []tool.ToolInfo{{Name: "mandatory-tool-bundle", Version: "1.0"}}
		stage.toolBundlesSupplier = func() []deploymentsupport.ToolBundleInfo {
			return []deploymentsupport.ToolBundleInfo{{
				Name:     "recipe-tool-bundle",
				Version:  "2.0",
				Locality: deploymentsupport.DeploymentLocalityTarget,
			}}
		}

		ctx := &recipe.StageContext{Context: context.Background()}
		cleanup, err := stage.Execute(ctx)
		require.NoError(t, err)
		require.Nil(t, cleanup)

		assert.Equal(t, deployer.Deploy, stage.Result)
		assert.ElementsMatch(t, []tool.ToolInfo{
			{Name: "mandatory-tool-bundle", Version: "1.0"},
			{Name: "recipe-tool-bundle", Version: "2.0"},
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}, stage.ToolsToDeploy)
	})

	t.Run("successfully filters out tool bundles", func(t *testing.T) {
		stage := newTestToolDeploymentStage(t, pm, targetPlatform, basePaths)
		stage.locality = deploymentsupport.DeploymentLocalityTarget
		stage.ToolsToDeploy = []tool.ToolInfo{{Name: "mandatory-tool-bundle", Version: "1.0"}}
		stage.toolBundlesSupplier = func() []deploymentsupport.ToolBundleInfo {
			return []deploymentsupport.ToolBundleInfo{{
				Name:     "filtered-out",
				Version:  "2.0",
				Locality: deploymentsupport.DeploymentLocalityHost,
			}}
		}

		ctx := &recipe.StageContext{Context: context.Background()}
		cleanup, err := stage.Execute(ctx)
		require.NoError(t, err)
		require.Nil(t, cleanup)

		assert.Equal(t, deployer.Deploy, stage.Result)
		assert.ElementsMatch(t, []tool.ToolInfo{
			{Name: "mandatory-tool-bundle", Version: "1.0"},
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}, stage.ToolsToDeploy)
	})

	t.Run("does not add agent for host locality without host bundles", func(t *testing.T) {
		stage := newTestToolDeploymentStage(t, pm, targetPlatform, basePaths)
		stage.locality = deploymentsupport.DeploymentLocalityHost
		stage.toolBundlesSupplier = func() []deploymentsupport.ToolBundleInfo {
			return []deploymentsupport.ToolBundleInfo{{
				Name:     "filtered-out",
				Version:  "2.0",
				Locality: deploymentsupport.DeploymentLocalityTarget,
			}}
		}

		ctx := &recipe.StageContext{Context: context.Background()}
		cleanup, err := stage.Execute(ctx)
		require.NoError(t, err)
		require.Nil(t, cleanup)

		assert.Equal(t, deployer.NoAction, stage.Result)
		assert.Empty(t, stage.ToolsToDeploy)
	})

	t.Run("adds host bundles and agent for host locality", func(t *testing.T) {
		stage := newTestToolDeploymentStage(t, pm, targetPlatform, basePaths)
		stage.locality = deploymentsupport.DeploymentLocalityHost
		stage.ToolsToDeploy = []tool.ToolInfo{{Name: "mandatory-tool-bundle", Version: "1.0"}}
		stage.toolBundlesSupplier = func() []deploymentsupport.ToolBundleInfo {
			return []deploymentsupport.ToolBundleInfo{{
				Name:     "recipe-tool-bundle",
				Version:  "2.0",
				Locality: deploymentsupport.DeploymentLocalityHost,
			}}
		}

		ctx := &recipe.StageContext{Context: context.Background()}
		cleanup, err := stage.Execute(ctx)
		require.NoError(t, err)
		require.Nil(t, cleanup)

		assert.Equal(t, deployer.Deploy, stage.Result)
		assert.ElementsMatch(t, []tool.ToolInfo{
			{Name: "mandatory-tool-bundle", Version: "1.0"},
			{Name: "recipe-tool-bundle", Version: "2.0"},
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}, stage.ToolsToDeploy)
	})

	t.Run("fails when recipe tool integration is missing", func(t *testing.T) {
		stage := newTestToolDeploymentStage(t, pm, targetPlatform, basePaths)
		stage.ToolsToDeploy = []tool.ToolInfo{{Name: "mandatory-tool-bundle", Version: "1.0"}}
		stage.toolBundlesSupplier = func() []deploymentsupport.ToolBundleInfo {
			return []deploymentsupport.ToolBundleInfo{{
				Name:     "missing-tool",
				Version:  "1.0",
				Locality: deploymentsupport.DeploymentLocalityTarget,
			}}
		}

		ctx := &recipe.StageContext{Context: context.Background()}
		cleanup, err := stage.Execute(ctx)

		require.Nil(t, cleanup)
		require.Error(t, err)
	})

	t.Run("fails when a tool bundle is missing", func(t *testing.T) {
		stage := newTestToolDeploymentStage(t, pm, targetPlatform, basePaths)
		stage.ToolsToDeploy = []tool.ToolInfo{
			{Name: "mandatory-tool-bundle", Version: "1.0"},
			{Name: "missing-tool-bundle", Version: "3.0"},
		}

		ctx := &recipe.StageContext{Context: context.Background()}
		cleanup, err := stage.Execute(ctx)

		assert.Nil(t, cleanup)
		assert.Error(t, err)
	})
}

func TestToolDeploymentStage_GetMandatoryToolList(t *testing.T) {
	targetStage := &ToolDeploymentStage{locality: deploymentsupport.DeploymentLocalityTarget}

	t.Run("includes only agent for Linux", func(t *testing.T) {
		linuxPlatform := conductor.PlatformConfiguration{
			Architecture: conductor.X86_64,
			OS:           conductor.Linux,
		}
		tools := targetStage.getMandatoryToolList(linuxPlatform)

		assert.Equal(t, []tool.ToolInfo{
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}, tools)
	})

	t.Run("includes only agent for Windows", func(t *testing.T) {
		windowsPlatform := conductor.PlatformConfiguration{
			Architecture: conductor.AArch64,
			OS:           conductor.Win,
		}
		tools := targetStage.getMandatoryToolList(windowsPlatform)

		assert.Equal(t, []tool.ToolInfo{
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}, tools)
	})

	t.Run("includes only agent for Android", func(t *testing.T) {
		androidPlatform := conductor.PlatformConfiguration{
			Architecture: conductor.AArch64,
			OS:           conductor.Android,
		}
		tools := targetStage.getMandatoryToolList(androidPlatform)

		assert.Equal(t, []tool.ToolInfo{
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}, tools)
	})

	t.Run("includes only agent for Darwin", func(t *testing.T) {
		darwinPlatform := conductor.PlatformConfiguration{
			Architecture: conductor.AArch64,
			OS:           conductor.Darwin,
		}

		tools := targetStage.getMandatoryToolList(darwinPlatform)

		assert.Equal(t, []tool.ToolInfo{
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}, tools)
	})
}

func TestToolDeploymentStage_ClosesAgentConnection(t *testing.T) {
	pc := conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.AArch64}
	remoteToolsDir := "/tools"

	writeMandatoryPackages := func(t *testing.T, executableDir string) {
		t.Helper()
		mandatoryTools := []tool.ToolInfo{
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}
		for _, ti := range mandatoryTools {
			writeToolBundle(t, executableDir, ti, pc)
		}
	}

	newStage := func(t *testing.T, deployMode deployer.ToolDeploymentMode, pm *packages.PackageManager, remoteFS afero.Fs, session *targetsessionmocks.MockTargetSession) *ToolDeploymentStage {
		t.Helper()

		cmdRunner := &conductormocks.MockCommandRunner{}
		cmdRunner.On("RunCommand", mock.Anything).Return("", "", nil).Maybe()
		session.On("ResolveToolsDir").Return(remoteToolsDir).Maybe()

		return &ToolDeploymentStage{
			deploymentMode:        deployMode,
			machineTypeSupplier:   func() conductor.PlatformConfiguration { return pc },
			locality:              deploymentsupport.DeploymentLocalityTarget,
			commandRunnerSupplier: func() conductor.CommandRunner { return cmdRunner },
			targetFilesystemSupplier: func() conductor.TargetFilesystem {
				return conductor.NewAferoTargetFilesystemWithHostFS(remoteFS, afero.NewOsFs())
			},
			targetSessionSupplier: func() targetsession.TargetSession { return session },
			toolBundlesSupplier:   func() []deploymentsupport.ToolBundleInfo { return nil },
			packageManager:        pm,
			ProgressCallback:      func(int64, int64, time.Time) {},
		}
	}

	t.Run("does not close agent connection if doing a check", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeMandatoryPackages(t, tmpDir)
		pm := packages.NewPackageManager(tmpDir, "")

		session := &targetsessionmocks.MockTargetSession{}
		stage := newStage(t, deployer.ToolDeployCHECK, pm, afero.NewMemMapFs(), session)

		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		require.NoError(t, err)

		session.AssertNotCalled(t, "CloseTargetAgent")
	})

	t.Run("does not close agent connection if already deployed", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeMandatoryPackages(t, tmpDir)
		pm := packages.NewPackageManager(tmpDir, "")
		remoteFS := afero.NewMemMapFs()

		mandatoryTools := []tool.ToolInfo{
			{Name: terminology.GetAgentBinaryName(), Version: versions.GetVersion()},
		}
		for _, ti := range mandatoryTools {
			markToolDeployed(t, remoteFS, remoteToolsDir, ti, pc)
		}

		session := &targetsessionmocks.MockTargetSession{}
		stage := newStage(t, deployer.ToolDeployAUTO, pm, remoteFS, session)

		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		require.NoError(t, err)
		require.Equal(t, deployer.NoAction, stage.Result)

		session.AssertNotCalled(t, "CloseTargetAgent")
	})

	t.Run("closes agent connection if any tool was deployed", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeMandatoryPackages(t, tmpDir)
		pm := packages.NewPackageManager(tmpDir, "")

		session := &targetsessionmocks.MockTargetSession{}
		session.On("CloseTargetAgent").Return(nil).Once()
		stage := newStage(t, deployer.ToolDeployAUTO, pm, afero.NewMemMapFs(), session)

		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		require.NoError(t, err)
		require.Equal(t, deployer.Deployed, stage.Result)

		session.AssertCalled(t, "CloseTargetAgent")
	})

	t.Run("closes agent connection pre-deployment if mode is FORCE", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeMandatoryPackages(t, tmpDir)
		pm := packages.NewPackageManager(tmpDir, "")

		stage := newStage(t, deployer.ToolDeployAUTOFORCE, pm, afero.NewMemMapFs(), &targetsessionmocks.MockTargetSession{})

		// Overwrite mocking of ResolveToolsDir to ensure agent connection is closed beforehand
		var agentClosed bool
		session := &targetsessionmocks.MockTargetSession{}
		session.On("CloseTargetAgent").Run(func(args mock.Arguments) {
			agentClosed = true
		}).Return(nil).Once()
		session.On("ResolveToolsDir").Run(func(args mock.Arguments) {
			assert.True(t, agentClosed, "agent should have been closed prior to deployment")
		}).Return(remoteToolsDir)
		stage.targetSessionSupplier = func() targetsession.TargetSession {
			return session
		}

		_, err := stage.Execute(&recipe.StageContext{Context: context.Background()})
		require.NoError(t, err)
		require.Equal(t, deployer.Deployed, stage.Result)

		session.AssertCalled(t, "CloseTargetAgent")
	})

	t.Run("unable to close the agent connection", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeMandatoryPackages(t, tmpDir)
		pm := packages.NewPackageManager(tmpDir, "")

		logger, hook := test.NewNullLogger()
		ctx := logx.CtxWithLogger(context.Background(), logger)

		session := &targetsessionmocks.MockTargetSession{}
		session.On("CloseTargetAgent").Return(errors.New("connection refused")).Once()
		stage := newStage(t, deployer.ToolDeployAUTO, pm, afero.NewMemMapFs(), session)

		_, err := stage.Execute(&recipe.StageContext{Context: ctx})
		require.NoError(t, err)

		session.AssertCalled(t, "CloseTargetAgent")

		// Last log should be a warning
		lastEntry := hook.LastEntry()
		assert.Equal(t, logrus.WarnLevel, lastEntry.Level)
		assert.Contains(t, lastEntry.Message, "Closing target agent connection due to tool deployment failed")

	})
}
