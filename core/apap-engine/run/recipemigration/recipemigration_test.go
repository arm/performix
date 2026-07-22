// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipemigration

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
)

var (
	mig1 = migration{
		from:    "recipe_a1",
		to:      "recipe_a2",
		version: semver.SemVer{Major: 1, Minor: 0, Patch: 0},
	}
	mig2 = migration{
		from:    "recipe_a2",
		to:      "recipe_a3",
		version: semver.SemVer{Major: 1, Minor: 2, Patch: 0},
	}
	mig3 = migration{
		from:    "recipe_a3",
		to:      "recipe_a4",
		version: semver.SemVer{Major: 1, Minor: 2, Patch: 1},
	}
	mig4 = migration{
		from:    "recipe_a4",
		to:      "recipe_a2",
		version: semver.SemVer{Major: 2, Minor: 0, Patch: 6},
	}
	mig5 = migration{
		from:    "recipe_a2",
		to:      "recipe_a5",
		version: semver.SemVer{Major: 2, Minor: 2, Patch: 2},
	}
	mig6 = migration{
		from:    "recipe_a3",
		to:      "recipe_a7",
		version: semver.SemVer{Major: 3, Minor: 0, Patch: 0},
	}
)

func TestSortMigrations(t *testing.T) {
	t.Run("sorts the migrations slice in chronological order", func(t *testing.T) {
		migrations = []migration{mig2, mig5, mig3, mig1, mig4}
		sortMigrations()

		assert.Equal(t, []migration{mig1, mig2, mig3, mig4, mig5}, migrations)
	})
}

func TestGetMigratedName(t *testing.T) {
	migrations = []migration{mig2, mig5, mig3, mig6, mig1, mig4}
	testCases := []struct {
		name       string
		runID      string
		recipeName string
		runVer     string
		expected   string
	}{
		{
			name:       "ignores run whose recipe name doesn't have any migrations",
			runID:      "123",
			recipeName: "different_recipe",
			runVer:     "1.0.0",
			expected:   "different_recipe",
		},
		{
			name:       "applies migrations in chronological order",
			runID:      "456",
			recipeName: "recipe_a3",
			runVer:     "1.2.0",
			expected:   "recipe_a5",
		},
		{
			name:       "ignores migrations introduced before run was created",
			runID:      "789",
			recipeName: "recipe_a3",
			runVer:     "2.5.0",
			expected:   "recipe_a7",
		},
		{
			name:       "returns recipe name unchanged if run version cannot be parsed",
			runID:      "101112",
			recipeName: "recipe_a3",
			runVer:     "lemons",
			expected:   "recipe_a3",
		},
	}
	sortMigrations()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, GetMigratedName(tc.runID, tc.recipeName, tc.runVer))
		})
	}
}
