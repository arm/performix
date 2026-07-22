// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build darwin || linux

package fsutil

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func TestUnixFSManager_CreateTempDir(t *testing.T) {
	tests := []struct {
		name    string
		setupFS func(t *testing.T) afero.Fs
		wantErr bool
	}{
		{
			name: "succeeds",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			wantErr: false,
		},
		{
			name: "fails when fs not writable",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewReadOnlyFs(afero.NewMemMapFs())
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := tc.setupFS(t)
			mgr := &UnixFSManager{fs: fs}
			dir, err := mgr.CreateTempDir()

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// verify prefix
			expectedPrefix := fmt.Sprintf("%v-", terminology.GetAgentBinaryName())
			if !strings.HasPrefix(filepath.Base(dir), expectedPrefix) {
				t.Errorf("expected dir name to have prefix %q, got %q", expectedPrefix, filepath.Base(dir))
			}

			// verify it exists and is directory
			info, statErr := fs.Stat(dir)
			if statErr != nil {
				t.Fatalf("expected dir to exist, stat error: %v", statErr)
			} else if !info.IsDir() {
				t.Fatalf("expected %q to be directory, got mode: %#v", dir, info.Mode())
			}

			// verify it is traversable by all (execute bit set for user/group/other)
			perm := info.Mode().Perm()
			if perm&0o111 != 0o111 {
				t.Fatalf("expected temp dir %q to be traversable by all (x bits set), got perms %#o", dir, perm)
			}
		})
	}
}

