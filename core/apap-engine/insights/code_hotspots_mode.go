// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"fmt"
	"os"
	"strings"
)

const (
	CodeHotspotsRecipeName = "code_hotspots"

	// CodeHotspotsModeEnvironment selects the temporary Code Hotspots AI
	// Insights implementation while APAP-5690 compares curated summaries with
	// SQL queries.
	CodeHotspotsModeEnvironment = "APXD_CODE_HOTSPOTS_AI_INSIGHTS_MODE"
)

// CodeHotspotsMode selects the implementation used for Code Hotspots AI
// Insights.
type CodeHotspotsMode string

const (
	CodeHotspotsModeCurated  CodeHotspotsMode = "curated"
	CodeHotspotsModeRunQuery CodeHotspotsMode = "run_query"
)

// CodeHotspotsModeFromEnvironment returns the configured Code Hotspots
// implementation. The curated implementation is the default.
func CodeHotspotsModeFromEnvironment() (CodeHotspotsMode, error) {
	value := strings.TrimSpace(os.Getenv(CodeHotspotsModeEnvironment))
	switch CodeHotspotsMode(value) {
	case "", CodeHotspotsModeCurated:
		return CodeHotspotsModeCurated, nil
	case CodeHotspotsModeRunQuery:
		return CodeHotspotsModeRunQuery, nil
	default:
		return "", fmt.Errorf(
			"%s must be %q or %q, got %q",
			CodeHotspotsModeEnvironment,
			CodeHotspotsModeCurated,
			CodeHotspotsModeRunQuery,
			value,
		)
	}
}
