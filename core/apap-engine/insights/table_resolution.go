// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package insights

import (
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type summaryTableRequirement struct {
	field      *string
	sourceName string
}

func resolveSummaryTables(
	session render.Session,
	visualizationID string,
	summaryName string,
	requirements []summaryTableRequirement,
) error {
	sources := session.WidgetDataSources()
	var refs render.TableRefMap
	if sources != nil {
		refs = sources.Get()[visualizationID]
	}

	missingDataSources := []string{}
	for _, requirement := range requirements {
		*requirement.field = singleTableRef(refs, requirement.sourceName)
		if *requirement.field == "" {
			missingDataSources = append(missingDataSources, requirement.sourceName)
		}
	}

	if len(missingDataSources) > 0 {
		return message.New(message.EngineInsightsRenderTableNotFound).
			WithMetadata(map[string]string{
				"summaryName":    summaryName,
				"componentTypes": util.DisplayErrorStringSlice(missingDataSources),
			})
	}

	return nil
}

func resolveSummaryTablesByComponentType(
	session render.Session,
	summaryName string,
	requirements []summaryTableRequirement,
) error {
	missingComponentTypes := []string{}
	for _, requirement := range requirements {
		*requirement.field = singleTableByComponentType(session.Manifest().Entries(), requirement.sourceName)
		if *requirement.field == "" {
			missingComponentTypes = append(missingComponentTypes, requirement.sourceName)
		}
	}

	if len(missingComponentTypes) > 0 {
		return message.New(message.EngineInsightsRenderTableNotFound).
			WithMetadata(map[string]string{
				"summaryName":    summaryName,
				"componentTypes": util.DisplayErrorStringSlice(missingComponentTypes),
			})
	}

	return nil
}

// singleTableRef returns the single table for a visualization data source.
func singleTableRef(refs render.TableRefMap, name string) string {
	tables := refs[name]
	if len(tables) != 1 {
		return ""
	}
	return tables[0].Name
}

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