func TestUnixFSManager_Mkdir(t *testing.T) {
	tests := []struct {
		name    string
		setupFS func(t *testing.T) afero.Fs
		path    string
		wantErr bool
	}{
		{
			name: "succeeds",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			path:    "/some/dir",
			wantErr: false,
		},
		{
			name: "fails when file of the same name exists",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := afero.WriteFile(fs, "/some/dir", []byte("data"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:    "/some/dir",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := tc.setupFS(t)
			fm := &UnixFSManager{fs: fs}
			err := fm.Mkdir(tc.path)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			info, err := fs.Stat(tc.path)
			if err != nil {
				t.Fatalf("stat failed: %v", err)
			}
			if !info.IsDir() {
				t.Fatalf("expected a directory, got %#v", info.Mode())
			}
			if perm := info.Mode().Perm(); perm != perms.LocalDirPerm {
				t.Errorf("expected perms %s, got %#o", perms.LocalDirPermStr, perm)
			}
		})
	}
}

func TestUnixFSManager_Rm(t *testing.T) {
	tests := []struct {
		name      string
		setupFS   func(t *testing.T) afero.Fs
		path      string
		recursive bool
		force     bool
		wantErr   bool
	}{
		{
			name: "succeeds when removing existing file",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := afero.WriteFile(fs, "foo.txt", []byte("x"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "foo.txt",
			recursive: false,
			force:     false,
			wantErr:   false,
		},
		{
			name: "fails when missing file with no force",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			path:      "nofile",
			recursive: false,
			force:     false,
			wantErr:   true,
		},
		{
			name: "succeds when missing file with force",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			path:      "nofile",
			recursive: false,
			force:     true,
			wantErr:   false,
		},
		{
			name: "fails when removing dir without force",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := fs.MkdirAll("d/sub", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup: mkdir: %v", err)
				}
				if err := afero.WriteFile(fs, filepath.Join("d", "sub", "f"), []byte{}, perms.LocalFilePerm); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "d",
			recursive: false,
			force:     false,
			wantErr:   true,
		},
		{
			name: "succeeds when removing dir recursively",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := fs.MkdirAll("d/sub", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup: mkdir: %v", err)
				}
				if err := afero.WriteFile(fs, filepath.Join("d", "sub", "f"), []byte{}, perms.LocalFilePerm); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "d",
			recursive: true,
			force:     false,
			wantErr:   false,
		},
		{
			name: "succeeds when removing existing file recursively",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := afero.WriteFile(fs, "foo.txt", []byte("x"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "foo.txt",
			recursive: true,
			force:     false,
			wantErr:   false,
		},
		{
			name:      "fails when removing non-existent path recursively without force",
			setupFS:   func(t *testing.T) afero.Fs { return afero.NewMemMapFs() },
			path:      "doesnotexist",
			recursive: true,
			force:     false,
			wantErr:   true,
		},
		{
			name: "succeeds when removing non-empty dir recursively",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := fs.MkdirAll("d/sub", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup: mkdir: %v", err)
				}
				if err := afero.WriteFile(fs, filepath.Join("d", "sub", "f"), []byte{}, perms.LocalFilePerm); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "d",
			recursive: true,
			force:     false,
			wantErr:   false,
		},
		{
			name: "succeeds when missing dir with recursive and force",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			path:      "d",
			recursive: true,
			force:     true,
			wantErr:   false,
		},
		{
			name: "fails if the path has insuficcent permissions",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewReadOnlyFs(afero.NewMemMapFs())
			},
			path:      "d/permission",
			recursive: true,
			force:     false,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := tc.setupFS(t)

			mgr := &UnixFSManager{fs: fs}
			err := mgr.Rm(tc.path, tc.recursive, tc.force)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestUnixFSManager_MakeWritable(t *testing.T) {
	tests := []struct {
		name      string
		setupFS   func(t *testing.T) afero.Fs
		path      string
		recursive bool
		wantErr   bool
	}{
		{
			name: "fails when missing path",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			path:      "nofile",
			recursive: false,
			wantErr:   true,
		},
		{
			name: "succeeds when making single file writable",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := afero.WriteFile(fs, "f", []byte("x"), 0400); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "f",
			recursive: false,
			wantErr:   false,
		},
		{
			name: "succeeds when making single dir writable",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := fs.Mkdir("d", 0500); err != nil {
					t.Fatalf("setup: mkdir: %v", err)
				}
				return fs
			},
			path:      "d",
			recursive: false,
			wantErr:   false,
		},
		{
			name: "succeeds when making tree writable recursively",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := fs.MkdirAll("d/sub", 0500); err != nil {
					t.Fatalf("setup: mkdir: %v", err)
				}
				if err := afero.WriteFile(fs, filepath.Join("d", "sub", "f"), []byte("x"), 0400); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "d",
			recursive: true,
			wantErr:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := tc.setupFS(t)
			mgr := &UnixFSManager{fs: fs}
			err := mgr.MakeWritable(tc.path, tc.recursive)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check that the path got write bits
			info, statErr := fs.Stat(tc.path)
			if statErr != nil {
				t.Fatalf("stat failed: %v", statErr)
			}
			if info.Mode().Perm()&0o222 == 0 {
				t.Errorf("expected write bits on %q, got perms %#o",
					tc.path, info.Mode().Perm())
			}

			// If recursive, ensure all children got write bits too
			if tc.recursive {
				_ = afero.Walk(fs, tc.path, func(p string, fi os.FileInfo, err error) error {
					if err != nil {
						t.Fatalf("walk failed: %v", err)
					}
					if fi.Mode().Perm()&0o222 == 0 {
						t.Errorf("expected write bits on %q, got perms %#o",
							p, fi.Mode().Perm())
					}
					return nil
				})
			}
		})
	}
}

func TestUnixFSManager_Chown(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatalf("cannot lookup current user: %v", err)
	}
	validUser := current.Username

	tests := []struct {
		name      string
		setupFS   func(t *testing.T) afero.Fs
		path      string
		owner     string
		recursive bool
		wantErr   bool
	}{
		{
			name: "fails when cannot find user",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			path:      "any",
			owner:     "no_such_user",
			recursive: false,
			wantErr:   true,
		},
		{
			name: "fails when missing file",
			setupFS: func(t *testing.T) afero.Fs {
				return afero.NewMemMapFs()
			},
			path:      "nofile",
			owner:     validUser,
			recursive: false,
			wantErr:   true,
		},
		{
			name: "succeeds on single file",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := afero.WriteFile(fs, "f", []byte("x"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "f",
			owner:     validUser,
			recursive: false,
			wantErr:   false,
		},
		{
			name: "succeeds on dir non-recursively",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := fs.Mkdir("d", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup: mkdir: %v", err)
				}
				return fs
			},
			path:      "d",
			owner:     validUser,
			recursive: false,
			wantErr:   false,
		},
		{
			name: "succeeds on dir recursively",
			setupFS: func(t *testing.T) afero.Fs {
				fs := afero.NewMemMapFs()
				if err := fs.MkdirAll("d/sub", perms.LocalDirPerm); err != nil {
					t.Fatalf("setup: mkdir: %v", err)
				}
				if err := afero.WriteFile(fs, filepath.Join("d", "sub", "f"), []byte("x"), perms.LocalFilePerm); err != nil {
					t.Fatalf("setup: write file: %v", err)
				}
				return fs
			},
			path:      "d",
			owner:     validUser,
			recursive: true,
			wantErr:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := tc.setupFS(t)
			mgr := &UnixFSManager{fs: fs}
			err := mgr.Chown(tc.path, tc.owner, tc.recursive)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestUnixFSManager_ListFiles(t *testing.T) {
	mgr := &UnixFSManager{}

	t.Run("returns err when glob has no matches", func(t *testing.T) {
		tmp := t.TempDir()
		pattern := filepath.Join(tmp, "no-such-*.doesnotexist")

		infos := mgr.ListFiles(pattern)
		require.Error(t, infos[0].Error)
		assert.ErrorIs(t, infos[0].Error, os.ErrNotExist)
	})

	t.Run("returns err when invalid glob", func(t *testing.T) {
		// Unmatched '[' is an invalid pattern for filepath.Glob
		invalid := "foo["
		infos := mgr.ListFiles(invalid)
		require.Error(t, infos[0].Error)
	})

	t.Run("returns complete file info for 1 file", func(t *testing.T) {
		tmp := t.TempDir()

		// Create one file with known content and mode (chmod to avoid umask effects).
		fp := filepath.Join(tmp, "one.txt")
		data := []byte("hello\n") // 6 bytes
		require.NoError(t, os.WriteFile(fp, data, 0o600))
		require.NoError(t, os.Chmod(fp, 0o600))

		startMs := time.Now().Add(-2 * time.Minute).UnixMilli()
		infos := mgr.ListFiles(fp)
		require.NoError(t, infos[0].Error)
		require.Len(t, infos, 1)

		fi := infos[0]
		assert.Equal(t, fp, fi.Path)
		assert.False(t, fi.IsDir)
		assert.EqualValues(t, len(data), fi.Size)
		assert.InDelta(t, time.Now().UnixMilli(), fi.Mtime, float64(2*time.Minute.Milliseconds()))
		assert.Greater(t, fi.Atime, int64(0))
		assert.Greater(t, fi.Ctime, int64(0))
		assert.GreaterOrEqual(t, fi.Mtime, startMs)
		assert.EqualValues(t, uint32(0o600), fi.Mode)
		assert.NoError(t, fi.Error)

		// Owner/Group: best-effort checks. Lookup may fail in some CI images.
		curUID := fmt.Sprint(os.Getuid())
		if u, e := user.LookupId(curUID); e == nil {
			assert.Equal(t, u.Username, fi.Owner, "owner should be current user")
		} else {
			assert.Empty(t, fi.Owner, "owner may be empty if LookupId fails")
		}
		curGID := fmt.Sprint(os.Getgid())
		if g, e := user.LookupGroupId(curGID); e == nil {
			assert.Equal(t, g.Name, fi.Group, "group should be current group")
		} else {
			assert.Empty(t, fi.Group, "group may be empty if LookupGroupId fails")
		}
	})

	t.Run("returns correct name and file list for multiple files incl. subdirs", func(t *testing.T) {
		tmp := t.TempDir()

		writeFile := func(path string) {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
			require.NoError(t, os.WriteFile(path, []byte(""), perms.LocalFilePerm))
		}

		// Layout:
		// tmp/
		//   top.txt
		//   sub1/b.txt
		//   sub1/c.log
		//   sub2/depth/d.txt
		writeFile(filepath.Join(tmp, "top.txt"))
		writeFile(filepath.Join(tmp, "sub1", "b.txt"))
		writeFile(filepath.Join(tmp, "sub2", "c.txt"))
		writeFile(filepath.Join(tmp, "sub2", "depth", "d.txt"))

		// Use a glob that matches *.txt files one level below tmp (i.e., in subdirs),
		// ensuring "some are in subdirs" and excluding the top-level file.
		pattern := filepath.Join(tmp, "**", "*.txt")

		infos := mgr.ListFiles(pattern)
		require.NoError(t, infos[0].Error)

		findRes := func(path string) {
			for _, r := range infos {
				if r.Path == path {
					return
				}
			}
			assert.Fail(t, "failed to find path %s in results", path)
		}

		findRes(filepath.Join(tmp, "top.txt"))
		findRes(filepath.Join(tmp, "sub1", "b.txt"))
		findRes(filepath.Join(tmp, "sub2", "c.txt"))
		findRes(filepath.Join(tmp, "sub2", "depth", "d.txt"))
	})
}
