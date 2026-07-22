// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// -----------------------------------------------------------------------------
// Test-helpers & fakes
// -----------------------------------------------------------------------------

type fakePathUtils struct{}

func (fakePathUtils) GenerateChdirCommandLine(pwd, cmd string) string      { return "" }
func (fakePathUtils) GetPathEnvFromVenv(venv, pwd string) conductor.EnvVar { return conductor.EnvVar{} }
func (fakePathUtils) GetFullPath(dir, pwd string) string {
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(pwd, dir)
}
func (fakePathUtils) GenerateCommandLineWithEnv(cmd string, _ conductor.EnvVar) string { return cmd }
func (fakePathUtils) FormatPathForShell(p string) string                               { return p }
func (fakePathUtils) GetScriptExtension() string                                       { return "sh" }
func (fakePathUtils) IsAbs(path string) bool                                           { return filepath.IsAbs(path) }
func (fakePathUtils) GetEnvPathSep() string                                            { return ":" }
func (fakePathUtils) ToOSPath(path string) string                                      { return path }
func (fakePathUtils) GenerateRunScriptCommand(scriptFileName string, workingDir string) string {
	return scriptFileName
}
func (fakePathUtils) GetVenvBinDir() string {
	return "bin"
}

// fakeRunWriter captures run writer calls so tests can assert whether collector
// paths persisted run state without depending on the concrete run writer.

type fakeRunWriter struct {
	called        bool
	wroteManifest bool
	wantErr       bool
}

func (f *fakeRunWriter) WriteManifest(run.RunBuilder) error {
	f.called = true
	f.wroteManifest = true
	if f.wantErr {
		return errors.New("forced failure")
	}
	return nil
}

func (f *fakeRunWriter) WriteEntityDirs(run.RunBuilder) error {
	f.called = true
	if f.wantErr {
		return errors.New("forced failure")
	}
	return nil
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

func TestConfigureRunCreatorRunBuilder_AddsComponents(t *testing.T) {
	tempDir := t.TempDir()

	// Real RunCollection backed by temp dir
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)

	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	creator := &CollectionState{
		RunBuilder: builder,
		TargetInfoCollector: TargetInfoCollector{
			TargetCollectorOutput: util.Named[[]CollectorOutput]{
				Name: "target-info",
				Value: []CollectorOutput{
					{
						Filename:      "info.json",
						ComponentType: cdf.ComponentType{Name: "target-info", SchemaVersion: "1.0"},
					},
					{
						Filename:      "extra.json",
						ComponentType: cdf.ComponentType{Name: "target-info", SchemaVersion: "1.0"},
					},
				},
			},
			TargetPIDCollectorOutput: util.Named[CollectorOutput]{
				Name: "target-pid",
				Value: CollectorOutput{
					Filename:      "pid.json",
					ComponentType: cdf.ComponentType{Name: "target-pid", SchemaVersion: "1.0"},
				},
			},
		},
	}

	// Call the method under test
	require.NoError(t, creator.ConfigureCollectorRunBuilder(rc))

	// Two TargetCollector outputs + one PID output
	assert.Len(t, creator.TargetInfoCollector.TargetCollectionPath, 2)
	assert.NotEmpty(t, creator.TargetInfoCollector.TargetPIDCollectionPath)

	// Paths should be underneath the run directory
	for _, p := range creator.TargetInfoCollector.TargetCollectionPath {
		assert.True(t, strings.HasSuffix(p, filepath.Join("collector", "target-info")) ||
			strings.Contains(p, "info.json") || strings.Contains(p, "extra.json"))
	}
	assert.True(t, strings.HasSuffix(creator.TargetInfoCollector.TargetPIDCollectionPath, filepath.Join("collector", "target-pid", "pid.json")))
}

