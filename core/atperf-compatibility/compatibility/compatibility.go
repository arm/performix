// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package compatibility

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

// ------------------------
// Raw matrix representations
// ------------------------

//go:embed matrix.json
var rawMatrix []byte
var structMatrix matrix

type matrix struct {
	Compatibility map[string]recipeCompatibility `json:"compatibility"`
	Version       string                         `json:"version"`
}

type recipeCompatibility map[string]versionSpec

type versionSpec struct {
	Minimum string `json:"minimum_version"`
}

// ------------------------
// Internal variables for usage
// ------------------------

const nextVersionString = "NEXT_VERSION"

var compatibilityMatrix map[string]map[semver.SemVer]semver.VersionRange

// ------------------------
// Initialisation
// ------------------------

func init() {
	loadMatrix()
}

// loadMatrix unmarshals the raw representation of matrix.json stored in rawMatrix into a
// structured representation in compatibilityMatrix.
func loadMatrix() {
	if err := json.Unmarshal(rawMatrix, &structMatrix); err != nil {
		panic(fmt.Errorf("failed to load renderer compatibility matrix: %w", err))
	}

	compatibilityMatrix = make(map[string]map[semver.SemVer]semver.VersionRange)

	for name, recipeCompat := range structMatrix.Compatibility {
		entry := make(map[semver.SemVer]semver.VersionRange)
		for engineVersion, vs := range recipeCompat {
			if engineVersion == nextVersionString {
				continue
			}

			engVer, err := semver.ParseSemVer(engineVersion)
			if err != nil {
				panic(fmt.Sprintf("engine version '%v' for '%v' isn't a valid semver", engineVersion, name))
			}

			minVer, err := semver.ParseSemVer(vs.Minimum)
			if err != nil {
				panic(fmt.Sprintf("minimum_version '%v' for '%v' of '%v' isn't a valid semver", vs.Minimum, engineVersion, name))
			}

			entry[engVer] = semver.VersionRange{
				Min: &minVer,
			}
		}
		compatibilityMatrix[name] = entry
	}
}

// ------------------------
// CompatibilityChecker
// ------------------------

type CompatibilityChecker interface {
	GetCurrentCompatibilityForRecipe(string) (semver.VersionRange, error)
	CompatibleVersionRange(string, semver.SemVer) (semver.VersionRange, error)
	GetCurrentEngineVersion() semver.SemVer
}

type ConcreteCompatibilityChecker struct{}

// GetCurrentCompatibilityForRecipe returns a cdf.VersionRange representation of the range of engine
// versions whose runs can be rendered in the current engine version, for the provided recipe. Returns
// an error if the compatibility for the specified recipe (or that recipe in the current engine version)
// is not defined.
func (c *ConcreteCompatibilityChecker) GetCurrentCompatibilityForRecipe(recipeName string) (semver.VersionRange, error) {
	recipeVersioning, ok := compatibilityMatrix[recipeName]
	if !ok {
		return semver.VersionRange{}, fmt.Errorf("compatibility for recipe '%v' undefined", recipeName)
	}
	currentEngVer := versions.GetVersionSemVer()
	if vr, ok := recipeVersioning[currentEngVer]; ok {
		return vr, nil
	}
	return semver.VersionRange{}, fmt.Errorf("compatibility for recipe '%v' in current engine version '%v' undefined", recipeName, currentEngVer.String())
}

// CompatibleVersionRange takes the recipe and engine version with which a run was produced, and
// returns a cdf.VersionRange representing the minimum and maximum (inclusive) engine versions
// which are listed as being able to render such a run. Returns an error if compatibility for the
// provided recipe and run version is not defined.
func (c *ConcreteCompatibilityChecker) CompatibleVersionRange(recipeName string, runVersion semver.SemVer) (semver.VersionRange, error) {
	maxVer := runVersion
	vr := semver.VersionRange{Min: &runVersion, Max: &maxVer, InclusiveMax: true}
	foundCompatibleVersion := false
	for engVer, verRange := range compatibilityMatrix[recipeName] {
		if semver.Cmp(*verRange.Min, runVersion) <= 0 {
			foundCompatibleVersion = true
			if semver.Cmp(engVer, maxVer) > 0 {
				maxVer = engVer
			}
		}
	}
	if !foundCompatibleVersion {
		return semver.VersionRange{}, fmt.Errorf("no engine version found compatible with version '%v' of '%v'", runVersion, recipeName)
	}

	return vr, nil
}

// GetCurrentEngineVersion returns a semver.SemVer representation of the current engine version. Implemented as
// a method of the interface for easier mocking.
func (c *ConcreteCompatibilityChecker) GetCurrentEngineVersion() semver.SemVer {
	return versions.GetVersionSemVer()
}

// ------------------------
// Helper functions
// ------------------------

// IsRecipeArmProvided returns true if the provided recipe is one created by Arm (not a user-created recipe).
func IsRecipeArmProvided(recipeName string) bool {
	_, ok := compatibilityMatrix[recipeName]
	return ok
}
