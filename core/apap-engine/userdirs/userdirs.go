// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package userdirs provides an interface to retrieve current user's directories,
// taking into account the host's operating system.
//
// On Linux and Mac OS, it aims to follow the XDG Base Directory Specification:
// https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html.
//
// On Windows it will yield paths relative to %UserProfile%/AppData.
package userdirs

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

var ErrUnsupportedOS = errors.New("only Mac OS, Linux and Windows are supported")

var daemonDirName = terminology.GetDaemonDirName()

var LegacyDaemonDirName = []string{"atperfd"}

var getEffectiveUID = os.Geteuid

func joinHomeDir(elem ...string) (string, error) {
	localDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	chunks := append([]string{localDir}, elem...)
	return filepath.Join(chunks...), nil
}

// StateDir returns the current user's state directory.
func StateDir() (string, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		xdgStateHome := os.Getenv("XDG_STATE_HOME")
		if xdgStateHome != "" {
			return filepath.Join(xdgStateHome, daemonDirName), nil
		}
		return joinHomeDir(".local", "state", daemonDirName)
	case "windows":
		return joinHomeDir("AppData", "Local", daemonDirName)
	default:
		return "", ErrUnsupportedOS
	}
}

// defaultConfigDir returns the current user's config directory, based on the OS type.
func defaultConfigDir() (string, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfigHome != "" {
			return filepath.Join(xdgConfigHome, daemonDirName), nil
		}
		return joinHomeDir(".config", daemonDirName)
	case "windows":
		return joinHomeDir("AppData", "Local", daemonDirName)
	default:
		return "", ErrUnsupportedOS
	}
}

// ConfigDir returns the config directory. It attempts to set the config directory using the following priority order:
// 1. Environment variable
// 2. The invoking user's home directory
// 3. The default config directory
func ConfigDir() (string, error) {
	var err error = nil

	// First check the env variable
	env := strings.ToUpper(terminology.GetEnvVarPrefix()) + "_" + "CONFIG_DIR"
	configDir := os.Getenv(env)
	if configDir == "" {
		// If env variable not set, prefer the invoking user's config dir when available.
		if launchUser, ok := LaunchUser(); ok {
			configDir = configDirForHome(launchUser.HomeDir)
		}
		if configDir == "" {
			// If env variable not set, use default config dir
			configDir, err = defaultConfigDir()
		}
	}
	return configDir, err
}

// IsDefaultConfigDir reports whether the provided path matches the default config location.
func IsDefaultConfigDir(path string) bool {
	def, err := defaultConfigDir()
	if err != nil {
		return false
	}
	return filepath.Clean(path) == filepath.Clean(def)
}

// LaunchUserInfo holds information about the user that invoked the current process when it was elevated via sudo.
type LaunchUserInfo struct {
	HomeDir string
	UID     int
	GID     int
}

// LaunchUser attempts to determine the original user that launched the process before privilege escalation.
// On Unix systems it inspects the standard sudo environment variables (SUDO_UID, SUDO_GID, SUDO_USER).
// On Windows it returns (nil, false) because sudo-style elevation is not used.
// Returns the launch user info, and a boolean indicating whether lookup succeeded.
func LaunchUser() (*LaunchUserInfo, bool) {
	if runtime.GOOS == "windows" {
		return nil, false
	}

	if getEffectiveUID() != 0 {
		return nil, false
	}

	sudoUID := strings.TrimSpace(os.Getenv("SUDO_UID"))
	sudoGID := strings.TrimSpace(os.Getenv("SUDO_GID"))
	sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))

	if sudoUID == "" || sudoGID == "" {
		return nil, false
	}

	uid, err := strconv.Atoi(sudoUID)
	if err != nil {
		return nil, false
	}
	gid, err := strconv.Atoi(sudoGID)
	if err != nil {
		return nil, false
	}

	var u *user.User
	if sudoUID != "" {
		u, err = user.LookupId(sudoUID)
		if err != nil {
			u = nil
		}
	}
	if u == nil && sudoUser != "" {
		u, err = user.Lookup(sudoUser)
		if err != nil {
			u = nil
		}
	}
	if u == nil {
		return nil, false
	}

	return &LaunchUserInfo{
		HomeDir: u.HomeDir,
		UID:     uid,
		GID:     gid,
	}, true
}

// configDirForHome derives the daemon config directory for the provided home path.
func configDirForHome(home string) string {
	if home == "" {
		return ""
	}

	switch runtime.GOOS {
	case "darwin", "linux":
		return filepath.Join(home, ".config", daemonDirName)
	case "windows":
		return filepath.Join(home, "AppData", "Local", daemonDirName)
	default:
		return ""
	}
}

// ConfigDirForHome returns the daemon configuration directory derived from the provided home path. Used to ensure TLS
// artifacts are associated with the user who launched the process.
func ConfigDirForHome(home string) string {
	return configDirForHome(home)
}

// DefaultDataDir returns the current user's data directory, based on the OS type.
func DefaultDataDir(daemonName string) (string, error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		xdgDataHome := os.Getenv("XDG_DATA_HOME")
		if xdgDataHome != "" {
			return filepath.Join(xdgDataHome, daemonName), nil
		}
		return joinHomeDir(".local", "share", daemonName)
	case "windows":
		return joinHomeDir("AppData", "Local", daemonName)
	default:
		return "", ErrUnsupportedOS
	}
}

// defaultDaemonDataDir returns the current user's data directory for the daemon, based on the OS type.
func defaultDaemonDataDir() (string, error) {
	return DefaultDataDir(daemonDirName)
}

// DataDir returns the current user's data directory.
func DataDir() (string, error) {
	var err error = nil

	// First check the env variable
	env := strings.ToUpper(terminology.GetEnvVarPrefix()) + "_" + "DATA_DIR"
	dataDir := os.Getenv(env)
	if dataDir == "" {
		// If env variable not set, use default data dir
		dataDir, err = defaultDaemonDataDir()
	}
	return dataDir, err
}
