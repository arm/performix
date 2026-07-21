// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ARM-software/golang-utils/utils/filesystem"
	"github.com/gofrs/flock"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func NewTestRunCollection(t *testing.T, path string) *RunCollection {
	t.Helper()
	rc, err := NewRunCollection(path)
	require.NoError(t, err)
	return rc
}

func NewTestRunCollectionWithImports(t *testing.T, path string, imports importDeps) *RunCollection {
	t.Helper()
	rc, err := NewRunCollection(path)
	require.NoError(t, err)
	rc.importDeps = imports
	return rc
}

var pathLikeRunIDs = []RunID{
	{Value: "../../etc/password"},
	{Value: "C:\\Windows\\System32"},
	{Value: "."},
	{Value: "run.id"},
	{Value: ""},
	{Value: "/usr/bin/sudo"},
}

func assertRunDoesNotExistError(t *testing.T, err error, runID RunID) {
	t.Helper()

	expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID.Value})
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestRunCollectionExists(t *testing.T) {
	t.Run("run directory can be created on disk successfully if doesn't exist", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path/runCollection")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		// Ensure the directory path doesn't exist before starting
		err := os.RemoveAll(fakePath)
		assert.NoError(t, err)
		assert.NoDirExists(t, fakePath)
		err = runCollection.create()
		assert.NoError(t, err)
		assert.DirExists(t, fakePath)
	})
	t.Run("runCollection create returns error if run directory could not be created", func(t *testing.T) {
		var fakerunCollectionPath string
		switch os := runtime.GOOS; os {
		case "windows":
			{
				fakerunCollectionPath = "C:\\Windows"
			}
		case "linux":
			{
				fakerunCollectionPath = "/sys/kernel"
			}
		case "darwin":
			{
				fakerunCollectionPath = "/var/root"
			}
		default:
			t.Fatal("Failing test, operating system is not supported")
		}
		// TODO : Test is unstable due to os.Stat misbehaving on windows, disabling for now but this should be revisited
		if runtime.GOOS != "windows" {
			runCollection := RunCollection{primaryPath: fakerunCollectionPath}
			err := runCollection.create()
			assert.ErrorContains(t, err, "failed to create run directory")
		}
	})
	t.Run("run directory will not be created twice if already exists", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path/runCollection")
		defer os.RemoveAll(fakePath)
		err := filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		assert.DirExists(t, fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		err = runCollection.create()
		assert.ErrorContains(t, err, "failed to create run directory, already exists")
	})
	t.Run("runCollection reports as existing if exists on disk", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path/runCollection")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		err := filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		assert.DirExists(t, fakePath)
		assert.True(t, runCollection.exists())
	})
	t.Run("run directory reports as non-existing if does not exist on disk", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path/runCollection")
		runCollection := NewTestRunCollection(t, fakePath)
		assert.NoDirExists(t, fakePath)
		assert.False(t, runCollection.exists())
	})
	t.Run("run reports as existing if exists on disk", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path/runCollection")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		runID := RunID{"abcdef123456"}
		runPath := runCollection.GetRunPath(runID)
		err := filesystem.MkDir(runPath)
		assert.NoError(t, err)
		assert.DirExists(t, runPath)
		assert.True(t, runCollection.runExists(runID))
	})
	t.Run("run reports as non-existing if does not exist on disk", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path/runCollection")
		defer os.RemoveAll(fakePath)
		// Make the runCollection path
		runCollection := NewTestRunCollection(t, fakePath)
		err := filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		assert.DirExists(t, fakePath)
		// But not the run path
		runID := RunID{"abcdef123456"}
		assert.False(t, runCollection.runExists(runID))
	})
}

func TestRun(t *testing.T) {
	t.Run("run directory generates run IDs with valid length", func(t *testing.T) {
		runCollection := RunCollection{primaryPath: "fake/path"}
		builder, err := runCollection.RunBuilder()
		len := len(builder.runID.Value)
		assert.NoError(t, err)
		assert.Equal(t, len, runIDLength)
	})
	t.Run("Run IDs are generated containing valid chars", func(t *testing.T) {
		validChars := "abcdef0123456789"
		runCollection := RunCollection{primaryPath: "fake/path"}
		builder, err := runCollection.RunBuilder()
		runID := builder.runID.Value
		assert.NoError(t, err)
		for char := range runID {
			assert.True(t, strings.Contains(validChars, string(runID[char])))
		}
		assert.Equal(t, len(builder.runID.Value), runIDLength)
	})
	t.Run("run directory returns correct run paths", func(t *testing.T) {
		fakePath := "fake/path"
		runCollection := RunCollection{primaryPath: fakePath}
		fakeRunID := RunID{"abcdef123456"}
		runPath := runCollection.GetRunPath(fakeRunID)
		expectedRunPath := filepath.Join(fakePath, fakeRunID.Value)
		assert.Equal(t, runPath, expectedRunPath)
	})
}

func TestRunExistsTreatsPathLikeRunIDsAsMissing(t *testing.T) {
	runCollection := NewTestRunCollection(t, t.TempDir())

	for _, runID := range pathLikeRunIDs {
		assert.False(t, runCollection.runExists(runID))
	}

	assert.False(t, runCollection.runExists(RunID{Value: "run-123"}))
}

func TestRunCreate(t *testing.T) {
	t.Run("run directory creates run successfully if run does not already exist", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeBuilder, err := runCollection.RunBuilder()
		assert.NoError(t, err)
		fakeEntityName := "fakeEntity"
		fakeBuilder.AddEntity(fakeEntityName)
		fakeMetadata := &cdf.Metadata{}
		// Make the fake runCollection path for run to be created in
		err = filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		runID, err := runCollection.CreateRun(fakeBuilder, fakeMetadata)
		assert.NoError(t, err)
		assert.NotEqual(t, runID, InvalidRunID)
		assert.DirExists(t, runCollection.GetRunPath(runID))
		assert.DirExists(t, filepath.Join(runCollection.GetRunPath(runID), fakeEntityName))
		assert.True(t, runCollection.manifestExists(runID))
		assert.True(t, runCollection.metadataExists(runID))
	})
	t.Run("run directory fails to create run if run already exists", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeRunID := RunID{"abcdef123456"}
		runPath := runCollection.GetRunPath(fakeRunID)
		err := filesystem.MkDir(runPath)
		assert.NoError(t, err)
		assert.DirExists(t, runPath)
		fakeBuilder := RunBuilder{runID: fakeRunID}
		fakeMetadata := &cdf.Metadata{}
		_, err = runCollection.CreateRun(fakeBuilder, fakeMetadata)
		assert.ErrorContains(t, err, "failed to create run, already exists")
	})
	t.Run("runs are still created even if the directory doesn't exist", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		// Path looks real but don't actually make the runCollection on disk
		runCollection := NewTestRunCollection(t, fakePath)
		fakeBuilder, err := runCollection.RunBuilder()
		assert.NoError(t, err)
		fakeBuilder.AddEntity("fakeEntity")
		fakeMetadata := &cdf.Metadata{}
		runID, err := runCollection.CreateRun(fakeBuilder, fakeMetadata)
		runPath := filepath.Join(fakePath, runID.Value)
		assert.NoError(t, err)
		assert.DirExists(t, runPath)
	})
	t.Run("run creation does not materialize glob directories for pending components", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)

		componentType := cdf.ComponentType{Name: "capture_apc", SchemaVersion: "1.0"}
		builder.AddPendingComponent(componentType, "capture.apc/**/*")

		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{})
		require.NoError(t, err)

		runPath := runCollection.GetRunPath(runID)
		assert.DirExists(t, filepath.Join(runPath, "capture.apc"))
		assert.NoDirExists(t, filepath.Join(runPath, "capture.apc", "**"))
		assert.NoDirExists(t, filepath.Join(runPath, "capture.apc", "**", "*"))

		manifest, err := runCollection.readManifest(runID)
		require.NoError(t, err)
		assert.Equal(t, &cdf.ManifestEntry{
			Path:          "capture.apc/**/*",
			ComponentType: componentType,
			Pending:       true,
		}, manifest.Lookup("capture.apc/**/*"))
	})
}

func TestRunDelete(t *testing.T) {
	t.Run("runs can be deleted successfully", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeBuilder := RunBuilder{runID: RunID{"abcdef123456"}}
		fakeBuilder.AddEntity("fakeEntity")
		fakeMetadata := &cdf.Metadata{}
		// Make the fake runCollection path for run to be created in
		err := filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		runID, err := runCollection.CreateRun(fakeBuilder, fakeMetadata)
		assert.NoError(t, err)
		assert.NotEqual(t, runID, InvalidRunID)
		assert.DirExists(t, filepath.Join(runCollection.primaryPath, runID.Value))
		// Now delete the run and check it gets deleted
		err = runCollection.DeleteRun(context.Background(), runID)
		assert.NoError(t, err)
		assert.NoDirExists(t, filepath.Join(runCollection.primaryPath, runID.Value))
	})

	t.Run("non-existent runs are not deleted", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		runCollection := NewTestRunCollection(t, fakePath)
		runID := RunID{Value: "fake_run_id"}
		assert.NoDirExists(t, filepath.Join(runCollection.primaryPath, runID.Value))
		// Now try to delete the run and check it does not get deleted
		err := runCollection.DeleteRun(context.Background(), runID)
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "fake_run_id"})
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fails on a run that is locked", func(t *testing.T) {
		tmpDir := t.TempDir()
		rc := NewTestRunCollection(t, tmpDir)
		builder, err := rc.RunBuilder()
		require.NoError(t, err)

		runId, err := rc.CreateRun(builder, &cdf.Metadata{})
		require.NoError(t, err)

		// Lock the run
		unlock, err := rc.LockRun(context.Background(), runId)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(),
			200*time.Millisecond)
		defer cancel()

		err = rc.DeleteRun(ctx, runId)
		assert.ErrorContains(t, err, string(message.EngineRunBusy))

		// Unlock the run
		require.NoError(t, unlock())

		err = rc.DeleteRun(context.Background(), runId)
		assert.NoError(t, err)
	})

	t.Run("runs can be deleted from secondary paths", func(t *testing.T) {
		tmpPath := t.TempDir()
		secondaryPath := filepath.Join(tmpPath, "secondary")
		runCollection, err := NewRunCollectionWithSecondaryPaths(filepath.Join(tmpPath, "primary"), []string{secondaryPath})
		require.NoError(t, err)
		fakeBuilder1 := RunBuilder{runID: RunID{"abcdef123456"}}
		fakeBuilder1.AddEntity("fakeEntity1")
		_, err = runCollection.CreateRun(fakeBuilder1, &cdf.Metadata{})
		require.NoError(t, err)

		runCollectionB, err := NewRunCollectionWithSecondaryPaths(secondaryPath, nil)
		require.NoError(t, err)
		fakeBuilder2 := RunBuilder{runID: RunID{"123456abcdef"}}
		fakeBuilder2.AddEntity("fakeEntity1")
		runB, err := runCollectionB.CreateRun(fakeBuilder2, &cdf.Metadata{})
		require.NoError(t, err)

		err = runCollection.DeleteRun(context.Background(), runB)
		require.NoError(t, err)
		assert.NoDirExists(t, filepath.Join(secondaryPath, runB.Value))
	})
}

