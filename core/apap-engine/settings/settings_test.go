// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package settings

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

func TestSavingSettingReplacesTheFile(t *testing.T) {
	// Given
	dir := t.TempDir()
	path := filepath.Join(dir, Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	// Read the original file's identity now; os.Stat can defer this on Windows.
	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	before, err := file.Stat()
	require.NoError(t, err)
	require.NoError(t, file.Close())
	// When
	err = Write(context.Background(), path, ProfilingFocus, "server")
	// Then
	require.NoError(t, err)
	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.False(t, os.SameFile(before, after), "saving should replace the file rather than truncate it")
	value, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "server", value)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "only the settings and lock files should remain")
}

func TestReadingSettingReportsLockFailure(t *testing.T) {
	// Given
	// The settings filename fits the filesystem limit, but adding .lock exceeds it.
	path := filepath.Join(t.TempDir(), strings.Repeat("p", 252))
	require.NoError(t, os.WriteFile(path, []byte(`{"profilingFocus":"mobile"}`), perms.LocalFilePerm))
	// When
	_, err := Read(context.Background(), path, ProfilingFocus)
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsReadFailed, msg.Code())
	assert.Equal(t, path, msg.Metadata()["path"])
	assert.Error(t, msg.Unwrap())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"profilingFocus":"mobile"}`, string(data))
}

func TestSavingSettingReportsLockFailure(t *testing.T) {
	// Given
	// The settings filename fits the filesystem limit, but adding .lock exceeds it.
	path := filepath.Join(t.TempDir(), strings.Repeat("p", 252))
	require.NoError(t, os.WriteFile(path, []byte(`{"profilingFocus":"mobile"}`), perms.LocalFilePerm))
	// When
	err := Write(context.Background(), path, ProfilingFocus, "server")
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsWriteFailed, msg.Code())
	assert.Equal(t, path, msg.Metadata()["path"])
	assert.Error(t, msg.Unwrap())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"profilingFocus":"mobile"}`, string(data))
}

