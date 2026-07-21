// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

// fakeBuilder implements just enough of RunBuilder for our test.
type fakeBuilder struct {
	dir string
}

func (f *fakeBuilder) AddComponent(ct cdf.ComponentType, filename string) string {
	return filepath.Join(f.dir, filename)
}

func createFakeProjectDir(fs afero.Fs) (string, error) {
	tempDir, err := afero.TempDir(fs, "", "")
	if err != nil {
		return "", err
	}

	paths := []string{
		filepath.Join(tempDir, "foo", "bar", "file1.c"),
		filepath.Join(tempDir, "foo", "file2.c"),
		filepath.Join(tempDir, "file3.c"),
	}

	for _, p := range paths {
		if err := fs.MkdirAll(filepath.Dir(p), perms.LocalDirPerm); err != nil {
			return "", err
		}
		if err := afero.WriteFile(fs, p, []byte(filepath.Base(p)), perms.LocalFilePerm); err != nil {
			return "", err
		}
	}

	// Create symlink pointing to the first file (arbitrary choice)
	symlinkName := filepath.Join(tempDir, "symlinked-file1.c")
	symlinkTarget, _ := filepath.Rel(tempDir, paths[0])
	if err := os.Symlink(symlinkTarget, symlinkName); err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(tempDir)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

func listProjectFiles(fs afero.Fs, root string) ([]string, error) {
	var files []string
	if err := afero.Walk(fs, root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	}); err != nil {
		return nil, err
	}
	return files, nil
}

func computeChecksum(fs afero.Fs, filePath string) (string, error) {
	f, err := fs.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func computeChecksums(fs afero.Fs, files []string) (map[string]string, error) {
	checksums := make(map[string]string, len(files))
	for _, file := range files {
		realPath, err := filepath.EvalSymlinks(file)
		if err != nil {
			return nil, err
		}
		f, err := fs.Open(realPath)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, f); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
		checksums[file] = hex.EncodeToString(hash.Sum(nil))
	}
	return checksums, nil
}

func TestHostSourceCodePaths(t *testing.T) {
	orig := &HostSourceCodePath{Paths: []string{"one", "two"}}

	t.Run("WriteHostSourceCodePath success", func(t *testing.T) {
		tmp := t.TempDir()
		target := filepath.Join(tmp, SourceCodeFilename)

		require.NoError(t, WriteHostSourceCodePath(target, orig))

		data, err := os.ReadFile(target)
		require.NoError(t, err)

		var got HostSourceCodePath
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, orig.Paths, got.Paths)
	})

	t.Run("WriteHostSourceCodePath error", func(t *testing.T) {
		tmp := t.TempDir()
		// create a subdirectory
		badDir := filepath.Join(tmp, "subdir")
		require.NoError(t, os.Mkdir(badDir, perms.LocalDirPerm))
		// now try to write a file _to_ that subdirectory path
		err := WriteHostSourceCodePath(badDir, orig)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write host source code path")
	})

	t.Run("ReadHostSourceCodePath success", func(t *testing.T) {
		tmp := t.TempDir()
		source := filepath.Join(tmp, "in.json")

		data, err := json.Marshal(orig)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(source, data, perms.LocalFilePerm))

		got, err := ReadHostSourceCodePath(source)
		require.NoError(t, err)
		assert.Equal(t, orig.Paths, got.Paths)
	})

	t.Run("ReadHostSourceCodePath error", func(t *testing.T) {
		tmp := t.TempDir()
		missing := filepath.Join(tmp, "missing.json")

		got, err := ReadHostSourceCodePath(missing)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read host source code path")
		require.NotNil(t, got)
		assert.Empty(t, got.Paths)
	})

	t.Run("CreateInitialHostSourceCodePath success", func(t *testing.T) {
		tmp := t.TempDir()
		fb := &fakeBuilder{dir: tmp}

		require.NoError(t, CreateInitialHostSourceCodePath(fb, orig))

		// file should have been created at tmp/SourceCodeFilename
		created := filepath.Join(tmp, SourceCodeFilename)
		data, err := os.ReadFile(created)
		require.NoError(t, err)

		var got HostSourceCodePath
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, orig.Paths, got.Paths)
	})

	t.Run("CreateInitialHostSourceCodePath error", func(t *testing.T) {
		tmp := t.TempDir()
		// make fb.dir point at a file so that AddComponent → path is invalid
		bad := filepath.Join(tmp, "badfile")
		require.NoError(t, os.WriteFile(bad, []byte{}, perms.LocalFilePerm))
		fb := &fakeBuilder{dir: bad}

		err := CreateInitialHostSourceCodePath(fb, orig)
		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "failed to write initial host source code path"), "got %v", err)
	})

	t.Run("Wipe host source code path", func(t *testing.T) {
		tmp := t.TempDir()
		target := filepath.Join(tmp, SourceCodeFilename)

		// Write initial non-empty source code
		orig := &HostSourceCodePath{Paths: []string{"foo", "bar"}}
		require.NoError(t, WriteHostSourceCodePath(target, orig))

		// Overwrite with blank source code
		blank := &HostSourceCodePath{Paths: []string{}}
		require.NoError(t, WriteHostSourceCodePath(target, blank))

		// Read back and assert it's blank
		data, err := os.ReadFile(target)
		require.NoError(t, err)

		var got HostSourceCodePath
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Empty(t, got.Paths)
	})
}