func createFakeRun(runID RunID, runCollection RunCollection, t *testing.T) {
	fakeBuilder1 := RunBuilder{runID: runID}
	fakeBuilder1.AddEntity("fakeEntity")
	fakeMetadata := &cdf.Metadata{}

	producedID, err := runCollection.CreateRun(fakeBuilder1, fakeMetadata)
	assert.NoError(t, err)
	assert.Equal(t, runID, producedID)
	assert.DirExists(t, filepath.Join(runCollection.primaryPath, runID.Value))
}

func TestDeleteRuns(t *testing.T) {
	fakePath := filepath.Join(os.TempDir(), "fake/path")
	defer os.RemoveAll(fakePath)
	// Make the fake runCollection path for run to be created in
	err := filesystem.MkDir(fakePath)
	assert.NoError(t, err)
	runCollection := NewTestRunCollection(t, fakePath)

	t.Run("multiple runs can be deleted successfully", func(t *testing.T) {
		runIDs := []RunID{{Value: "abc"}, {Value: "def"}, {Value: "ghi"}}
		for _, runID := range runIDs {
			createFakeRun(runID, *runCollection, t)
		}

		errs := runCollection.DeleteRuns(context.Background(), runIDs)
		assert.Equal(t, len(runIDs), len(errs))
		for i := range runIDs {
			assert.NoError(t, errs[i])
			assert.NoDirExists(t, filepath.Join(runCollection.primaryPath, runIDs[i].Value))
		}
	})
	t.Run("corresponding errors returned if any runs cannot be deleted", func(t *testing.T) {
		runIDs := []RunID{{Value: "abc"}, {Value: "def"}}
		for _, runID := range runIDs {
			createFakeRun(runID, *runCollection, t)
		}

		// Try to delete 2 runs that exist, and 2 that don't
		deleteIDs := []RunID{{Value: "abc"}, {Value: "NON_EXISTENT"}, {Value: "ALSO_FAKE"}, {Value: "def"}}
		assert.NoDirExists(t, filepath.Join(runCollection.primaryPath, "NON_EXISTENT"))
		assert.NoDirExists(t, filepath.Join(runCollection.primaryPath, "ALSO_FAKE"))
		errs := runCollection.DeleteRuns(context.Background(), deleteIDs)

		assert.Equal(t, len(deleteIDs), len(errs))
		// 1st and 4th runs should have deleted successfully
		assert.NoError(t, errs[0])
		assert.NoDirExists(t, filepath.Join(runCollection.primaryPath, "abc"))
		assert.NoError(t, errs[3])
		assert.NoDirExists(t, filepath.Join(runCollection.primaryPath, "def"))

		// 2nd and 3rd runs should have failed
		assert.True(t, errors.Is(errs[1], message.New(message.EngineRunDoesNotExist)))
		assert.True(t, errors.Is(errs[2], message.New(message.EngineRunDoesNotExist)))
	})
}

func TestDeleteAllRuns(t *testing.T) {
	fakePath := filepath.Join(os.TempDir(), "fake/path")
	defer os.RemoveAll(fakePath)
	err := filesystem.MkDir(fakePath)
	assert.NoError(t, err)
	runCollection := NewTestRunCollection(t, fakePath)

	t.Run("no runs returns empty", func(t *testing.T) {
		ids, errs, err := runCollection.DeleteAllRuns(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, ids)
		assert.Empty(t, errs)
	})

	t.Run("deletes all runs and reports results", func(t *testing.T) {
		runIDs := []RunID{{Value: "abc"}, {Value: "def"}}
		for _, runID := range runIDs {
			createFakeRun(runID, *runCollection, t)
		}

		ids, errs, err := runCollection.DeleteAllRuns(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, runIDs, ids)
		assert.Len(t, errs, len(runIDs))
		for i := range errs {
			assert.NoError(t, errs[i])
		}
		for _, runID := range runIDs {
			assert.NoDirExists(t, filepath.Join(runCollection.primaryPath, runID.Value))
		}
	})
}

var emptySSHTargetConfig = target.JSONTarget{Value: &target.JSONSSHTarget{}}

func TestRunLoad(t *testing.T) {
	t.Run("runs can be loaded successfully", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeBuilder := RunBuilder{runID: RunID{"abcdef123456"}}
		fakeBuilder.AddEntity("fakeEntity")
		fakeMetadata := &cdf.Metadata{TargetConfig: emptySSHTargetConfig}
		// Make the fake runCollection path for run to be created in
		err := filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		runID, err := runCollection.CreateRun(fakeBuilder, fakeMetadata)
		assert.NoError(t, err)
		model, err := runCollection.LoadRun(runID)
		assert.NoError(t, err)
		assert.NotNil(t, model)
	})
	t.Run("runs cannot be loaded if they don't exist", func(t *testing.T) {
		fakePath := "fake/path"
		runCollection := NewTestRunCollection(t, fakePath)
		fakeRunID := RunID{"abcdef123456"}
		model, err := runCollection.LoadRun(fakeRunID)
		assert.Nil(t, model)
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "abcdef123456"})
		assert.Equal(t, expectedErr, err)
	})

	t.Run("path-like run IDs cannot be loaded", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())

		for _, runID := range pathLikeRunIDs {
			model, err := runCollection.LoadRun(runID)
			assert.Nil(t, model)
			assertRunDoesNotExistError(t, err, runID)
		}
	})

	t.Run("cleanup removes rerender dir without lockfile", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, filepath.Join(t.TempDir(), "runs"))
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})
		require.NoError(t, err)

		rerenderID := "stale-no-lock"
		rerenderDir := filepath.Join(runCollection.GetRunPath(runID), renderDirName, rerenderID)
		require.NoError(t, os.MkdirAll(rerenderDir, perms.LocalDirPerm))

		_, err = runCollection.LoadRun(runID)
		require.NoError(t, err)

		_, err = os.Stat(rerenderDir)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("cleanup removes rerender dir with unlocked lockfile", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, filepath.Join(t.TempDir(), "runs"))
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})
		require.NoError(t, err)

		rerenderID := "stale-unlocked"
		rerenderDir := filepath.Join(runCollection.GetRunPath(runID), renderDirName, rerenderID)
		require.NoError(t, os.MkdirAll(rerenderDir, perms.LocalDirPerm))

		lockPath := filepath.Join(runCollection.getLockDir(runID), "render", rerenderID)
		require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(lockPath, []byte("lock"), perms.LocalFilePerm))

		_, err = runCollection.LoadRun(runID)
		require.NoError(t, err)

		_, err = os.Stat(rerenderDir)
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(lockPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("cleanup keeps rerender dirs when lockfile is locked", func(t *testing.T) {
		// Test that if there are multiple rerender dirs with lockfiles, only the ones that
		// are locked are kept, and the unlocked ones are removed.
		runCollection := NewTestRunCollection(t, filepath.Join(t.TempDir(), "runs"))
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})
		require.NoError(t, err)

		lockedIDs := []string{
			"locked-00",
			"locked-01",
			"locked-02",
			"locked-03",
			"locked-04",
			"locked-05",
			"locked-06",
			"locked-07",
			"locked-08",
		}
		unlockedID := "unlocked-special"

		allIDs := append([]string{}, lockedIDs...)
		allIDs = append(allIDs, unlockedID)

		for _, renderID := range allIDs {
			rerenderDir := filepath.Join(runCollection.GetRunPath(runID), renderDirName, renderID)
			require.NoError(t, os.MkdirAll(rerenderDir, perms.LocalDirPerm))
			lockPath := filepath.Join(runCollection.getLockDir(runID), "render", renderID)
			require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), perms.LocalDirPerm))
			require.NoError(t, os.WriteFile(lockPath, []byte("lock"), perms.LocalFilePerm))
		}

		heldLocks := make([]*flock.Flock, 0, len(lockedIDs))
		for _, renderID := range lockedIDs {
			lockPath := filepath.Join(runCollection.getLockDir(runID), "render", renderID)
			lock := flock.New(lockPath)
			locked, err := lock.TryLock()
			require.NoError(t, err)
			require.True(t, locked)
			heldLocks = append(heldLocks, lock)
		}
		defer func() {
			for _, lock := range heldLocks {
				_ = lock.Unlock()
			}
		}()

		// Now load the run which will trigger the cleanup of renders.
		_, err = runCollection.LoadRun(runID)
		require.NoError(t, err)

		// Check that the locked render dirs still exist.
		for _, renderID := range lockedIDs {
			rerenderDir := filepath.Join(runCollection.GetRunPath(runID), renderDirName, renderID)
			_, err = os.Stat(rerenderDir)
			assert.NoError(t, err)
		}

		// Check that the unlocked render dir has been removed.
		unlockedDir := filepath.Join(runCollection.GetRunPath(runID), renderDirName, unlockedID)
		_, err = os.Stat(unlockedDir)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("cleanup removes orphan lockfile", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, filepath.Join(t.TempDir(), "runs"))
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})
		require.NoError(t, err)

		lockPath := filepath.Join(runCollection.getLockDir(runID), "render", "orphan-lock")
		require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(lockPath, []byte("lock"), perms.LocalFilePerm))

		_, err = runCollection.LoadRun(runID)
		require.NoError(t, err)

		_, err = os.Stat(lockPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("cleanup errors are logged and load still succeeds", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, filepath.Join(t.TempDir(), "runs"))
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})
		require.NoError(t, err)

		oldLevel := log.GetLevel()
		log.SetLevel(log.WarnLevel)
		hook := logtest.NewGlobal()
		t.Cleanup(func() { log.SetLevel(oldLevel) })

		// Force CleanupStaleRenders to error by making the lock dir path a file.
		lockDir := filepath.Join(runCollection.getLockDir(runID), "render")
		require.NoError(t, os.MkdirAll(filepath.Dir(lockDir), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(lockDir, []byte("not-a-dir"), perms.LocalFilePerm))
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(lockDir)) })

		model, err := runCollection.LoadRun(runID)
		require.NoError(t, err)
		assert.NotNil(t, model)

		foundLog := false
		for _, entry := range hook.AllEntries() {
			if entry.Level == log.WarnLevel &&
				entry.Message == "failed to cleanup stale rerenders" &&
				entry.Data["runID"] == runID.Value {
				foundLog = true
				break
			}
		}
		assert.True(t, foundLog)
	})
}
func TestRunList(t *testing.T) {
	t.Run("runs can be listed successfully", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeBuilder1 := RunBuilder{runID: RunID{"abcdef123456"}}
		fakeBuilder1.AddEntity("fakeEntity1")
		fakeBuilder2 := RunBuilder{runID: RunID{"654321fedcba"}}
		fakeBuilder2.AddEntity("fakeEntity2")
		fakeMetadata := &cdf.Metadata{TargetConfig: emptySSHTargetConfig}
		// Make the fake runCollection path for runs to be created in
		err := filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		// Create two dummy runs
		fakeRunID1, err := runCollection.CreateRun(fakeBuilder1, fakeMetadata)
		assert.NoError(t, err)
		fakeRunID2, err := runCollection.CreateRun(fakeBuilder2, fakeMetadata)
		assert.NotEqual(t, fakeRunID1, fakeRunID2)
		assert.NoError(t, err)
		runs, err := runCollection.ListRuns(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, len(runs), 2)
	})
	t.Run("no runs are listed (and without error) if the run directory doesn't exist", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		runCollection := NewTestRunCollection(t, fakePath)
		defer os.RemoveAll(fakePath)
		assert.False(t, runCollection.exists())
		runs, err := runCollection.ListRuns(context.Background())
		assert.Empty(t, runs)
		assert.NoError(t, err)
	})
	t.Run("run directory is created when listing runs if the run directory doesn't exist", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		runCollection := NewTestRunCollection(t, fakePath)
		defer os.RemoveAll(fakePath)
		assert.False(t, runCollection.exists())
		_, err := runCollection.ListRuns(context.Background())
		assert.NoError(t, err)
		assert.True(t, runCollection.exists())
	})
	t.Run("runs missing metadata are ignored in run list output", func(t *testing.T) {
		invalidRun := RunID{"invalidrun"}
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeBuilder := RunBuilder{runID: invalidRun}
		fakeBuilder.AddEntity("fakeEntity")
		fakeMetadata := &cdf.Metadata{}
		// Make the fake runCollection path for runs to be created in
		err := filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		// Create a dummy run
		_, err = runCollection.CreateRun(fakeBuilder, fakeMetadata)
		assert.NoError(t, err)
		// Delete the metadata file to make the run invalid
		err = os.Remove(filepath.Join(runCollection.GetRunPath(invalidRun), metadataFileName))
		assert.NoError(t, err)
		assert.False(t, runCollection.entryLooksValid(context.Background(), invalidRun))
		runs, err := runCollection.ListRuns(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, runs)
	})
	t.Run("runs missing manifest are ignored in run list output", func(t *testing.T) {
		invalidRun := RunID{"invalidrun"}
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeBuilder := RunBuilder{runID: invalidRun}
		fakeBuilder.AddEntity("fakeEntity")
		fakeMetadata := &cdf.Metadata{}
		// Make the fake runCollection path for runs to be created in
		err := filesystem.MkDir(fakePath)
		assert.NoError(t, err)
		// Create a dummy run
		_, err = runCollection.CreateRun(fakeBuilder, fakeMetadata)
		assert.NoError(t, err)
		// Delete the manifest file to make the run invalid
		err = os.Remove(filepath.Join(runCollection.GetRunPath(invalidRun), manifestFileName))
		assert.NoError(t, err)
		assert.False(t, runCollection.entryLooksValid(context.Background(), invalidRun))
		runs, err := runCollection.ListRuns(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, runs)
	})

	t.Run("runs are listed from primary and secondary paths", func(t *testing.T) {
		tmpPath := t.TempDir()
		secondaryPath := filepath.Join(tmpPath, "secondary")
		runCollection, err := NewRunCollectionWithSecondaryPaths(filepath.Join(tmpPath, "primary"), []string{secondaryPath})
		require.NoError(t, err)
		fakeBuilder1 := RunBuilder{runID: RunID{"abcdef123456"}}
		fakeBuilder1.AddEntity("fakeEntity1")
		runA, err := runCollection.CreateRun(fakeBuilder1, &cdf.Metadata{})
		require.NoError(t, err)

		runCollectionB, err := NewRunCollectionWithSecondaryPaths(secondaryPath, nil)
		require.NoError(t, err)
		fakeBuilder2 := RunBuilder{runID: RunID{"123456abcdef"}}
		fakeBuilder2.AddEntity("fakeEntity1")
		runB, err := runCollectionB.CreateRun(fakeBuilder2, &cdf.Metadata{})
		require.NoError(t, err)

		runs, err := runCollection.ListRuns(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, len(runs), 2)
		assert.Contains(t, runs, runA)
		assert.Contains(t, runs, runB)
	})
}