func TestSavingSettingWaitsForOtherWriters(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	lock := flock.New(path + ".lock")
	require.NoError(t, lock.Lock())
	t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
	finished := make(chan error, 1)
	// When
	go func() { finished <- Write(context.Background(), path, ProfilingFocus, "server") }()
	// Then
	select {
	case err := <-finished:
		t.Fatalf("save finished before the other writer released the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// The other writer updates the file before releasing its lock.
	require.NoError(t, os.WriteFile(path, []byte(`{"profilingFocus":"mobile","other":true}`), perms.LocalFilePerm))
	require.NoError(t, lock.Unlock())
	require.NoError(t, <-finished)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"profilingFocus":"server","other":true}`, string(data))
}

func TestSettingCanBeReadWhileOtherCallsSaveIt(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	start := make(chan struct{})
	errors := make(chan error, 4)
	var workers sync.WaitGroup
	// When
	for _, choice := range []string{"mobile", "server", "none"} {
		workers.Go(func() {
			<-start
			for range 20 {
				if err := Write(context.Background(), path, ProfilingFocus, choice); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	workers.Go(func() {
		<-start
		for range 200 {
			value, err := Read(context.Background(), path, ProfilingFocus)
			if err != nil {
				errors <- err
				return
			}
			if value == nil {
				errors <- assert.AnError
				return
			}
		}
	})
	close(start)
	workers.Wait()
	close(errors)
	// Then
	for err := range errors {
		assert.NoError(t, err, "readers should always see a complete saved setting")
	}
}

func TestCancelledSettingReadLeavesTheFileUnchanged(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// When
	_, err := Read(ctx, path, ProfilingFocus)
	// Then
	require.ErrorIs(t, err, context.Canceled)
	value, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

func TestCancelledSettingSaveLeavesTheFileUnchanged(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// When
	err := Write(ctx, path, ProfilingFocus, "server")
	// Then
	require.ErrorIs(t, err, context.Canceled)
	value, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

func TestCancelledSettingClearLeavesTheFileUnchanged(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// When
	err := Write(ctx, path, ProfilingFocus, nil)
	// Then
	require.ErrorIs(t, err, context.Canceled)
	value, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

const settingLockProcessEnv = "PERFORMIX_SETTINGS_TEST_PATH"

// TestSettingsReadersCanShareLockAcrossProcesses holds a read lock and starts a
// second process to check that it can still read the saved setting.
func TestSettingsReadersCanShareLockAcrossProcesses(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	lock := flock.New(path + ".lock")
	require.NoError(t, lock.RLock())
	t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
	// When
	runSettingLockProcess(t, path, "read_succeeds")
	// Then
	require.NoError(t, lock.Unlock())
	value, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

// TestSettingsReadWaitsForWriterAcrossProcesses checks that a separate reader
// stops waiting when its deadline expires while a writer holds the lock.
func TestSettingsReadWaitsForWriterAcrossProcesses(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	lock := flock.New(path + ".lock")
	require.NoError(t, lock.Lock())
	t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
	// When
	runSettingLockProcess(t, path, "read_times_out")
	// Then
	require.NoError(t, lock.Unlock())
	value, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

// TestSettingsWriteWaitsForReaderAcrossProcesses checks that a separate writer
// times out without changing the file while a reader holds the lock.
func TestSettingsWriteWaitsForReaderAcrossProcesses(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	lock := flock.New(path + ".lock")
	require.NoError(t, lock.RLock())
	t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
	// When
	runSettingLockProcess(t, path, "write_times_out")
	// Then
	require.NoError(t, lock.Unlock())
	value, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

// TestSettingsWriteWaitsForWriterAcrossProcesses checks that a separate writer
// times out without changing the file while another writer holds the lock.
func TestSettingsWriteWaitsForWriterAcrossProcesses(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	lock := flock.New(path + ".lock")
	require.NoError(t, lock.Lock())
	t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
	// When
	runSettingLockProcess(t, path, "write_times_out")
	// Then
	require.NoError(t, lock.Unlock())
	value, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

// runSettingLockProcess passes the temporary file path to a child process and runs
// just the named helper case. A failed child assertion also fails the parent test.
func runSettingLockProcess(t *testing.T, path, helperCase string) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestSettingLockHelperProcess$/^"+helperCase+"$")
	cmd.Env = append(os.Environ(), settingLockProcessEnv+"="+path)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

// TestSettingLockHelperProcess provides the child operations for the tests above.
// Normal test runs skip it because only the parent tests supply the file path.
func TestSettingLockHelperProcess(t *testing.T) {
	path := os.Getenv(settingLockProcessEnv)
	if path == "" {
		t.Skip("only used by the tests for locks across processes")
	}
	t.Run("read_succeeds", func(t *testing.T) {
		// Given
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// When
		value, err := Read(ctx, path, ProfilingFocus)
		// Then
		require.NoError(t, err)
		assert.Equal(t, "mobile", value)
	})
	t.Run("read_times_out", func(t *testing.T) {
		// Given
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		// When
		_, err := Read(ctx, path, ProfilingFocus)
		// Then
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
	t.Run("write_times_out", func(t *testing.T) {
		// Given
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		// When
		err := Write(ctx, path, ProfilingFocus, "server")
		// Then
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestSavedFocusCanBeReadBack(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{name: "mobile", value: "mobile"},
		{name: "server", value: "server"},
		{name: "explicit opt-out", value: "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), "config", Filename)
			// When
			err := Write(context.Background(), path, ProfilingFocus, tc.value)
			actual, readErr := Read(context.Background(), path, ProfilingFocus)
			// Then
			require.NoError(t, err)
			require.NoError(t, readErr)
			assert.Equal(t, tc.value, actual)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.JSONEq(t, `{"profilingFocus":"`+tc.value+`"}`, string(data))
		})
	}
}

func TestReadingMissingSettingsFileReturnsUnset(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	// When
	actual, err := Read(context.Background(), path, ProfilingFocus)
	// Then
	require.NoError(t, err)
	assert.Nil(t, actual)
}

func TestReadingFileWithoutSavedFocusReturnsUnset(t *testing.T) {
	for _, tc := range []struct{ name, contents string }{
		{name: "empty file", contents: ""},
		{name: "whitespace", contents: " \n\t"},
		{name: "missing property", contents: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			require.NoError(t, os.WriteFile(path, []byte(tc.contents), perms.LocalFilePerm))
			// When
			actual, err := Read(context.Background(), path, ProfilingFocus)
			// Then
			require.NoError(t, err)
			assert.Nil(t, actual)
		})
	}
}

func TestReadingSavedFocusFromFile(t *testing.T) {
	for _, focus := range []string{"mobile", "server", "none"} {
		t.Run(focus, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			require.NoError(t, os.WriteFile(path, []byte(`{"profilingFocus":"`+focus+`"}`), perms.LocalFilePerm))
			// When
			actual, err := Read(context.Background(), path, ProfilingFocus)
			// Then
			require.NoError(t, err)
			assert.Equal(t, focus, actual)
		})
	}
}

func TestReadingInvalidSettingsFileReportsParseError(t *testing.T) {
	for _, tc := range []struct{ name, contents string }{
		{name: "malformed JSON", contents: `{`},
		{name: "invalid value", contents: `{"profilingFocus":"cloud"}`},
		{name: "empty string", contents: `{"profilingFocus":""}`},
		{name: "wrong type", contents: `{"profilingFocus":42}`},
		{name: "null value", contents: `{"profilingFocus":null}`},
		{name: "null document", contents: `null`},
		{name: "array document", contents: `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			require.NoError(t, os.WriteFile(path, []byte(tc.contents), perms.LocalFilePerm))
			// When
			_, err := Read(context.Background(), path, ProfilingFocus)
			// Then
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineSettingsParseFailed, msg.Code())
			assert.Equal(t, path, msg.Metadata()["path"])
		})
	}
}

