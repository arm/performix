// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

// newTestFilePortWriter makes a file port writer with temp dir/file
func newTestFilePortWriter(t *testing.T) (*FilePortWriter, string) {
	t.Helper()
	dir := t.TempDir()
	filename := filepath.Join(dir, "performix-test.port")
	return &FilePortWriter{
		filename: filename,
		rename:   os.Rename,
	}, filename
}

// readFileIfExists reads the file's contents if present
func readFileIfExists(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestWriteCreatesFileWithContentAndPerms(t *testing.T) {
	w, fname := newTestFilePortWriter(t)

	require.NoError(t, w.Write(12345))

	got, err := readFileIfExists(fname)
	require.NoError(t, err)
	assert.Equal(t, "12345\n", got)

	// Check permissions (when not on Windows)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(fname)
		require.NoError(t, err)
		assert.Equal(t, perms.TargetAgentSocketPerm, info.Mode().Perm())
	}
}

func TestWriteReplacesExistingFile(t *testing.T) {
	w, fname := newTestFilePortWriter(t)

	require.NoError(t, w.Write(1111))
	require.NoError(t, w.Write(2222))

	got, err := readFileIfExists(fname)
	require.NoError(t, err)
	assert.Equal(t, "2222\n", got)

	entries, err := os.ReadDir(filepath.Dir(fname))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp")
	}
}

func TestWriteCleansUpOnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions differ on Windows")
	}

	dir := t.TempDir()
	roDir := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(roDir, perms.LocalDirPerm))

	w := &FilePortWriter{
		filename: filepath.Join(roDir, "performix-test.port"),
		rename:   os.Rename,
	}

	// Normal write should work
	require.NoError(t, w.Write(1))

	// Make dir read-only
	require.NoError(t, os.Chmod(roDir, 0o500))
	defer func() { _ = os.Chmod(roDir, perms.LocalDirPerm) }()

	// Attempt a failing write
	err := w.Write(2)
	assert.Error(t, err)

	// Confirm content is unchanged
	content, err := readFileIfExists(w.filename)
	require.NoError(t, err)
	assert.Equal(t, "1\n", content)

	// Ensure no *.tmp leftovers
	entries, err := os.ReadDir(roDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp")
	}
}

func TestWriteRenameFailure(t *testing.T) {
	w, fname := newTestFilePortWriter(t)
	w.rename = func(old, new string) error { return errors.New("rekt") }

	err := w.Write(1111)
	require.Error(t, err)

	entries, derr := os.ReadDir(filepath.Dir(fname))
	require.NoError(t, derr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "leftover temp file: %s", e.Name())
	}
}

