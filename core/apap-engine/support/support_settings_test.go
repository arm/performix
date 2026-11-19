// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/settings"
)

func TestCollectSettingsCopiesFileWithoutWaitingForLock(t *testing.T) {
	for _, tc := range []struct{ name, contents string }{
		{"valid JSON", `{ "profilingFocus": "mobile", "futureSetting": true }`},
		{"invalid JSON", `{"profilingFocus":`},
		{"empty file", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			configDir := t.TempDir()
			source := filepath.Join(configDir, settings.Filename)
			require.NoError(t, os.WriteFile(source, []byte(tc.contents), perms.LocalFilePerm))
			lock := flock.New(source + ".lock")
			require.NoError(t, lock.Lock())
			t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			pkgRoot := t.TempDir()

			// When
			err := collectSettings(ctx, pkgRoot, configDir)

			// Then
			require.NoError(t, err)
			data, err := os.ReadFile(filepath.Join(pkgRoot, settingsDirName, settings.Filename))
			require.NoError(t, err)
			require.Equal(t, tc.contents, string(data))
			original, err := os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, tc.contents, string(original))
			entries, err := os.ReadDir(filepath.Join(pkgRoot, settingsDirName))
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.Equal(t, settings.Filename, entries[0].Name())
		})
	}
}

func TestCollectSettingsSkipsMissingFile(t *testing.T) {
	for _, name := range []string{"missing file", "missing directory", "no configuration directory"} {
		t.Run(name, func(t *testing.T) {
			// Given
			configDir := t.TempDir()
			if name == "missing directory" {
				configDir = filepath.Join(configDir, "missing")
			}
			if name == "no configuration directory" {
				configDir = ""
			}
			pkgRoot := t.TempDir()

			// When
			err := collectSettings(context.Background(), pkgRoot, configDir)

			// Then
			require.NoError(t, err)
			require.NoDirExists(t, filepath.Join(pkgRoot, settingsDirName))
		})
	}
}

func TestCollectSettingsReportsFileErrors(t *testing.T) {
	for _, name := range []string{"source is a directory", "destination parent is a file", "destination is a directory"} {
		t.Run(name, func(t *testing.T) {
			// Given
			configDir := t.TempDir()
			source := filepath.Join(configDir, settings.Filename)
			pkgRoot := t.TempDir()
			if name == "source is a directory" {
				require.NoError(t, os.Mkdir(source, perms.LocalDirPerm))
			} else {
				require.NoError(t, os.WriteFile(source, []byte(`{}`), perms.LocalFilePerm))
				if name == "destination parent is a file" {
					require.NoError(t, os.WriteFile(filepath.Join(pkgRoot, settingsDirName), []byte("file"), perms.LocalFilePerm))
				} else {
					require.NoError(t, os.MkdirAll(filepath.Join(pkgRoot, settingsDirName, settings.Filename), perms.LocalDirPerm))
				}
			}

			// When
			err := collectSettings(context.Background(), pkgRoot, configDir)

			// Then
			require.Error(t, err)
		})
	}
}

func TestCollectSettingsStopsWhenCanceled(t *testing.T) {
	// Given
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pkgRoot, configDir := t.TempDir(), t.TempDir()

	// When
	err := collectSettings(ctx, pkgRoot, configDir)

	// Then
	require.ErrorIs(t, err, context.Canceled)
	require.NoDirExists(t, filepath.Join(pkgRoot, settingsDirName))
}

func TestSupportPackageIncludesSettingsWhenAvailable(t *testing.T) {
	for _, name := range []string{"valid JSON", "invalid JSON", "missing file", "unreadable file"} {
		t.Run(name, func(t *testing.T) {
			// Given
			dataDir, _, configDir, _ := configureSupportEnv(t)
			source := filepath.Join(configDir, settings.Filename)
			contents := `{"profilingFocus":"server"}`
			if name == "invalid JSON" {
				contents = `{"profilingFocus":`
			}
			switch name {
			case "missing file":
			case "unreadable file":
				require.NoError(t, os.Mkdir(source, perms.LocalDirPerm))
			default:
				require.NoError(t, os.WriteFile(source, []byte(contents), perms.LocalFilePerm))
			}
			runs, err := run.NewRunCollection(filepath.Join(dataDir, run.RunDirName))
			require.NoError(t, err)
			opts := PackageOptions{OutputDir: t.TempDir(), ConfigDir: configDir}

			// When
			result, err := CreateSupportPackage(context.Background(), opts, nil, runs)

			// Then
			require.NoError(t, err)
			archive, err := zip.OpenReader(result.PackagePath)
			require.NoError(t, err)
			defer archive.Close()
			found := false
			for _, file := range archive.File {
				require.False(t, strings.HasSuffix(file.Name, settings.Filename+".lock"))
				if !strings.HasSuffix(file.Name, "/settings/settings.json") {
					continue
				}
				found = true
				reader, err := file.Open()
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				require.NoError(t, reader.Close())
				require.Equal(t, contents, string(data))
			}
			require.Equal(t, name == "valid JSON" || name == "invalid JSON", found)
		})
	}
}