func TestQueueFileRetrieval_Success(t *testing.T) {
	tempDir := t.TempDir()
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)

	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	retriever := RetrieveAgentFilesStageRetriever{}
	collectionState := &CollectionState{RunBuilder: builder}

	platform := &conductor.TargetPlatform{Path: fakePathUtils{}}

	outputDir := "/remote/workdir"

	writer := &fakeRunWriter{}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, writer)
	col := &Collector{CollectionState: collectionState, FileRetriever: &retriever}

	relativeDest := filepath.Join("entity", "file.txt")
	err = col.QueueFileRetrieval(platform, nil, outputDir, "file.txt", relativeDest, cdf.ComponentType{Name: "data", SchemaVersion: "1.0"}, tool.TransferOptions{})
	require.NoError(t, err)
	require.True(t, writer.called)

	if assert.Len(t, retriever.FileTransfers, 1) {
		ft := retriever.FileTransfers[0]
		// Remote path should be absolute on target
		assert.Equal(t, "/remote/workdir/file.txt", filepath.ToSlash(ft.RemotePath))
		// Local path must lie within the run collection directory and end with the relative path we provided
		assert.True(t, strings.HasPrefix(ft.LocalPath, tempDir))
		assert.True(t, strings.HasSuffix(ft.LocalPath, relativeDest))
	}
}

func TestQueueFileRetrieval_LogComponentGoesToLogTransfers(t *testing.T) {
	tempDir := t.TempDir()
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)

	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	retriever := RetrieveAgentFilesStageRetriever{}
	collectionState := &CollectionState{RunBuilder: builder}

	platform := &conductor.TargetPlatform{Path: fakePathUtils{}}
	outputDir := "/remote/workdir"
	writer := &fakeRunWriter{}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, writer)
	col := &Collector{CollectionState: collectionState, FileRetriever: &retriever}

	relativeDest := filepath.Join("entity", "log.txt")

	err = col.QueueFileRetrieval(platform, nil, outputDir, "log.txt", relativeDest, cdf.ComponentType{Name: "log-text"}, tool.TransferOptions{})
	require.NoError(t, err)
	require.True(t, writer.called)

	// Desired behaviour: log component should be tracked in LogTransfers, NOT FileTransfers
	assert.Len(t, retriever.FileTransfers, 0)
	if assert.Len(t, retriever.LogTransfers, 1) {
		lt := retriever.LogTransfers[0]
		assert.Equal(t, "/remote/workdir/log.txt", filepath.ToSlash(lt.RemotePath))
		assert.True(t, strings.HasPrefix(lt.LocalPath, tempDir))
		assert.True(t, strings.HasSuffix(lt.LocalPath, relativeDest))
	}
}

func TestQueueFileRetrieval_RunWriterErrorPropagates(t *testing.T) {
	tempDir := t.TempDir()
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)

	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	retriever := RetrieveAgentFilesStageRetriever{}
	collectionState := &CollectionState{RunBuilder: builder}
	platform := &conductor.TargetPlatform{Path: fakePathUtils{}}
	outputDir := "/remote/workdir"
	writer := &fakeRunWriter{wantErr: true}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, writer)
	col := &Collector{CollectionState: collectionState, FileRetriever: &retriever}

	err = col.QueueFileRetrieval(platform, nil, outputDir, "file.txt", "entity/file.txt", cdf.ComponentType{Name: "data"}, tool.TransferOptions{})
	require.ErrorContains(t, err, "forced failure")
	assert.Empty(t, retriever.FileTransfers)
	assert.Equal(t, 0, col.CollectionState.RunBuilder.ComponentCount())
}

