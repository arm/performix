// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
)

// mockRenderFS is a SessionRenderFS backed by testify.Mock.
type mockRenderFS struct {
	mock.Mock
}

type slAnalyzeTestSession struct {
	content  *render.ContentMap
	manifest *render.Manifest
	rerender render.SessionRenderFS
}

func (s *slAnalyzeTestSession) ID() string                  { return "session-sl-analyze" }
func (s *slAnalyzeTestSession) DatabaseKey() string         { return "" }
func (s *slAnalyzeTestSession) Close()                      {}
func (s *slAnalyzeTestSession) Content() *render.ContentMap { return s.content }
func (s *slAnalyzeTestSession) Manifest() *render.Manifest  { return s.manifest }
func (s *slAnalyzeTestSession) Database() *render.Database  { return nil }
func (s *slAnalyzeTestSession) WidgetDataSources() *render.WidgetDataSources {
	return nil
}
func (s *slAnalyzeTestSession) Reference() render.Hub { return nil }
func (s *slAnalyzeTestSession) Rerender() render.SessionRenderFS {
	return s.rerender
}
func (s *slAnalyzeTestSession) TargetSessions() targetsession.TargetSessionProvider {
	return nil
}

// EmitOutputForRun records calls and returns configured errors.
func (fs *mockRenderFS) EmitOutputForRun(runID run.RunID, filePath string, rendererRelPath string, meta render.OutputMetadata) error {
	args := fs.Called(runID, filePath, rendererRelPath, meta)
	return args.Error(0)
}

// EmitPendingOutputForRun records calls and returns configured errors.
func (fs *mockRenderFS) EmitPendingOutputForRun(runID run.RunID, rendererRelPath string, meta render.OutputMetadata) error {
	args := fs.Called(runID, rendererRelPath, meta)
	return args.Error(0)
}

// RemoveRenderForRun is a no-op implementation for SessionRenderFS.
func (fs *mockRenderFS) RemoveRenderForRun(runID run.RunID) error {
	args := fs.Called(runID)
	return args.Error(0)
}

// CreateTempDirForRun is a no-op implementation for SessionRenderFS.
func (fs *mockRenderFS) CreateTempDirForRun(runID run.RunID) (string, error) {
	args := fs.Called(runID)
	return args.String(0), args.Error(1)
}

// Cleanup is a no-op implementation for SessionRenderFS.
func (fs *mockRenderFS) Cleanup() error {
	args := fs.Called()
	return args.Error(0)
}

// slAnalyzeOutputSpec describes a single output emitted by SlAnalyzeRenderer.
type slAnalyzeOutputSpec struct {
	sourceRel string
	destRel   string
	meta      render.OutputMetadata
}

// slAnalyzeOutputs returns the list of output specs used by emitOutputs for a given entity root.
func slAnalyzeOutputs(entity string) []slAnalyzeOutputSpec {
	return []slAnalyzeOutputSpec{
		{
			sourceRel: "symbols.json",
			destRel:   entity + "output/symbols.json",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-symbols", Version: "1.1"},
		},
		{
			sourceRel: "sources-capture-periodic_sampling*",
			destRel:   entity + "output/sources-capture-periodic_sampling*",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-source-line-attribution", Version: "1.0"},
		},
		{
			sourceRel: "call_tree.json",
			destRel:   entity + "output/call_tree.json",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-call-tree", Version: "1.0"},
		},
		{
			sourceRel: "call_tree_samples.json",
			destRel:   entity + "output/call_tree_samples.json",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-call-tree", Version: "1.0"},
		},
		{
			sourceRel: "callpath_self_samples.json",
			destRel:   entity + "output/callpath_self_samples.json",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-metrics", Version: "1.0"},
		},
		{
			sourceRel: "callpath_self_metrics.json",
			destRel:   entity + "output/callpath_self_metrics.json",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-metrics", Version: "1.0"},
		},
		{
			sourceRel: "callpath_total_samples.json",
			destRel:   entity + "output/callpath_total_samples.json",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-metrics", Version: "1.0"},
		},
		{
			sourceRel: "callpath_total_metrics.json",
			destRel:   entity + "output/callpath_total_metrics.json",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-metrics", Version: "1.0"},
		},
		{
			sourceRel: "callpaths-capture-periodic_sampling.csv",
			destRel:   entity + "output/callpaths-capture-periodic_sampling.csv",
			meta:      render.OutputMetadata{ComponentType: "sl-collect", Version: "1.0"},
		},
		{
			sourceRel: "functions-capture-periodic_sampling.csv",
			destRel:   entity + "output/functions-capture-periodic_sampling.csv",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-flat-functions-csv", Version: "1.1"},
		},
		{
			sourceRel: "functions-capture-metrics.csv",
			destRel:   entity + "output/functions-capture-metrics.csv",
			meta:      render.OutputMetadata{ComponentType: "sl-collect-flat-functions-csv", Version: "1.1"},
		},
		{
			sourceRel: "disassembly-capture-periodic_sampling*",
			destRel:   entity + "output/disassembly-capture-periodic_sampling*",
			meta:      render.OutputMetadata{ComponentType: "disassembly_capture_samples", Version: "1.1"},
		},
	}
}

