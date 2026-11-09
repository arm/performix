// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package producttelemetry

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserIDPersistsWithoutGUI(t *testing.T) {
	// Given an installation with no telemetry state.
	directory := filepath.Join(t.TempDir(), "state")
	// When the engine requests an identity repeatedly.
	first, err := GetOrCreateUserID(directory)
	require.NoError(t, err)
	second, err := GetOrCreateUserID(directory)
	require.NoError(t, err)
	// Then one valid UUID is persisted and reused.
	assert.NoError(t, uuid.Validate(first))
	assert.Equal(t, first, second)
	data, err := os.ReadFile(filepath.Join(directory, userIDFile))
	require.NoError(t, err)
	assert.Equal(t, first+"\n", string(data))
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestConcurrentUserIDCreation(t *testing.T) {
	// Given concurrent first requests for the same per-user state directory.
	directory := t.TempDir()
	const count = 20
	ids := make([]string, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	// When all requests create or read the identity.
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i], errs[i] = GetOrCreateUserID(directory)
		}()
	}
	wg.Wait()
	// Then every caller receives the same complete UUID.
	for i := range count {
		require.NoError(t, errs[i])
		assert.Equal(t, ids[0], ids[i])
	}
	assert.NoError(t, uuid.Validate(ids[0]))
}

func TestUserIDDoesNotReplaceInvalidState(t *testing.T) {
	for _, contents := range []string{"", "invalid"} {
		t.Run(contents, func(t *testing.T) {
			// Given an invalid identity file.
			directory := t.TempDir()
			path := filepath.Join(directory, userIDFile)
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
			// When the engine requests an identity.
			id, err := GetOrCreateUserID(directory)
			// Then it reports failure without silently rotating the identity.
			require.Error(t, err)
			assert.Empty(t, id)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, contents, string(data))
		})
	}
}

func TestUserIDStorageFailure(t *testing.T) {
	// Given a file where the state directory should be.
	directory := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(directory, nil, 0o600))
	// When the engine requests an identity.
	id, err := GetOrCreateUserID(directory)
	// Then it reports the storage failure instead of returning an unpersisted ID.
	require.Error(t, err)
	assert.Empty(t, id)
}
