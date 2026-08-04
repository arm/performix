// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestCreateSupportPackage_Basic(t *testing.T) {
	dataDir, stateDir, configDir, homeDir := configureSupportEnv(t)

	originalTargetPath := target.DefaultTargetFilepath
	target.DefaultTargetFilepath = filepath.Join(configDir, target.DefaultTargetFilename)
	t.Cleanup(func() { target.DefaultTargetFilepath = originalTargetPath })

	require.NoError(t, os.MkdirAll(stateDir, perms.LocalDirPerm))

	engineLog := writeEngineLog(t, stateDir, "engine.log", "engine log contents")
	guiLog := writeGUILog(t, homeDir, "gui log contents")
	guiLogDir := filepath.Dir(guiLog)

	runDir := filepath.Join(dataDir, run.RunDirName)
	rc, err := run.NewRunCollection(runDir)
	require.NoError(t, err)
	builder, err := rc.RunBuilder()
	require.NoError(t, err)
	builder.AddEntity("artifacts")
	runID, err := rc.CreateRun(builder, &cdf.Metadata{
		Name:         "example run",
		RecipeName:   "test_recipe",
		StartTime:    util.CurrentTime(),
		EndTime:      util.CurrentTime(),
		RunResult:    string(run.RecipeSuccess),
		TargetName:   "local",
		TargetConfig: target.JSONTarget{Value: &target.JSONLocalTarget{}},
	})
	require.NoError(t, err)

	outputDir := t.TempDir()
	result, err := CreateSupportPackage(context.Background(), PackageOptions{
		RunIDs:     []run.RunID{runID},
		OutputDir:  outputDir,
		CLIVersion: "1.2.3",
		GUIVersion: "4.5.6",
		LogCount:   2,
		GUILogDir:  guiLogDir,
	}, map[string]any{"custom": "value"}, rc)
	require.NoError(t, err)
	require.FileExists(t, result.PackagePath)
	require.Equal(t, outputDir, filepath.Dir(result.PackagePath))
	require.True(t, strings.HasPrefix(filepath.Base(result.PackagePath), supportPkgPrefix+"_"))
	require.Greater(t, result.PackageSizeBytes, int64(0))

	r, err := zip.OpenReader(result.PackagePath)
	require.NoError(t, err)
	defer r.Close()

	expected := map[string]bool{
		filepath.ToSlash(filepath.Join("logs", "engine", filepath.Base(engineLog))): false,
		filepath.ToSlash(filepath.Join("logs", "gui", filepath.Base(guiLog))):       false,
		filepath.ToSlash(filepath.Join("runs", runID.Value+".zip")):                 false,
		filepath.ToSlash(filepath.Join("runs", runSummariesFile)):                   false,
		filepath.ToSlash(filepath.Join("targets", target.DefaultTargetFilename)):    false,
		filepath.ToSlash(filepath.Join("system", hostInfoFilename)):                 false,
		filepath.ToSlash(filepath.Join("system", diskUsageFilename)):                false,
		metadataFilename: false,
		versionsFilename: false,
	}

	var hostInfo map[string]any
	var versions map[string]string
	var runListing struct {
		Runs []map[string]any `json:"runs"`
	}

	for _, file := range r.File {
		for suffix := range expected {
			if strings.HasSuffix(file.Name, suffix) {
				expected[suffix] = true
			}
		}

		switch {
		case strings.HasSuffix(file.Name, filepath.ToSlash(filepath.Join("system", hostInfoFilename))):
			readZipJSON(t, file, &hostInfo)
		case strings.HasSuffix(file.Name, versionsFilename):
			readZipJSON(t, file, &versions)
		case strings.HasSuffix(file.Name, filepath.ToSlash(filepath.Join("runs", runSummariesFile))):
			readZipJSON(t, file, &runListing)
		}
	}

	for name, found := range expected {
		require.Truef(t, found, "expected entry ending with %q in archive", name)
	}

	require.NotNil(t, hostInfo)
	configAny, ok := hostInfo["config"]
	require.True(t, ok, "expected config section in host info")
	configMap, ok := configAny.(map[string]any)
	require.True(t, ok, "expected config section to be a map")
	require.Equal(t, dataDir, configMap["data-dir"])
	require.Equal(t, stateDir, configMap["state-dir"])
	require.Equal(t, configDir, configMap["config-dir"])
	require.Equal(t, "value", configMap["custom"])

	require.NotNil(t, versions)
	require.Equal(t, "1.2.3", versions["cli_version"])
	require.Equal(t, "4.5.6", versions["gui_version"])
	require.NotNil(t, runListing.Runs)
	require.Len(t, runListing.Runs, 1)
	require.Equal(t, runID.Value, runListing.Runs[0]["id"])
	require.Equal(t, "example run", runListing.Runs[0]["name"])
	require.Equal(t, "test_recipe", runListing.Runs[0]["recipe_name"])
	require.Equal(t, string(run.RecipeSuccess), runListing.Runs[0]["run_result"])
}