// expectSlAnalyzeOutputs registers expectations for all SlAnalyzeRenderer outputs.
func expectSlAnalyzeOutputs(t *testing.T, fs *mockRenderFS, runID run.RunID, entity string, errorsBySource map[string]error) {
	t.Helper()

	for _, output := range slAnalyzeOutputs(entity) {
		err := errorsBySource[output.sourceRel]
		fs.On("EmitOutputForRun", runID, output.sourceRel, output.destRel, output.meta).Return(err)
	}
}

func expectSlAnalyzePendingOutputs(t *testing.T, fs *mockRenderFS, runID run.RunID, entity string) {
	t.Helper()

	for _, output := range slAnalyzeOutputs(entity) {
		fs.On("EmitPendingOutputForRun", runID, output.destRel, output.meta).Return(nil)
	}
}

// expectSlAnalyzeOutputsUntil registers expectations up to and including stopAfterSource.
func expectSlAnalyzeOutputsUntil(
	t *testing.T,
	fs *mockRenderFS,
	runID run.RunID,
	entity string,
	errorsBySource map[string]error,
	stopAfterSource string,
) {
	t.Helper()

	for _, output := range slAnalyzeOutputs(entity) {
		err := errorsBySource[output.sourceRel]
		fs.On("EmitOutputForRun", runID, output.sourceRel, output.destRel, output.meta).Return(err)
		if output.sourceRel == stopAfterSource {
			break
		}
	}
}

// TestSlAnalyzeRendererConfigureAndParamsExist verifies config parsing and param detection.
func TestSlAnalyzeRendererConfigureAndParamsExist(t *testing.T) {
	t.Run("empty config does not enable rendering", func(t *testing.T) {
		renderer := &SlAnalyzeRenderer{}
		err := renderer.Configure(&render.Config{JSON: `{}`})
		require.NoError(t, err)
		assert.False(t, renderer.DoRenderParamsExist())
		assert.Equal(t, "tool/neoprof/0/", renderer.getEntity())
	})

	t.Run("filter pid enables rendering", func(t *testing.T) {
		renderer := &SlAnalyzeRenderer{}
		err := renderer.Configure(&render.Config{JSON: `{"filter_pid": 1234}`})
		require.NoError(t, err)
		assert.True(t, renderer.DoRenderParamsExist())
	})

	t.Run("grouping and entity enable rendering", func(t *testing.T) {
		renderer := &SlAnalyzeRenderer{}
		err := renderer.Configure(&render.Config{JSON: `{"grouping": ["thread"], "entity": "tool/custom/0/"}`})
		require.NoError(t, err)
		assert.True(t, renderer.DoRenderParamsExist())
		assert.Equal(t, "tool/custom/0/", renderer.getEntity())
	})
}

// TestSlAnalyzeRendererBuildArgs ensures filter and grouping flags are emitted correctly.
func TestSlAnalyzeRendererBuildArgs(t *testing.T) {
	t.Run("filter and grouping flags", func(t *testing.T) {
		renderer := &SlAnalyzeRenderer{}
		err := renderer.Configure(&render.Config{JSON: `{"filter_pid": 3620, "grouping": ["process", "thread"]}`})
		require.NoError(t, err)

		args := renderer.buildSlAnalyzeArgs("/bin/sl-analyze", "/tmp/out", "/tmp/capture.apc")
		assert.Equal(t, []string{
			"/bin/sl-analyze",
			"-o",
			"/tmp/out",
			"--collect-images",
			"--all-images",
			"--apap-export",
			"--include-empty-columns",
			"--annotate-source",
			"--disassemble",
			"--pid",
			"3620",
			"--group-by",
			"process,thread",
			"/tmp/capture.apc",
		}, args)
	})

	t.Run("time range and default grouping", func(t *testing.T) {
		renderer := &SlAnalyzeRenderer{}
		err := renderer.Configure(&render.Config{JSON: `{"filter_start_time_ns": 30000000000, "filter_end_time_ns": 60000000000}`})
		require.NoError(t, err)

		args := renderer.buildSlAnalyzeArgs("/bin/sl-analyze", "/tmp/out", "/tmp/capture.apc")
		assert.Equal(t, []string{
			"/bin/sl-analyze",
			"-o",
			"/tmp/out",
			"--collect-images",
			"--all-images",
			"--apap-export",
			"--include-empty-columns",
			"--annotate-source",
			"--disassemble",
			"--between",
			"30000000000-60000000000",
			"--group-by",
			"none",
			"/tmp/capture.apc",
		}, args)
	})
}