func TestRunManifest(t *testing.T) {
	t.Run("manifest can be written successfully", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeRunID := RunID{"abcdef123456"}
		fakeManifest := &cdf.Manifest{}
		// Make the fake runCollection run path for manifest file to be created in
		err := filesystem.MkDir(filepath.Join(fakePath, fakeRunID.Value))
		assert.NoError(t, err)
		err = runCollection.writeManifest(fakeRunID, fakeManifest)
		assert.True(t, runCollection.manifestExists(fakeRunID))
		assert.NoError(t, err)
	})
	t.Run("manifest can be read successfully when file exists", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeRunID := RunID{"abcdef123456"}
		fakeManifest := cdf.Manifest{}
		// Make the fake runCollection run path for manifest file to be created in
		err := filesystem.MkDir(filepath.Join(fakePath, fakeRunID.Value))
		assert.NoError(t, err)
		err = runCollection.writeManifest(fakeRunID, &fakeManifest)
		assert.NoError(t, err)
		realManifest, err := runCollection.readManifest(fakeRunID)
		assert.NoError(t, err)
		assert.Equal(t, fakeManifest, realManifest)
	})
	t.Run("manifest write does not leave atomic temp file", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeRunID := RunID{"abcdef123456"}
		runPath := filepath.Join(fakePath, fakeRunID.Value)
		err := filesystem.MkDir(runPath)
		assert.NoError(t, err)

		err = runCollection.writeManifest(fakeRunID, &cdf.Manifest{})
		assert.NoError(t, err)

		entries, err := os.ReadDir(runPath)
		require.NoError(t, err)
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		assert.ElementsMatch(t, []string{manifestFileName}, names)
	})
	t.Run("RemovePendingManifestEntries removes pending manifest entries and preserves complete entries", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		completeType := cdf.ComponentType{Name: "complete", SchemaVersion: "1.0"}
		pendingType := cdf.ComponentType{Name: "pending", SchemaVersion: "1.0"}
		builder.AddComponent(completeType, "entity/complete.txt")
		builder.AddPendingComponent(pendingType, "entity/pending.txt")
		runID, err := runCollection.CreateRun(builder, &cdf.Metadata{})
		require.NoError(t, err)

		removed, err := runCollection.RemovePendingManifestEntries(runID)
		require.NoError(t, err)
		assert.True(t, removed)

		manifest, err := runCollection.readManifest(runID)
		require.NoError(t, err)
		assert.Equal(t, &cdf.ManifestEntry{Path: "entity/complete.txt", ComponentType: completeType}, manifest.Lookup("entity/complete.txt"))
		assert.Nil(t, manifest.Lookup("entity/pending.txt"))
	})
	t.Run("manifest cannot be read when file does not exist", func(t *testing.T) {
		fakePath := "fake/path"
		runCollection := RunCollection{primaryPath: fakePath}
		fakeRunID := RunID{"abcdef123456"}
		manifest, err := runCollection.readManifest(fakeRunID)
		assert.Equal(t, cdf.Manifest{}, manifest)

		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineRunReadManifest, msg.Code())
		assert.Equal(t, "abcdef123456", msg.Metadata()["runID"])
		expectedPath := filepath.FromSlash("fake/path/abcdef123456/manifest.json")
		assert.Equal(t, expectedPath, msg.Metadata()["path"])
	})
}

func TestRunMetadata(t *testing.T) {
	t.Run("metadata can be written successfully", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeRunID := RunID{"abcdef123456"}
		fakeMetadata := cdf.Metadata{Name: "fakeName"}
		// Make the fake runCollection run path for metadata file to be created in
		err := filesystem.MkDir(filepath.Join(fakePath, fakeRunID.Value))
		assert.NoError(t, err)
		err = runCollection.writeMetadata(fakeRunID, &fakeMetadata)
		assert.True(t, runCollection.metadataExists(fakeRunID))
		assert.NoError(t, err)
	})
	t.Run("metadata can be read successfully when file exists", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeRunID := RunID{"abcdef123456"}
		fakeMetadata := cdf.Metadata{Name: "fakeName", TargetConfig: emptySSHTargetConfig}
		// Make the fake runCollection run path for metadata file to be created in
		err := filesystem.MkDir(filepath.Join(fakePath, fakeRunID.Value))
		assert.NoError(t, err)
		err = runCollection.writeMetadata(fakeRunID, &fakeMetadata)
		assert.NoError(t, err)
		realMetadata, err := runCollection.readMetadata(fakeRunID)
		assert.NoError(t, err)
		assert.Equal(t, fakeMetadata, realMetadata)
	})
	t.Run("metadata cannot be read when file does not exist", func(t *testing.T) {
		fakePath := "fake/path"
		runCollection := RunCollection{primaryPath: fakePath}
		fakeRunID := RunID{"abcdef123456"}
		metadata, err := runCollection.readMetadata(fakeRunID)
		assert.Equal(t, cdf.Metadata{}, metadata)

		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineRunReadMetadata, msg.Code())
		assert.Equal(t, "abcdef123456", msg.Metadata()["runID"])
		expectedPath := filepath.FromSlash("fake/path/abcdef123456/metadata.json")
		assert.Equal(t, expectedPath, msg.Metadata()["path"])
	})
	t.Run("fields can be updated successfully in metadata", func(t *testing.T) {
		fakePath := filepath.Join(os.TempDir(), "fake/path")
		defer os.RemoveAll(fakePath)
		runCollection := NewTestRunCollection(t, fakePath)
		fakeRunID := RunID{"abcdef123456"}
		// Make the fake runCollection run path for metadata file to be created in
		err := filesystem.MkDir(filepath.Join(fakePath, fakeRunID.Value))
		assert.NoError(t, err)

		// Write metadata with old time
		oldMetadata := cdf.Metadata{Name: "oldName", TargetConfig: emptySSHTargetConfig}
		_ = runCollection.writeMetadata(fakeRunID, &oldMetadata)
		assert.True(t, runCollection.metadataExists(fakeRunID))
		assert.NoError(t, err)
		// Read it back to check it looks right
		buffer, err := runCollection.readMetadata(fakeRunID)
		assert.NoError(t, err)
		assert.Equal(t, buffer, oldMetadata)

		// Write metadata with old time
		newMetadata := cdf.Metadata{Name: "newName", TargetConfig: emptySSHTargetConfig}
		_ = runCollection.writeMetadata(fakeRunID, &newMetadata)
		assert.True(t, runCollection.metadataExists(fakeRunID))
		assert.NoError(t, err)
		// Read it back to check it was applied correctly
		buffer, err = runCollection.readMetadata(fakeRunID)
		assert.NoError(t, err)
		assert.Equal(t, buffer, newMetadata)
	})
}

func TestRunCollectionCategorization(t *testing.T) {
	t.Run("missing categorization returns empty categorization", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := RunID{"abcdef123456"}
		require.NoError(t, os.MkdirAll(runCollection.GetRunPath(runID), perms.LocalDirPerm))

		categorization, err := runCollection.readCategorization(runID)

		require.NoError(t, err)
		assert.Equal(t, RunCategorization{Tags: []string{}}, categorization)
	})

	t.Run("invalid categorization returns cataloged error", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := RunID{"abcdef123456"}
		require.NoError(t, os.MkdirAll(runCollection.GetRunPath(runID), perms.LocalDirPerm))
		require.NoError(t, os.WriteFile(runCollection.getCategorizationPath(runID), []byte("{"), perms.LocalFilePerm))

		categorization, err := runCollection.readCategorization(runID)

		assert.Equal(t, RunCategorization{}, categorization)
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineRunReadCategorization, msg.Code())
		assert.Equal(t, "abcdef123456", msg.Metadata()["runID"])
		assert.Equal(t, runCollection.getCategorizationPath(runID), msg.Metadata()["path"])
	})
}

