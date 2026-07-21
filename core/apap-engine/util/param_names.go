// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"strings"
)

func MakeVisualizationParameterKey(vizID string, paramID string) string {
	return fmt.Sprintf("%s.%s", vizID, paramID)
}

func SplitVisualizationParameterKey(key string) (vizID, paramID string, ok bool) {
	vizID, paramID, ok = strings.Cut(key, ".")
	if vizID == "" || paramID == "" || !ok {
		return "", "", false
	}

	return vizID, paramID, true
}
