// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package apiversion

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
)

// These versions are used to identify recipes that use the legacy API of
// defining radio/select parameter options as an array of strings. They maintain
// backwards compatibility while the richer option-object API is adopted.
var LegacyRecipeRadioStringOptionsAPIVersion = semver.SemVer{Major: 1, Minor: 0, Patch: 0}

var LegacyRecipeSelectStringOptionsAPIVersion = semver.SemVer{Major: 1, Minor: 0, Patch: 0}
