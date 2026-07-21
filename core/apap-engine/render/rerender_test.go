// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

// TestSessionRerender exercises the happy-path behavior for a single run.
func TestSessionRerender(t *testing.T) {
	// Build a concrete run to attach rerender output under.
	runCollection, runID := createRerenderRun(t)
	rerenderID := "test123"
	rerenderPath := run.RenderPath(rerenderID)

	builder, err := runCollection.NewRunRenderFS(runID)
	require.NoError(t, err)

	// Build a render model with an in-memory manifest we can inspect.
	rerenderManifest := &cdf.Manifest{}
	rerenderModel := newRerenderModel(runCollection, runID, rerenderID, rerenderManifest)
	builders := map[run.RunID]*run.RunRenderFS{runID: builder}
	targets := map[run.RunID]*RenderTarget{runID: NewRenderTarget(rerenderID, rerenderModel)}

	// Construct the session-scoped rerender helper.
	rerender := NewSessionRenderFS(context.Background(), builders, targets)

	t.Run("creates temp dir for run", func(t *testing.T) {
		// Validate the temp directory path is created on disk.
		tempDir, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)
		info, err := os.Stat(tempDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())

		lockDir := filepath.Join(filepath.Dir(filepath.Dir(runCollection.GetRunPath(runID))), "locks", runID.Value, "render")
		lockPath := filepath.Join(lockDir, filepath.Base(rerenderPath))
		_, err = os.Stat(lockPath)
		assert.NoError(t, err)
	})

	t.Run("emit output moves file and updates manifest", func(t *testing.T) {
		// Arrange a file inside the temp directory.
		tempDir, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)

		tempRelPath := filepath.Join("inputs", "file.txt")
		tempAbsPath := filepath.Join(tempDir, tempRelPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(tempAbsPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(tempAbsPath, []byte("data"), perms.LocalFilePerm))

		// Emit output and verify that both the file move and manifest update occur.
		meta := OutputMetadata{ComponentType: "rerender", Version: "1.0"}
		err = rerender.EmitOutputForRun(runID, tempRelPath, filepath.Join("out", "result.txt"), meta)
		require.NoError(t, err)

		destAbsPath := filepath.Join(runCollection.GetRunPath(runID), rerenderPath, "out", "result.txt")
		info, err := os.Stat(destAbsPath)
		require.NoError(t, err)
		assert.False(t, info.IsDir())

		require.Len(t, rerenderManifest.Entries, 1)
		assert.Equal(t, "out/result.txt", rerenderManifest.Entries[0].Path)
		assert.Equal(t, meta.ComponentType, rerenderManifest.Entries[0].ComponentType.Name)
		assert.Equal(t, meta.Version, rerenderManifest.Entries[0].ComponentType.SchemaVersion)
	})

	t.Run("emit output supports glob patterns", func(t *testing.T) {
		// Arrange globbed files inside the temp directory.
		tempDir, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)

		globRelPath := filepath.Join("output", "sources*")
		files := []string{
			filepath.Join("output", "sources-foo.csv"),
			filepath.Join("output", "sources-bar.csv"),
		}
		for _, rel := range files {
			absPath := filepath.Join(tempDir, rel)
			require.NoError(t, os.MkdirAll(filepath.Dir(absPath), perms.LocalDirPerm))
			require.NoError(t, os.WriteFile(absPath, []byte("data"), perms.LocalFilePerm))
		}

		meta := OutputMetadata{ComponentType: "rerender", Version: "1.0"}
		err = rerender.EmitOutputForRun(runID, globRelPath, filepath.Join("out", "sources*"), meta)
		require.NoError(t, err)

		for _, rel := range files {
			destAbsPath := filepath.Join(runCollection.GetRunPath(runID), rerenderPath, "out", filepath.Base(rel))
			info, statErr := os.Stat(destAbsPath)
			require.NoError(t, statErr)
			assert.False(t, info.IsDir())
		}

		lastEntry := rerenderManifest.Entries[len(rerenderManifest.Entries)-1]
		assert.Equal(t, "out/sources*", lastEntry.Path)

		err = rerender.EmitOutputForRun(runID, filepath.Join("output", "missing*"), filepath.Join("out", "missing*"), meta)
		assert.ErrorIs(t, err, ErrRenderNoMatches)
	})

	t.Run("emit output supports structured wildcard overlays", func(t *testing.T) {
		tempDir, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)

		filePathPattern := filepath.Join("report-new", "apx", "timeline", "series_id=*", "bin_duration=*", "counter.parquet")
		sourceRelPath := filepath.Join("report-new", "apx", "timeline", "series_id=4", "bin_duration=10000", "counter.parquet")
		sourceAbsPath := filepath.Join(tempDir, sourceRelPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(sourceAbsPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(sourceAbsPath, []byte("data"), perms.LocalFilePerm))

		rendererRelPath := filepath.Join("render", "timeline", "series_id=*", "bin_duration=*", "counter.parquet")
		meta := OutputMetadata{ComponentType: "rerender", Version: "1.0"}
		err = rerender.EmitOutputForRun(runID, filePathPattern, rendererRelPath, meta)
		require.NoError(t, err)

		destRelPath := filepath.Join("render", "timeline", "series_id=4", "bin_duration=10000", "counter.parquet")
		destAbsPath := filepath.Join(runCollection.GetRunPath(runID), rerenderPath, destRelPath)
		info, err := os.Stat(destAbsPath)
		require.NoError(t, err)
		assert.False(t, info.IsDir())

		entry := rerenderManifest.Lookup("render/timeline/series_id=*/bin_duration=*/counter.parquet")
		require.NotNil(t, entry)
		assert.Equal(t, meta.ComponentType, entry.ComponentType.Name)
		assert.Equal(t, meta.Version, entry.ComponentType.SchemaVersion)
	})

	t.Run("emit output supports wildcard roots relative to temp dir", func(t *testing.T) {
		tempDir, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)

		filePathPattern := filepath.Join("**", "counter.parquet")
		sourceRelPath := filepath.Join("nested", "report", "counter.parquet")
		sourceAbsPath := filepath.Join(tempDir, sourceRelPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(sourceAbsPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(sourceAbsPath, []byte("data"), perms.LocalFilePerm))

		rendererRelPath := filepath.Join("render", "**", "counter.parquet")
		meta := OutputMetadata{ComponentType: "rerender", Version: "1.0"}
		err = rerender.EmitOutputForRun(runID, filePathPattern, rendererRelPath, meta)
		require.NoError(t, err)

		destRelPath := filepath.Join("render", "nested", "report", "counter.parquet")
		destAbsPath := filepath.Join(runCollection.GetRunPath(runID), rerenderPath, destRelPath)
		info, err := os.Stat(destAbsPath)
		require.NoError(t, err)
		assert.False(t, info.IsDir())

		entry := rerenderManifest.Lookup("render/**/counter.parquet")
		require.NotNil(t, entry)
		assert.Equal(t, meta.ComponentType, entry.ComponentType.Name)
		assert.Equal(t, meta.Version, entry.ComponentType.SchemaVersion)
	})

	t.Run("emit output glob errors when destination is not globbed", func(t *testing.T) {
		tempDir, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)

		sourceRelPath := filepath.Join("report-new", "apx", "timeline", "series_id=4", "bin_duration=10000", "counter.parquet")
		sourceAbsPath := filepath.Join(tempDir, sourceRelPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(sourceAbsPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(sourceAbsPath, []byte("data"), perms.LocalFilePerm))

		meta := OutputMetadata{ComponentType: "rerender", Version: "1.0"}
		err = rerender.EmitOutputForRun(
			runID,
			filepath.Join("report-new", "apx", "timeline", "series_id=*", "bin_duration=*", "counter.parquet"),
			filepath.Join("out", "result.csv"),
			meta,
		)
		expected := message.New(message.EnginePathRemapWildcardSuffixMismatch).WithMetadata(map[string]string{
			"localPath":  filepath.ToSlash(filepath.Join("out", "result.csv")),
			"remoteBase": filepath.ToSlash(filepath.Join("report-new", "apx", "timeline", "series_id=*", "bin_duration=*", "counter.parquet")),
		})
		assert.ErrorIs(t, err, expected)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		err = rerender.EmitOutputForRun(
			runID,
			filepath.Join("report-new", "apx", "timeline", "series_id=*", "bin_duration=*", "counter.parquet"),
			filepath.Join("render", "timeline", "counter-*.parquet"),
			meta,
		)
		expected = message.New(message.EnginePathRemapWildcardSuffixMismatch).WithMetadata(map[string]string{
			"localPath":  filepath.ToSlash(filepath.Join("render", "timeline", "counter-*.parquet")),
			"remoteBase": filepath.ToSlash(filepath.Join("report-new", "apx", "timeline", "series_id=*", "bin_duration=*", "counter.parquet")),
		})
		assert.ErrorIs(t, err, expected)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("emit output glob errors on unsupported metacharacters", func(t *testing.T) {
		tempDir, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)

		sourceRelPath := filepath.Join("output", "source2.csv")
		sourceAbsPath := filepath.Join(tempDir, sourceRelPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(sourceAbsPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(sourceAbsPath, []byte("data"), perms.LocalFilePerm))

		meta := OutputMetadata{ComponentType: "rerender", Version: "1.0"}
		err = rerender.EmitOutputForRun(runID, filepath.Join("output", "*.csv"), filepath.Join("out", "result?.csv*"), meta)
		assert.ErrorContains(t, err, "unsupported meta character")
	})

	t.Run("emit output returns errors for invalid inputs", func(t *testing.T) {
		t.Run("returns error when source missing", func(t *testing.T) {
			// Missing temp file should be surfaced as an error.
			meta := OutputMetadata{ComponentType: "rerender", Version: "1.0"}
			err := rerender.EmitOutputForRun(runID, "missing.txt", "out/missing.txt", meta)
			assert.ErrorContains(t, err, "render temp file not found")
		})

		t.Run("returns error when destination exists", func(t *testing.T) {
			// Pre-create destination file to trigger the collision error.
			tempDir, err := rerender.CreateTempDirForRun(runID)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(tempDir, "source.txt"), []byte("data"), perms.LocalFilePerm))

			destAbsPath := filepath.Join(runCollection.GetRunPath(runID), rerenderPath, "out", "existing.txt")
			require.NoError(t, os.MkdirAll(filepath.Dir(destAbsPath), perms.LocalDirPerm))
			require.NoError(t, os.WriteFile(destAbsPath, []byte("data"), perms.LocalFilePerm))

			meta := OutputMetadata{ComponentType: "rerender", Version: "1.0"}
			err = rerender.EmitOutputForRun(runID, "source.txt", filepath.Join("out", "existing.txt"), meta)
			assert.ErrorContains(t, err, "destination already exists")
		})
	})

	t.Run("removes rerender dir for run", func(t *testing.T) {
		// Create a rerender directory and confirm removal works.
		_, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)

		err = rerender.RemoveRenderForRun(runID)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(runCollection.GetRunPath(runID), rerenderPath))
		assert.True(t, os.IsNotExist(err))

		lockDir := filepath.Join(filepath.Dir(filepath.Dir(runCollection.GetRunPath(runID))), "locks", runID.Value, "render")
		lockPath := filepath.Join(lockDir, filepath.Base(rerenderPath))
		_, err = os.Stat(lockPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("cleanup removes rerender dir", func(t *testing.T) {
		// Cleanup should remove all render directories tied to the session.
		_, err := rerender.CreateTempDirForRun(runID)
		require.NoError(t, err)

		err = rerender.Cleanup()
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(runCollection.GetRunPath(runID), rerenderPath))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("returns error when run missing", func(t *testing.T) {
		// Requests for unknown runs should return the proper sentinel error.
		missingRun := run.RunID{Value: "missing"}
		_, err := rerender.CreateTempDirForRun(missingRun)
		assert.ErrorIs(t, err, ErrRenderFSRunNotFound)
	})
}

func TestEmitPendingOutputForRun(t *testing.T) {
	t.Run("emit pending output updates manifest without moving files", func(t *testing.T) {
		runCollection, runID := createRerenderRun(t)
		rerenderID := "pending-render"
		rerenderManifest := &cdf.Manifest{}
		rerenderModel := newRerenderModel(runCollection, runID, rerenderID, rerenderManifest)
		targets := map[run.RunID]*RenderTarget{runID: NewRenderTarget(rerenderID, rerenderModel)}
		rerender := NewSessionRenderFS(context.Background(), map[run.RunID]*run.RunRenderFS{}, targets)

		meta := OutputMetadata{ComponentType: "pending_component", Version: "1.0"}
		err := rerender.EmitPendingOutputForRun(runID, filepath.Join("out", "pending.txt"), meta)
		require.NoError(t, err)

		require.Len(t, rerenderManifest.Entries, 1)
		entry := rerenderManifest.Entries[0]
		assert.Equal(t, "out/pending.txt", entry.Path)
		assert.Equal(t, meta.ComponentType, entry.ComponentType.Name)
		assert.Equal(t, meta.Version, entry.ComponentType.SchemaVersion)
		assert.True(t, entry.Pending)

		destAbsPath := filepath.Join(runCollection.GetRunPath(runID), run.RenderPath(rerenderID), "out", "pending.txt")
		_, err = os.Stat(destAbsPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("returns error when target missing", func(t *testing.T) {
		runCollection, runID := createRerenderRun(t)
		builder, err := runCollection.NewRunRenderFS(runID)
		require.NoError(t, err)

		builders := map[run.RunID]*run.RunRenderFS{runID: builder}
		rerender := NewSessionRenderFS(context.Background(), builders, map[run.RunID]*RenderTarget{})

		meta := OutputMetadata{ComponentType: "pending_component", Version: "1.0"}
		err = rerender.EmitPendingOutputForRun(runID, filepath.Join("out", "pending.txt"), meta)
		require.ErrorIs(t, err, ErrRenderTargetNotFound)
	})
}

// TestSessionRerenderErrorPaths covers missing builder/target and cleanup error paths.
func TestSessionRerenderErrorPaths(t *testing.T) {
	// Build a concrete run to validate error paths against.
	runCollection, runID := createRerenderRun(t)

	t.Run("returns error when builder missing", func(t *testing.T) {
		// Targets exist but builder map is empty.
		rerenderModel := newRerenderModel(runCollection, runID, "missing-builder", &cdf.Manifest{})
		targets := map[run.RunID]*RenderTarget{runID: NewRenderTarget("missing-builder", rerenderModel)}
		rerender := NewSessionRenderFS(context.Background(), map[run.RunID]*run.RunRenderFS{}, targets)

		_, err := rerender.CreateTempDirForRun(runID)
		assert.ErrorIs(t, err, ErrRenderFSRunNotFound)
	})

	t.Run("returns error when target missing", func(t *testing.T) {
		// Builder exists but target map is empty.
		builder, err := runCollection.NewRunRenderFS(runID)
		require.NoError(t, err)

		builders := map[run.RunID]*run.RunRenderFS{runID: builder}
		rerender := NewSessionRenderFS(context.Background(), builders, map[run.RunID]*RenderTarget{})

		_, err = rerender.CreateTempDirForRun(runID)
		assert.ErrorIs(t, err, ErrRenderTargetNotFound)
	})

	t.Run("cleanup returns error when lock fails", func(t *testing.T) {
		// Force the builder to attempt a lock with a canceled context.
		builder, err := runCollection.NewRunRenderFS(runID)
		require.NoError(t, err)
		_, err = builder.CreateTempRenderDir(context.Background(), "cleanup-error")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		builders := map[run.RunID]*run.RunRenderFS{runID: builder}
		rerenderModel := newRerenderModel(runCollection, runID, "cleanup-error", &cdf.Manifest{})
		targets := map[run.RunID]*RenderTarget{runID: NewRenderTarget("cleanup-error", rerenderModel)}
		rerender := NewSessionRenderFS(ctx, builders, targets)

		err = rerender.Cleanup()
		assert.Error(t, err)
	})

	t.Run("create temp dir returns error when render entity locked", func(t *testing.T) {
		builder, err := runCollection.NewRunRenderFS(runID)
		require.NoError(t, err)

		renderID := "locked-render"
		unlock, locked, err := builder.TryLockRender(renderID)
		require.NoError(t, err)
		require.True(t, locked)
		defer func() { _ = unlock() }()

		rerenderModel := newRerenderModel(runCollection, runID, renderID, &cdf.Manifest{})
		builders := map[run.RunID]*run.RunRenderFS{runID: builder}
		targets := map[run.RunID]*RenderTarget{runID: NewRenderTarget(renderID, rerenderModel)}
		rerender := NewSessionRenderFS(context.Background(), builders, targets)

		_, err = rerender.CreateTempDirForRun(runID)
		assert.ErrorContains(t, err, "render entity is locked")
	})

	t.Run("remove render returns error when remove render dir fails", func(t *testing.T) {
		builder, err := runCollection.NewRunRenderFS(runID)
		require.NoError(t, err)

		unlockCalled := false
		rerenderModel := newRerenderModel(runCollection, runID, "", &cdf.Manifest{})
		session := &SessionRenderFSImpl{
			ctx:      context.Background(),
			builders: map[run.RunID]*run.RunRenderFS{runID: builder},
			targets: map[run.RunID]*RenderTarget{
				runID: NewRenderTarget("", rerenderModel),
			},
			renderLocks: map[run.RunID]func() error{
				runID: func() error {
					unlockCalled = true
					return nil
				},
			},
		}

		err = session.RemoveRenderForRun(runID)
		assert.ErrorContains(t, err, "render ID is empty")
		assert.False(t, unlockCalled)
	})

	t.Run("remove render returns error when unlock fails", func(t *testing.T) {
		builder, err := runCollection.NewRunRenderFS(runID)
		require.NoError(t, err)

		renderID := "unlock-error"
		_, err = builder.CreateTempRenderDir(context.Background(), renderID)
		require.NoError(t, err)

		unlockCalled := false
		unlockErr := errors.New("unlock failed")
		rerenderModel := newRerenderModel(runCollection, runID, renderID, &cdf.Manifest{})
		session := &SessionRenderFSImpl{
			ctx:      context.Background(),
			builders: map[run.RunID]*run.RunRenderFS{runID: builder},
			targets: map[run.RunID]*RenderTarget{
				runID: NewRenderTarget(renderID, rerenderModel),
			},
			renderLocks: map[run.RunID]func() error{
				runID: func() error {
					unlockCalled = true
					return unlockErr
				},
			},
		}

		err = session.RemoveRenderForRun(runID)
		assert.ErrorIs(t, err, unlockErr)
		assert.True(t, unlockCalled)
		_, stillTracked := session.renderLocks[runID]
		assert.True(t, stillTracked)
	})
}

// newRerenderModel builds an OnDiskModel rooted in the rerender directory.
func newRerenderModel(
	runCollection *run.RunCollection,
	runID run.RunID,
	rerenderID string,
	manifest *cdf.Manifest,
) *cdf.OnDiskModel {
	// Use the rerender directory path to keep AddEntry aligned with file placement.
	basePath := filepath.Join(runCollection.GetRunPath(runID), run.RenderPath(rerenderID))
	return cdf.NewOnDiskModel(basePath, manifest, cdf.Metadata{})
}

// createRerenderRun creates a minimal run on disk for rerender filesystem tests.
func createRerenderRun(t *testing.T) (*run.RunCollection, run.RunID) {
	t.Helper()

	// Create a run collection under a temp directory.
	runRoot := filepath.Join(t.TempDir(), "runs")
	runCollection, err := run.NewRunCollection(runRoot)
	require.NoError(t, err)

	// Use the normal run builder path to create an on-disk run.
	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)

	// Minimal metadata keeps run creation consistent with production usage.
	metadata := &cdf.Metadata{TargetConfig: target.JSONTarget{Value: &target.JSONLocalTarget{}}}
	runID, err := runCollection.CreateRun(builder, metadata)
	require.NoError(t, err)

	return runCollection, runID
}