func TestQueueFileRetrieval_OptionsReachTransferManager(t *testing.T) {
	tempDir := t.TempDir()
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)

	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	tm := NewTransferManager(1, nil)
	close(tm.ListeningStarted)
	tm.transferRequestChannel = make(chan AddTransferMessage, 1)
	tm.listeningDone = make(chan struct{})
	collectionState := &CollectionState{RunBuilder: builder}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, &fakeRunWriter{})
	col := &Collector{
		CollectionState: collectionState,
		FileRetriever:   &TransferManagerRetriever{TransferManager: tm},
	}

	platform := &conductor.TargetPlatform{Path: fakePathUtils{}}

	options := tool.TransferOptions{
		ImmediateRetrieval: true,
		Exclude:            []string{"a/b/c"},
		BackgroundTransfer: true,
	}
	go func() {
		err = col.QueueFileRetrieval(platform, nil, "/remote/workdir", "file.txt", "entity/file.txt", cdf.ComponentType{Name: "data"}, options)
		require.NoError(t, err)
	}()

	var got AddTransferMessage
	select {
	case got = <-tm.transferRequestChannel:
		close(got.confirm)
	case <-time.After(time.Second):
		t.Fatal("background transfer request was not queued")
	}
	require.True(t, got.t.ImmediateRetrieval)
	require.True(t, got.t.BackgroundTransfer)
	require.Equal(t, options.Exclude, got.t.Exclude)
	require.Equal(t, "/remote/workdir/file.txt", filepath.ToSlash(got.t.RemotePath))
	require.Equal(t, "entity/file.txt", filepath.ToSlash(got.t.ManifestRelativePath))
}

func TestQueueFileRetrieval_TransferManagerSkipsCollectorStorage(t *testing.T) {
	tempDir := t.TempDir()
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)

	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	tm := NewTransferManager(1, nil)
	close(tm.ListeningStarted)
	tm.transferRequestChannel = make(chan AddTransferMessage, 1)
	tm.listeningDone = make(chan struct{})
	collectionState := &CollectionState{RunBuilder: builder}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, &fakeRunWriter{})
	col := &Collector{
		CollectionState: collectionState,
		FileRetriever:   &TransferManagerRetriever{TransferManager: tm},
	}

	platform := &conductor.TargetPlatform{Path: fakePathUtils{}}
	writer := &fakeRunWriter{}

	go func() {
		err = col.QueueFileRetrieval(platform, nil, "/remote/workdir", "file.txt", "entity/file.txt", cdf.ComponentType{Name: "data"}, tool.TransferOptions{})
		require.NoError(t, err)
	}()

	var got AddTransferMessage
	select {
	case got = <-tm.transferRequestChannel:
		close(got.confirm)
	case <-time.After(time.Second):
		t.Fatal("transfer request was not queued")
	}
	require.Equal(t, 0, builder.ComponentCount())
	require.False(t, writer.wroteManifest)
}

func TestStoreComponent_WritesAndReturnsPath(t *testing.T) {
	tempDir := t.TempDir()
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)

	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	writer := &fakeRunWriter{}
	collectionState := &CollectionState{RunBuilder: builder}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, writer)
	col := &Collector{CollectionState: collectionState}

	returnedPath, err := col.StoreComponent(filepath.Join("entity", "artifact.bin"), cdf.ComponentType{Name: "data"})
	require.NoError(t, err)

	// Writer should have been invoked and path returned should end with relative path
	assert.True(t, writer.called)
	assert.True(t, strings.HasSuffix(returnedPath, filepath.Join("entity", "artifact.bin")))

	// Confirm the component count in builder is exactly 1
	assert.Equal(t, 1, col.CollectionState.RunBuilder.ComponentCount())
}

