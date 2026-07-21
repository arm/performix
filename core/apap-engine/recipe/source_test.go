// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

// mockRunRecipeLookup is a testify mock that implements RunRecipeLookup
type mockRunRecipeLookup struct {
	mock.Mock
}

func (m *mockRunRecipeLookup) CheckRunsUseSameRecipe(ids []run.RunID) error {
	args := m.Called(ids)
	return args.Error(0)
}

func (m *mockRunRecipeLookup) CheckRunsUseSameRecipeNameOnly(ids []run.RunID) error {
	args := m.Called(ids)
	return args.Error(0)
}

func (m *mockRunRecipeLookup) GetNewestRun(ids []run.RunID) (run.RunID, error) {
	args := m.Called(ids)
	return args.Get(0).(run.RunID), args.Error(1)
}

func (m *mockRunRecipeLookup) GetRecipeComponentPath(id run.RunID) (string, error) {
	args := m.Called(id)
	return args.String(0), args.Error(1)
}

func (m *mockRunRecipeLookup) GetRecipeName(id run.RunID) (string, error) {
	args := m.Called(id)
	return args.String(0), args.Error(1)
}

func TestSelectRenderSourceRecipe(t *testing.T) {
	rid := run.RunID{Value: "abc"}

	t.Run("OverrideByName returns override", func(t *testing.T) {
		mockC := new(mockRunRecipeLookup)

		result, err := SelectRenderSourceRecipe(mockC, SelectionOptions{
			Policy:       OverrideByName,
			OverrideName: "manual-recipe",
		}, nil)

		assert.NoError(t, err)
		assert.Equal(t, "manual-recipe", result)
	})

	t.Run("FromContent uses newest run's recipe path", func(t *testing.T) {
		mockC := new(mockRunRecipeLookup)
		mockC.On("CheckRunsUseSameRecipe", mock.Anything).Return(nil)
		mockC.On("GetNewestRun", mock.Anything).Return(rid, nil)
		mockC.On("GetRecipeComponentPath", rid).Return("/recipe/path.js", nil)

		result, err := SelectRenderSourceRecipe(mockC, SelectionOptions{
			Policy: FromContent,
		}, []run.RunID{rid})

		assert.NoError(t, err)
		assert.Equal(t, "/recipe/path.js", result)
		mockC.AssertExpectations(t)
	})

	t.Run("UseInstalledVersion returns recipe name", func(t *testing.T) {
		mockC := new(mockRunRecipeLookup)
		mockC.On("CheckRunsUseSameRecipeNameOnly", mock.Anything).Return(nil)
		mockC.On("GetNewestRun", mock.Anything).Return(rid, nil)
		mockC.On("GetRecipeName", rid).Return("installed-recipe", nil)

		result, err := SelectRenderSourceRecipe(mockC, SelectionOptions{
			Policy: UseInstalledVersion,
		}, []run.RunID{rid})

		assert.NoError(t, err)
		assert.Equal(t, "installed-recipe", result)
		mockC.AssertExpectations(t)
	})

	t.Run("Fails on unknown strategy", func(t *testing.T) {
		mockC := new(mockRunRecipeLookup)

		_, err := SelectRenderSourceRecipe(mockC, SelectionOptions{
			Policy: SelectionPolicy(999),
		}, nil)

		expectedErr := message.New(message.CommonUnknownError).WithCause(errors.New("unknown recipe selection strategy: 999"))
		assert.Equal(t, expectedErr, err)
	})

	t.Run("FromContent falls back to UseInstalledVersion if recipe path is missing, and returns combined error if that fails too", func(t *testing.T) {
		mockC := new(mockRunRecipeLookup)
		rid := run.RunID{Value: "fallback-run"}

		// Simulated distinct error messages
		getPathErr := message.New(message.EngineRunDoesNotExist).WithCause(errors.New("no component path found in run"))
		checkNameErr := message.New(message.EngineRunReadManifest).WithCause(errors.New("run recipes are inconsistent"))

		// FromContent phase
		mockC.On("CheckRunsUseSameRecipe", mock.Anything).Return(nil)
		mockC.On("GetNewestRun", mock.Anything).Return(rid, nil)
		mockC.On("GetRecipeComponentPath", rid).Return("", getPathErr)

		// Fallback to UseInstalledVersion phase
		mockC.On("CheckRunsUseSameRecipeNameOnly", mock.Anything).Return(checkNameErr)

		_, err := SelectRenderSourceRecipe(mockC, SelectionOptions{
			Policy: FromContent,
		}, []run.RunID{rid})

		require.Error(t, err)
		expectedErr := errors.Join(checkNameErr, getPathErr)
		assert.Equal(t, expectedErr, err)
		assert.ErrorIs(t, err, getPathErr)
		assert.ErrorIs(t, err, checkNameErr)

		mockC.AssertExpectations(t)
	})
}
