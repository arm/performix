// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"fmt"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-compatibility/compatibility"
)

// RunDetails is a struct containing the information about a run needed to determine compatibility
type RunDetails struct {
	runID              run.RunID
	compatibilityRange semver.VersionRange
	runVersion         semver.SemVer
}

// CheckRunCompatibility takes a list of runs, and computes their compatibility. It returns a warning if one or more
// runs are incompatible with the current engine version, mutually incompatible in any engine version, or if the
// compatibility of one or more runs cannot be determined. Returns nil if the run(s) are compatible in the current engine
// version, if the recipe selection policy is anything other than `use-installed-version`, or if the recipe used in
// the run(s) is not an Arm-provided recipe.
func CheckRunCompatibility(
	ctx context.Context,
	runs run.RunDescriptionProvider,
	policy recipe.SelectionPolicy,
	runIDs []run.RunID,
	compChecker compatibility.CompatibilityChecker) (message.Message, error) {
	if policy != recipe.UseInstalledVersion {
		// We can only statically check compatibility if the user is trying to render their run(s) with the latest
		// version of the associated recipe - otherwise, return no warning
		logx.FromContext(ctx).Debugf("recipe selection policy is not use-installed-version (policy='%v'), skipping static compatibility check", policy)
		return nil, nil
	}

	if len(runIDs) == 0 {
		return nil, nil
	}

	var recipeName string
	runDetails := []RunDetails{}

	for _, id := range runIDs {
		description, err := runs.RunDescription(ctx, id)
		if err != nil {
			return nil, err
		}

		recipeName = description.RecipeName
		if !compatibility.IsRecipeArmProvided(recipeName) {
			// We can only statically check compatibility for runs of Arm-provided recipes - otherwise, return no warning
			logx.FromContext(ctx).Debugf("recipe is user-created ('%v'), skipping static compatibility check", recipeName)
			return nil, nil
		}

		runVersionSemVer, err := semver.ParseSemVer(description.EngineVersion)
		if err != nil {
			// Invalid run engine version, compatibility undefined, log appropriate warning
			logx.FromContext(ctx).Warnf("can't parse engine version of run '%v' (%v), skipping (%v)", id.Value, description.EngineVersion, err.Error())
			continue
		}

		if semver.Cmp(runVersionSemVer, compChecker.GetCurrentEngineVersion()) > 0 {
			// Run too new, compatibility undefined
			runDetails = append(runDetails, RunDetails{
				runID:      id,
				runVersion: runVersionSemVer,
			})
			logx.FromContext(ctx).Warnf("run '%v' is too new (engine version %v)", id.Value, runVersionSemVer)
			continue
		}

		compatibilityRange, err := compChecker.CompatibleVersionRange(recipeName, runVersionSemVer)
		if err != nil {
			// Compatibility undefined, log appropriate warning
			logx.FromContext(ctx).Warnf("compatibility for run '%v' undefined, skipping (%v)", id.Value, err.Error())
			continue
		}
		runDetails = append(runDetails, RunDetails{
			runID:              id,
			compatibilityRange: compatibilityRange,
			runVersion:         runVersionSemVer,
		})
		logx.FromContext(ctx).Debugf("run '%v' from engine version %v, compatible with %v-%v", id.Value, runVersionSemVer, compatibilityRange.Min.String(), compatibilityRange.Max.String())
	}

	currentMinSupportedVersion, err := getMinSupportedVersion(recipeName, compChecker, runIDs, ctx)
	if err != nil {
		// Compatibility of current engine version undefined, log appropriate warning
		logx.FromContext(ctx).Warnf("compatibility range for '%v' undefined for current engine version (%v)", recipeName, err.Error())
		return nil, nil
	}

	return consolidateRuns(runDetails, compChecker, currentMinSupportedVersion), nil
}

