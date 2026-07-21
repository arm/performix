// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package userdirs

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func TestStateDir(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	t.Run("Uses XDG spec default state location", func(t *testing.T) {
		want := filepath.Join(homeDir, ".local", "state", daemonDirName)
		got, err := StateDir()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("Respects XDG_STATE_HOME env variable", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "/foo/bar")
		want := filepath.Join("/foo/bar", daemonDirName)
		got, err := StateDir()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

func TestConfigDir(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	t.Run("Uses XDG spec default config location", func(t *testing.T) {
		want := filepath.Join(homeDir, ".config", daemonDirName)
		got, err := ConfigDir()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("Respects XDG_CONFIG_HOME env variable", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/foo/baz")
		want := filepath.Join("/foo/baz", daemonDirName)
		got, err := ConfigDir()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("Prefers launch user home when sudo metadata present", func(t *testing.T) {
		currentUser, err := user.Current()
		if err != nil || currentUser == nil || currentUser.HomeDir == "" {
			t.Skip("unable to determine current user for test")
		}
		t.Setenv(strings.ToUpper(terminology.GetEnvVarPrefix())+"_"+"CONFIG_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("SUDO_UID", currentUser.Uid)
		t.Setenv("SUDO_GID", currentUser.Gid)
		t.Setenv("SUDO_USER", currentUser.Username)

		want := filepath.Join(currentUser.HomeDir, ".config", daemonDirName)
		got, err := ConfigDir()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

func TestIsDefaultConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	defaultDir, err := defaultConfigDir()
	require.NoError(t, err)

	require.True(t, IsDefaultConfigDir(defaultDir))
	require.False(t, IsDefaultConfigDir(filepath.Join("/custom", "config", daemonDirName)))
}

func TestDataDir(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	t.Run("Uses XDG spec default data location", func(t *testing.T) {
		want := filepath.Join(homeDir, ".local", "share", daemonDirName)
		got, err := DataDir()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("Respects XDG_DATA_HOME env variable", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "/foo/bax")
		want := filepath.Join("/foo/bax", daemonDirName)
		got, err := DataDir()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

func TestLaunchUserRequiresElevation(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Skip("unable to determine current user")
	}

	t.Setenv("SUDO_UID", currentUser.Uid)
	t.Setenv("SUDO_GID", currentUser.Gid)
	t.Setenv("SUDO_USER", currentUser.Username)

	original := getEffectiveUID
	getEffectiveUID = func() int { return 1000 }
	defer func() { getEffectiveUID = original }()

	info, ok := LaunchUser()
	require.False(t, ok)
	require.Nil(t, info)
}

func TestLaunchUserHonorsElevation(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Skip("unable to determine current user")
	}
	uid, err := strconv.Atoi(currentUser.Uid)
	if err != nil {
		t.Skip("current user UID not numeric")
	}
	gid, err := strconv.Atoi(currentUser.Gid)
	if err != nil {
		t.Skip("current user GID not numeric")
	}

	t.Setenv("SUDO_UID", currentUser.Uid)
	t.Setenv("SUDO_GID", currentUser.Gid)
	t.Setenv("SUDO_USER", currentUser.Username)

	original := getEffectiveUID
	getEffectiveUID = func() int { return 0 }
	defer func() { getEffectiveUID = original }()

	info, ok := LaunchUser()
	require.True(t, ok)
	require.NotNil(t, info)
	require.Equal(t, uid, info.UID)
	require.Equal(t, gid, info.GID)
	require.Equal(t, currentUser.HomeDir, info.HomeDir)
}
