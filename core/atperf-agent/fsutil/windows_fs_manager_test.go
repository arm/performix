// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func newWindowsTestFSManager() *WindowsFSManager {
	return &WindowsFSManager{fs: afero.NewOsFs()}
}

func TestWindowsFSManagerCreateTempDir(t *testing.T) {
	fm := newWindowsTestFSManager()

	dir, err := fm.CreateTempDir()
	if err != nil {
		t.Fatalf("CreateTempDir failed: %v", err)
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatalf("expected temp dir %q to exist: %v", dir, statErr)
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
}

func TestWindowsFSManagerMkdirAndRm(t *testing.T) {
	fm := newWindowsTestFSManager()

	root := t.TempDir()
	target := filepath.Join(root, "nested", "deeper")

	if err := fm.Mkdir(filepath.Dir(target)); err != nil {
		t.Fatalf("Mkdir parent failed: %v", err)
	}
	if err := fm.Mkdir(target); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(target, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := fm.Rm(target, true, false); err != nil {
		t.Fatalf("Rm failed: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected directory %q to be removed, got err=%v", target, err)
	}
}

func TestWindowsFSManagerListFiles(t *testing.T) {
	fm := newWindowsTestFSManager()

	root := t.TempDir()
	target := filepath.Join(root, "file.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	results := fm.ListFiles(filepath.Join(root, "*.txt"))
	if len(results) == 0 {
		t.Fatalf("expected at least one match")
	}

	found := false
	for _, info := range results {
		if info.Path == target && info.Error == nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find file %q in glob results, got %+v", target, results)
	}
}

func TestWindowsFSManagerRmNonRecursiveDir(t *testing.T) {
	fm := newWindowsTestFSManager()

	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("failed to set up dir: %v", err)
	}

	if err := fm.Rm(dir, false, false); err == nil {
		t.Fatalf("expected error when removing directory without recursive flag")
	}
}

func TestWindowsFSManagerRmForceMissing(t *testing.T) {
	fm := newWindowsTestFSManager()

	missing := filepath.Join(t.TempDir(), "missing.txt")
	if err := fm.Rm(missing, false, true); err != nil {
		t.Fatalf("expected no error when force removing missing path, got %v", err)
	}
}

func TestWindowsFSManagerRmPermissionError(t *testing.T) {
	// We can't rely on pkg.go.dev/os or pkg.go.dev/github.com/spf13/afero
	// to set file permissions (i.e., `chmod 0o000`) on Windows as they do
	// not expose functions for modifying Windows NTFS Access Control Lists (ACLs).
	// See: https://learn.microsoft.com/en-us/windows-server/storage/file-server/ntfs-overview

	// So, instead of testing on NTFS, we rely on afero's ReadOnlyFs sandboxing
	// that is layerd on top of afero.MemMapFs to simulate permission errors.

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.Mkdir("readonly_dir", 0o000))
	t.Cleanup(func() { _ = fs.RemoveAll("readonly_dir") })

	readOnlyFs := afero.NewReadOnlyFs(fs)
	fm := WindowsFSManager{fs: readOnlyFs}

	err := fm.Rm("readonly_dir", true, true)
	require.Error(t, err, "expected permission error when removing read-only dir")

	var msgErr message.Message
	if !errors.As(err, &msgErr) || msgErr.Code() != message.AgentFsutilCommonPermissionError {
		t.Fatalf("expected catalog common permission error, got %v", err)
	}
}

func TestWindowsFSManagerMetadataFields(t *testing.T) {
	fm := newWindowsTestFSManager()

	root := t.TempDir()
	target := filepath.Join(root, "meta.bin")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	results := fm.ListFiles(target)
	if len(results) != 1 {
		t.Fatalf("expected exactly one result, got %d", len(results))
	}
	info := results[0]
	if info.Error != nil {
		t.Fatalf("unexpected error: %v", info.Error)
	}
	if info.Path != target {
		t.Fatalf("expected path %q, got %q", target, info.Path)
	}
	if info.Size <= 0 {
		t.Fatalf("expected size > 0, got %d", info.Size)
	}
	if info.Mtime == 0 {
		t.Fatalf("expected non-zero mtime")
	}
	if info.Mode == 0 {
		t.Fatalf("expected non-zero mode")
	}
}

func setWindowsReadonly(t *testing.T, path string) {
	t.Helper()

	ptr, err := windows.UTF16PtrFromString(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("utf16 error for %q: %v", path, err)
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil {
		t.Fatalf("get attrs error for %q: %v", path, err)
	}
	if err := windows.SetFileAttributes(ptr, attrs|windows.FILE_ATTRIBUTE_READONLY); err != nil {
		t.Fatalf("set readonly error for %q: %v", path, err)
	}
}

func assertWindowsReadonly(t *testing.T, path string) bool {
	t.Helper()

	ptr, err := windows.UTF16PtrFromString(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("utf16 error for %q: %v", path, err)
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil {
		t.Fatalf("get attrs error for %q: %v", path, err)
	}

	// Returns true if the FILE_ATTRIBUTE_READONLY flag is set on the path.
	// Callers can assert on the semantics:
	//   - expect not readonly: if !assertWindowsReadonly(...)
	//   - expect readonly:     if  assertWindowsReadonly(...)
	return attrs&windows.FILE_ATTRIBUTE_READONLY != 0
}

func TestWindowsFSManagerMakeWritableFile(t *testing.T) {
	fm := newWindowsTestFSManager()

	path := filepath.Join(t.TempDir(), "readonly.txt")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	setWindowsReadonly(t, path)

	if err := fm.MakeWritable(path, false); err != nil {
		t.Fatalf("MakeWritable failed: %v", err)
	}
	if readonly := assertWindowsReadonly(t, path); readonly {
		t.Fatalf("expected readonly flag to be cleared")
	}
}

func TestWindowsFSManagerRmMissingWithoutForce(t *testing.T) {
	fm := newWindowsTestFSManager()

	missing := filepath.Join(t.TempDir(), "missing.txt")
	if err := fm.Rm(missing, false, false); err == nil {
		t.Fatalf("expected error when removing missing file without force")
	}
}

func TestWindowsFSManagerListFilesNoMatch(t *testing.T) {
	fm := newWindowsTestFSManager()

	root := t.TempDir()
	results := fm.ListFiles(filepath.Join(root, "*.doesnotexist"))
	if len(results) != 1 {
		t.Fatalf("expected single result containing error, got %d", len(results))
	}
	if results[0].Error == nil || !os.IsNotExist(results[0].Error) {
		t.Fatalf("expected os.ErrNotExist, got %v", results[0].Error)
	}
}

func TestWindowsFSManagerListFilesGlobError(t *testing.T) {
	fm := newWindowsTestFSManager()

	results := fm.ListFiles("[")
	if len(results) != 1 {
		t.Fatalf("expected single glob error result, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Fatalf("expected glob error, got nil")
	}
}

func TestNormalizeWindowsPath(t *testing.T) {
	normalized := normalizeWindowsPath(`C:\temp\..\dir\file.txt`)
	if !filepath.IsAbs(normalized) {
		t.Fatalf("expected absolute path, got %q", normalized)
	}
	if filepath.Base(normalized) != "file.txt" {
		t.Fatalf("expected cleaned base file.txt, got %q", normalized)
	}
}

func TestFiletimeToMillis(t *testing.T) {
	ft := syscall.Filetime{
		LowDateTime:  3577643008,
		HighDateTime: 30877589,
	}
	if got := filetimeToMillis(ft); got == 0 {
		t.Fatalf("expected non-zero millis from filetime")
	}
	if filetimeToMillis(syscall.Filetime{}) != 0 {
		t.Fatalf("expected zero for empty filetime")
	}
}

func TestWindowsFSManagerRmFileNonRecursive(t *testing.T) {
	fm := newWindowsTestFSManager()
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	if err := fm.Rm(file, false, false); err != nil {
		t.Fatalf("Rm on file failed: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, got err=%v", err)
	}
}

func TestWindowsFSManagerMakeWritableRecursive(t *testing.T) {
	fm := newWindowsTestFSManager()
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	nestedDir := filepath.Join(dir, "child")
	nested := filepath.Join(nestedDir, "file.txt")

	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	if err := os.WriteFile(nested, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	setWindowsReadonly(t, dir)
	setWindowsReadonly(t, nestedDir)
	setWindowsReadonly(t, nested)

	if err := fm.MakeWritable(dir, true); err != nil {
		t.Fatalf("MakeWritable recursive failed: %v", err)
	}

	if readonly := assertWindowsReadonly(t, dir); readonly {
		t.Fatalf("expected readonly cleared for %q", dir)
	}
	if readonly := assertWindowsReadonly(t, nestedDir); readonly {
		t.Fatalf("expected readonly cleared for %q", nestedDir)
	}
	if readonly := assertWindowsReadonly(t, nested); readonly {
		t.Fatalf("expected readonly cleared for %q", nested)
	}
}

func TestWindowsFSManagerMakeWritableNonRecursiveDir(t *testing.T) {
	fm := newWindowsTestFSManager()
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	child := filepath.Join(dir, "child.txt")

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(child, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create child: %v", err)
	}

	setWindowsReadonly(t, dir)
	setWindowsReadonly(t, child)

	if err := fm.MakeWritable(dir, false); err != nil {
		t.Fatalf("MakeWritable non-recursive failed: %v", err)
	}

	if readonly := assertWindowsReadonly(t, dir); readonly {
		t.Fatalf("expected dir readonly cleared")
	}
	if readonly := assertWindowsReadonly(t, child); !readonly {
		t.Fatalf("expected child to remain readonly when not recursive")
	}
}

func TestWindowsFSManagerMakeWritableMissing(t *testing.T) {
	fm := newWindowsTestFSManager()
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	if err := fm.MakeWritable(missing, false); err == nil {
		t.Fatalf("expected error for missing path")
	}
}

func TestWindowsFSManagerMakeWritableAlreadyWritable(t *testing.T) {
	fm := newWindowsTestFSManager()
	path := filepath.Join(t.TempDir(), "already.txt")

	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	ptr, err := windows.UTF16PtrFromString(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("utf16 error: %v", err)
	}
	before, err := windows.GetFileAttributes(ptr)
	if err != nil {
		t.Fatalf("get attrs error: %v", err)
	}

	if err := fm.MakeWritable(path, false); err != nil {
		t.Fatalf("MakeWritable failed: %v", err)
	}

	after, err := windows.GetFileAttributes(ptr)
	if err != nil {
		t.Fatalf("get attrs error: %v", err)
	}
	if before != after {
		t.Fatalf("expected attrs unchanged for already writable file, before=%d after=%d", before, after)
	}
}

func TestWindowsFSManagerChownUnimplemented(t *testing.T) {
	fm := newWindowsTestFSManager()
	path := filepath.Join(t.TempDir(), "noop")

	if err := os.WriteFile(path, []byte("noop"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	if err := fm.Chown(path, "nobody", true); err.Error() != "Chown is not implemented on this platform" {
		t.Fatalf("expected Chown to return unimplemented error, got %v", err)
	}
}

type fakeFS struct {
	afero.Fs
	info os.FileInfo
}

func (f *fakeFS) Stat(name string) (os.FileInfo, error) {
	return f.info, nil
}

type walkErrorFs struct {
	afero.Fs
	failPath string
	statErr  error
}

func (f *walkErrorFs) Stat(name string) (os.FileInfo, error) {
	if filepath.Clean(name) == filepath.Clean(f.failPath) {
		return nil, f.statErr
	}
	return f.Fs.Stat(name)
}

func newWalkErrorFs(t *testing.T) (*walkErrorFs, string, string) {
	t.Helper()

	// Use the real filesystem so Windows attribute calls succeed.
	root := t.TempDir()
	badPath := filepath.Join(root, "bad.txt")

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("failed to create root dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("failed to write ok file: %v", err)
	}
	if err := os.WriteFile(badPath, []byte("bad"), 0o644); err != nil {
		t.Fatalf("failed to write bad file: %v", err)
	}

	return &walkErrorFs{
		Fs:       afero.NewOsFs(),
		failPath: badPath,
		statErr:  errors.New("stat failure"),
	}, root, badPath
}

func newFakeStatFs(t *testing.T, dir bool) *fakeFS {
	t.Helper()
	mem := afero.NewMemMapFs()
	name := "dummy"
	if dir {
		if err := mem.Mkdir(name, 0o755); err != nil {
			t.Fatalf("failed to create dummy dir: %v", err)
		}
	} else {
		if err := afero.WriteFile(mem, name, []byte{}, 0o644); err != nil {
			t.Fatalf("failed to create dummy file: %v", err)
		}
	}
	fi, err := mem.Stat(name)
	if err != nil {
		t.Fatalf("failed to stat dummy: %v", err)
	}
	return &fakeFS{Fs: mem, info: fi}
}

func TestWindowsFSManagerMakeWritableUTF16Error(t *testing.T) {
	fm := &WindowsFSManager{fs: newFakeStatFs(t, false)}
	// Embed a NUL to make UTF16PtrFromString fail.
	path := "C:\\invalid\x00path"

	if err := fm.MakeWritable(path, false); err == nil {
		t.Fatalf("expected utf16 error")
	}
}

func TestWindowsFSManagerMakeWritableGetAttrError(t *testing.T) {
	fm := &WindowsFSManager{fs: newFakeStatFs(t, false)}
	path := filepath.Join(t.TempDir(), "missing-for-attrs.txt")

	if err := fm.MakeWritable(path, false); err == nil {
		t.Fatalf("expected get attrs error for missing path")
	}
}

func TestWindowsFSManagerMakeWritableWalkError(t *testing.T) {
	fs, root, _ := newWalkErrorFs(t)
	fm := &WindowsFSManager{fs: fs}
	err := fm.MakeWritable(root, true)
	assert.Error(t, err, "expected walk error")
	msg, ok := err.(message.Message)
	assert.True(t, ok, "expected error to be of type message.Message")
	assert.Equal(t, message.AgentFsutilMakeWritableWalkFailed, msg.Code())
}