func TestCreateRun_HappyPath(t *testing.T) {
	tempDir := t.TempDir()
	runDir := filepath.Join(tempDir, "runs")

	rc, err := run.NewRunCollection(runDir)
	require.NoError(t, err)

	// Create a temporary recipe file to be preserved into the run
	recipeFile := filepath.Join(tempDir, "recipe.yaml")
	require.NoError(t, os.WriteFile(recipeFile, []byte("name: test-recipe"), perms.LocalFilePerm))

	creator := &CollectionState{
		TargetInfoCollector: TargetInfoCollector{
			TargetCollectorOutput: util.Named[[]CollectorOutput]{
				Name:  "target-info",
				Value: nil, // keep small
			},
			TargetPIDCollectorOutput: util.Named[CollectorOutput]{
				Name: "target-pid",
				Value: CollectorOutput{
					Filename:      "pid.json",
					ComponentType: cdf.ComponentType{Name: "target-pid"},
				},
			},
		},
	}

	meta := RecipeMetadata{
		Name:       "myRecip",
		Version:    "1.0.0",
		APIVersion: "1.0.0",
	}

	rctx := &RecipeCtx{
		RecipePath:     recipeFile,
		Target:         &target.LocalTarget{},
		RecipeMetadata: meta,
	}

	rec := &Recipe{
		Name: meta.Name,
		Parameters: parameters.Parameters{
			Input:       []parameters.InputParameter{{Parameter: parameters.Parameter{ID: "inputParam"}}},
			MultiSelect: []parameters.MultiSelectParameter{{Parameter: parameters.Parameter{ID: "selectParam"}}},
			Checkbox:    []parameters.CheckboxParameter{{Parameter: parameters.Parameter{ID: "checkboxParam"}}},
			Radio:       []parameters.RadioParameter{{Parameter: parameters.Parameter{ID: "radioParam"}}},
		},
	}
	rctx.ParamValues, err = parameters.BindRecipeParameters(map[string]any{
		"radioParam":    "radioValue",
		"selectParam":   []string{"option1", "option2"},
		"inputParam":    "inputValue",
		"checkboxParam": true,
	}, rec.Parameters, rec.Name)
	require.NoError(t, err)

	var nilMap map[string]string

	var wls = []struct {
		name     string
		wl       tool.Workload
		expected []any
	}{
		{
			name: "launch",
			wl: &tool.WorkloadLaunch{
				RawCommand:  "abcde",
				Environment: map[string]string{"FOO": "bar", "ABC": "123"},
				WorkingDir:  "/home/myDir",
				UseShell:    true,
			},
			// Working dir should not be populated as it may be overwritten by the collect_target_info stage; run.go:RunRecipe
			// is instead responsible for ensuring this field is updated after the run completes
			expected: []any{"Launch", "abcde", map[string]string{"FOO": "bar", "ABC": "123"}, "", true, int64(-2), "", ""},
		},
		{
			name: "Android launch",
			wl: &tool.WorkloadAndroidLaunch{
				PackageName:  "com.example.app",
				ActivityName: ".MainActivity",
			},
			expected: []any{"Android Launch", "", nilMap, "", false, int64(-2), "com.example.app", ".MainActivity"},
		},
		{
			name:     "attach",
			wl:       &tool.WorkloadAttach{PID: 123},
			expected: []any{"Attach", "", nilMap, "", false, int64(123), "", ""},
		},
		{
			name:     "system-wide",
			wl:       &tool.WorkloadSystemWide{},
			expected: []any{"System Wide", "", nilMap, "", false, int64(-1), "", ""},
		},
	}

	for _, wl := range wls {
		t.Run(fmt.Sprintf("creates metadata correctly for %v", wl.name), func(t *testing.T) {
			rctx.OrigWorkload = wl.wl
			runID, release, err := creator.CreateRun(context.Background(), rc, rctx)
			require.NoError(t, err)
			require.NotNil(t, release)
			t.Cleanup(func() { _ = release() })

			assert.NotEqual(t, run.InvalidRunID, runID)

			// Extra assertions on run contents
			runPath := rc.GetRunPath(runID)
			info, err := os.Stat(runPath)
			require.NoError(t, err)
			assert.True(t, info.IsDir())

			// The preserved recipe should exist inside the run
			recipeComponentPath, err := rc.GetRecipeComponentPath(runID)
			require.NoError(t, err)
			if assert.FileExists(t, recipeComponentPath) {
				contents, _ := os.ReadFile(recipeComponentPath)
				assert.Contains(t, string(contents), "test-recipe")
			}

			// The default categorization component should exist and be listed in the manifest.
			categorizationPath := filepath.Join(runPath, run.CategorizationFilename)
			assert.FileExists(t, categorizationPath)
			categorization, err := run.ReadRunCategorization(categorizationPath)
			require.NoError(t, err)
			assert.Equal(t, run.RunCategorization{Tags: []string{}}, *categorization)

			manifest, err := util.ReadJSONFile[cdf.Manifest](filepath.Join(runPath, "manifest.json"))
			require.NoError(t, err)
			entry := manifest.Lookup(run.CategorizationFilename)
			require.NotNil(t, entry)
			assert.Equal(t, run.CategorizationCT(), entry.ComponentType)

			// The run should load without error via the collection API
			model, err := rc.LoadRun(runID)
			require.NoError(t, err)
			assert.NotNil(t, model)
			assert.Equal(t, "myRecip", model.Metadata().RecipeName)
			assert.Equal(t, map[string]any{
				"radioParam":    "radioValue",
				"selectParam":   []any{"option1", "option2"},
				"inputParam":    "inputValue",
				"checkboxParam": true,
			}, model.Metadata().Parameters)
			assert.Equal(t, wl.expected[0], model.Metadata().WorkloadType)
			assert.Equal(t, wl.expected[1], model.Metadata().Cmdline)
			assert.Equal(t, wl.expected[2], model.Metadata().Env)
			assert.Equal(t, wl.expected[3], model.Metadata().WorkingDir)
			assert.Equal(t, wl.expected[4], model.Metadata().UseShell)
			assert.Equal(t, wl.expected[5], model.Metadata().Pid)
			assert.Equal(t, wl.expected[6], model.Metadata().AndroidPackageName)
			assert.Equal(t, wl.expected[7], model.Metadata().AndroidActivityName)
		})
	}
}