// TestSlAnalyzeRendererEmitOutputsSkipsOptionalGlobs ensures optional globs are ignored when missing.
func TestSlAnalyzeRendererEmitOutputsSkipsOptionalGlobs(t *testing.T) {
	renderer := &SlAnalyzeRenderer{specificConfig: &SlAnalyzeRendererConfigJSON{Entity: "tool/neoprof/0/"}}
	runID := run.RunID{Value: "run1"}
	fs := &mockRenderFS{}
	expectSlAnalyzeOutputs(t, fs, runID, "tool/neoprof/0/", map[string]error{
		"sources-capture-periodic_sampling*":     render.ErrRenderNoMatches,
		"disassembly-capture-periodic_sampling*": render.ErrRenderNoMatches,
	})

	err := renderer.emitOutputs(fs, runID, false)
	assert.NoError(t, err)
	fs.AssertExpectations(t)
}

// TestSlAnalyzeRendererEmitOutputsErrorsOnRequiredFile ensures required files still propagate errors.
func TestSlAnalyzeRendererEmitOutputsErrorsOnRequiredFile(t *testing.T) {
	renderer := &SlAnalyzeRenderer{specificConfig: &SlAnalyzeRendererConfigJSON{Entity: "tool/neoprof/0/"}}
	runID := run.RunID{Value: "run1"}
	fs := &mockRenderFS{}
	expectSlAnalyzeOutputsUntil(t, fs, runID, "tool/neoprof/0/", map[string]error{
		"call_tree_samples.json": run.ErrRenderTempFileNotFound,
	}, "call_tree_samples.json")

	err := renderer.emitOutputs(fs, runID, false)
	assert.ErrorIs(t, err, run.ErrRenderTempFileNotFound)
	fs.AssertExpectations(t)
}

func TestSlAnalyzeRendererInitializeReturnsPendingForPendingCaptureManifest(t *testing.T) {
	renderer := &SlAnalyzeRenderer{}
	err := renderer.Configure(&render.Config{JSON: `{"filter_pid": 1234}`})
	require.NoError(t, err)

	runID := run.RunID{Value: "run1"}
	model := cdf.NewOnDiskModel("/base", &cdf.Manifest{Entries: []cdf.ManifestEntry{
		{
			Path:          "tool/neoprof/0/capture.apc/**/*",
			ComponentType: cdf.ComponentType{Name: "capture_apc", SchemaVersion: "1.0"},
			Pending:       true,
		},
	}}, cdf.Metadata{})
	fs := &mockRenderFS{}
	expectSlAnalyzePendingOutputs(t, fs, runID, "tool/neoprof/0/")

	manifest := render.NewManifest()
	session := &slAnalyzeTestSession{
		content: &render.ContentMap{
			Entries: []render.ContentMapEntry{{
				ID:    runID,
				Model: model,
			}},
		},
		manifest: &manifest,
		rerender: fs,
	}

	err = renderer.Initialize(session, nil)
	require.ErrorIs(t, err, cdf.ErrComponentPending)
	fs.AssertExpectations(t)
}

func TestSlAnalyzeRendererResolveCaptureRootReturnsCaptureDirectory(t *testing.T) {
	renderer := &SlAnalyzeRenderer{}
	err := renderer.Configure(&render.Config{JSON: `{"filter_pid": 1234}`})
	require.NoError(t, err)

	model := cdf.NewOnDiskModel("/base", &cdf.Manifest{Entries: []cdf.ManifestEntry{
		{
			Path:          "tool/neoprof/0/capture.apc/**/*",
			ComponentType: cdf.ComponentType{Name: "capture_apc", SchemaVersion: "1.0"},
		},
	}}, cdf.Metadata{})

	captureDir, err := renderer.resolveCaptureRoot(model)
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/base/tool/neoprof/0/capture.apc"), captureDir)
}

func TestSlAnalyzeHostSubdirFor(t *testing.T) {
	t.Run("darwin arm64", func(t *testing.T) {
		subdir, err := slAnalyzeHostSubdirFor("darwin", "arm64")
		require.NoError(t, err)
		assert.Equal(t, "darwin-arm64", subdir)
	})

	t.Run("linux amd64", func(t *testing.T) {
		subdir, err := slAnalyzeHostSubdirFor("linux", "amd64")
		require.NoError(t, err)
		assert.Equal(t, "linux-x64", subdir)
	})

	t.Run("linux arm64", func(t *testing.T) {
		subdir, err := slAnalyzeHostSubdirFor("linux", "arm64")
		require.NoError(t, err)
		assert.Equal(t, "linux-arm64", subdir)
	})

	t.Run("windows amd64", func(t *testing.T) {
		subdir, err := slAnalyzeHostSubdirFor("windows", "amd64")
		require.NoError(t, err)
		assert.Equal(t, "windows-x64", subdir)
	})

	t.Run("windows arm64", func(t *testing.T) {
		subdir, err := slAnalyzeHostSubdirFor("windows", "arm64")
		require.NoError(t, err)
		assert.Equal(t, "windows-arm64", subdir)
	})

	t.Run("unsupported platform", func(t *testing.T) {
		_, err := slAnalyzeHostSubdirFor("plan9", "amd64")
		assert.ErrorContains(t, err, "unsupported host platform")
	})
}

