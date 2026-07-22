// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

func configureSupportEnv(t *testing.T) (dataDir, stateDir, configDir, homeDir string) {
	t.Helper()

	base := t.TempDir()
	dataDir = filepath.Join(base, "data")
	configDir = filepath.Join(base, "config")
	stateRoot := filepath.Join(base, "state")
	homeDir = filepath.Join(base, "home")

	require.NoError(t, os.MkdirAll(dataDir, perms.LocalDirPerm))
	require.NoError(t, os.MkdirAll(configDir, perms.LocalDirPerm))
	require.NoError(t, os.MkdirAll(stateRoot, perms.LocalDirPerm))
	require.NoError(t, os.MkdirAll(homeDir, perms.LocalDirPerm))

	envPrefix := strings.ToUpper(terminology.GetEnvVarPrefix())
	t.Setenv(envPrefix+"_DATA_DIR", dataDir)
	t.Setenv(envPrefix+"_CONFIG_DIR", configDir)
	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv("HOME", homeDir)

	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", homeDir)
		t.Setenv("APPDATA", filepath.Join(homeDir, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(homeDir, "AppData", "Local"))
	}

	resolvedDataDir, err := userdirs.DataDir()
	require.NoError(t, err)
	resolvedStateDir, err := userdirs.StateDir()
	require.NoError(t, err)
	resolvedConfigDir, err := userdirs.ConfigDir()
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(resolvedStateDir, perms.LocalDirPerm))

	return resolvedDataDir, resolvedStateDir, resolvedConfigDir, homeDir
}

func writeEngineLog(t *testing.T, stateDir, name, contents string) string {
	t.Helper()
	path := filepath.Join(stateDir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(path, []byte(contents), perms.LocalFilePerm))
	require.NoError(t, os.Chtimes(path, time.Now(), time.Now()))
	return path
}

func writeGUILog(t *testing.T, homeDir, contents string) string {
	t.Helper()
	path := guiLogPath(homeDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(path, []byte(contents), perms.LocalFilePerm))
	require.NoError(t, os.Chtimes(path, time.Now(), time.Now()))
	return path
}

func guiLogPath(home string) string {
	productName := terminology.GetProductFullName()
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", productName, "logs", "main.log")
	case "darwin":
		return filepath.Join(home, "Library", "Logs", productName, "main.log")
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", productName, "logs", "main.log")
	default:
		return filepath.Join(home, "logs", "main.log")
	}
}

func readZipJSON[T any](t *testing.T, file *zip.File, target *T) {
	t.Helper()
	rc, err := file.Open()
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, target))
}