func TestInvalidFocusDoesNotReplaceSavedChoice(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	require.NoError(t, Write(context.Background(), path, ProfilingFocus, "mobile"))
	// When
	err := Write(context.Background(), path, ProfilingFocus, "cloud")
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsInvalidValue, msg.Code())
	assert.Equal(t, ProfilingFocus, msg.Metadata()["name"])
	actual, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", actual)
}

func TestReadingSettingsReportsFileReadFailure(t *testing.T) {
	// Given
	path := t.TempDir()
	// When
	_, err := Read(context.Background(), path, ProfilingFocus)
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsReadFailed, msg.Code())
	assert.Equal(t, path, msg.Metadata()["path"])
	var cause *os.PathError
	assert.ErrorAs(t, msg.Unwrap(), &cause)
}

func TestSavingSettingsReportsFileReadFailure(t *testing.T) {
	// Given
	path := t.TempDir()
	// When
	err := Write(context.Background(), path, ProfilingFocus, "server")
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsReadFailed, msg.Code())
	assert.Equal(t, path, msg.Metadata()["path"])
	var cause *os.PathError
	assert.ErrorAs(t, msg.Unwrap(), &cause)
}

func TestSavingSettingsReportsDirectoryCreationFailure(t *testing.T) {
	// Given
	parent := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(parent, nil, perms.LocalFilePerm))
	path := filepath.Join(parent, Filename)
	// When
	err := Write(context.Background(), path, ProfilingFocus, "server")
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsWriteFailed, msg.Code())
	assert.Equal(t, path, msg.Metadata()["path"])
	var cause *os.PathError
	assert.ErrorAs(t, msg.Unwrap(), &cause)
}

