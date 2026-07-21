// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package versions

import (
	"fmt"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
)

// version is the build version. The actual version string is injected by CI
// for release and dev builds. Local builds will have the default value.
// When referencing the version in code, use GetVersion() instead of version.
// GetVersion() will use the version from version.py if version is not set.
//
// If this variable is renamed or moved, the goreleaser job will need to be
// updated accordingly - check all goreleaser files
var version = "0.0.0-dev"

var effectiveVersionString string
var effectiveVersionSemVer semver.SemVer

// init() initialises the effective version variables at initialisation time, to fail early if the version is invalid,
// and avoid repeated reads of version.py
func init() {
	initEffectiveVersionString()
	initEffectiveVersionSemVer()
}

func initEffectiveVersionString() {
	v := strings.TrimSpace(version)
	if v == "" || v == "0.0.0-dev" {
		if ev := strings.TrimSpace(getVersionPy()); ev != "" {
			v = ev
		}
	}

	effectiveVersionString = v
}

func initEffectiveVersionSemVer() {
	if v, err := semver.ParseSemVer(GetVersion()); err == nil {
		effectiveVersionSemVer = v
	} else {
		panic(fmt.Sprintf("current engine version '%v' isn't a valid semver", GetVersion()))
	}
}

// GetVersion returns the effective build version.
func GetVersion() string {
	return effectiveVersionString
}

// GetVersionSemVer returns the effective build version as a semver.SemVer
func GetVersionSemVer() semver.SemVer {
	return effectiveVersionSemVer
}