func TestSearchSourceFile(t *testing.T) {
	fs := afero.NewOsFs()

	// Fake project 1
	proj1TargetDir, err := createFakeProjectDir(fs)
	require.NoError(t, err)
	proj1TargetFiles, err := listProjectFiles(fs, proj1TargetDir)
	require.NoError(t, err)
	proj1Checksums, err := computeChecksums(fs, proj1TargetFiles)
	require.NoError(t, err)

	proj1HostDir, err := afero.TempDir(fs, "", "")
	require.NoError(t, err)
	require.NoError(t, fs.RemoveAll(proj1HostDir))
	require.NoError(t, fs.Rename(proj1TargetDir, proj1HostDir))

	// Fake project 2
	proj2TargetDir, err := createFakeProjectDir(fs)
	require.NoError(t, err)
	proj2TargetFiles, err := listProjectFiles(fs, proj2TargetDir)
	require.NoError(t, err)
	proj2Checksums, err := computeChecksums(fs, proj2TargetFiles)
	require.NoError(t, err)

	proj2HostDir, err := afero.TempDir(fs, "", "")
	require.NoError(t, err)
	require.NoError(t, fs.RemoveAll(proj2HostDir))
	require.NoError(t, fs.Rename(proj2TargetDir, proj2HostDir))

	// Cleanup
	defer func() {
		require.NoError(t, fs.RemoveAll(proj1HostDir))
		require.NoError(t, fs.RemoveAll(proj2HostDir))
	}()

	t.Run("returns original string if unable to find the source file ", func(t *testing.T) {
		sourceCode := HostSourceCodePath{Paths: []string{proj1HostDir}}

		remappedFile, found := SearchSourceFile(sourceCode, "idontexist")
		assert.Equal(t, "idontexist", remappedFile)
		assert.False(t, found)
	})

	t.Run("successfully evaluates symlinks", func(t *testing.T) {
		sourceCode := HostSourceCodePath{Paths: []string{proj1HostDir}}
		sourceFiles := append([]string{}, proj1TargetFiles...)

		var symlink string
		for _, f := range sourceFiles {
			if strings.Contains(f, "symlinked-file1.c") {
				symlink = f
				break
			}
		}
		require.NotNil(t, symlink)

		remappedFile, found := SearchSourceFile(sourceCode, symlink)
		assert.True(t, found)

		// Make sure the returned file's name is 'file1.c'
		// and is the final destination (i.e. not a symlink itself)
		assert.Equal(t, "file1.c", filepath.Base(remappedFile))
		finalPath, err := filepath.EvalSymlinks(remappedFile)
		require.NoError(t, err)
		assert.Equal(t, finalPath, remappedFile)
	})

	t.Run("fails to evaluate symlinks", func(t *testing.T) {
		// Create a bad link (circular)
		symlinkName := filepath.Join(proj1HostDir, "circular-symlink.c")
		require.NoError(t, os.Symlink(symlinkName, symlinkName))

		sourceCode := HostSourceCodePath{Paths: []string{proj1HostDir}}

		log.SetLevel(log.WarnLevel)
		hook := test.NewGlobal()

		remappedFile, found := SearchSourceFile(sourceCode, symlinkName)
		assert.False(t, found)
		assert.Equal(t, symlinkName, remappedFile)

		// Check the logs
		errVal, ok := hook.LastEntry().Data["err"].(error)
		assert.True(t, ok)
		assert.Contains(t, errVal.Error(), "failed to eval symlink")
	})

	t.Run("returns the correct remapping", func(t *testing.T) {
		sourceCode := HostSourceCodePath{Paths: []string{proj1HostDir}}
		sourceFiles := append([]string{}, proj1TargetFiles...)
		checksums := proj1Checksums

		log.SetLevel(log.InfoLevel)
		hook := test.NewGlobal()

		// 1. Remap
		remapped := map[string]string{}
		for _, sourceFile := range sourceFiles {
			remappedFile, found := SearchSourceFile(sourceCode, sourceFile)
			require.True(t, found)

			remapped[sourceFile] = remappedFile
		}

		// 2. Verify
		for target, host := range remapped {
			expected := checksums[target]
			calculated, err := computeChecksum(fs, host)
			require.NoError(t, err)

			assert.Equal(t, expected, calculated)
		}

		// 3. Check logs
		entry := hook.LastEntry()
		assert.NotNil(t, entry)
		assert.Contains(t, entry.Message, "Found source file")
	})

	t.Run("returns the first find with multiple projects", func(t *testing.T) {
		// join multiple paths with the OS-specific list separator
		sourceCode := HostSourceCodePath{Paths: []string{proj1HostDir, proj2HostDir}}
		sourceFiles := append([]string{}, proj1TargetFiles...)
		sourceFiles = append(sourceFiles, proj2TargetFiles...)
		checksums := proj1Checksums
		for k, v := range proj2Checksums {
			checksums[k] = v
		}

		// 1. Remap
		remapped := map[string]string{}
		for _, sourceFile := range sourceFiles {
			remappedFile, found := SearchSourceFile(sourceCode, sourceFile)
			require.True(t, found)

			remapped[sourceFile] = remappedFile
		}

		// 2. Verify
		for target, host := range remapped {
			expected := checksums[target]
			calculated, err := computeChecksum(fs, host)
			require.NoError(t, err)

			assert.Equal(t, expected, calculated)
		}
	})

	t.Run("normalizeToSuffixes correctly handles path on running OS", func(t *testing.T) {
		tempDir, err := afero.TempDir(fs, "", "foo")
		require.NoError(t, err)

		sourceFile, err := afero.TempFile(fs, tempDir, "file1.c")
		require.NoError(t, err)

		suffixes := normalizeToSuffixes(sourceFile.Name())

		// Length should be at least 2
		//  - foo/file1.c
		//  - file1.c
		assert.GreaterOrEqual(t, len(suffixes), 2)

		// Each suffix is a substring of the original path
		for _, suffix := range suffixes {
			assert.True(t, strings.HasSuffix(sourceFile.Name(), suffix))
		}
	})

	t.Run("normalizeToSuffixes correctly handles Windows paths", func(t *testing.T) {
		windowsPath := `C:\Users\developer\projects\foo\bar\file1.c`

		suffixes := normalizeToSuffixes(windowsPath)
		require.GreaterOrEqual(t, len(suffixes), 6)

		// The 1st last suffix should be just the filename
		// The 2nd-to-last should be bar/file1.c
		// The 3rd-to-last should be foo/bar/file1.c
		assert.Equal(t, "file1.c", suffixes[len(suffixes)-1])
		assert.Equal(t, filepath.Join("bar", "file1.c"), suffixes[len(suffixes)-2])
		assert.Equal(t, filepath.Join("foo", "bar", "file1.c"), suffixes[len(suffixes)-3])
	})

	t.Run("normalizeToSuffixes correctly handles Unix paths", func(t *testing.T) {
		unixPath := "/home/developer/projects/foo/bar/file1.c"

		suffixes := normalizeToSuffixes(unixPath)
		require.Len(t, suffixes, 6)

		assert.Equal(t, "file1.c", suffixes[len(suffixes)-1])
		assert.Equal(t, filepath.Join("bar", "file1.c"), suffixes[len(suffixes)-2])
		assert.Equal(t, filepath.Join("foo", "bar", "file1.c"), suffixes[len(suffixes)-3])
	})

	t.Run("normalizeToSuffixes handles mixed separators", func(t *testing.T) {
		// Test path with mixed separators (shouldn't happen in practice, but good to handle)
		mixedPath := `foo/bar\baz/file1.c`
		suffixes := normalizeToSuffixes(mixedPath)
		require.Len(t, suffixes, 4)

		assert.Equal(t, "file1.c", suffixes[3])
		assert.Equal(t, filepath.Join("baz", "file1.c"), suffixes[2])
		assert.Equal(t, filepath.Join("bar", "baz", "file1.c"), suffixes[1])
		assert.Equal(t, filepath.Join("foo", "bar", "baz", "file1.c"), suffixes[0])
	})
}
