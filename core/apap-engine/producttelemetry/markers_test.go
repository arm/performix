// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package producttelemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkerClassification(t *testing.T) {
	// Given existing GUI markers in the shared state directory.
	directory := t.TempDir()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, timestamp string
		coreDev         bool
	}{
		{"recent", now.Format(time.RFC3339Nano), true},
		{"exactly 90 days", now.Add(-coreDeveloperMaxAge).Format(time.RFC3339Nano), true},
		{"expired", now.Add(-coreDeveloperMaxAge - time.Millisecond).Format(time.RFC3339Nano), false},
		{"invalid", "not a timestamp", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given an Arm marker whose contents and age do not affect classification.
			require.NoError(t, os.WriteFile(filepath.Join(directory, armInternalMarker), []byte("legacy"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(directory, coreDeveloperMarker), []byte(tc.timestamp), 0o600))
			// When classification is requested.
			armInternal, coreDev := Classify(directory, now)
			// Then the flags are independent and only the developer marker expires.
			assert.True(t, armInternal)
			assert.Equal(t, tc.coreDev, coreDev)
		})
	}
}

func TestMarkCreatesDeveloperMarkerInNewStateDirectory(t *testing.T) {
	unmarkedStateDirectory := filepath.Join(t.TempDir(), "state")
	isArmInternal, isCoreDeveloper := Classify(unmarkedStateDirectory, time.Now())
	require.False(t, isArmInternal)
	require.False(t, isCoreDeveloper)

	err := Mark(unmarkedStateDirectory, false, true)

	require.NoError(t, err)
	isArmInternal, isCoreDeveloper = Classify(unmarkedStateDirectory, time.Now())
	assert.False(t, isArmInternal)
	assert.True(t, isCoreDeveloper)
}

func TestMarkRecordsInternalUsageWithoutClearingDeveloperClassification(t *testing.T) {
	stateDirectory := t.TempDir()
	require.NoError(t, Mark(stateDirectory, false, true))

	err := Mark(stateDirectory, true, false)

	require.NoError(t, err)
	isArmInternal, isCoreDeveloper := Classify(stateDirectory, time.Now())
	assert.True(t, isArmInternal)
	assert.True(t, isCoreDeveloper)
}

func TestMarkPreservesClassificationWhenNeitherMarkerIsRequested(t *testing.T) {
	stateDirectory := t.TempDir()
	require.NoError(t, Mark(stateDirectory, false, true))
	require.NoError(t, Mark(stateDirectory, true, false))

	err := Mark(stateDirectory, false, false)

	require.NoError(t, err)
	isArmInternal, isCoreDeveloper := Classify(stateDirectory, time.Now())
	assert.True(t, isArmInternal)
	assert.True(t, isCoreDeveloper)
}

func TestMarkRefreshesExpiredDeveloperMarker(t *testing.T) {
	stateDirectory := t.TempDir()
	developerMarkerPath := filepath.Join(stateDirectory, coreDeveloperMarker)
	expiredDeveloperMarker := []byte("2000-01-01T00:00:00.000Z")
	require.NoError(t, Mark(stateDirectory, true, false))
	require.NoError(t, os.WriteFile(developerMarkerPath, expiredDeveloperMarker, 0o600))

	err := Mark(stateDirectory, false, true)

	require.NoError(t, err)
	_, isCoreDeveloper := Classify(stateDirectory, time.Now())
	assert.True(t, isCoreDeveloper)
}

func TestMarkFailsWhenStateDirectoryIsAFile(t *testing.T) {
	fileBlockingStateDirectory := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(fileBlockingStateDirectory, nil, 0o600))

	err := Mark(fileBlockingStateDirectory, true, false)

	require.Error(t, err)
}

func TestMarkerDirectory(t *testing.T) {
	for _, tc := range []struct{ platform, envKey, envValue, expected string }{
		{"darwin", "", "", "Library/Application Support/Arm/Performix"},
		{"windows", "", "", "AppData/Local/Arm/Performix"},
		{"linux", "", "", ".local/state/arm/performix"},
		{"windows", "LOCALAPPDATA", "custom", "custom/Arm/Performix"},
		{"linux", "XDG_STATE_HOME", "custom", "custom/arm/performix"},
	} {
		t.Run(tc.platform+tc.envKey, func(t *testing.T) {
			// Given an OS user home and optional state directory override.
			home := t.TempDir()
			getenv := func(key string) string {
				if key == tc.envKey {
					return filepath.Join(home, tc.envValue)
				}
				return ""
			}
			// When resolving the marker location.
			directory := markerDirectory(tc.platform, home, getenv)
			// Then existing GUI marker locations are retained on every platform.
			assert.Equal(t, filepath.Join(home, filepath.FromSlash(tc.expected)), directory)
		})
	}
}
