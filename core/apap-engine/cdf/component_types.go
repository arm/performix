// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cdf

const TypeLogText string = "log-text"
const TypeLogJSON string = "log-json"
const TypeVersion string = "version"
const TypeMetadata string = "metadata"

// IsLogComponentType returns true if the given ComponentType is a log type (either text or JSON).
func IsLogComponentType(t ComponentType) bool {
	return t.Name == TypeLogText || t.Name == TypeLogJSON
}