func TestSavingSettingKeepsOtherSettings(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	initial := `{"profilingFocus":"mobile","other":true,"future":{"count":9007199254740993}}`
	require.NoError(t, os.WriteFile(path, []byte(initial), perms.LocalFilePerm))
	// When
	err := Write(context.Background(), path, ProfilingFocus, "server")
	// Then
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"profilingFocus":"server","other":true,"future":{"count":9007199254740993}}`, string(data))
	assert.Contains(t, string(data), "9007199254740993")
}

func TestReadingSettingRejectsUnknownNames(t *testing.T) {
	for _, tc := range []struct{ label, name string }{
		{label: "empty name", name: ""},
		{label: "unknown name", name: "unknown"},
		{label: "path traversal", name: "../other"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			// When
			_, err := Read(context.Background(), path, tc.name)
			// Then
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineSettingsInvalidName, msg.Code())
			assert.Equal(t, tc.name, msg.Metadata()["name"])
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr))
		})
	}
}

func TestSavingSettingRejectsUnknownNames(t *testing.T) {
	for _, tc := range []struct{ label, name string }{
		{label: "empty name", name: ""},
		{label: "unknown name", name: "unknown"},
		{label: "path traversal", name: "../other"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			// When
			err := Write(context.Background(), path, tc.name, "mobile")
			// Then
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineSettingsInvalidName, msg.Code())
			assert.Equal(t, tc.name, msg.Metadata()["name"])
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr))
		})
	}
}

func TestClearingSettingRejectsUnknownNames(t *testing.T) {
	for _, tc := range []struct{ label, name string }{
		{label: "empty name", name: ""},
		{label: "unknown name", name: "unknown"},
		{label: "path traversal", name: "../other"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			// When
			err := Write(context.Background(), path, tc.name, nil)
			// Then
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineSettingsInvalidName, msg.Code())
			assert.Equal(t, tc.name, msg.Metadata()["name"])
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr))
		})
	}
}

func TestSettingRejectsInvalidTypesWithoutReplacingFile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "boolean", value: true},
		{name: "number", value: 42.0},
		{name: "empty string", value: ""},
		{name: "array", value: []any{"mobile"}},
		{name: "object", value: map[string]any{"focus": "mobile"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			initial := []byte(`{"profilingFocus":"server"}`)
			require.NoError(t, os.WriteFile(path, initial, perms.LocalFilePerm))
			// When
			err := Write(context.Background(), path, ProfilingFocus, tc.value)
			// Then
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineSettingsInvalidValue, msg.Code())
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, initial, data)
		})
	}
}

func TestSettingDoesNotOverwriteMalformedFile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "save", value: "server"},
		{name: "clear", value: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			initial := []byte(`{"broken":`)
			require.NoError(t, os.WriteFile(path, initial, perms.LocalFilePerm))
			// When
			err := Write(context.Background(), path, ProfilingFocus, tc.value)
			// Then
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineSettingsParseFailed, msg.Code())
			assert.Equal(t, path, msg.Metadata()["path"])
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, initial, data)
		})
	}
}

func TestClearingSettingRemovesOnlyThatSetting(t *testing.T) {
	for _, tc := range []struct{ name, contents, want string }{
		{name: "saved choice", contents: `{"profilingFocus":"mobile","other":true,"future":{"count":9007199254740993}}`, want: `{"other":true,"future":{"count":9007199254740993}}`},
		{name: "only setting", contents: `{"profilingFocus":"none"}`, want: `{}`},
		{name: "already unset", contents: `{"other":true}`, want: `{"other":true}`},
		{name: "empty file", want: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), Filename)
			require.NoError(t, os.WriteFile(path, []byte(tc.contents), perms.LocalFilePerm))
			// When
			err := Write(context.Background(), path, ProfilingFocus, nil)
			// Then
			require.NoError(t, err)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(data))
			assert.NotContains(t, string(data), ProfilingFocus)
			actual, err := Read(context.Background(), path, ProfilingFocus)
			require.NoError(t, err)
			assert.Nil(t, actual)
		})
	}
}

func TestClearingSettingWhenFileIsMissingCreatesEmptyFile(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), Filename)
	// When
	err := Write(context.Background(), path, ProfilingFocus, nil)
	// Then
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(data))
	actual, err := Read(context.Background(), path, ProfilingFocus)
	require.NoError(t, err)
	assert.Nil(t, actual)
}