func TestRunDescription(t *testing.T) {
	t.Run("returns empty categorization fields when categorization file is missing", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)

		metadata := &cdf.Metadata{
			Name:         "legacy run",
			TargetConfig: emptySSHTargetConfig,
		}
		runID, err := runCollection.CreateRun(builder, metadata)
		require.NoError(t, err)

		desc, err := runCollection.RunDescription(context.Background(), runID)

		require.NoError(t, err)
		assert.Empty(t, desc.Group)
		assert.Equal(t, []string{}, desc.Tags)
	})

	t.Run("returns categorization fields", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)

		metadata := &cdf.Metadata{
			Name:         "categorized run",
			TargetConfig: emptySSHTargetConfig,
		}
		runID, err := runCollection.CreateRun(builder, metadata)
		require.NoError(t, err)
		require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
			Group: "compiler",
			Tags:  []string{"nightly", "baseline"},
		}))

		desc, err := runCollection.RunDescription(context.Background(), runID)

		require.NoError(t, err)
		assert.Equal(t, "compiler", desc.Group)
		assert.Equal(t, []string{"nightly", "baseline"}, desc.Tags)
	})

	t.Run("returns error when manifest cannot be read", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := RunID{Value: "abcd1234"}
		err := os.MkdirAll(runCollection.GetRunPath(runID), perms.LocalDirPerm)
		require.NoError(t, err)

		metadata := &cdf.Metadata{}
		require.NoError(t, runCollection.writeMetadata(runID, metadata))

		desc, err := runCollection.RunDescription(context.Background(), runID)
		require.Error(t, err)
		require.NotNil(t, desc)
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		require.Equal(t, message.EngineRunReadManifest, msg.Code())
	})
}

func TestRunDescriptionsForExport(t *testing.T) {
	runCollection := NewTestRunCollection(t, t.TempDir())
	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)

	metadata := &cdf.Metadata{
		Name:         "categorized run",
		TargetConfig: emptySSHTargetConfig,
	}
	runID, err := runCollection.CreateRun(builder, metadata)
	require.NoError(t, err)
	require.NoError(t, WriteRunCategorization(runCollection.getCategorizationPath(runID), &RunCategorization{
		Group: "compiler",
		Tags:  []string{"nightly", "baseline"},
	}))

	summaries, err := runCollection.RunDescriptionsForExport(context.Background())

	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, "compiler", summaries[0]["group"])
	assert.Equal(t, []string{"nightly", "baseline"}, summaries[0]["tags"])
}

