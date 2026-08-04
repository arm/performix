// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package deployer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func writeLocalToolFile(t *testing.T, testTool tool.ToolInfo, targetMachineType conductor.PlatformConfiguration) (ToolPaths, func()) {
	tempDir, err := os.MkdirTemp("", "apapInstall-")
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "tools", "testTool", "1.0"), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "tools", "testTool", "1.0", "testTool-Linux-x86_64.tar.gz"), []byte{}, perms.LocalFilePerm))

	pm := packages.NewPackageManager(tempDir, "")
	tp, err := CreateToolDeploymentPaths(targetMachineType, testTool, pm, "apapDeploy")
	require.NoError(t, err)

	return tp, func() { require.NoError(t, os.RemoveAll(tempDir)) }
}

func writeMarkerFile(t *testing.T, fs afero.Fs, tp ToolPaths) {
	t.Helper()
	markerPath := filepath.ToSlash(filepath.Join(tp.DstDir, deployedMarker))
	require.NoError(t, afero.WriteFile(fs, markerPath, []byte{}, perms.LocalFilePerm))
}

func targetFilesystem(fs afero.Fs) conductor.TargetFilesystem {
	return conductor.NewAferoTargetFilesystemWithHostFS(fs, afero.NewOsFs())
}

func TestDeployTools(t *testing.T) {
	testTool := tool.ToolInfo{
		Name:    "testTool",
		Version: "1.0",
	}

	targetMachineType := conductor.PlatformConfiguration{OS: conductor.Linux, Architecture: conductor.X86_64}

	t.Run("Reconcile fails, error produced when unzip fails, destination is cleaned up", func(t *testing.T) {
		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", `tar zxf apapDeploy/testTool/1.0/testTool-Linux-x86_64.tar.gz -C apapDeploy/testTool/1.0`).Return("", "", errors.New("rekt"))

		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		remoteFS := afero.NewMemMapFs()

		_, err := ReconcileTool(targetMachineType, testTool, ToolDeployAUTO, mcr, targetFilesystem(remoteFS), tp, nil)
		expectedMetadata := map[string]string{
			"toolName": testTool.Name,
			"version":  testTool.Version,
			"toolPath": filepath.ToSlash(tp.TargetBundle),
			"toolDir":  filepath.ToSlash(tp.DstDir),
		}
		expectedErr := message.New(message.EngineToolDeployerInflateToolBundle).WithCause(errors.New("rekt")).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		// Check the target directory was removed
		exists, err := afero.DirExists(remoteFS, "apapDeploy/testTool/1.0")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Reconcile succeeds, no deployment required", func(t *testing.T) {
		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).Return("", "", nil)

		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		remoteFS := afero.NewMemMapFs()
		require.NoError(t, remoteFS.MkdirAll("apapDeploy/testTool/1.0", perms.LocalDirPerm))
		require.NoError(t, afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/testTool-Linux-x86_64.tar.gz", []byte{23, 53, 9, 3}, perms.LocalFilePerm))
		writeMarkerFile(t, remoteFS, tp)

		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployAUTO, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.NoError(t, err)
		assert.Equal(t, NoAction, reconcileResult)
	})

	t.Run("Reconcile fails, source tool unavailable", func(t *testing.T) {
		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).Return("", "", nil)

		tempDir, err := os.MkdirTemp("", "apapInstall-")
		require.NoError(t, err)
		defer func(path string) { require.NoError(t, os.RemoveAll(path)) }(tempDir)

		pm := packages.NewPackageManager(tempDir, "")
		// ReconcileTool not called due to preceding error
		_, err = CreateToolDeploymentPaths(targetMachineType, testTool, pm, "apapDeploy")
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EnginePackagesLoadCoreToolBundles, msgErr.Code())
		assert.Equal(t, filepath.Join(tempDir, "tools"), msgErr.Metadata()["executableToolsDir"])
	})

	t.Run("Reconcile succeeds, deployment triggered", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		// Empty remoteFS (missing tool) triggers deployment
		remoteFS := afero.NewMemMapFs()

		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", `tar zxf apapDeploy/testTool/1.0/testTool-Linux-x86_64.tar.gz -C apapDeploy/testTool/1.0`).Return("", "", nil).Run(func(args mock.Arguments) {
			_ = afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/extractedTool", []byte{}, perms.LocalFilePerm)
		})

		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployAUTO, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.NoError(t, err)
		assert.Equal(t, Deployed, reconcileResult)

		// Ensure the tool file was deployed to the targetFS
		_, err = remoteFS.Open("apapDeploy/testTool/1.0/extractedTool")
		assert.NoError(t, err)
	})

	t.Run("Tool deployed when target directory is empty", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		// Empty target directory triggers deployment
		remoteFS := afero.NewMemMapFs()
		_ = remoteFS.Mkdir("apapDeploy/testTool/1.0/", perms.LocalDirPerm)

		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", `tar zxf apapDeploy/testTool/1.0/testTool-Linux-x86_64.tar.gz -C apapDeploy/testTool/1.0`).Return("", "", nil).Run(func(args mock.Arguments) {
			_ = afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/extractedTool", []byte{}, perms.LocalFilePerm)
		})

		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployAUTO, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.NoError(t, err)
		assert.Equal(t, Deployed, reconcileResult)

		// Ensure the tool file was deployed to the targetFS
		_, err = remoteFS.Open("apapDeploy/testTool/1.0/extractedTool")
		assert.NoError(t, err)
	})

	t.Run("Force reconcile cleans existing directory", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		remoteFS := afero.NewMemMapFs()
		// Simulate a valid deployment with marker
		require.NoError(t, remoteFS.MkdirAll("apapDeploy/testTool/1.0", perms.LocalDirPerm))
		require.NoError(t, afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/testTool-Linux-x86_64.tar.gz", []byte{1, 2, 3}, perms.LocalFilePerm))
		writeMarkerFile(t, remoteFS, tp)

		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", `tar zxf apapDeploy/testTool/1.0/testTool-Linux-x86_64.tar.gz -C apapDeploy/testTool/1.0`).Return("", "", nil).Run(func(args mock.Arguments) {
			_ = afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/extractedTool", []byte{}, perms.LocalFilePerm)
		})

		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployAUTOFORCE, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.NoError(t, err)
		assert.Equal(t, Deployed, reconcileResult)

		// Ensure the tool file was deployed to the targetFS
		_, err = remoteFS.Open("apapDeploy/testTool/1.0/extractedTool")
		assert.NoError(t, err)

		exists, err := afero.Exists(remoteFS, "apapDeploy/testTool/1.0/garbageDeploy")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Force reconcile fails when existing directory cannot be removed", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		baseFS := afero.NewMemMapFs()
		require.NoError(t, baseFS.MkdirAll("apapDeploy/testTool/1.0", perms.LocalDirPerm))
		require.NoError(t, afero.WriteFile(baseFS, "apapDeploy/testTool/1.0/garbageDeploy", []byte{}, perms.LocalFilePerm))

		remoteFS := afero.NewReadOnlyFs(baseFS)

		mcr := &conductormocks.MockCommandRunner{}

		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployAUTOFORCE, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.Equal(t, NoAction, reconcileResult)
		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineToolDeployerRemoveExistingTool, msg.Code())
		expectedMetadata := map[string]string{
			"toolName": testTool.Name,
			"version":  testTool.Version,
			"toolDir":  filepath.ToSlash(tp.DstDir),
		}
		assert.Equal(t, expectedMetadata, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Check tool deployment returns NoAction when tool already deployed", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		remoteFS := afero.NewMemMapFs()
		require.NoError(t, remoteFS.MkdirAll("apapDeploy/testTool/1.0", perms.LocalDirPerm))
		require.NoError(t, afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/testTool-Linux-x86_64.tar.gz", []byte{4, 5, 6}, perms.LocalFilePerm))
		writeMarkerFile(t, remoteFS, tp)

		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).Return("", "", nil).Run(func(args mock.Arguments) {
			_ = afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/extractedTool", []byte{}, perms.LocalFilePerm)
		})

		// Check deployment - should return NoAction
		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployCHECK, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.NoError(t, err)
		assert.Equal(t, NoAction, reconcileResult)
	})

	t.Run("Check tool deployment returns Deploy when tool not already deployed", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		// Empty remoteFS (missing tool)
		remoteFS := afero.NewMemMapFs()

		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).Return("", "", nil).Run(func(args mock.Arguments) {
			_ = afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/extractedTool", []byte{}, perms.LocalFilePerm)
		})

		// Check deployment - should return Deploy
		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployCHECK, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.NoError(t, err)
		assert.Equal(t, Deploy, reconcileResult)
	})

	t.Run("Check tool deployment returns Deploy when only bundle exists without marker", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		remoteFS := afero.NewMemMapFs()
		require.NoError(t, remoteFS.MkdirAll("apapDeploy/testTool/1.0", perms.LocalDirPerm))
		require.NoError(t, afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/testTool-Linux-x86_64.tar.gz", []byte{9, 9, 9}, perms.LocalFilePerm))

		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).Return("", "", nil)

		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployCHECK, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.NoError(t, err)
		assert.Equal(t, Deploy, reconcileResult)
	})

	t.Run("Check tool deployment returns Deploy when bundle file is zero length", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		remoteFS := afero.NewMemMapFs()
		require.NoError(t, remoteFS.MkdirAll("apapDeploy/testTool/1.0", perms.LocalDirPerm))
		require.NoError(t, afero.WriteFile(remoteFS, tp.TargetBundle, []byte{}, perms.LocalFilePerm))
		writeMarkerFile(t, remoteFS, tp)

		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).Return("", "", nil)

		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployCHECK, mcr, targetFilesystem(remoteFS), tp, nil)
		assert.NoError(t, err)
		assert.Equal(t, Deploy, reconcileResult)
	})

	t.Run("deployTools fails when failing to copy tool", func(t *testing.T) {
		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).Return("", "", nil)

		remoteFS := afero.NewMemMapFs()

		tp := ToolPaths{
			DstDir:       "a",
			TargetBundle: "b",
			SrcBundle:    "c",
		}

		err := deployTool(targetMachineType, testTool, mcr, targetFilesystem(remoteFS), tp, []conductor.ReportProgressRequest{}, false)

		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineToolDeployerCopyTool, msg.Code())
		expectedMetadata := map[string]string{
			"toolName": testTool.Name,
			"version":  testTool.Version,
			"srcPath":  "c",
			"toolPath": "b",
		}
		assert.Equal(t, expectedMetadata, msg.Metadata())
	})

	t.Run("deployTools succeeds for Windows target", func(t *testing.T) {
		windowsTarget := conductor.PlatformConfiguration{OS: conductor.Win, Architecture: conductor.X86_64}
		tempDir := t.TempDir()
		srcBundle := filepath.Join(tempDir, "c")
		require.NoError(t, os.WriteFile(srcBundle, []byte("bundle"), perms.LocalFilePerm))

		tp := ToolPaths{
			DstDir:       `C:\tools\pkg`,
			TargetBundle: `C:\tmp\bundle.tar.gz`,
			SrcBundle:    srcBundle,
		}

		remoteFS := afero.NewMemMapFs()
		mcr := &conductormocks.MockCommandRunner{}
		expectedCmd := fmt.Sprintf(
			`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$bundle = %s; Set-Location -LiteralPath ([System.IO.Path]::GetDirectoryName($bundle)); tar -zxf ([System.IO.Path]::GetFileName($bundle)) -C %s"`,
			conductor.QuotePowershellString(filepath.ToSlash(tp.TargetBundle)),
			conductor.QuotePowershellString(filepath.ToSlash(tp.DstDir)),
		)
		mcr.On("RunCommand", expectedCmd).Return("", "", nil).Once()

		err := deployTool(windowsTarget, testTool, mcr, targetFilesystem(remoteFS), tp, []conductor.ReportProgressRequest{}, false)

		assert.NoError(t, err)
		mcr.AssertExpectations(t)
	})

	t.Run("Force deployment succeeds if tool is not already deployed", func(t *testing.T) {
		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		// Empty fs, no existing deployment
		remoteFS := afero.NewMemMapFs()

		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).Return("", "", nil).Run(func(args mock.Arguments) {
			_ = afero.WriteFile(remoteFS, "apapDeploy/testTool/1.0/extractedTool", []byte{}, perms.LocalFilePerm)
		})

		reconcileResult, err := ReconcileTool(targetMachineType, testTool, ToolDeployAUTOFORCE, mcr, targetFilesystem(remoteFS), tp, nil)

		assert.NoError(t, err)
		assert.Equal(t, Deployed, reconcileResult)

		// Ensure the tool file was deployed to the targetFS
		_, err = remoteFS.Open("apapDeploy/testTool/1.0/extractedTool")
		assert.NoError(t, err)

		exists, err := afero.Exists(remoteFS, "apapDeploy/testTool/1.0/garbageDeploy")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("returns a permission error if tool deployment fails with permission denied", func(t *testing.T) {
		mcr := &conductormocks.MockCommandRunner{}
		mcr.On("RunCommand", mock.Anything).
			Return("some-user\n", "", nil).Once()

		tp, cleanup := writeLocalToolFile(t, testTool, targetMachineType)
		defer cleanup()

		// Attempt to deploy tools to a read-only target filesystem so MkdirAll fails with permission denied.
		remoteFS := afero.NewReadOnlyFs(afero.NewMemMapFs())
		err := deployTool(
			targetMachineType, testTool, mcr, targetFilesystem(remoteFS), tp,
			[]conductor.ReportProgressRequest{}, false)

		// Expected err
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineToolDeployerCreateToolDeploymentDirPermissionDenied, msg.Code())
	})
}
