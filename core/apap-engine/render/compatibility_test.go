// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

var armProvidedRecipeName = "cpu_microarchitecture"

type MockDescriptionProvider struct {
	mock.Mock
}

func (dp *MockDescriptionProvider) RunDescription(ctx context.Context, runID run.RunID) (*run.RunDescription, error) {
	mockArgs := dp.Called(ctx, runID)
	return mockArgs.Get(0).(*run.RunDescription), mockArgs.Error(1)
}

type MockCompatibilityChecker struct {
	mock.Mock
}

func (cc *MockCompatibilityChecker) GetCurrentCompatibilityForRecipe(recipeName string) (semver.VersionRange, error) {
	mockArgs := cc.Called(recipeName)
	return mockArgs.Get(0).(semver.VersionRange), mockArgs.Error(1)
}

func (cc *MockCompatibilityChecker) CompatibleVersionRange(recipeName string, runVersion semver.SemVer) (semver.VersionRange, error) {
	mockArgs := cc.Called(recipeName, runVersion)
	return mockArgs.Get(0).(semver.VersionRange), mockArgs.Error(1)
}

func (cc *MockCompatibilityChecker) GetCurrentEngineVersion() semver.SemVer {
	mockArgs := cc.Called()
	return mockArgs.Get(0).(semver.SemVer)
}

func newMockCompatibilityChecker(currentEngineVersion semver.SemVer, currentSupportedRange semver.VersionRange) *MockCompatibilityChecker {
	cc := &MockCompatibilityChecker{}
	cc.On("GetCurrentEngineVersion").Return(currentEngineVersion)
	cc.On("GetCurrentCompatibilityForRecipe", mock.Anything).Return(currentSupportedRange, nil)
	return cc
}

func createArmProvidedRecipeRun(engineVersion string) *run.RunDescription {
	return &run.RunDescription{EngineVersion: engineVersion, RecipeName: armProvidedRecipeName}
}

