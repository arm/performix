// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package producttelemetry owns persistent product telemetry classification.
package producttelemetry

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

const coreDeveloperMarker = "core-developer-marker"
const armInternalMarker = "arm-internal-marker"
const coreDeveloperMaxAge = 90 * 24 * time.Hour

// Serialize reads and writes so readers cannot observe a partially written timestamp.
var markerMu sync.Mutex

// MarkerDirectory preserves the shared per-user location used by earlier GUI versions.
func MarkerDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return markerDirectory(runtime.GOOS, home, os.Getenv), nil
}

func markerDirectory(platform, home string, getenv func(string) string) string {
	switch platform {
	case "windows":
		base := getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "Arm", "Performix")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Arm", "Performix")
	default:
		base := getenv("XDG_STATE_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "arm", "performix")
	}
}

// Mark writes or refreshes only the requested markers. False never clears a marker.
func Mark(directory string, armInternal, coreDev bool) error {
	markerMu.Lock()
	defer markerMu.Unlock()
	if !armInternal && !coreDev {
		return nil
	}
	if err := os.MkdirAll(directory, perms.LocalDirPerm); err != nil {
		return err
	}
	timestamp := []byte(time.Now().UTC().Format(time.RFC3339Nano))
	for name, enabled := range map[string]bool{armInternalMarker: armInternal, coreDeveloperMarker: coreDev} {
		if enabled {
			if err := os.WriteFile(filepath.Join(directory, name), timestamp, perms.LocalFilePerm); err != nil {
				return err
			}
		}
	}
	return nil
}

// Classify treats unreadable markers as absent, matching the original GUI behavior.
func Classify(directory string, now time.Time) (armInternal, coreDev bool) {
	markerMu.Lock()
	defer markerMu.Unlock()
	_, err := os.ReadFile(filepath.Join(directory, armInternalMarker))
	armInternal = err == nil
	timestamp, err := os.ReadFile(filepath.Join(directory, coreDeveloperMarker))
	if err == nil {
		markerTime, err := time.Parse(time.RFC3339Nano, string(timestamp))
		coreDev = err == nil && now.Sub(markerTime) <= coreDeveloperMaxAge
	}
	return
}