func TestRemove(t *testing.T) {
	w, fname := newTestFilePortWriter(t)

	require.NoError(t, w.Remove())

	require.NoError(t, w.Write(8080))
	require.NoError(t, w.Remove())

	_, err := os.Stat(fname)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestNewFilePortWriter(t *testing.T) {
	w := NewFilePortWriter("").(*FilePortWriter)
	assert.Contains(t, w.filename, os.TempDir())
	assert.Regexp(t, fmt.Sprintf(`%v_\d+\.port$`, regexp.QuoteMeta(terminology.GetAgentBinaryName())), filepath.Base(w.filename))
}
func TestNewFilePortWriterWithDir(t *testing.T) {
	w := NewFilePortWriter("my_temp_dir").(*FilePortWriter)
	assert.Contains(t, w.filename, ("my_temp_dir"))
	assert.Regexp(t, fmt.Sprintf(`%v_\d+\.port$`, regexp.QuoteMeta(terminology.GetAgentBinaryName())), filepath.Base(w.filename))
}

func TestNullPortWriter(t *testing.T) {
	w := NewNullPortWriter()
	require.NoError(t, w.Write(1234))
	require.NoError(t, w.Remove())
}

func TestWrapPortWriter(t *testing.T) {
	t.Run("nil returns NullPortWriter", func(t *testing.T) {
		pw := WrapPortWriter(nil)
		_, isNull := pw.(*NullPortWriter)
		assert.True(t, isNull)
	})
	t.Run("non-nil is returned as-is", func(t *testing.T) {
		orig := NewLoggingPortWriter()
		pw := WrapPortWriter(orig)
		assert.Equal(t, orig, pw)
	})
}

func TestTerminalPortWriterWritesPort(t *testing.T) {
	var buf bytes.Buffer
	tw := &TerminalPortWriter{Out: &buf}
	require.NoError(t, tw.Write(4321))
	assert.Equal(t, "4321\n", buf.String())

	// Remove is a no-op
	require.NoError(t, tw.Remove())
}

func TestLoggingPortWriter_LogsInfoWithPortField(t *testing.T) {
	hook := test.NewGlobal()
	defer hook.Reset()

	pw := NewLoggingPortWriter()
	require.NoError(t, pw.Write(2468))

	// Remove is a no-op
	require.NoError(t, pw.Remove())

	e := hook.LastEntry()
	require.NotNil(t, e, "expected a log entry")
	assert.Equal(t, log.InfoLevel, e.Level)
	assert.Equal(t, "Server port chosen", e.Message)
	assert.Equal(t, 2468, e.Data["port"])
}

func TestCompositePortWriterNoErrors(t *testing.T) {
	m1 := &MockPortWriter{}
	m2 := &MockPortWriter{}

	port := 8080
	m1.On("Write", port).Return(nil).Once()
	m2.On("Write", port).Return(nil).Once()
	m1.On("Remove").Return(nil).Once()
	m2.On("Remove").Return(nil).Once()

	c := &CompositePortWriter{Writers: []PortWriter{m1, m2}}

	require.NoError(t, c.Write(port))
	require.NoError(t, c.Remove())

	m1.AssertExpectations(t)
	m2.AssertExpectations(t)
}

func TestCompositePortWriterWithErrors(t *testing.T) {
	e1 := errors.New("writer1 error")
	e2 := errors.New("writer2 error")

	m1 := &MockPortWriter{}
	m2 := &MockPortWriter{}
	m3 := &MockPortWriter{}

	port := 1234

	m1.On("Write", port).Return(e1).Once()
	m2.On("Write", port).Return(nil).Once()
	m3.On("Write", port).Return(e2).Once()

	m1.On("Remove").Return(e2).Once()
	m2.On("Remove").Return(nil).Once()
	m3.On("Remove").Return(e1).Once()

	c := &CompositePortWriter{Writers: []PortWriter{m1, m2, m3}}

	err := c.Write(port)
	require.Error(t, err)
	assert.ErrorIs(t, err, e1)
	assert.ErrorIs(t, err, e2)

	err = c.Remove()
	require.Error(t, err)
	assert.ErrorIs(t, err, e1)
	assert.ErrorIs(t, err, e2)

	m1.AssertExpectations(t)
	m2.AssertExpectations(t)
	m3.AssertExpectations(t)
}

func TestCompositePortWriterEmptyWriters(t *testing.T) {
	c := &CompositePortWriter{}
	require.NoError(t, c.Write(42))
	require.NoError(t, c.Remove())
}

func TestCompositePortWriterSkipsNil(t *testing.T) {
	m1 := new(MockPortWriter)
	m2 := new(MockPortWriter)

	port := 1234

	m1.On("Write", port).Return(nil).Once()
	m2.On("Write", port).Return(nil).Once()
	m1.On("Remove").Return(nil).Once()
	m2.On("Remove").Return(nil).Once()

	c := &CompositePortWriter{Writers: []PortWriter{m1, nil, m2}}

	require.NoError(t, c.Write(port))
	require.NoError(t, c.Remove())

	m1.AssertExpectations(t)
	m2.AssertExpectations(t)
}