func must(t *testing.T, s string) *semver.SemVer {
	v, err := semver.ParseSemVer(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return &v
}

func TestCheckRunCompatibility(t *testing.T) {
	testError := errors.New("this is a test!")
	engineVersion := must(t, "5.0.0")

	t.Run("no warnings if recipe selection policy is not use-installed-version", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		cc := &MockCompatibilityChecker{}

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.OverrideByName, []run.RunID{}, cc)
		assert.Nil(t, result)
		assert.NoError(t, err)
	})
	t.Run("run description errors are propagated", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runId := run.RunID{Value: "abcd"}
		dp.On("RunDescription", mock.Anything, runId).Return(&run.RunDescription{}, testError)
		cc := &MockCompatibilityChecker{}

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, []run.RunID{runId}, cc)
		assert.Nil(t, result)
		assert.Equal(t, testError, err)
	})
	t.Run("no warnings if runs use user-created recipe", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runId := run.RunID{Value: "abcd"}
		dp.On("RunDescription", mock.Anything, runId).Return(&run.RunDescription{}, nil)

		cc := &MockCompatibilityChecker{}

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, []run.RunID{runId}, cc)
		assert.Nil(t, result)
		assert.NoError(t, err)
	})
	t.Run("no warnings if no runs provided", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		cc := newMockCompatibilityChecker(*engineVersion, semver.VersionRange{Min: must(t, "1.0.0")})

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, []run.RunID{}, cc)
		assert.Nil(t, result)
		assert.NoError(t, err)
	})
	t.Run("no warnings if all runs are compatible", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runIDs := []run.RunID{{Value: "1"}, {Value: "2"}, {Value: "3"}, {Value: "4"}, {Value: "5"}}
		dp.On("RunDescription", mock.Anything, mock.Anything).Return(createArmProvidedRecipeRun("5.0.0"), nil)

		compatibleRange := semver.VersionRange{Min: engineVersion, Max: engineVersion, InclusiveMax: true}
		cc := newMockCompatibilityChecker(*engineVersion, semver.VersionRange{Min: must(t, "0.0.0")})
		cc.On("CompatibleVersionRange", mock.Anything, *engineVersion).Return(compatibleRange, nil)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, runIDs, cc)
		assert.Nil(t, result)
		assert.NoError(t, err)
	})
	t.Run("correct warning if one run is indeterminable due to being too new", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runId := run.RunID{Value: "abcd"}
		dp.On("RunDescription", mock.Anything, runId).Return(createArmProvidedRecipeRun("6.0.0"), nil)

		cc := newMockCompatibilityChecker(*engineVersion, semver.VersionRange{Min: must(t, "0.0.0")})

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, []run.RunID{runId}, cc)
		assert.NoError(t, err)

		expectedMetadata := map[string]string{
			"runID":                "abcd",
			"currentEngineVersion": "5.0.0",
			"runVersion":           "6.0.0",
		}
		expectedWarning := message.New(message.EngineRenderCompatibilityIndeterminableTooNew).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedWarning, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("correct warning if one run is indeterminable due to being too new, and one or more too old", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runIDs := []run.RunID{{Value: "1"}, {Value: "2"}, {Value: "3"}, {Value: "4"}}
		dp.On("RunDescription", mock.Anything, runIDs[0]).Return(createArmProvidedRecipeRun("5.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[1]).Return(createArmProvidedRecipeRun("6.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[2]).Return(createArmProvidedRecipeRun("2.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[3]).Return(createArmProvidedRecipeRun("2.0.0"), nil)

		compatibleRange1 := semver.VersionRange{Min: must(t, "1.0.0"), Max: engineVersion, InclusiveMax: true}
		compatibleRange2 := semver.VersionRange{Min: must(t, "1.0.0"), Max: must(t, "3.0.0"), InclusiveMax: true}
		currentSupportedRange := semver.VersionRange{Min: must(t, "3.8.0")}
		cc := newMockCompatibilityChecker(*engineVersion, currentSupportedRange)
		cc.On("CompatibleVersionRange", mock.Anything, *engineVersion).Return(compatibleRange1, nil)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "2.0.0")).Return(compatibleRange2, nil)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, runIDs, cc)
		assert.NoError(t, err)

		expectedMetadata := map[string]string{
			"newRunID":             "2",
			"oldRunID":             "3",
			"currentEngineVersion": "5.0.0",
			"newRunVersion":        "6.0.0",
		}
		expectedWarning := message.New(message.EngineRenderCompatibilityIndeterminableOldAndNew).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedWarning, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("correct warning if multiple runs are indeterminable due to being too new (same version)", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runIDs := []run.RunID{{Value: "1"}, {Value: "2"}, {Value: "3"}}
		dp.On("RunDescription", mock.Anything, runIDs[0]).Return(createArmProvidedRecipeRun("5.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[1]).Return(createArmProvidedRecipeRun("6.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[2]).Return(createArmProvidedRecipeRun("6.0.0"), nil)

		compatibleRange := semver.VersionRange{Min: engineVersion, Max: engineVersion, InclusiveMax: true}
		cc := newMockCompatibilityChecker(*engineVersion, semver.VersionRange{Min: must(t, "0.0.0")})
		cc.On("CompatibleVersionRange", mock.Anything, *engineVersion).Return(compatibleRange, nil)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, runIDs, cc)
		assert.NoError(t, err)

		expectedMetadata := map[string]string{
			"runIDs":               "`2`, `3`",
			"currentEngineVersion": "5.0.0",
			"runVersion":           "6.0.0",
		}
		expectedWarning := message.New(message.EngineRenderCompatibilityIndeterminableTooNewSameVersion).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedWarning, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("correct warning if multiple runs are indeterminable due to being too new (different versions)", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runIDs := []run.RunID{{Value: "1"}, {Value: "2"}, {Value: "3"}}
		dp.On("RunDescription", mock.Anything, runIDs[0]).Return(createArmProvidedRecipeRun("8.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[1]).Return(createArmProvidedRecipeRun("5.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[2]).Return(createArmProvidedRecipeRun("6.0.0"), nil)

		compatibleRange := semver.VersionRange{Min: engineVersion, Max: engineVersion, InclusiveMax: true}
		cc := newMockCompatibilityChecker(*engineVersion, semver.VersionRange{Min: must(t, "0.0.0")})
		cc.On("CompatibleVersionRange", mock.Anything, *engineVersion).Return(compatibleRange, nil)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, runIDs, cc)
		assert.NoError(t, err)

		expectedMetadata := map[string]string{
			"versionMapping":       "`1 (8.0.0)`, `3 (6.0.0)`",
			"currentEngineVersion": "5.0.0",
			"newestRunVersion":     "8.0.0",
		}
		expectedWarning := message.New(message.EngineRenderCompatibilityIndeterminableTooNewPlural).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedWarning, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("correct warning if one run is incompatible due to being too old", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runId := run.RunID{Value: "abcd"}
		dp.On("RunDescription", mock.Anything, runId).Return(createArmProvidedRecipeRun("4.0.0"), nil)

		compatibleRange := semver.VersionRange{Min: must(t, "4.0.0"), Max: must(t, "4.5.6"), InclusiveMax: true}
		currentSupportedRange := semver.VersionRange{Min: must(t, "4.8.0")}
		cc := newMockCompatibilityChecker(*engineVersion, currentSupportedRange)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "4.0.0")).Return(compatibleRange, nil)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, []run.RunID{runId}, cc)
		assert.NoError(t, err)

		expectedMetadata := map[string]string{
			"runID":                      "abcd",
			"runVersion":                 "4.0.0",
			"currentEngineVersion":       "5.0.0",
			"currentMinSupportedVersion": "4.8.0",
			"minCompatibleVersion":       "4.0.0",
			"maxCompatibleVersion":       "4.5.6",
		}
		expectedWarning := message.New(message.EngineRenderCompatibilityIncompatibleTooOld).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedWarning, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("correct warning if multiple runs are incompatible due to being too old (same versions)", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runIDs := []run.RunID{{Value: "1"}, {Value: "2"}, {Value: "3"}}
		dp.On("RunDescription", mock.Anything, runIDs[0]).Return(createArmProvidedRecipeRun("3.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[1]).Return(createArmProvidedRecipeRun("4.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[2]).Return(createArmProvidedRecipeRun("3.0.0"), nil)

		compatibleRange1 := semver.VersionRange{Min: must(t, "3.0.0"), Max: must(t, "4.5.6"), InclusiveMax: true}
		compatibleRange2 := semver.VersionRange{Min: must(t, "4.0.0"), Max: engineVersion, InclusiveMax: true}
		currentSupportedRange := semver.VersionRange{Min: must(t, "3.8.0")}
		cc := newMockCompatibilityChecker(*engineVersion, currentSupportedRange)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "3.0.0")).Return(compatibleRange1, nil)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "4.0.0")).Return(compatibleRange2, nil)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, runIDs, cc)
		assert.NoError(t, err)

		expectedMetadata := map[string]string{
			"runVersion":                 "3.0.0",
			"runIDs":                     "`1`, `3`",
			"currentEngineVersion":       "5.0.0",
			"currentMinSupportedVersion": "3.8.0",
			"minCompatibleVersion":       "4.0.0",
			"maxCompatibleVersion":       "4.5.6",
		}
		expectedWarning := message.New(message.EngineRenderCompatibilityIncompatibleTooOldSameVersion).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedWarning, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("correct warning if multiple runs are incompatible due to being too old (different versions)", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runIDs := []run.RunID{{Value: "1"}, {Value: "2"}, {Value: "3"}}
		dp.On("RunDescription", mock.Anything, runIDs[0]).Return(createArmProvidedRecipeRun("1.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[1]).Return(createArmProvidedRecipeRun("4.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[2]).Return(createArmProvidedRecipeRun("3.0.0"), nil)

		compatibleRange1 := semver.VersionRange{Min: must(t, "1.0.0"), Max: must(t, "4.3.2"), InclusiveMax: true}
		compatibleRange2 := semver.VersionRange{Min: must(t, "4.0.0"), Max: engineVersion, InclusiveMax: true}
		compatibleRange3 := semver.VersionRange{Min: must(t, "3.0.0"), Max: must(t, "4.5.6"), InclusiveMax: true}
		currentSupportedRange := semver.VersionRange{Min: must(t, "4.8.0")}
		cc := newMockCompatibilityChecker(*engineVersion, currentSupportedRange)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "1.0.0")).Return(compatibleRange1, nil)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "4.0.0")).Return(compatibleRange2, nil)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "3.0.0")).Return(compatibleRange3, nil)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, runIDs, cc)
		assert.NoError(t, err)

		expectedMetadata := map[string]string{
			"versionMapping":             "`1 (1.0.0)`, `3 (3.0.0)`",
			"currentEngineVersion":       "5.0.0",
			"currentMinSupportedVersion": "4.8.0",
			"minCompatibleVersion":       "4.0.0",
			"maxCompatibleVersion":       "4.3.2",
		}
		expectedWarning := message.New(message.EngineRenderCompatibilityIncompatibleTooOldPlural).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedWarning, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("correct warning if runs are mutually incompatible", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runIDs := []run.RunID{{Value: "1"}, {Value: "2"}, {Value: "3"}}
		dp.On("RunDescription", mock.Anything, runIDs[0]).Return(createArmProvidedRecipeRun("1.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[1]).Return(createArmProvidedRecipeRun("5.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[2]).Return(createArmProvidedRecipeRun("3.0.0"), nil)

		compatibleRange1 := semver.VersionRange{Min: must(t, "1.0.0"), Max: must(t, "4.1.2"), InclusiveMax: true}
		compatibleRange2 := semver.VersionRange{Min: engineVersion, Max: engineVersion, InclusiveMax: true}
		compatibleRange3 := semver.VersionRange{Min: must(t, "3.0.0"), Max: must(t, "4.5.6"), InclusiveMax: true}
		currentSupportedRange := semver.VersionRange{Min: must(t, "4.8.0")}
		cc := newMockCompatibilityChecker(*engineVersion, currentSupportedRange)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "1.0.0")).Return(compatibleRange1, nil)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "5.0.0")).Return(compatibleRange2, nil)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "3.0.0")).Return(compatibleRange3, nil)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, runIDs, cc)
		assert.NoError(t, err)

		expectedMetadata := map[string]string{
			"runID1":         "2",
			"run1MinVersion": "5.0.0",
			"run1MaxVersion": "5.0.0",
			"runID2":         "3",
			"run2MinVersion": "3.0.0",
			"run2MaxVersion": "4.5.6",
		}
		expectedWarning := message.New(message.EngineRenderCompatibilityIncompatibleMutuallyIncompatible).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedWarning, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("no warning if run version is invalid", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runId := run.RunID{Value: "abcd"}
		dp.On("RunDescription", mock.Anything, runId).Return(createArmProvidedRecipeRun("abc"), nil)

		cc := newMockCompatibilityChecker(*engineVersion, semver.VersionRange{Min: must(t, "0.0.0")})

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, []run.RunID{runId}, cc)
		assert.NoError(t, err)
		assert.NoError(t, result)
	})
	t.Run("no warning if can't determine compatible version range", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runId := run.RunID{Value: "abcd"}
		dp.On("RunDescription", mock.Anything, runId).Return(createArmProvidedRecipeRun("1.0.0"), nil)

		cc := newMockCompatibilityChecker(*engineVersion, semver.VersionRange{Min: must(t, "0.0.0")})
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "1.0.0")).Return(semver.VersionRange{}, testError)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, []run.RunID{runId}, cc)
		assert.NoError(t, err)
		assert.NoError(t, result)
	})
	t.Run("no warning if can't determine current engine compatibility", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runId := run.RunID{Value: "abcd"}
		dp.On("RunDescription", mock.Anything, runId).Return(createArmProvidedRecipeRun("1.0.0"), nil)

		cc := &MockCompatibilityChecker{}
		cc.On("IsRecipeArmProvided", mock.Anything).Return(true)
		cc.On("GetCurrentEngineVersion").Return(*engineVersion)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "1.0.0")).Return(semver.VersionRange{Min: must(t, "1.0.0"), Max: must(t, "1.0.0")}, nil)
		cc.On("GetCurrentCompatibilityForRecipe", mock.Anything).Return(semver.VersionRange{}, testError)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, []run.RunID{runId}, cc)
		assert.NoError(t, err)
		assert.NoError(t, result)
	})
	t.Run("no warning if can't determine current engine compatibility, multiple runs", func(t *testing.T) {
		dp := &MockDescriptionProvider{}
		runIDs := []run.RunID{{Value: "1"}, {Value: "2"}}
		dp.On("RunDescription", mock.Anything, runIDs[0]).Return(createArmProvidedRecipeRun("1.0.0"), nil)
		dp.On("RunDescription", mock.Anything, runIDs[1]).Return(createArmProvidedRecipeRun("1.0.0"), nil)

		cc := &MockCompatibilityChecker{}
		cc.On("IsRecipeArmProvided", mock.Anything).Return(true)
		cc.On("GetCurrentEngineVersion").Return(*engineVersion)
		cc.On("CompatibleVersionRange", mock.Anything, *must(t, "1.0.0")).Return(semver.VersionRange{Min: must(t, "1.0.0"), Max: must(t, "1.0.0")}, nil)
		cc.On("GetCurrentCompatibilityForRecipe", mock.Anything).Return(semver.VersionRange{}, testError)

		result, err := CheckRunCompatibility(context.Background(), dp, recipe.UseInstalledVersion, runIDs, cc)
		assert.NoError(t, err)
		assert.NoError(t, result)
	})
}