func TestSlAnalyzeBinaryNameForPlatform(t *testing.T) {
	t.Run("darwin uses sl-analyze", func(t *testing.T) {
		binary, err := slAnalyzeBinaryNameForPlatform("darwin", "arm64")
		require.NoError(t, err)
		assert.Equal(t, "sl-analyze", binary)
	})

	t.Run("linux uses sl-analyze", func(t *testing.T) {
		binary, err := slAnalyzeBinaryNameForPlatform("linux", "amd64")
		require.NoError(t, err)
		assert.Equal(t, "sl-analyze", binary)
	})

	t.Run("windows uses sl-analyze.exe", func(t *testing.T) {
		binary, err := slAnalyzeBinaryNameForPlatform("windows", "amd64")
		require.NoError(t, err)
		assert.Equal(t, "sl-analyze.exe", binary)
	})

	t.Run("unsupported platform", func(t *testing.T) {
		_, err := slAnalyzeBinaryNameForPlatform("plan9", "amd64")
		assert.ErrorContains(t, err, "unsupported host platform")
	})
}

func TestResolveSlAnalyzeBinaryPathWithFS(t *testing.T) {
	t.Run("prefers prod path when present for all supported platforms", func(t *testing.T) {
		cases := []struct {
			name   string
			goos   string
			goarch string
		}{
			{name: "darwin arm64", goos: "darwin", goarch: "arm64"},
			{name: "linux amd64", goos: "linux", goarch: "amd64"},
			{name: "linux arm64", goos: "linux", goarch: "arm64"},
			{name: "windows amd64", goos: "windows", goarch: "amd64"},
			{name: "windows arm64", goos: "windows", goarch: "arm64"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fs := afero.NewMemMapFs()
				exePath := filepath.FromSlash("/opt/atperf/atperf")
				binaryName, err := slAnalyzeBinaryNameForPlatform(tc.goos, tc.goarch)
				require.NoError(t, err)
				prodPath := filepath.Join(filepath.Dir(exePath), "tools", "sl-analyze-host", binaryName)
				require.NoError(t, fs.MkdirAll(filepath.Dir(prodPath), 0o755))
				require.NoError(t, afero.WriteFile(fs, prodPath, []byte("bin"), 0o755))

				path, err := resolveSlAnalyzeBinaryPathWithFS(fs, exePath, tc.goos, tc.goarch)
				require.NoError(t, err)
				assert.Equal(t, prodPath, path)
			})
		}
	})

	t.Run("falls back to dev path when prod missing", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		exePath := filepath.FromSlash("/opt/atperf/atperf")
		devPath := filepath.FromSlash("/opt/atperf/sl-analyze-host-tools/linux-x64/sl-analyze")
		require.NoError(t, fs.MkdirAll(filepath.Dir(devPath), 0o755))
		require.NoError(t, afero.WriteFile(fs, devPath, []byte("bin"), 0o755))

		path, err := resolveSlAnalyzeBinaryPathWithFS(fs, exePath, "linux", "amd64")
		require.NoError(t, err)
		assert.Equal(t, devPath, path)
	})

	t.Run("returns not found when neither path exists", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		exePath := filepath.FromSlash("/opt/atperf/atperf")

		_, err := resolveSlAnalyzeBinaryPathWithFS(fs, exePath, "linux", "amd64")
		assert.ErrorIs(t, err, ErrSlAnalyzeBinaryNotFound)
	})
}

func TestSlAnalyzeRendererGetInputSpec(t *testing.T) {
	renderer := &SlAnalyzeRenderer{}
	spec := renderer.GetInputSpec()
	assert.Len(t, spec.Ports, 0)
}

func TestSlAnalyzeRendererGetOutputSpec(t *testing.T) {
	renderer := &SlAnalyzeRenderer{}
	spec := renderer.GetOutputSpec()
	assert.Len(t, spec.Ports, 0)
}

func TestSlAnalyzeRendererNameAndVersion(t *testing.T) {
	renderer := &SlAnalyzeRenderer{}
	assert.Equal(t, slAnalyzeRendererName, renderer.Name())
	assert.Equal(t, slAnalyzeRendererVersion, renderer.Version())
}