// consolidateRuns takes the runDetails for all runs to be rendered and returns an appropriate warning
// (if applicable)
func consolidateRuns(runDetails []RunDetails, compChecker compatibility.CompatibilityChecker, currentMinSupportedVersion semver.SemVer) message.Message {
	compatibleVersionRanges := util.Map(runDetails, func(r RunDetails) semver.VersionRange { return r.compatibilityRange })
	index1, index2 := semver.FindDisjointRanges(compatibleVersionRanges)
	if index1 != -1 && index2 != -1 {
		detail1 := runDetails[index1]
		detail2 := runDetails[index2]
		metadata := map[string]string{
			"runID1":         detail1.runID.Value,
			"run1MinVersion": detail1.compatibilityRange.Min.String(),
			"run1MaxVersion": detail1.compatibilityRange.Max.String(),
			"runID2":         detail2.runID.Value,
			"run2MinVersion": detail2.compatibilityRange.Min.String(),
			"run2MaxVersion": detail2.compatibilityRange.Max.String(),
		}
		return message.New(message.EngineRenderCompatibilityIncompatibleMutuallyIncompatible).WithMetadata(metadata)
	}

	tooOldRuns := []RunDetails{}
	tooNewRuns := []RunDetails{}
	for _, detail := range runDetails {
		if semver.Cmp(detail.runVersion, compChecker.GetCurrentEngineVersion()) > 0 {
			tooNewRuns = append(tooNewRuns, detail)
		} else if !semver.InRange(compChecker.GetCurrentEngineVersion(), detail.compatibilityRange) {
			tooOldRuns = append(tooOldRuns, detail)
		}
	}

	if len(tooOldRuns) == 0 && len(tooNewRuns) == 0 {
		return nil
	}

	// Too new runs take priority over too old (as too new means we can't determine compatibility at all)
	if len(tooNewRuns) > 0 {
		return handleTooNew(tooOldRuns, tooNewRuns, compChecker)
	}
	// len(tooOldRuns) must be > 0
	return handleTooOld(tooOldRuns, compChecker, currentMinSupportedVersion, semver.Intersection(compatibleVersionRanges...))
}

// handleTooNew takes a list of runs that are too old, and of runs that are too new, and returns an appropriate warning
func handleTooNew(tooOldRuns []RunDetails, tooNewRuns []RunDetails, compChecker compatibility.CompatibilityChecker) message.Message {
	if len(tooNewRuns) == 0 {
		return nil
	}

	// Handling order:
	// 1. 1 run too old (by this point we know there are no runs that are too new)
	// 2. Multiple runs too old
	if len(tooNewRuns) == 1 {
		if len(tooOldRuns) == 0 {
			runDetails := tooNewRuns[0]
			metadata := map[string]string{
				"runID":                runDetails.runID.Value,
				"runVersion":           runDetails.runVersion.String(),
				"currentEngineVersion": compChecker.GetCurrentEngineVersion().String(),
			}
			return message.New(message.EngineRenderCompatibilityIndeterminableTooNew).WithMetadata(metadata)
		} else {
			oldRunDetails := tooOldRuns[0]
			newRunDetails := tooNewRuns[0]
			metadata := map[string]string{
				"oldRunID":             oldRunDetails.runID.Value,
				"newRunID":             newRunDetails.runID.Value,
				"currentEngineVersion": compChecker.GetCurrentEngineVersion().String(),
				"newRunVersion":        newRunDetails.runVersion.String(),
			}
			return message.New(message.EngineRenderCompatibilityIndeterminableOldAndNew).WithMetadata(metadata)
		}
	} else {
		runIDs := []string{}
		versionMapping := []string{}
		newestRunVersion := tooNewRuns[0].runVersion

		sameVersions := true
		for _, runDetails := range tooNewRuns {
			runIDs = append(runIDs, runDetails.runID.Value)
			versionMapping = append(versionMapping, fmt.Sprintf("%v (%v)", runDetails.runID.Value, runDetails.runVersion.String()))

			if runDetails.runVersion != newestRunVersion {
				sameVersions = false
				if semver.Cmp(runDetails.runVersion, newestRunVersion) > 0 {
					newestRunVersion = runDetails.runVersion
				}
			}
		}

		metadata := map[string]string{
			"currentEngineVersion": compChecker.GetCurrentEngineVersion().String(),
		}
		if sameVersions {
			metadata["runVersion"] = newestRunVersion.String()
			metadata["runIDs"] = util.DisplayErrorStringSlice(runIDs)
			return message.New(message.EngineRenderCompatibilityIndeterminableTooNewSameVersion).WithMetadata(metadata)
		} else {
			metadata["versionMapping"] = util.DisplayErrorStringSlice(versionMapping)
			metadata["newestRunVersion"] = newestRunVersion.String()
			return message.New(message.EngineRenderCompatibilityIndeterminableTooNewPlural).WithMetadata(metadata)
		}
	}
}

