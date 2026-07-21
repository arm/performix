// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package fsutil

import (
	"errors"
	"fmt"
	iofs "io/fs"

	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

// FileInfo contains information about a file or directory on the target system.
type FileInfo struct {
	Path  string
	IsDir bool
	Size  int64
	Mtime int64 // epoch millis
	Atime int64 // epoch millis
	Ctime int64 // epoch millis
	Owner string
	Group string
	Mode  uint32 // unix-style permission bits
	Error error
}

// FSManager contains methods for file system management on the target.
// Common implementations for all OS'es should be placed here, whilst OS specific implementation should go
// in their respective file.
type FSManager interface {
	CreateTempDir() (string, error)
	Mkdir(path string) error
	Rm(path string, recursive bool, force bool) error
	MakeWritable(path string, recursive bool) error
	Chown(path string, owner string, recursive bool) error
	ListFiles(path string) []FileInfo
}

type CommonFSManager struct{}

func (*CommonFSManager) CreateTempDir() (string, error) {
	// Stubbed method
	return "", fmt.Errorf("CreateTempDir is not implemented on this platform")
}

func (*CommonFSManager) Mkdir(path string) error {
	// Stubbed method
	return fmt.Errorf("Mkdir is not implemented on this platform")
}

func (*CommonFSManager) Rm(path string, recursive bool, force bool) error {
	// Stubbed method
	return fmt.Errorf("Rm is not implemented on this platform")
}

func (*CommonFSManager) MakeWritable(path string, recursive bool) error {
	// Stubbed method
	return fmt.Errorf("MakeWritable is not implemented on this platform")
}

func (*CommonFSManager) Chown(path string, owner string, recursive bool) error {
	// Stubbed method
	return fmt.Errorf("Chown is not implemented on this platform")
}

func (*CommonFSManager) ListFiles(path string) []FileInfo {
	// Stubbed method
	return []FileInfo{{Error: fmt.Errorf("ListFiles is not implemented on this platform")}}
}

//nolint:unused
func createTempDir(fs afero.Fs) (string, error) {
	dir, err := afero.TempDir(fs, "", fmt.Sprintf("%v-", terminology.GetAgentBinaryName()))
	if err != nil {
		return "", message.New(message.AgentFsutilCreateTempDirFailed).WithMetadata(map[string]string{"path": dir}).
			WithCause(err)
	}
	return dir, nil
}

//nolint:unused
func mkdir(fs afero.Fs, path string) error {
	if err := fs.Mkdir(path, perms.LocalDirPerm); err != nil {
		return message.New(message.AgentFsutilMkdirFailed).
			WithMetadata(map[string]string{"path": path}).
			WithCause(err)
	}
	return nil
}

//nolint:unused
func rm(fs afero.Fs, path string, recursive bool, force bool) error {
	if !force || !recursive {
		info, err := fs.Stat(path)
		if err != nil {
			if force && errors.Is(err, iofs.ErrNotExist) {
				return nil
			}
			return message.New(message.AgentFsutilRmStatFailed).
				WithMetadata(map[string]string{"path": path}).
				WithCause(err)
		}

		if !recursive && info.IsDir() {
			return message.New(message.AgentFsutilRmDirNonRecursive).
				WithMetadata(map[string]string{"path": path})
		}
	}

	remove := fs.Remove
	if recursive {
		remove = fs.RemoveAll
	}

	if err := remove(path); err != nil {
		if errors.Is(err, iofs.ErrPermission) {
			return message.New(message.AgentFsutilCommonPermissionError).
				WithMetadata(map[string]string{"operation": "remove", "path": path}).
				WithCause(err)
		}

		if force && errors.Is(err, iofs.ErrNotExist) {
			return nil
		}
		return message.New(message.AgentFsutilRmFailed).
			WithMetadata(map[string]string{"path": path}).
			WithCause(err)
	}
	return nil
}
