// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemonstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

func TestStoreWriteAndRead(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), "state", fileName)}
	want := validDaemon()

	require.NoError(t, store.Write(want))

	got, err := store.Read()
	require.NoError(t, err)
	assert.Equal(t, &want, got)
	assert.Equal(t, filepath.Join(filepath.Dir(store.path), fileName), store.path)

	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(store.path)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(filePerm), info.Mode().Perm())
	}
}

func TestStoreWriteAtomicallyReplacesDaemon(t *testing.T) {
	store := newTestStore(t)
	first := validDaemon()
	require.NoError(t, store.Write(first))

	second := first
	second.ServerPort = 12000
	require.NoError(t, store.Write(second))

	got, err := store.Read()
	require.NoError(t, err)
	assert.Equal(t, &second, got)
	matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(store.path), ".daemon.json.*.tmp"))
	require.NoError(t, globErr)
	assert.Empty(t, matches)
}

func TestStoreReadRejectsInvalidMetadata(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, os.WriteFile(store.path, []byte(`{"pid":1234,"host":"127.0.0.1","server_port":0}`), filePerm))

	_, err := store.Read()
	require.Error(t, err)
	assert.ErrorContains(t, err, "server port must be between 1 and 65535")
}

func TestStoreReadIdentifiesMalformedMetadata(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, os.WriteFile(store.path, []byte(`{"version":`), filePerm))

	_, err := store.Read()
	require.ErrorIs(t, err, ErrInvalidMetadata)
}

func TestStoreWriteRejectsInvalidMetadata(t *testing.T) {
	tests := map[string]func(*Daemon){
		"empty host":          func(daemon *Daemon) { daemon.Host = "" },
		"invalid server port": func(daemon *Daemon) { daemon.ServerPort = 65536 },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			store := newTestStore(t)
			daemon := validDaemon()
			mutate(&daemon)

			err := store.Write(daemon)
			require.Error(t, err)
			assert.NoFileExists(t, store.path)
		})
	}
}

func TestStoreRemove(t *testing.T) {
	store := newTestStore(t)
	require.NoError(t, store.Write(validDaemon()))

	require.NoError(t, store.Remove())
	assert.NoFileExists(t, store.path)
	require.NoError(t, store.Remove())
}

func TestNewStoreUsesPerUserStateDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG_STATE_HOME is not used on Windows")
	}
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	store, err := NewStore()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(stateHome, "apxd", fileName), store.path)
}

func TestAcquireStateLockSerializesAccess(t *testing.T) {
	setTemporaryStateHome(t)
	first, err := AcquireStateLock(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = AcquireStateLock(ctx)
	require.ErrorIs(t, err, context.Canceled)

	require.NoError(t, first.Release())
	second, err := AcquireStateLock(context.Background())
	require.NoError(t, err)
	require.NoError(t, second.Release())
}

func TestAcquireStateLockFailsWhenStatePathIsAFile(t *testing.T) {
	setTemporaryStateHome(t)
	stateDir, err := userdirs.StateDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(stateDir), stateDirPerm))
	require.NoError(t, os.WriteFile(stateDir, nil, filePerm))

	_, err = AcquireStateLock(context.Background())
	require.Error(t, err)
	require.False(t, errors.Is(err, context.Canceled))
}

func TestLifetimeLockReportsWhetherDaemonHoldsIt(t *testing.T) {
	setTemporaryStateHome(t)
	held, err := LifetimeLockHeld()
	require.NoError(t, err)
	assert.False(t, held)

	lock, err := AcquireLifetimeLock()
	require.NoError(t, err)
	held, err = LifetimeLockHeld()
	require.NoError(t, err)
	assert.True(t, held)
	_, err = AcquireLifetimeLock()
	require.ErrorIs(t, err, errLifetimeLockHeld)

	require.NoError(t, lock.Release())
	held, err = LifetimeLockHeld()
	require.NoError(t, err)
	assert.False(t, held)
}

func TestFindRunningDaemonRemovesMetadataWhenLifetimeLockIsFree(t *testing.T) {
	setTemporaryStateHome(t)
	store, err := NewStore()
	require.NoError(t, err)
	require.NoError(t, store.Write(validDaemon()))

	_, err = FindRunningDaemon(context.Background())

	require.ErrorIs(t, err, ErrNoRunningDaemon)
	_, err = store.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestFindRunningDaemonReportsMissingMetadataWhileLifetimeLockIsHeld(t *testing.T) {
	setTemporaryStateHome(t)
	lifetimeLock, err := AcquireLifetimeLock()
	require.NoError(t, err)
	defer func() { require.NoError(t, lifetimeLock.Release()) }()

	_, err = FindRunningDaemon(context.Background())

	require.ErrorContains(t, err, "daemon is running but its metadata is unavailable")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestFindRunningDaemonRetainsInvalidMetadataWhileLifetimeLockIsHeld(t *testing.T) {
	setTemporaryStateHome(t)
	store, err := NewStore()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(store.path), stateDirPerm))
	invalidMetadata := []byte(`{"host":"127.0.0.1","server_port":0}`)
	require.NoError(t, os.WriteFile(store.path, invalidMetadata, filePerm))
	lifetimeLock, err := AcquireLifetimeLock()
	require.NoError(t, err)
	defer func() { require.NoError(t, lifetimeLock.Release()) }()

	_, err = FindRunningDaemon(context.Background())

	require.ErrorContains(t, err, "daemon is running but its metadata is unavailable")
	require.ErrorIs(t, err, ErrInvalidMetadata)
	storedMetadata, err := os.ReadFile(store.path)
	require.NoError(t, err)
	assert.Equal(t, invalidMetadata, storedMetadata)
}

func TestRemoveIfStoppedRetainsMetadataWhileLifetimeLockIsHeld(t *testing.T) {
	setTemporaryStateHome(t)
	store, err := NewStore()
	require.NoError(t, err)
	daemon := validDaemon()
	require.NoError(t, store.Write(daemon))
	lifetimeLock, err := AcquireLifetimeLock()
	require.NoError(t, err)
	defer func() { require.NoError(t, lifetimeLock.Release()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, RemoveIfStopped(ctx, daemon), context.DeadlineExceeded)

	savedDaemon, err := store.Read()
	require.NoError(t, err)
	assert.Equal(t, daemon, *savedDaemon)
}

func TestRemoveIfStoppedWaitsForLifetimeLock(t *testing.T) {
	setTemporaryStateHome(t)
	store, err := NewStore()
	require.NoError(t, err)
	daemon := validDaemon()
	require.NoError(t, store.Write(daemon))
	lifetimeLock, err := AcquireLifetimeLock()
	require.NoError(t, err)
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = lifetimeLock.Release()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, RemoveIfStopped(ctx, daemon))

	_, err = store.Read()
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRemoveIfStoppedRetainsReplacementMetadata(t *testing.T) {
	setTemporaryStateHome(t)
	store, err := NewStore()
	require.NoError(t, err)
	expected := validDaemon()
	replacement := expected
	replacement.ServerPort = 12000
	require.NoError(t, store.Write(replacement))
	lifetimeLock, err := AcquireLifetimeLock()
	require.NoError(t, err)
	defer func() { require.NoError(t, lifetimeLock.Release()) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, RemoveIfStopped(ctx, expected))

	savedDaemon, err := store.Read()
	require.NoError(t, err)
	assert.Equal(t, replacement, *savedDaemon)
}

func setTemporaryStateHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{path: filepath.Join(t.TempDir(), fileName)}
}

func validDaemon() Daemon {
	return Daemon{
		Host:       "127.0.0.1",
		ServerPort: 9000,
	}
}