func TestCreateSupportPackage_CollectsLogFileConfiguredByEnvVar(t *testing.T) {
	dataDir, _, configDir, homeDir := configureSupportEnv(t)

	originalTargetPath := target.DefaultTargetFilepath
	target.DefaultTargetFilepath = filepath.Join(configDir, target.DefaultTargetFilename)
	t.Cleanup(func() { target.DefaultTargetFilepath = originalTargetPath })

	customLog := writeEngineLog(t, t.TempDir(), "apxd_custom_log_file", "custom engine log contents")
	t.Setenv(util.ApplyEnvPrefix("LOG_FILE"), customLog)
	guiLog := writeGUILog(t, homeDir, "gui log contents")

	rc, err := run.NewRunCollection(filepath.Join(dataDir, run.RunDirName))
	require.NoError(t, err)

	result, err := CreateSupportPackage(context.Background(), PackageOptions{
		OutputDir: t.TempDir(),
		LogCount:  1,
		LogFile:   os.Getenv(util.ApplyEnvPrefix("LOG_FILE")),
		GUILogDir: filepath.Dir(guiLog),
	}, nil, rc)
	require.NoError(t, err)
	require.FileExists(t, result.PackagePath)

	r, err := zip.OpenReader(result.PackagePath)
	require.NoError(t, err)
	defer r.Close()

	engineLogSuffix := filepath.ToSlash(filepath.Join("logs", "engine", filepath.Base(customLog)))
	var foundCustomLog bool
	for _, file := range r.File {
		if strings.HasSuffix(file.Name, engineLogSuffix) {
			foundCustomLog = true
			break
		}
	}

	require.True(t, foundCustomLog)
}

func TestCreateSupportPackage_DefaultsToUnknownVersions(t *testing.T) {
	dataDir, stateDir, configDir, homeDir := configureSupportEnv(t)

	originalTargetPath := target.DefaultTargetFilepath
	target.DefaultTargetFilepath = filepath.Join(configDir, target.DefaultTargetFilename)
	t.Cleanup(func() { target.DefaultTargetFilepath = originalTargetPath })

	require.NoError(t, os.MkdirAll(stateDir, perms.LocalDirPerm))
	writeEngineLog(t, stateDir, "engine.log", "engine log contents")
	guiLog := writeGUILog(t, homeDir, "gui log contents")
	guiLogDir := filepath.Dir(guiLog)

	runDir := filepath.Join(dataDir, run.RunDirName)
	rc, err := run.NewRunCollection(runDir)
	require.NoError(t, err)

	result, err := CreateSupportPackage(context.Background(), PackageOptions{
		OutputDir: t.TempDir(),
		LogCount:  1,
		GUILogDir: guiLogDir,
	}, nil, rc)
	require.NoError(t, err)
	require.FileExists(t, result.PackagePath)

	r, err := zip.OpenReader(result.PackagePath)
	require.NoError(t, err)
	defer r.Close()

	var versions map[string]string
	var runListing struct {
		Runs []map[string]any `json:"runs"`
	}
	for _, file := range r.File {
		if strings.HasSuffix(file.Name, versionsFilename) {
			readZipJSON(t, file, &versions)
		}
		if strings.HasSuffix(file.Name, filepath.ToSlash(filepath.Join("runs", runSummariesFile))) {
			readZipJSON(t, file, &runListing)
		}
	}

	require.NotNil(t, versions)
	require.Equal(t, unknownValue, versions["cli_version"])
	require.Equal(t, unknownValue, versions["gui_version"])
	require.NotNil(t, runListing.Runs)
	require.Len(t, runListing.Runs, 0)
}

func TestCreateSupportPackage_MissingRunsProduceError(t *testing.T) {
	rc, err := run.NewRunCollection(filepath.Join(t.TempDir(), run.RunDirName))
	require.NoError(t, err)

	outputDir := t.TempDir()
	result, err := CreateSupportPackage(context.Background(), PackageOptions{
		OutputDir: outputDir,
		RunIDs: []run.RunID{{
			Value: "run-123",
		}},
	}, nil, rc)
	require.Nil(t, result)
	require.Error(t, err)

	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineRunDoesNotExist, msg.Code())
	require.Equal(t, "run-123", msg.Metadata()["runID"])

	entries, readErr := os.ReadDir(outputDir)
	require.NoError(t, readErr)
	require.Len(t, entries, 0)
}

func TestCreateSupportPackage_CanceledContextProducesCancellationError(t *testing.T) {
	rc, err := run.NewRunCollection(filepath.Join(t.TempDir(), run.RunDirName))
	require.NoError(t, err)

	outputDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := CreateSupportPackage(ctx, PackageOptions{
		OutputDir: outputDir,
	}, nil, rc)
	require.Nil(t, result)
	require.Error(t, err)

	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineCommonUserCanceled, msg.Code())

	entries, readErr := os.ReadDir(outputDir)
	require.NoError(t, readErr)
	require.Len(t, entries, 0)
}

