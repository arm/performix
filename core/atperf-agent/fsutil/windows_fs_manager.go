// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bmatcuk/doublestar"
	"github.com/spf13/afero"
	"golang.org/x/sys/windows"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type WindowsFSManager struct {
	fs afero.Fs
}

func (fm *WindowsFSManager) CreateTempDir() (string, error) {
	dir, err := createTempDir(fm.fs)
	dir = normalizeWindowsPath(dir)
	return dir, err
}

func (fm *WindowsFSManager) Mkdir(path string) error {
	path = normalizeWindowsPath(path)

	return mkdir(fm.fs, path)
}

func (fm *WindowsFSManager) Rm(path string, recursive bool, force bool) error {
	path = normalizeWindowsPath(path)

	return rm(fm.fs, path, recursive, force)
}

func (fm *WindowsFSManager) MakeWritable(path string, recursive bool) error {
	path = normalizeWindowsPath(path)

	info, err := fm.fs.Stat(path)
	if err != nil {
		return message.New(message.AgentFsutilMakeWritableStatFailed).
			WithMetadata(map[string]string{"path": path}).
			WithCause(err)
	}

	clearReadOnly := func(p string) error {
		native := filepath.FromSlash(p)
		wptr, err := windows.UTF16PtrFromString(native)
		if err != nil {
			return message.New(message.AgentFsutilMakeWritableEncodePathFailed).
				WithMetadata(map[string]string{"path": p}).
				WithCause(err)
		}

		attrs, err := windows.GetFileAttributes(wptr)
		if err != nil {
			return message.New(message.AgentFsutilMakeWritableGetAttrFailed).
				WithMetadata(map[string]string{"path": p}).
				WithCause(err)
		}

		if attrs&windows.FILE_ATTRIBUTE_READONLY != 0 {
			attrs &^= windows.FILE_ATTRIBUTE_READONLY
			if err := windows.SetFileAttributes(wptr, attrs); err != nil {
				return message.New(message.AgentFsutilMakeWritableSetAttrFailed).
					WithMetadata(map[string]string{"path": p}).
					WithCause(err)
			}
		}
		return nil
	}

	if recursive && info.IsDir() {
		return afero.Walk(fm.fs, path, func(p string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return message.New(message.AgentFsutilMakeWritableWalkFailed).
					WithMetadata(map[string]string{"path": p}).
					WithCause(walkErr)
			}
			return clearReadOnly(p)
		})
	}

	return clearReadOnly(path)
}

func (fm *WindowsFSManager) Chown(path string, owner string, recursive bool) error {
	// TODO : Implement Chown for Windows (APAP-3045)
	return fmt.Errorf("Chown is not implemented on this platform")
}

func (fm *WindowsFSManager) ListFiles(path string) []FileInfo {
	matches, err := doublestar.Glob(path)
	if err != nil {
		return []FileInfo{{Path: path, Error: err}}
	}
	if len(matches) == 0 {
		return []FileInfo{{Path: path, Error: os.ErrNotExist}}
	}

	infos := make([]FileInfo, len(matches))
	for i, m := range matches {
		stat, err := os.Lstat(m)
		if err != nil {
			infos[i].Error = err
			continue
		}

		sys, ok := stat.Sys().(*syscall.Win32FileAttributeData)
		if !ok || sys == nil {
			infos[i].Error = message.New(message.AgentFsutilListFilesUnexpectedFileinfo).
				WithMetadata(map[string]string{"path": m}).
				WithCause(fmt.Errorf("unexpected fileinfo type %T", stat.Sys()))
			continue
		}

		infos[i] = FileInfo{
			Path:  m,
			IsDir: stat.IsDir(),
			Size:  stat.Size(),
			Mtime: filetimeToMillis(sys.LastWriteTime),
			Atime: filetimeToMillis(sys.LastAccessTime),
			Ctime: filetimeToMillis(sys.CreationTime),
			Mode:  uint32(stat.Mode().Perm()),
		}
	}

	return infos
}

func normalizeWindowsPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func filetimeToMillis(ft syscall.Filetime) int64 {
	if ft.LowDateTime == 0 && ft.HighDateTime == 0 {
		return 0
	}
	return time.Unix(0, ft.Nanoseconds()).UnixMilli()
}