func TestCreateRun_ErrorProducedIfRecipeNotFound(t *testing.T) {
	baseDir := t.TempDir()
	creator := CollectionState{}
	meta := RecipeMetadata{
		Name:       "myRecip",
		Version:    "1.0.0",
		APIVersion: "1.0.0",
	}

	recipeCtx := RecipeCtx{
		RecipePath:     filepath.Join(baseDir, "myRecip"),
		Target:         &target.LocalTarget{},
		RecipeMetadata: meta,
	}

	runCollection, err := run.NewRunCollection(baseDir)
	assert.NoError(t, err)

	_, release, err := creator.CreateRun(context.Background(), runCollection, &recipeCtx)
	require.Error(t, err)
	require.Nil(t, release)

	var msgErr message.Message
	ok := errors.As(err, &msgErr)
	assert.True(t, ok)
	assert.Equal(t, message.EngineConductorFileTransferOpenSrcFile, msgErr.Code())
	assert.Equal(t, filepath.ToSlash(recipeCtx.RecipePath), msgErr.Metadata()["srcFilePath"])
}

func TestRecipeFileCollector_AddComponent_Success(t *testing.T) {
	tempDir := t.TempDir()
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)
	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	toolDir := "tool-output"
	fileName := "artifact.bin"
	componentType := cdf.ComponentType{Name: "data"}

	writer := &fakeRunWriter{}
	collectionState := &CollectionState{RunBuilder: builder}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, writer)
	collector := &Collector{CollectionState: collectionState}

	rfc := &RecipeFileCollector{
		Collector: collector,
	}

	path, err := rfc.AddComponent(toolDir, componentType, fileName)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join(toolDir, fileName)))
	assert.True(t, writer.called)
}

func TestRecipeFileCollector_AddComponent_Error(t *testing.T) {
	tempDir := t.TempDir()
	rc, err := run.NewRunCollection(tempDir)
	require.NoError(t, err)
	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	toolDir := "tool-output"
	fileName := "artifact.bin"
	componentType := cdf.ComponentType{Name: "data"}

	writer := &fakeRunWriter{wantErr: true}
	collectionState := &CollectionState{RunBuilder: builder}
	collectionState.RunManifestUpdater = run.NewRunManifestUpdater(&collectionState.RunBuilder, writer)
	collector := &Collector{CollectionState: collectionState}

	rfc := &RecipeFileCollector{
		Collector: collector,
	}

	path, err := rfc.AddComponent(toolDir, componentType, fileName)
	require.Error(t, err)
	assert.Empty(t, path)
	assert.Equal(t, 0, collector.CollectionState.RunBuilder.ComponentCount())
}
