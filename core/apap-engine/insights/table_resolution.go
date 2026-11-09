// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import "github.com/Arm-Debug/apap-cli/apap-engine/render"

func singleTableByComponentType(entries []render.ManifestEntry, componentTypeName string) string {
	tableName := ""
	for _, entry := range entries {
		if entry.Info().ComponentType().Name != componentTypeName {
			continue
		}
		if tableName != "" {
			return ""
		}
		tableName = entry.TableName()
	}
	return tableName
}
