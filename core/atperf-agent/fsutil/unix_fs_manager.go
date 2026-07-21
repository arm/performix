// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

// unix_fs_manager.go
// Build on Linux and Darwin

package fsutil

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/bmatcuk/doublestar"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

type UnixFSManager struct {
	fs afero.Fs
}

// CreateTempDir creates a new temp directory with prefix
// "apx-agent-" in the file system's default tmp directory.
// On success it returns the full path of the new temp directory.
//
// We chmod the directory to 0711 to ensure it is traversable by all users
// (execute bit set for group/other) even when the agent is running with a
// restrictive umask.
//
// Some downstream tooling needs to access paths beneath this directory as a
// different user (i.e. not the agent's UID). For example, tools that are run by
// arbitrary users might need access, such as a Java runtime injected with the JVM
// agent writing jitdump files into a subdirectory.
func (fm *UnixFSManager) CreateTempDir() (string, error) {
	dir, err := createTempDir(fm.fs)
	if err != nil {
		return "", err
	}
	if chmodErr := fm.fs.Chmod(dir, perms.TargetAgentTempDirPerm); chmodErr != nil {
		return "", fmt.Errorf("chmod error for temp dir %q: %v", dir, chmodErr)
	}
	return dir, nil
}

// Mkdir creates a single directory at path with 0755 perms.
func (fm *UnixFSManager) Mkdir(path string) error {
	path = normalizePath(path)

	return mkdir(fm.fs, path)
}

// Rm removes the given path. The recursive param acts like rm's -r flag
// (remove dirs & contents). The force param acts (partially) like rm's
// -f flag (ignore “not found” errors)
func (fm *UnixFSManager) Rm(path string, recursive bool, force bool) error {
	path = normalizePath(path)

	return rm(fm.fs, path, recursive, force)
}

// MakeWritable changes the permissions of the given path so it is writable.
// If the given path is a directory, the traversal (execution) bit will also be enabled
// If recursive is true and path is a directory, it will descend into all children.
func (fm *UnixFSManager) MakeWritable(path string, recursive bool) error {
	path = normalizePath(path)

	info, err := fm.fs.Stat(path)
	if err != nil {
		return fmt.Errorf("stat error for path %q: %v", path, err)
	}

	addWrite := func(p string, fi os.FileInfo) error {
		newPerm := fi.Mode().Perm() | 0o222

		// Additionallty set traversal (execution) bit for directories only
		if fi.IsDir() {
			newPerm |= perms.DirTraverseMask
		}
		err := fm.fs.Chmod(p, newPerm)
		if err != nil {
			return fmt.Errorf("chmod error for path %q: %v", p, err)
		}
		return nil
	}

	if recursive && info.IsDir() {
		return afero.Walk(fm.fs, path, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("walk error for path %q: %v", p, err)
			}
			return addWrite(p, fi)
		})
	}

	return addWrite(path, info)
}

// Chown sets ownership of path to the given user (using their primary GID).
// If recursive is true and path is a directory, it will recurse into all children.
func (fm *UnixFSManager) Chown(path string, owner string, recursive bool) error {
	path = normalizePath(path)

	usr, err := user.Lookup(owner)
	if err != nil {
		return fmt.Errorf("lookup error for user %q: %v", owner, err)
	}
	uid, err := strconv.Atoi(usr.Uid)
	if err != nil {
		return fmt.Errorf("parse error for uid %q: %v", usr.Uid, err)
	}
	gid, err := strconv.Atoi(usr.Gid)
	if err != nil {
		return fmt.Errorf("parse error for gid %q: %v", usr.Gid, err)
	}

	chownFunc := func(p string) error {
		if err := fm.fs.Chown(p, uid, gid); err != nil {
			return fmt.Errorf("chown error for path %q: %v", p, err)
		}
		return nil
	}

	if recursive {
		info, err := fm.fs.Stat(path)
		if err != nil {
			return fmt.Errorf("stat error for path %q: %v", path, err)
		}
		if info.IsDir() {
			return afero.Walk(fm.fs, path, func(p string, fi os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return fmt.Errorf("walk error for path %q: %v", p, walkErr)
				}
				return chownFunc(p)
			})
		}

	}

	return chownFunc(path)
}

// ListFiles returns information about the file at path, globs are expanded and all matching files are returned.
func (*UnixFSManager) ListFiles(path string) []FileInfo {
	matches, err := doublestar.Glob(path)
	if err != nil {
		return []FileInfo{{Path: path, Error: err}}
	}
	if len(matches) == 0 {
		return []FileInfo{{Path: path, Error: os.ErrNotExist}}
	}

	infos := make([]FileInfo, len(matches))
	var u *user.User
	var g *user.Group
	for i, match := range matches {
		stat, err := os.Lstat(match)
		if err != nil {
			infos[i].Error = err
			continue
		}

		sys := stat.Sys().(*syscall.Stat_t)

		owner := ""
		if u, infos[i].Error = user.LookupId(fmt.Sprint(sys.Uid)); infos[i].Error == nil {
			owner = u.Username
		}
		group := ""
		if g, infos[i].Error = user.LookupGroupId(fmt.Sprint(sys.Gid)); infos[i].Error == nil {
			group = g.Name
		}

		atime, ctime := statTimes(sys)
		infos[i] = FileInfo{
			Path:  match,
			IsDir: stat.IsDir(),
			Size:  stat.Size(),
			Mtime: stat.ModTime().UnixMilli(),
			Atime: atime,
			Ctime: ctime,
			Owner: owner,
			Group: group,
			Mode:  uint32(stat.Mode().Perm()),
		}
	}

	return infos
}

func NewFSManager() FSManager {
	return &UnixFSManager{fs: afero.NewOsFs()}
}

// NormalizePath returns the path modified with Linux separators (slashes)
func normalizePath(path string) string {
	return filepath.ToSlash(path)
}