func TestRunExport(t *testing.T) {
	testDir := t.TempDir()
	fakerunCollectionPath := filepath.Join(testDir, "fake/path/runCollection")
	runCollection := NewTestRunCollection(t, fakerunCollectionPath)
	err := runCollection.create()
	assert.NoError(t, err)
	eb, err := runCollection.RunBuilder()
	assert.NoError(t, err)
	eb.AddEntity("fakeEntity")
	err = os.MkdirAll(fakerunCollectionPath, os.ModePerm)
	assert.NoError(t, err)
	runID, err := runCollection.CreateRun(eb, &cdf.Metadata{Name: "Testing!"})
	assert.NoError(t, err)

	t.Run("run can be exported to valid directory", func(t *testing.T) {
		err := runCollection.ExportRun(context.Background(), runID, filepath.Join(testDir, "export/path"))
		assert.NoError(t, err)
		expectedZipFile := filepath.Join(testDir, "export/path", fmt.Sprintf("%s.zip", runID.Value))
		assert.FileExists(t, expectedZipFile)

		info, err := os.Stat(expectedZipFile)
		assert.NoError(t, err)
		assert.True(t, info.Size() > 0)
	})

	t.Run("run export fails with invalid runID", func(t *testing.T) {
		err := runCollection.ExportRun(context.Background(), RunID{Value: "111111111"}, fakerunCollectionPath)
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "111111111"})
		assert.Equal(t, expectedErr, err)
	})

	t.Run("run export fails writing to a dangerous path", func(t *testing.T) {
		switch os := runtime.GOOS; os {
		case "windows":
			{
				// TODO - Disabling test for now, seems as though GitHub runner actually allows writing to C:\Windows and so this test behaves differently

				// dangerousPath := "C:\\Windows"
				// err := runCollection.ExportRun(runID, dangerousPath)
				// assert.ErrorContains(t, err, "Access is denied")
			}
		case "linux":
			{
				dangerousPath := "/sys/kernel"
				err := runCollection.ExportRun(context.Background(), runID, dangerousPath)
				var msg message.Message
				assert.True(t, errors.As(err, &msg))
				assert.Equal(t, message.EngineRunCreateZipFile, msg.Code())
				assert.Equal(t, filepath.Join(dangerousPath, runID.Value+".zip"), msg.Metadata()["dstPath"])
				assert.Contains(t, msg.Unwrap().Error(), "permission denied")
			}
		case "darwin":
			{
				dangerousPath := "/var/root"
				err := runCollection.ExportRun(context.Background(), runID, dangerousPath)
				var msg message.Message
				assert.True(t, errors.As(err, &msg))
				assert.Equal(t, message.EngineRunCreateZipFile, msg.Code())
				assert.Equal(t, filepath.Join(dangerousPath, runID.Value+".zip"), msg.Metadata()["dstPath"])
				assert.Contains(t, msg.Unwrap().Error(), "permission denied")
			}
		default:
			t.Fatal("Failing test, operating system is not supported")
		}
	})

	t.Run("check exported zip has normalised path", func(t *testing.T) {
		err := runCollection.ExportRun(context.Background(), runID, filepath.Join(testDir, "export/path/two"))
		assert.NoError(t, err)

		expectedZipFile := filepath.Join(testDir, "export/path/two", fmt.Sprintf("%s.zip", runID.Value))
		assert.FileExists(t, expectedZipFile)

		// Check zip headers
		archive, err := zip.OpenReader(expectedZipFile)
		assert.NoError(t, err)
		defer archive.Close()

		for _, file := range archive.File {
			if strings.Contains(file.Name, `\`) {
				assert.Fail(t, fmt.Sprintf("file path %s is not correctly normalised", file.Name))
			}
		}
	})

	t.Run("run export preserves file modified time", func(t *testing.T) {
		expectedModTime := time.Date(2002, time.February, 3, 23, 0, 0, 0, time.UTC)
		manifestPath := filepath.Join(runCollection.GetRunPath(runID), manifestFileName)
		require.NoError(t, os.Chtimes(manifestPath, expectedModTime, expectedModTime))

		exportDir := filepath.Join(testDir, "export/path/file-modtime")
		err := runCollection.ExportRun(context.Background(), runID, exportDir)
		require.NoError(t, err)

		zipPath := filepath.Join(exportDir, fmt.Sprintf("%s.zip", runID.Value))
		archive, err := zip.OpenReader(zipPath)
		require.NoError(t, err)
		defer archive.Close()

		manifestZipPath := filepath.ToSlash(filepath.Join(runID.Value, manifestFileName))
		for _, file := range archive.File {
			if file.Name == manifestZipPath {
				assert.True(t, file.Modified.Equal(expectedModTime), "got %s, want %s", file.Modified, expectedModTime)
				return
			}
		}

		assert.Fail(t, fmt.Sprintf("manifest file %q not found in export", manifestZipPath))
	})

	t.Run("run export preserves directory modified time", func(t *testing.T) {
		expectedModTime := time.Date(2005, time.June, 7, 12, 0, 0, 0, time.UTC)
		runPath := runCollection.GetRunPath(runID)
		require.NoError(t, os.Chtimes(runPath, expectedModTime, expectedModTime))

		exportDir := filepath.Join(testDir, "export/path/dir-modtime")
		err := runCollection.ExportRun(context.Background(), runID, exportDir)
		require.NoError(t, err)

		zipPath := filepath.Join(exportDir, fmt.Sprintf("%s.zip", runID.Value))
		archive, err := zip.OpenReader(zipPath)
		require.NoError(t, err)
		defer archive.Close()

		runZipPath := filepath.ToSlash(runID.Value) + "/"
		for _, file := range archive.File {
			if file.Name == runZipPath {
				assert.True(t, file.Modified.Equal(expectedModTime), "got %s, want %s", file.Modified, expectedModTime)
				assert.True(t, file.FileInfo().IsDir(), "expected %s to be a directory", file.Name)
				return
			}
		}

		assert.Fail(t, fmt.Sprintf("directory %q not found in export", runZipPath))
	})

	t.Run("run export excludes rerender output", func(t *testing.T) {
		rerenderDir := filepath.Join(runCollection.GetRunPath(runID), renderDirName, "abc123")
		err := os.MkdirAll(rerenderDir, perms.LocalDirPerm)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(rerenderDir, "data.json"), []byte(`{"ok":true}`), perms.LocalFilePerm)
		assert.NoError(t, err)

		exportDir := filepath.Join(testDir, "export/path/skip-rerender")
		err = runCollection.ExportRun(context.Background(), runID, exportDir)
		assert.NoError(t, err)

		zipPath := filepath.Join(exportDir, fmt.Sprintf("%s.zip", runID.Value))
		archive, err := zip.OpenReader(zipPath)
		assert.NoError(t, err)
		defer archive.Close()

		for _, file := range archive.File {
			assert.NotContains(t, file.Name, "/"+renderDirName+"/")
		}
	})

	t.Run("run export fails on locked run", func(t *testing.T) {
		unlock, err := runCollection.LockRun(context.Background(), runID)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(),
			10*time.Millisecond)
		defer cancel()

		err = runCollection.ExportRun(ctx, runID, filepath.Join(testDir, "/foo/bar"))
		expectedErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID.Value})
		assert.Equal(t, expectedErr, err)

		require.NoError(t, unlock())
	})
}

func TestRunImport(t *testing.T) {
	// Setup directories & empty runCollection
	testDir := t.TempDir()
	fakeRunCollectionPath := filepath.Join(testDir, "fake/runCollection/path")
	fakePlaygroundPath := filepath.Join(testDir, "playground")
	runCollection := NewTestRunCollection(t, fakeRunCollectionPath)
	err := runCollection.create()
	assert.NoError(t, err)

	// Add run to runCollection
	rb, err := runCollection.RunBuilder()
	assert.NoError(t, err)
	rb.AddEntity(filepath.Join("myFirstEntity", "sub_dir", "sub_sub_dir"))
	rb.AddEntity("mySecondEntity")
	rb.AddComponent(cdf.ComponentType{Name: "component_1", SchemaVersion: "1.0"}, filepath.Join("myFirstEntity", "component_1.txt"))
	rb.AddComponent(cdf.ComponentType{Name: "component_2", SchemaVersion: "1.0"}, filepath.Join("mySecondEntity", "component_2.txt"))
	err = os.MkdirAll(fakeRunCollectionPath, os.ModePerm)
	assert.NoError(t, err)
	validRunID, err := runCollection.CreateRun(rb, &cdf.Metadata{Name: "Testing!"})
	assert.NoError(t, err)

	// Export run to a directory
	validZipPath := filepath.Join(fakePlaygroundPath, fmt.Sprintf("%s.zip", validRunID.Value))
	err = runCollection.ExportRun(context.Background(), validRunID, fakePlaygroundPath)
	assert.NoError(t, err)
	assert.FileExists(t, validZipPath)

	// Remove run from runCollection
	err = runCollection.DeleteRun(context.Background(), validRunID)
	assert.NoError(t, err)

	t.Run("test valid zip file is imported into empty run with original ID", func(t *testing.T) {
		assert.False(t, runCollection.runExists(validRunID))
		importedID, err := runCollection.ImportRun(validZipPath)
		assert.NoError(t, err)
		assert.Equal(t, validRunID.Value, importedID.Value)
		assert.True(t, runCollection.runExists(validRunID))

		// Check contents of run have been imported correctly
		assert.DirExists(t, filepath.Join(runCollection.GetRunPath(importedID), "myFirstEntity", "sub_dir", "sub_sub_dir"))
		assert.DirExists(t, filepath.Join(runCollection.GetRunPath(importedID), "mySecondEntity"))

		// Cleanup
		err = runCollection.DeleteRun(context.Background(), importedID)
		assert.NoError(t, err)
	})

	t.Run("test importing the same run twice have different IDs", func(t *testing.T) {
		assert.False(t, runCollection.runExists(validRunID))
		firstRun, err := runCollection.ImportRun(validZipPath)
		assert.NoError(t, err)
		secondRun, err := runCollection.ImportRun(validZipPath)
		assert.NoError(t, err)

		assert.Equal(t, firstRun.Value, validRunID.Value)
		assert.NotEqual(t, firstRun.Value, secondRun.Value)

		// Cleanup
		err = runCollection.DeleteRun(context.Background(), firstRun)
		assert.NoError(t, err)
		err = runCollection.DeleteRun(context.Background(), secondRun)
		assert.NoError(t, err)
	})

	t.Run("import uses temp dir under runs directory", func(t *testing.T) {
		rc := NewTestRunCollectionWithImports(t,
			fakeRunCollectionPath,
			importDeps{
				getRunIDFromZip: func(zipPath string) (string, error) {
					return validRunID.Value, nil
				},
				mkdirTemp: func(dir, pattern string) (string, error) {
					// Verify run is imported under primary runs path
					assert.Equal(t, dir, fakeRunCollectionPath)
					tempDir, err := os.MkdirTemp(dir, pattern)
					if err != nil {
						return "", err
					}
					return tempDir, nil
				},
				unzip: func(zipPath, dstDir string) error {
					return os.MkdirAll(filepath.Join(dstDir, validRunID.Value), perms.LocalDirPerm)
				},
				rename: func(oldPath, newPath string) error {
					return nil
				},
			},
		)

		importedID, err := rc.ImportRun(validZipPath)
		require.NoError(t, err)
		assert.Equal(t, validRunID.Value, importedID.Value)
	})

	t.Run("test importing a non-existent file fails", func(t *testing.T) {
		filePath := filepath.Join(testDir, "not_real")
		_, err := runCollection.ImportRun(filePath)
		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineRunZipFileNotExist, msg.Code())
		assert.Equal(t, filePath, msg.Metadata()["zipPath"])
	})

	t.Run("test importing a non-zip file fails", func(t *testing.T) {
		badFilePath := filepath.Join(fakePlaygroundPath, "test_file.txt")
		file, err := os.Create(badFilePath)
		assert.NoError(t, err)
		defer file.Close()

		_, err = runCollection.ImportRun(badFilePath)
		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineRunOpenZipFile, msg.Code())
		assert.Equal(t, badFilePath, msg.Metadata()["zipPath"])
	})

	t.Run("importing a zip archive that's missing manifest fails", func(t *testing.T) {
		rb, err = runCollection.RunBuilder()
		assert.NoError(t, err)
		noManifestID, err := runCollection.CreateRun(rb, &cdf.Metadata{Name: "Testing!"})
		assert.NoError(t, err)

		// Remove manifest
		manifestLocation := filepath.Join(fakeRunCollectionPath, noManifestID.Value, manifestFileName)
		assert.NoError(t, os.Remove(manifestLocation))

		// Export run to a directory
		noManifestZipPath := filepath.Join(fakePlaygroundPath, fmt.Sprintf("%s.zip", noManifestID.Value))
		err = runCollection.ExportRun(context.Background(), noManifestID, fakePlaygroundPath)
		assert.NoError(t, err)
		assert.FileExists(t, noManifestZipPath)

		_, err = runCollection.ImportRun(noManifestZipPath)

		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineRunZipFileInvalid, msg.Code())

		expectedMetadata := map[string]string{
			"zipPath": noManifestZipPath,
		}
		assert.Equal(t, expectedMetadata, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("zip directory with canceled context leaves no archive", func(t *testing.T) {
		archiveLocation := filepath.Join(fakeRunCollectionPath, "cancelled")
		assert.NoError(t, os.Mkdir(archiveLocation, perms.LocalDirPerm))
		assert.NoError(t, os.WriteFile(filepath.Join(archiveLocation, "manifest.json"), []byte("{}"), perms.LocalFilePerm))

		cancelledZipPath := filepath.Join(fakePlaygroundPath, "cancelled.zip")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := zipDirectory(ctx, archiveLocation, cancelledZipPath)
		assert.ErrorIs(t, err, context.Canceled)
		_, statErr := os.Stat(cancelledZipPath)
		assert.True(t, errors.Is(statErr, os.ErrNotExist))
	})

	t.Run("importing a zip archive with multiple top-level directories fails", func(t *testing.T) {
		runID := "abcde12345"
		archiveLocation := filepath.Join(fakeRunCollectionPath, runID)
		innerDir1 := filepath.Join(archiveLocation, "innerDir1")
		innerDir2 := filepath.Join(archiveLocation, "innerDir2")

		assert.NoError(t, os.Mkdir(archiveLocation, perms.LocalDirPerm))
		assert.NoError(t, os.Mkdir(innerDir1, perms.LocalDirPerm))
		assert.NoError(t, os.Mkdir(innerDir2, perms.LocalDirPerm))

		multipleDirsZipPath := filepath.Join(fakePlaygroundPath, fmt.Sprintf("%s.zip", runID))
		assert.NoError(t, zipDirectory(context.Background(), archiveLocation, multipleDirsZipPath))

		_, err = runCollection.ImportRun(multipleDirsZipPath)

		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineRunZipFileInvalid, msg.Code())

		expectedMetadata := map[string]string{
			"zipPath": multipleDirsZipPath,
		}
		assert.Equal(t, expectedMetadata, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Low disk space error caused by ENOSPC (Unix) or syscall.Errno(0x70) (Windows) from os.MkdirTemp", func(t *testing.T) {
		// Create an isolated RunCollection instance (same path), overriding only mkdirTemp.
		// This avoids mutating the shared runCollection variable used by other subtests.
		var expectedErrno error
		switch runtime.GOOS {
		case "windows":
			expectedErrno = syscall.Errno(0x70)
		case "linux", "darwin":
			expectedErrno = syscall.ENOSPC
		default:
			t.Skipf("unsupported OS: %s", runtime.GOOS)
		}

		rc := RunCollection{
			primaryPath: fakeRunCollectionPath,
			importDeps: importDeps{
				mkdirTemp: func(dir, pattern string) (string, error) {
					// Windows "disk full" errno checked by util.IsLowDiskSpace
					return "", &os.PathError{
						Op:   "mkdirtemp",
						Path: dir,
						Err:  expectedErrno,
					}
				},
			},
		}

		gotID, err := rc.ImportRun(validZipPath)
		assert.Equal(t, RunID{}, gotID)

		var msg message.Message
		require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
		assert.Equal(t, message.EngineRunLowDiskSpaceTempDir, msg.Code())

		// Ensure the underlying errno is still discoverable through wrapping.
		assert.True(t, errors.Is(err, expectedErrno))
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Low disk space error caused by ENOSPC (Unix) or syscall.Errno(0x70) (Windows) from unzip", func(t *testing.T) {
		// Create an isolated RunCollection instance (same path), overriding only unzip.
		// This avoids mutating the shared runCollection variable used by other subtests.
		var expectedErrno error
		switch runtime.GOOS {
		case "windows":
			expectedErrno = syscall.Errno(0x70)
		case "linux", "darwin":
			expectedErrno = syscall.ENOSPC
		default:
			t.Skipf("unsupported OS: %s", runtime.GOOS)
		}

		rc := NewTestRunCollectionWithImports(t,
			fakeRunCollectionPath,
			importDeps{
				unzip: func(zipPath, dstDir string) error {
					// Windows "disk full" errno checked by util.IsLowDiskSpace
					return &os.PathError{
						Op:   "unzip",
						Path: dstDir,
						Err:  expectedErrno,
					}
				},
			},
		)

		gotID, err := rc.ImportRun(validZipPath)
		assert.Equal(t, RunID{}, gotID)

		var msg message.Message
		require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
		assert.Equal(t, message.EngineRunLowDiskSpaceTempDir, msg.Code())

		// Ensure the underlying errno is still discoverable through wrapping.
		assert.True(t, errors.Is(err, expectedErrno))
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("Low disk space error caused by EDQUOT from unzip (Unix only)", func(t *testing.T) {
		// Create an isolated RunCollection instance (same path), overriding only unzip.
		// This avoids mutating the shared runCollection variable used by other subtests.
		switch goos := runtime.GOOS; goos {
		case "linux", "darwin":
			{
				rc := NewTestRunCollectionWithImports(t,
					fakeRunCollectionPath,
					importDeps{
						unzip: func(zipPath, dstDir string) error {
							return &os.PathError{
								Op:   "unzip",
								Path: dstDir,
								Err:  syscall.EDQUOT,
							}
						},
					},
				)

				gotID, err := rc.ImportRun(validZipPath)
				assert.Equal(t, RunID{}, gotID)

				var msg message.Message
				require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
				assert.Equal(t, message.EngineRunLowDiskSpaceTempDir, msg.Code())

				assert.True(t, errors.Is(err, syscall.EDQUOT))
				assert.NoError(t, message.ValidateMetadataPlaceholders(err))
			}
		case "windows":
			t.Skipf("Skipped on Windows systems as EDQUOT is not used on Windows")
		default:
			t.Skipf("unsupported OS: %s", runtime.GOOS)
		}
	})

	t.Run("Low disk space error caused by syscall.Errno(0x27) from unzip (Windows only)", func(t *testing.T) {
		// Create an isolated RunCollection instance (same path), overriding only unzip.
		// This avoids mutating the shared runCollection variable used by other subtests.
		switch goos := runtime.GOOS; goos {
		case "windows":
			{
				rc := NewTestRunCollectionWithImports(t,
					fakeRunCollectionPath,
					importDeps{
						unzip: func(zipPath, dstDir string) error {
							// Windows "disk full" errno checked by util.IsLowDiskSpace
							return &os.PathError{
								Op:   "unzip",
								Path: dstDir,
								Err:  syscall.Errno(0x27),
							}
						},
					},
				)

				gotID, err := rc.ImportRun(validZipPath)
				assert.Equal(t, RunID{}, gotID)

				var msg message.Message
				require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
				assert.Equal(t, message.EngineRunLowDiskSpaceTempDir, msg.Code())

				// Ensure the underlying errno is still discoverable through wrapping.
				assert.True(t, errors.Is(err, syscall.Errno(0x27)))
				assert.NoError(t, message.ValidateMetadataPlaceholders(err))
			}
		case "linux", "darwin":
			t.Skipf("Skipped on Unix systems as syscall.Errno(0x27) is not used on Unix")
		default:
			t.Skipf("unsupported OS: %s", runtime.GOOS)
		}
	})

	t.Run("Low disk space error caused by ENOSPC (Unix) or syscall.Errno(0x70) (Windows) during rename (file moving)", func(t *testing.T) {
		// Create an isolated RunCollection instance (same path), overriding only rename.
		// This avoids mutating the shared runCollection variable used by other subtests.
		var expectedErrno error
		switch runtime.GOOS {
		case "windows":
			expectedErrno = syscall.Errno(0x70)
		case "linux", "darwin":
			expectedErrno = syscall.ENOSPC
		default:
			t.Skipf("unsupported OS: %s", runtime.GOOS)
		}

		rc := NewTestRunCollectionWithImports(t,
			fakeRunCollectionPath,
			importDeps{
				rename: func(oldPath, newPath string) error {
					// Windows "disk full" errno checked by util.IsLowDiskSpace
					return &os.PathError{
						Op:   "rename",
						Path: newPath,
						Err:  expectedErrno,
					}
				},
			},
		)

		gotID, err := rc.ImportRun(validZipPath)
		assert.Equal(t, RunID{}, gotID)

		var msg message.Message
		require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
		assert.Equal(t, message.EngineRunLowDiskSpaceRunDir, msg.Code())

		// Ensure the underlying errno is still discoverable through wrapping.
		assert.True(t, errors.Is(err, expectedErrno))
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("returns CommonUnknownError when mkdirTemp fails with non-low-disk error", func(t *testing.T) {
		sentinel := errors.New("mkdirTemp failed")

		rc := NewTestRunCollectionWithImports(t,
			fakeRunCollectionPath,
			importDeps{
				getRunIDFromZip: func(zipPath string) (string, error) {
					return validRunID.Value, nil
				},
				mkdirTemp: func(dir, pattern string) (string, error) {
					return "", &os.PathError{Op: "mkdirtemp", Path: dir, Err: sentinel}
				},
			},
		)

		gotID, err := rc.ImportRun(validZipPath)
		assert.Equal(t, RunID{}, gotID)

		var msg message.Message
		require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
		assert.Equal(t, message.CommonUnknownError, msg.Code())
		assert.True(t, errors.Is(err, sentinel))
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("requiredDiskSpace is 'unknown' when size estimation fails", func(t *testing.T) {
		sizeErr := errors.New("cannot estimate")

		rc := NewTestRunCollectionWithImports(t,
			fakeRunCollectionPath,
			importDeps{
				getRunIDFromZip: func(zipPath string) (string, error) {
					return validRunID.Value, nil
				},
				estimatedUnzipSize: func(zipPath string) (uint64, error) {
					return 0, sizeErr
				},
				mkdirTemp: func(dir, pattern string) (string, error) {
					// Force the low-disk-space path so we can inspect metadata.
					switch runtime.GOOS {
					case "windows":
						return "", &os.PathError{Op: "mkdirtemp", Path: dir, Err: syscall.Errno(0x70)}
					default:
						return "", &os.PathError{Op: "mkdirtemp", Path: dir, Err: syscall.ENOSPC}
					}
				},
			},
		)

		gotID, err := rc.ImportRun(validZipPath)
		assert.Equal(t, RunID{}, gotID)

		var msg message.Message
		require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
		assert.Equal(t, message.EngineRunLowDiskSpaceTempDir, msg.Code())
		assert.Equal(t, "unknown", msg.Metadata()["requiredDiskSpace"])
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("requiredDiskSpace is formatted using IEC suffixes", func(t *testing.T) {
		kib := uint64(1024)
		mib := kib * 1024
		gib := mib * 1024
		tib := gib * 1024
		pib := tib * 1024
		eib := pib * 1024

		cases := []struct {
			name     string
			bytes    uint64
			expected string
		}{
			{name: "KiB", bytes: kib, expected: "1.0 KiB"},
			{name: "MiB", bytes: mib, expected: "1.0 MiB"},
			{name: "GiB", bytes: gib, expected: "1.0 GiB"},
			{name: "TiB", bytes: tib, expected: "1.0 TiB"},
			{name: "PiB", bytes: pib, expected: "1.0 PiB"},
			{name: "EiB", bytes: eib, expected: "1.0 EiB"},
			{name: ">=10 uses no decimals", bytes: 10 * kib, expected: "10 KiB"},
		}

		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				rc := NewTestRunCollectionWithImports(t,
					fakeRunCollectionPath,
					importDeps{
						getRunIDFromZip: func(zipPath string) (string, error) {
							return validRunID.Value, nil
						},
						estimatedUnzipSize: func(zipPath string) (uint64, error) {
							return tc.bytes, nil
						},
						mkdirTemp: func(dir, pattern string) (string, error) {
							// Force the low-disk-space path so we can inspect requiredDiskSpace.
							switch runtime.GOOS {
							case "windows":
								return "", &os.PathError{Op: "mkdirtemp", Path: dir, Err: syscall.Errno(0x70)}
							default:
								return "", &os.PathError{Op: "mkdirtemp", Path: dir, Err: syscall.ENOSPC}
							}
						},
					},
				)

				gotID, err := rc.ImportRun(validZipPath)
				assert.Equal(t, RunID{}, gotID)

				var msg message.Message
				require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
				assert.Equal(t, message.EngineRunLowDiskSpaceTempDir, msg.Code())
				assert.Equal(t, tc.expected, msg.Metadata()["requiredDiskSpace"])
				assert.NoError(t, message.ValidateMetadataPlaceholders(err))
			})
		}
	})

	t.Run("returns EngineRunZipFileInvalid when unzip fails with non-low-disk error", func(t *testing.T) {
		sentinel := errors.New("unzip failed")
		tmpDir := t.TempDir()

		rc := NewTestRunCollectionWithImports(t,
			fakeRunCollectionPath,
			importDeps{
				getRunIDFromZip: func(zipPath string) (string, error) {
					return validRunID.Value, nil
				},
				mkdirTemp: func(dir, pattern string) (string, error) {
					return tmpDir, nil
				},
				unzip: func(zipPath, dstDir string) error {
					return &os.PathError{Op: "unzip", Path: dstDir, Err: sentinel}
				},
			},
		)

		gotID, err := rc.ImportRun(validZipPath)
		assert.Equal(t, RunID{}, gotID)

		var msg message.Message
		require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
		assert.Equal(t, message.EngineRunZipFileInvalid, msg.Code())
		assert.True(t, errors.Is(err, sentinel))
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("returns EngineRunZipFileInvalid when rename fails with non-low-disk error", func(t *testing.T) {
		sentinel := errors.New("rename failed")
		tmpDir := t.TempDir()

		rc := NewTestRunCollectionWithImports(t,
			fakeRunCollectionPath,
			importDeps{
				getRunIDFromZip: func(zipPath string) (string, error) {
					return validRunID.Value, nil
				},
				mkdirTemp: func(dir, pattern string) (string, error) {
					return tmpDir, nil
				},
				unzip: func(zipPath, dstDir string) error {
					// Create the expected top-level directory so ImportRun reaches rename.
					return os.MkdirAll(filepath.Join(dstDir, validRunID.Value), perms.LocalDirPerm)
				},
				rename: func(oldPath, newPath string) error {
					return &os.PathError{Op: "rename", Path: newPath, Err: sentinel}
				},
			},
		)

		gotID, err := rc.ImportRun(validZipPath)
		assert.Equal(t, RunID{}, gotID)

		var msg message.Message
		require.True(t, errors.As(err, &msg), "expected message.Message, got %T (%v)", err, err)
		assert.Equal(t, message.EngineRunZipFileInvalid, msg.Code())
		assert.True(t, errors.Is(err, sentinel))
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestRunRename(t *testing.T) {
	tmpDir := t.TempDir()
	rc, err := NewRunCollection(filepath.Join(tmpDir, "runs"))
	require.NoError(t, err)

	t.Run("successfully renames a run", func(t *testing.T) {
		builder, err := rc.RunBuilder()
		require.NoError(t, err)

		oldName := "out name"
		metadata := &cdf.Metadata{Name: oldName, TargetConfig: emptySSHTargetConfig}
		runId, err := rc.CreateRun(builder, metadata)
		require.NoError(t, err)

		loadedMetadata, err := rc.readMetadata(runId)
		require.NoError(t, err)
		assert.Equal(t, oldName, loadedMetadata.Name)

		newName := "new name"
		err = rc.RenameRun(context.Background(), runId, newName)
		require.NoError(t, err)

		loadedMetadata, err = rc.readMetadata(runId)
		require.NoError(t, err)
		assert.Equal(t, newName, loadedMetadata.Name)
	})

	t.Run("fails to rename a non-existent run", func(t *testing.T) {
		err := rc.RenameRun(context.Background(), RunID{Value: "i-dont-exist"}, "new name")
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "i-dont-exist"})
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fails to rename a locked run", func(t *testing.T) {
		builder, err := rc.RunBuilder()
		require.NoError(t, err)

		metadata := &cdf.Metadata{Name: "locked", TargetConfig: emptySSHTargetConfig}
		runId, err := rc.CreateRun(builder, metadata)
		require.NoError(t, err)

		cleanup, err := rc.LockRun(context.Background(), runId)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(),
			10*time.Millisecond)
		defer cancel()

		err = rc.RenameRun(ctx, runId, "unlocked")
		expectedErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runId.Value})
		assert.Equal(t, expectedErr, err)

		err = cleanup()
		assert.NoError(t, err)
	})
}

func TestRunLock(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "testRun")
	lockDir := filepath.Join(tmpDir, lockDirName)
	runCollection, err := NewRunCollection(runDir)
	require.NoError(t, err)

	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)
	builder.AddEntity("fakeEntity")
	metadata := &cdf.Metadata{TargetConfig: emptySSHTargetConfig}
	runID, err := runCollection.CreateRun(builder, metadata)
	require.NoError(t, err)

	t.Run("successfully locks & unlocks", func(t *testing.T) {
		// Lock
		cleanup, err := runCollection.LockRun(context.Background(), runID)
		assert.NoError(t, err)

		// Confirm lock
		lockFilePath := filepath.Join(lockDir, runID.Value+"/lockfile")
		assert.FileExists(t, lockFilePath)

		secondLock := flock.New(lockFilePath)
		require.NotNil(t, secondLock)

		locked, err := secondLock.TryLock()
		require.NoError(t, err)
		assert.False(t, locked)

		// Unlock
		err = cleanup()
		assert.NoError(t, err)

		// Confirm unlock
		locked, err = secondLock.TryLock()
		require.NoError(t, err)
		assert.True(t, locked)
		err = secondLock.Unlock()
		require.NoError(t, err)
	})

	t.Run("fails to lock non-existent run", func(t *testing.T) {
		invalidRunID := RunID{Value: "i-dont-exist"}
		_, err := runCollection.LockRun(context.Background(), invalidRunID)
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "i-dont-exist"})
		assert.Equal(t, expectedErr, err)
	})

	t.Run("times out if run is already locked", func(t *testing.T) {
		cleanup, err := runCollection.LockRun(context.Background(), runID)
		assert.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(),
			10*time.Millisecond)
		defer cancel()

		_, err = runCollection.LockRun(ctx, runID)
		expectedErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID.Value})
		assert.Equal(t, expectedErr, err)

		err = cleanup()
		assert.NoError(t, err)
	})
}

func TestRunLease(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "testRun")
	lockDir := filepath.Join(tmpDir, lockDirName)
	runCollection, err := NewRunCollection(runDir)
	require.NoError(t, err)

	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)
	builder.AddEntity("fakeEntity")
	metadata := &cdf.Metadata{TargetConfig: emptySSHTargetConfig}
	runID, err := runCollection.CreateRun(builder, metadata)
	require.NoError(t, err)

	t.Run("successfully leases & releases", func(t *testing.T) {
		// Lease
		release, err := runCollection.LeaseRun(context.Background(), runID)
		assert.NoError(t, err)

		// Confirm lease
		leaseFilePath := filepath.Join(lockDir, runID.Value+"/leasefile")
		assert.FileExists(t, leaseFilePath)

		secondLock := flock.New(leaseFilePath)
		require.NotNil(t, secondLock)

		locked, err := secondLock.TryLock()
		require.NoError(t, err)
		assert.False(t, locked)

		// Release
		err = release()
		assert.NoError(t, err)

		// Confirm release
		locked, err = secondLock.TryLock()
		require.NoError(t, err)
		assert.True(t, locked)
		err = secondLock.Unlock()
		require.NoError(t, err)
	})

	t.Run("successfully leases generated run before it exists", func(t *testing.T) {
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		newRunID := builder.RunID()

		release, err := runCollection.LeaseRun(context.Background(), newRunID)
		assert.NoError(t, err)

		leaseFilePath := filepath.Join(lockDir, newRunID.Value+"/leasefile")
		assert.FileExists(t, leaseFilePath)

		secondLock := flock.New(leaseFilePath)
		require.NotNil(t, secondLock)

		locked, err := secondLock.TryLock()
		require.NoError(t, err)
		assert.False(t, locked)

		err = release()
		assert.NoError(t, err)
	})

	t.Run("times out if run is already leased", func(t *testing.T) {
		release, err := runCollection.LeaseRun(context.Background(), runID)
		assert.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(),
			200*time.Millisecond)
		defer cancel()

		_, err = runCollection.LeaseRun(ctx, runID)
		expectedErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID.Value})
		assert.Equal(t, expectedErr, err)

		err = release()
		assert.NoError(t, err)
	})

	t.Run("fails to lease path-like run IDs", func(t *testing.T) {
		for _, runID := range pathLikeRunIDs {
			_, err := runCollection.LeaseRun(context.Background(), runID)
			assertRunDoesNotExistError(t, err, runID)
		}
	})
}

func TestUpdateRunResult(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "testRun")
	runCollection, err := NewRunCollection(runDir)
	require.NoError(t, err)

	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)
	builder.AddEntity("fakeEntity")
	metadata := &cdf.Metadata{TargetConfig: emptySSHTargetConfig, WorkloadType: "Launch"}
	runID, err := runCollection.CreateRun(builder, metadata)
	require.NoError(t, err)

	t.Run("successfully updates run result", func(t *testing.T) {
		err := runCollection.UpdateRunResult(context.Background(), runID, RecipeSuccess, errors.New("i'm an error"))
		require.NoError(t, err)

		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		assert.Contains(t, loadedMetadata.RunResult, RecipeSuccess)
		assert.Contains(t, loadedMetadata.RunError, "i'm an error")
	})

	t.Run("fails on a non-existent run", func(t *testing.T) {
		err := runCollection.UpdateRunResult(context.Background(), RunID{Value: "i-dont-exist"}, RecipeSuccess, nil)
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "i-dont-exist"})
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fails on a locked run", func(t *testing.T) {
		cleanup, err := runCollection.LockRun(context.Background(), runID)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(),
			10*time.Millisecond)
		defer cancel()

		err = runCollection.UpdateRunResult(ctx, runID, RecipeSuccess, nil)
		expectedErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID.Value})
		assert.Equal(t, expectedErr, err)

		err = cleanup()
		assert.NoError(t, err)
	})

	t.Run("formats Message error correctly in error field of metadata", func(t *testing.T) {
		msgErr := message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "abcdef"})
		catErr, _ := message.LookupMessage(msgErr)
		err := runCollection.UpdateRunResult(context.Background(), runID, RecipeSuccess, msgErr)
		require.NoError(t, err)

		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		assert.Contains(t, loadedMetadata.RunResult, RecipeSuccess)
		assert.Equal(t, fmt.Sprintf("%v %v %v", catErr.Message, catErr.Explanation, catErr.Advice), loadedMetadata.RunError)
		assert.Contains(t, loadedMetadata.RunError, "abcdef")
	})

	t.Run("does not edit the run end time in the metadata", func(t *testing.T) {
		err := runCollection.UpdateRunResult(context.Background(), runID, RecipeSuccess, errors.New("i'm an error"))
		require.NoError(t, err)

		// Assert that end time hasn't been set
		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		assert.Equal(t, util.InvalidTime(), loadedMetadata.EndTime)
	})

}

func TestUpdateWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "testRun")
	runCollection, err := NewRunCollection(runDir)
	require.NoError(t, err)

	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)
	builder.AddEntity("fakeEntity")
	metadata := &cdf.Metadata{TargetConfig: emptySSHTargetConfig, WorkloadType: "Launch"}
	runID, err := runCollection.CreateRun(builder, metadata)
	require.NoError(t, err)

	t.Run("updates working dir if it is empty and home dir is non-empty", func(t *testing.T) {
		err := runCollection.UpdateWorkingDir(context.Background(), runID, "/home/someone")
		require.NoError(t, err)

		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		assert.Equal(t, "/home/someone", loadedMetadata.WorkingDir)
	})

	t.Run("update run result preserves working dir", func(t *testing.T) {
		builder, err = runCollection.RunBuilder()
		require.NoError(t, err)
		builder.AddEntity("fakeEntity")
		metadata = &cdf.Metadata{TargetConfig: emptySSHTargetConfig, WorkloadType: "Launch"}
		runID, err = runCollection.CreateRun(builder, metadata)
		require.NoError(t, err)

		err = runCollection.UpdateWorkingDir(context.Background(), runID, "/home/someone")
		require.NoError(t, err)

		err = runCollection.UpdateRunResult(context.Background(), runID, RecipeSuccess, nil)
		require.NoError(t, err)

		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		assert.Equal(t, "/home/someone", loadedMetadata.WorkingDir)
		assert.Equal(t, string(RecipeSuccess), loadedMetadata.RunResult)
	})

	t.Run("doesn't update working dir if it is non-empty", func(t *testing.T) {
		// Create a new run with a non-empty working dir
		builder, err = runCollection.RunBuilder()
		require.NoError(t, err)
		builder.AddEntity("fakeEntity")
		metadata = &cdf.Metadata{TargetConfig: emptySSHTargetConfig, WorkloadType: "Launch", WorkingDir: "/a/working/dir"}
		runID, err = runCollection.CreateRun(builder, metadata)
		require.NoError(t, err)

		// Now we try to update it with our home dir
		err = runCollection.UpdateWorkingDir(context.Background(), runID, "/something/else")
		require.NoError(t, err)

		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		assert.Equal(t, "/a/working/dir", loadedMetadata.WorkingDir)
	})

	t.Run("doesn't update working dir if workload is not launch", func(t *testing.T) {
		builder, err = runCollection.RunBuilder()
		require.NoError(t, err)
		builder.AddEntity("fakeEntity")
		metadata = &cdf.Metadata{TargetConfig: emptySSHTargetConfig, WorkloadType: "Attach"}
		runID, err = runCollection.CreateRun(builder, metadata)
		require.NoError(t, err)

		err = runCollection.UpdateWorkingDir(context.Background(), runID, "/home/someone")
		require.NoError(t, err)

		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		assert.Empty(t, loadedMetadata.WorkingDir)
	})

	t.Run("fails on a non-existent run", func(t *testing.T) {
		err := runCollection.UpdateWorkingDir(context.Background(), RunID{Value: "i-dont-exist"}, "/home/someone")
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "i-dont-exist"})
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fails on a locked run", func(t *testing.T) {
		cleanup, err := runCollection.LockRun(context.Background(), runID)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(),
			10*time.Millisecond)
		defer cancel()

		err = runCollection.UpdateWorkingDir(ctx, runID, "/home/someone")
		expectedErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID.Value})
		assert.Equal(t, expectedErr, err)

		err = cleanup()
		assert.NoError(t, err)
	})
}

func TestUpdateRunResultPreservesExistingEndTime(t *testing.T) {
	tmpDir := t.TempDir()
	runDir := filepath.Join(tmpDir, "testRun")
	runCollection, err := NewRunCollection(runDir)
	require.NoError(t, err)

	builder, err := runCollection.RunBuilder()
	require.NoError(t, err)
	builder.AddEntity("fakeEntity")
	phase1EndTime := util.CurrentTime()
	metadata := &cdf.Metadata{
		TargetConfig: emptySSHTargetConfig,
		RunResult:    string(RecipeInProgressPhase1Complete),
		EndTime:      phase1EndTime,
	}
	runID, err := runCollection.CreateRun(builder, metadata)
	require.NoError(t, err)

	err = runCollection.UpdateRunResult(context.Background(), runID, RecipeFailureRetrievePhase1Complete, errors.New("background failed"))
	require.NoError(t, err)

	loadedMetadata, err := runCollection.readMetadata(runID)
	require.NoError(t, err)
	assert.Equal(t, string(RecipeFailureRetrievePhase1Complete), loadedMetadata.RunResult)
	assert.Equal(t, phase1EndTime.ToFormattedString(), loadedMetadata.EndTime.ToFormattedString())
	assert.Equal(t, "background failed", loadedMetadata.RunError)
}

func TestSetRunEndTime(t *testing.T) {
	createRun := func(t *testing.T, metadata *cdf.Metadata) (*RunCollection, RunID) {
		t.Helper()

		runCollection, err := NewRunCollection(filepath.Join(t.TempDir(), "testRun"))
		require.NoError(t, err)

		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)
		builder.AddEntity("fakeEntity")

		runID, err := runCollection.CreateRun(builder, metadata)
		require.NoError(t, err)

		return runCollection, runID
	}

	t.Run("sets end time", func(t *testing.T) {
		runCollection, runID := createRun(t, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})

		before := time.Now().UTC().Add(-time.Second)
		err := runCollection.SetRunEndTime(context.Background(), runID)
		after := time.Now().UTC().Add(time.Second)
		require.NoError(t, err)

		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		require.NotEqual(t, util.InvalidTime(), loadedMetadata.EndTime)

		loadedEndTime := time.Time(loadedMetadata.EndTime)
		require.False(t, loadedEndTime.Before(before))
		require.False(t, loadedEndTime.After(after))
	})

	t.Run("preserves existing end time", func(t *testing.T) {
		fixedEndTime := util.UTCRFC3339Time(time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC))
		runCollection, runID := createRun(t, &cdf.Metadata{
			TargetConfig: emptySSHTargetConfig,
			EndTime:      fixedEndTime,
		})

		err := runCollection.SetRunEndTime(context.Background(), runID)
		require.NoError(t, err)

		loadedMetadata, err := runCollection.readMetadata(runID)
		require.NoError(t, err)
		require.Equal(t, fixedEndTime.ToFormattedString(), loadedMetadata.EndTime.ToFormattedString())
	})

	t.Run("fails on a non-existent run", func(t *testing.T) {
		runCollection := NewTestRunCollection(t, t.TempDir())
		runID := RunID{Value: "i-dont-exist"}

		err := runCollection.SetRunEndTime(context.Background(), runID)

		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID.Value})
		require.Equal(t, expectedErr, err)
		require.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("fails on a locked run", func(t *testing.T) {
		runCollection, runID := createRun(t, &cdf.Metadata{TargetConfig: emptySSHTargetConfig})
		cleanup, err := runCollection.LockRun(context.Background(), runID)
		require.NoError(t, err)
		defer func() { require.NoError(t, cleanup()) }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		err = runCollection.SetRunEndTime(ctx, runID)

		expectedErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID.Value})
		require.Equal(t, expectedErr, err)
		require.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestRunAccessorFunctions_RunCollection(t *testing.T) {
	baseDir := t.TempDir()
	runDir := filepath.Join(baseDir, "testRun")

	runCollection, err := NewRunCollection(runDir)
	assert.NoError(t, err)

	// Helper to create a run with a specific recipe name
	createRunWithRecipe := func(recipeName string, sc HostSourceCodePath) RunID {
		builder, err := runCollection.RunBuilder()
		assert.NoError(t, err)

		metadata := &cdf.Metadata{
			RecipeName:   recipeName,
			StartTime:    util.CurrentTime(),
			EndTime:      util.CurrentTime(),
			TargetConfig: emptySSHTargetConfig,
		}
		runID, err := runCollection.CreateRun(builder, metadata)
		assert.NoError(t, err)

		// Simulate a minimal recipe component file to allow GetRecipeComponentPath to work
		componentPath := filepath.Join(runCollection.GetRunPath(runID), RecipeSourceRelativePath)
		err = os.MkdirAll(filepath.Dir(componentPath), perms.LocalDirPerm)
		assert.NoError(t, err)
		err = os.WriteFile(componentPath, []byte("// dummy recipe"), perms.LocalFilePerm)
		assert.NoError(t, err)

		// Simulate a minimal source-code.json
		componentPath = filepath.Join(runCollection.GetRunPath(runID), SourceCodeFilename)
		data, err := json.Marshal(sc)
		assert.NoError(t, err)
		err = os.WriteFile(componentPath, data, perms.LocalFilePerm)
		assert.NoError(t, err)
		return runID
	}

	runID1 := createRunWithRecipe("cpu_microarchitecture", HostSourceCodePath{Paths: []string{"/foo/", "/bar/baz"}})
	time.Sleep(10 * time.Millisecond) // ensure ordering
	runID2 := createRunWithRecipe("cpu_microarchitecture", HostSourceCodePath{Paths: []string{}})
	time.Sleep(10 * time.Millisecond)
	runID3 := createRunWithRecipe("other_recipe", HostSourceCodePath{Paths: []string{}})

	t.Run("GetRecipeComponentPath produces expected output", func(t *testing.T) {
		componentPath, err := runCollection.GetRecipeComponentPath(runID1)
		assert.NoError(t, err)
		assert.Contains(t, componentPath, RecipeSourceRelativePath)
	})

	t.Run("GetNewestRun returns the latest run", func(t *testing.T) {
		runID, err := runCollection.GetNewestRun([]RunID{runID1, runID2, runID3})
		assert.NoError(t, err)
		assert.Equal(t, runID, runID3)
	})

	t.Run("CheckRunsUseSameRecipe succeeds when the 2 runs use the same recipe", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipe([]RunID{runID1, runID2})
		assert.NoError(t, err)
	})

	t.Run("CheckRunsUseSameRecipe fails when the 2 runs use different recipes", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipe([]RunID{runID1, runID3})
		expectedMetadata := map[string]string{
			"runID1":  runID1.Value,
			"runID2":  runID3.Value,
			"recipe1": "cpu_microarchitecture",
			"recipe2": "other_recipe",
		}
		expectedErr := message.New(message.EngineRunDifferentRecipes).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("CheckRunsUseSameRecipeNameOnly succeeds with identical names", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipeNameOnly([]RunID{runID1, runID2})
		assert.NoError(t, err)
	})

	t.Run("CheckRunsUseSameRecipeNameOnly fails on name mismatch", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipeNameOnly([]RunID{runID1, runID3})
		expectedMetadata := map[string]string{
			"runID1":  runID1.Value,
			"runID2":  runID3.Value,
			"recipe1": "cpu_microarchitecture",
			"recipe2": "other_recipe",
		}
		expectedErr := message.New(message.EngineRunDifferentRecipes).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
	})
}

func TestCheckRunsUseSameRecipe_ToleratesMissingSchemas(t *testing.T) {
	runDir := t.TempDir()
	runCollection, err := NewRunCollection(runDir)
	require.NoError(t, err)

	// Helper to create a run with a specific recipe name and optional schema
	createRun := func(recipeName string, schema *string) RunID {
		builder, err := runCollection.RunBuilder()
		require.NoError(t, err)

		metadata := &cdf.Metadata{
			RecipeName:   recipeName,
			StartTime:    util.CurrentTime(),
			EndTime:      util.CurrentTime(),
			TargetConfig: emptySSHTargetConfig,
		}
		runID, err := runCollection.CreateRun(builder, metadata)
		require.NoError(t, err)

		// Write dummy recipe component if schema is present
		if schema != nil {
			component := &cdf.Component{
				Type: cdf.ComponentType{
					SchemaVersion: *schema,
				},
			}
			componentPath := builder.AddComponent(component.Type, RecipeSourceRelativePath)
			err := os.MkdirAll(filepath.Dir(componentPath), perms.LocalDirPerm)
			require.NoError(t, err)
			err = util.WriteJSONFile(componentPath, component, perms.LocalFilePerm)
			require.NoError(t, err)
		}

		err = runCollection.UpdateManifest(builder)
		require.NoError(t, err)

		return runID
	}

	runWithSchemaA := createRun("cpu_microarchitecture", util.Ptr("1.0"))
	runWithSchemaA2 := createRun("cpu_microarchitecture", util.Ptr("1.0"))
	runWithSchemaB := createRun("cpu_microarchitecture", util.Ptr("2.0")) // incompatible
	runWithoutSchema := createRun("cpu_microarchitecture", nil)           // simulates older run with no schema

	t.Run("succeeds when all runs use the same recipe and missing schemas are ignored", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipe([]RunID{runWithSchemaA, runWithoutSchema, runWithSchemaA2})
		assert.NoError(t, err)
	})

	t.Run("succeeds regardless of order of missing-schema runs", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipe([]RunID{runWithoutSchema, runWithSchemaA, runWithSchemaA2})
		assert.NoError(t, err)
	})

	t.Run("fails when a run has a different recipe schema", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipe([]RunID{runWithSchemaA, runWithSchemaB})
		expectedMetadata := map[string]string{
			"runID1":  runWithSchemaA.Value,
			"runID2":  runWithSchemaB.Value,
			"recipe":  "cpu_microarchitecture",
			"schema1": "1.0",
			"schema2": "2.0",
		}
		expectedErr := message.New(message.EngineRunDifferentRecipeSchemas).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("fails when a mismatched schema comes after a missing-schema run", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipe([]RunID{runWithoutSchema, runWithSchemaB, runWithSchemaA})
		expectedMetadata := map[string]string{
			"runID1":  runWithSchemaB.Value,
			"runID2":  runWithSchemaA.Value,
			"recipe":  "cpu_microarchitecture",
			"schema1": "2.0",
			"schema2": "1.0",
		}
		expectedErr := message.New(message.EngineRunDifferentRecipeSchemas).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("checkRunsUseSameRecipeNameOnly ignores mismatched schemas", func(t *testing.T) {
		err := runCollection.CheckRunsUseSameRecipeNameOnly([]RunID{runWithSchemaA, runWithSchemaB})
		assert.NoError(t, err)
	})
}

func TestSanitizeArchiveEntry(t *testing.T) {
	t.Run("validates archive path correctly", func(t *testing.T) {
		destinationDir := filepath.Join("tmp", "destination")
		validPaths := []string{
			"hello_world.txt",
			"metadata.json",
			filepath.Join("subdir", "elliot.json"),
			filepath.Join("subdir", "subdir_again", "elliot_again.json"),
		}
		for _, path := range validPaths {
			_, err := sanitizeArchiveEntry(path, destinationDir)
			assert.NoError(t, err, "expected path %q to be valid", path)
		}
	})

	t.Run("is robust against zip-slip attacks", func(t *testing.T) {
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			destinationDir := "/tmp/destination"
			unixEvilPaths := []string{
				"../malware.txt",
				"/tmp/destination/../evil.txt",
				"/tmp/destination-not-really/trojan.yaml",
				"/tmp/destination/subdir/../../../explode.exe",
				"../../../../etc/passwd",
				"/",
			}
			for _, path := range unixEvilPaths {
				_, err := sanitizeArchiveEntry(path, destinationDir)
				assert.ErrorContains(t, err, "illegal archive entry:", "expected path %q to be rejected", path)
			}
		}

		if runtime.GOOS == "windows" {
			destinationDir := "C:\\tmp\\destination"
			windowsEvilPaths := []string{
				"..\\malware.txt",
				"C:\\tmp\\destination\\..\\evil.txt",
				"C:\\tmp\\destination-not-really\\trojan.yaml",
				"C:\\tmp\\destination\\subdir\\..\\..\\..\\explode.exe",
				"..\\..\\..\\..\\Windows\\System32\\config\\SAM",
				"C:\\",
			}
			for _, path := range windowsEvilPaths {
				_, err := sanitizeArchiveEntry(path, destinationDir)
				assert.ErrorContains(t, err, "illegal archive entry:", "expected path %q to be rejected", path)
			}

		}
	})
}