// handleTooNew takes a list of runs that are too old, and returns an appropriate warning
func handleTooOld(tooOldRuns []RunDetails, compChecker compatibility.CompatibilityChecker,
	currentMinSupportedVersion semver.SemVer, compatIntersection *semver.VersionRange) message.Message {
	if len(tooOldRuns) == 0 {
		return nil
	}

	// Handling order:
	// 1. 1 run too new, 0 runs too old
	// 2. 1 run too new, 1 or more runs too old
	// 3. Multiple runs too new (any number of runs too old)
	if len(tooOldRuns) == 1 {
		runDetails := tooOldRuns[0]
		metadata := map[string]string{
			"runID":                      runDetails.runID.Value,
			"runVersion":                 runDetails.runVersion.String(),
			"currentEngineVersion":       compChecker.GetCurrentEngineVersion().String(),
			"currentMinSupportedVersion": currentMinSupportedVersion.String(),
			"minCompatibleVersion":       compatIntersection.Min.String(),
			"maxCompatibleVersion":       compatIntersection.Max.String(),
		}
		return message.New(message.EngineRenderCompatibilityIncompatibleTooOld).WithMetadata(metadata)
	}

	// Now plural case
	runIDs := []string{}
	versionMapping := []string{}

	firstVersion := tooOldRuns[0].runVersion
	sameVersions := true

	for _, runDetails := range tooOldRuns {
		runIDs = append(runIDs, runDetails.runID.Value)
		versionMapping = append(versionMapping, fmt.Sprintf("%v (%v)", runDetails.runID.Value, runDetails.runVersion.String()))

		if runDetails.runVersion != firstVersion {
			sameVersions = false
		}
	}

	metadata := map[string]string{
		"currentEngineVersion":       compChecker.GetCurrentEngineVersion().String(),
		"currentMinSupportedVersion": currentMinSupportedVersion.String(),
		"minCompatibleVersion":       compatIntersection.Min.String(),
		"maxCompatibleVersion":       compatIntersection.Max.String(),
	}
	if sameVersions {
		metadata["runVersion"] = firstVersion.String()
		metadata["runIDs"] = util.DisplayErrorStringSlice(runIDs)
		return message.New(message.EngineRenderCompatibilityIncompatibleTooOldSameVersion).WithMetadata(metadata)
	} else {
		metadata["versionMapping"] = util.DisplayErrorStringSlice(versionMapping)
		return message.New(message.EngineRenderCompatibilityIncompatibleTooOldPlural).WithMetadata(metadata)
	}
}

// getMinSupportedVersion returns the minimum run version supported by the current engine. Returns a warning message
// if the current compatibility is not defined for the specified recipe.
func getMinSupportedVersion(recipeName string, compChecker compatibility.CompatibilityChecker, runIDs []run.RunID, ctx context.Context) (semver.SemVer, error) {
	currentSupportedRange, err := compChecker.GetCurrentCompatibilityForRecipe(recipeName)
	if err != nil {
		return semver.SemVer{}, err
	}
	return *currentSupportedRange.Min, nil
}
