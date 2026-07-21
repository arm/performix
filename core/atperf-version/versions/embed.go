// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package versions

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:embed version.py
var versionPy string

var (
	reVersion = regexp.MustCompile(`(?m)^\s*VERSION\s*=\s*"([^"]+)"\s*$`)
)

// getVersionPy returns VERSION from the embedded version.py, or "" if not found.
func getVersionPy() string {
	m := reVersion.FindStringSubmatch(versionPy)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