func TestCreateSupportPackage_OutputDirCreateErrorIsCataloged(t *testing.T) {
	rc, err := run.NewRunCollection(filepath.Join(t.TempDir(), run.RunDirName))
	require.NoError(t, err)

	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), perms.LocalFilePerm))

	result, err := CreateSupportPackage(context.Background(), PackageOptions{
		OutputDir: filepath.Join(blockingFile, "child"),
	}, nil, rc)
	require.Nil(t, result)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineSupportCollectFailed, msg.Code())
}

func TestCreateSupportPackage_ConfiguredEngineLogAccessErrorStopsCollection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports file/child paths as not found, which support package collection treats as missing logs")
	}

	dataDir, _, _, _ := configureSupportEnv(t)

	rc, err := run.NewRunCollection(filepath.Join(dataDir, run.RunDirName))
	require.NoError(t, err)

	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), perms.LocalFilePerm))

	result, err := CreateSupportPackage(context.Background(), PackageOptions{
		OutputDir: t.TempDir(),
		LogFile:   filepath.Join(blockingFile, "engine.log"),
	}, nil, rc)
	require.Nil(t, result)
	require.ErrorContains(t, err, "failed to access configured engine log file")
}

func TestCreateSupportPackage_MissingEngineLogsDontError(t *testing.T) {
	dataDir, stateDir, configDir, homeDir := configureSupportEnv(t)

	originalTargetPath := target.DefaultTargetFilepath
	target.DefaultTargetFilepath = filepath.Join(configDir, target.DefaultTargetFilename)
	t.Cleanup(func() { target.DefaultTargetFilepath = originalTargetPath })

	require.NoError(t, os.MkdirAll(stateDir, perms.LocalDirPerm))
	guiLog := writeGUILog(t, homeDir, "gui log contents")
	guiLogDir := filepath.Dir(guiLog)

	rc, err := run.NewRunCollection(filepath.Join(dataDir, run.RunDirName))
	require.NoError(t, err)

	result, err := CreateSupportPackage(context.Background(), PackageOptions{
		OutputDir: t.TempDir(),
		LogCount:  1,
		GUILogDir: guiLogDir,
	}, nil, rc)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.FileExists(t, result.PackagePath)

	r, err := zip.OpenReader(result.PackagePath)
	require.NoError(t, err)
	defer r.Close()

	guiLogSuffix := filepath.ToSlash(filepath.Join("logs", "gui", filepath.Base(guiLog)))
	engineLogPrefix := "/" + filepath.ToSlash(filepath.Join("logs", "engine")) + "/"

	var foundGUILog bool
	for _, file := range r.File {
		if strings.HasSuffix(file.Name, guiLogSuffix) {
			foundGUILog = true
		}
		if !file.FileInfo().IsDir() {
			require.NotContainsf(t, file.Name, engineLogPrefix, "unexpected engine log archive entry %q", file.Name)
		}
	}

	require.True(t, foundGUILog)
}

func TestCreateSupportPackage_MissingGUILogsDontError(t *testing.T) {
	dataDir, stateDir, configDir, homeDir := configureSupportEnv(t)

	originalTargetPath := target.DefaultTargetFilepath
	target.DefaultTargetFilepath = filepath.Join(configDir, target.DefaultTargetFilename)
	t.Cleanup(func() { target.DefaultTargetFilepath = originalTargetPath })

	require.NoError(t, os.MkdirAll(stateDir, perms.LocalDirPerm))
	engineLog := writeEngineLog(t, stateDir, "engine.log", "engine log contents")
	missingGUILogDir := filepath.Join(homeDir, "missing-gui-logs")

	rc, err := run.NewRunCollection(filepath.Join(dataDir, run.RunDirName))
	require.NoError(t, err)

	result, err := CreateSupportPackage(context.Background(), PackageOptions{
		OutputDir: t.TempDir(),
		LogCount:  1,
		GUILogDir: missingGUILogDir,
	}, nil, rc)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.FileExists(t, result.PackagePath)

	r, err := zip.OpenReader(result.PackagePath)
	require.NoError(t, err)
	defer r.Close()

	engineLogSuffix := filepath.ToSlash(filepath.Join("logs", "engine", filepath.Base(engineLog)))
	guiLogPrefix := "/" + filepath.ToSlash(filepath.Join("logs", "gui")) + "/"

	var foundEngineLog bool
	for _, file := range r.File {
		if strings.HasSuffix(file.Name, engineLogSuffix) {
			foundEngineLog = true
		}
		if !file.FileInfo().IsDir() {
			require.NotContainsf(t, file.Name, guiLogPrefix, "unexpected GUI log archive entry %q", file.Name)
		}
	}

	require.True(t, foundEngineLog)
}
