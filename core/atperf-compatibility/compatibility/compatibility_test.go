// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package compatibility

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

const FAKE_RECIPE_NAME = "myRecipe"

var currentEngVer = versions.GetVersionSemVer()

var compChecker ConcreteCompatibilityChecker

func must(t *testing.T, s string) *semver.SemVer {
	v, err := semver.ParseSemVer(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return &v
}

func TestGetCurrentCompatibilityForRecipe(t *testing.T) {
	t.Run("succeeds when current compatibility for recipe is defined", func(t *testing.T) {
		defer loadMatrix()
		expected := semver.VersionRange{
			Min: must(t, "0.3.8"),
			Max: must(t, "2.1.4"),
		}
		compatibilityMatrix = map[string]map[semver.SemVer]semver.VersionRange{}
		compatibilityMatrix[FAKE_RECIPE_NAME] = map[semver.SemVer]semver.VersionRange{currentEngVer: expected}
		actual, err := compChecker.GetCurrentCompatibilityForRecipe(FAKE_RECIPE_NAME)
		assert.NoError(t, err)
		assert.Equal(t, expected, actual)
	})
	t.Run("fails when compatibility for recipe is not defined", func(t *testing.T) {
		defer loadMatrix()
		compatibilityMatrix = map[string]map[semver.SemVer]semver.VersionRange{}
		_, err := compChecker.GetCurrentCompatibilityForRecipe(FAKE_RECIPE_NAME)
		expectedErr := fmt.Errorf("compatibility for recipe '%v' undefined", FAKE_RECIPE_NAME)
		assert.Equal(t, expectedErr, err)
	})
	t.Run("fails when current compatibility for recipe is not defined", func(t *testing.T) {
		defer loadMatrix()
		compatibilityMatrix = map[string]map[semver.SemVer]semver.VersionRange{}
		compatibilityMatrix[FAKE_RECIPE_NAME] = map[semver.SemVer]semver.VersionRange{}
		_, err := compChecker.GetCurrentCompatibilityForRecipe(FAKE_RECIPE_NAME)
		expectedErr := fmt.Errorf("compatibility for recipe '%v' in current engine version '%v' undefined", FAKE_RECIPE_NAME, versions.GetVersion())
		assert.Equal(t, expectedErr, err)
	})
}

func TestCompatibleVersionRange(t *testing.T) {
	runVersion := *must(t, "1.1.1")
	testCases := []struct {
		name              string
		versioning        map[semver.SemVer]semver.VersionRange
		expectedRange     semver.VersionRange
		expectedErrString string
		unset             bool
	}{
		{
			name: "no compatible engine version found",
			versioning: map[semver.SemVer]semver.VersionRange{
				*must(t, "2.0.0"): {Min: must(t, "2.0.0")},
				*must(t, "2.0.1"): {Min: must(t, "2.0.0")},
				*must(t, "3.0.0"): {Min: must(t, "2.0.0")},
				*must(t, "3.0.1"): {Min: must(t, "2.0.0")},
			},
			expectedErrString: fmt.Sprintf("no engine version found compatible with version '%v' of '%v'", runVersion.String(), FAKE_RECIPE_NAME),
		},
		{
			name:              "no compatible engine version found (empty versioning map)",
			versioning:        map[semver.SemVer]semver.VersionRange{},
			expectedErrString: fmt.Sprintf("no engine version found compatible with version '%v' of '%v'", runVersion.String(), FAKE_RECIPE_NAME),
		},
		{
			name: "all versions support",
			versioning: map[semver.SemVer]semver.VersionRange{
				runVersion:        {Min: must(t, "0.2.0")},
				*must(t, "2.0.0"): {Min: must(t, "0.3.0")},
				*must(t, "3.0.0"): {Min: must(t, "0.3.1")},
				*must(t, "3.0.1"): {Min: must(t, "1.0.0")},
			},
			expectedRange: semver.VersionRange{Min: must(t, "1.1.1"), Max: must(t, "3.0.1"), InclusiveMax: true},
		},
		{
			name: "some versions support",
			versioning: map[semver.SemVer]semver.VersionRange{
				runVersion:        {Min: must(t, "0.2.0")},
				*must(t, "2.0.0"): {Min: must(t, "0.3.0")},
				*must(t, "3.0.0"): {Min: must(t, "2.0.0")},
				*must(t, "3.0.1"): {Min: must(t, "2.0.1")},
			},
			expectedRange: semver.VersionRange{Min: must(t, "1.1.1"), Max: must(t, "2.0.0"), InclusiveMax: true},
		},
		{
			name: "ignores versions earlier than run version",
			versioning: map[semver.SemVer]semver.VersionRange{
				*must(t, "1.0.0"): {Min: must(t, "0.3.0")},
				runVersion:        {Min: must(t, "0.3.0")},
				*must(t, "3.0.0"): {Min: must(t, "0.3.0")},
				*must(t, "3.0.1"): {Min: must(t, "0.3.0")},
			},
			expectedRange: semver.VersionRange{Min: must(t, "1.1.1"), Max: must(t, "3.0.1"), InclusiveMax: true},
		},
	}
	for _, testCase := range testCases {
		if testCase.unset {
			delete(compatibilityMatrix, FAKE_RECIPE_NAME)
		} else {
			compatibilityMatrix[FAKE_RECIPE_NAME] = testCase.versioning
		}

		vr, err := compChecker.CompatibleVersionRange(FAKE_RECIPE_NAME, runVersion)
		if testCase.expectedErrString != "" {
			assert.ErrorContains(t, err, testCase.expectedErrString, fmt.Sprintf("test '%v' failed", testCase.name))
		} else {
			assert.Equal(t, testCase.expectedRange, vr, fmt.Sprintf("test '%v' failed", testCase.name))
		}

		loadMatrix()
	}
}

func TestValidateMatrix(t *testing.T) {
	var armProvidedRecipes = []string{"code_hotspots", "instruction_mix", "memory_access", "cpu_microarchitecture"}
	loadMatrix()
	t.Run("one entry exists for all Arm-provided recipes, and only for these recipes", func(t *testing.T) {
		var foundRecipes = []string{}
		for recipeName := range compatibilityMatrix {
			if !slices.Contains(armProvidedRecipes, recipeName) {
				assert.Fail(t, fmt.Sprintf("unexpected recipe '%v' in matrix.json. If this is a new Arm-provided recipe, please update the list of core recipes in this test", recipeName))
			}
			foundRecipes = append(foundRecipes, recipeName)
		}
		for _, coreRecipe := range armProvidedRecipes {
			if !slices.Contains(foundRecipes, coreRecipe) {
				assert.Fail(t, fmt.Sprintf("compatibility for Arm-provided recipe '%v' not defined in matrix.json", coreRecipe))
			}
		}
	})
	t.Run("entries for current engine version for all Arm-provided recipes are present", func(t *testing.T) {
		for _, recipeName := range armProvidedRecipes {
			_, ok := compatibilityMatrix[recipeName][currentEngVer]
			assert.True(t, ok, fmt.Sprintf("compatibility for Arm-provided recipe '%v' undefined for current engine version '%v'", recipeName, currentEngVer))
		}
	})
	t.Run("no entry exists that doesn't support runs from its own version", func(t *testing.T) {
		for recipeName, compat := range compatibilityMatrix {
			for engVer, versionRange := range compat {
				assert.True(t, semver.Cmp(engVer, *versionRange.Min) >= 0, fmt.Sprintf("engine version '%v' is listed as not being compatible with '%v' runs of the same version", engVer, recipeName))
			}
		}
	})
}
