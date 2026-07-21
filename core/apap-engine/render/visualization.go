// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import "fmt"

// WidgetDataSources is an object that holds the mapping from visualization (and other widget) names to their associated data sources.
type WidgetDataSources struct {
	sourcesByWidget map[string]TableRefMap
}

func NewWidgetDataSources() *WidgetDataSources {
	return &WidgetDataSources{sourcesByWidget: map[string]TableRefMap{}}
}

// AddDataSources adds a new visualization and its associated data sources to the WidgetDataSources object.
// Returns an error if data sources for the given visualization name already exist.
func (v *WidgetDataSources) AddDataSources(visName string, dataSources TableRefMap) error {
	if entry := v.sourcesByWidget[visName]; entry != nil {
		return fmt.Errorf("data sources for visualization '%s' already exist", visName)
	}
	v.sourcesByWidget[visName] = dataSources
	return nil
}

// Get returns the internal map of visualization names to their associated data sources.
func (v *WidgetDataSources) Get() map[string]TableRefMap {
	return v.sourcesByWidget
}

// ResolveWidgetDataSources resolves and adds data sources for each visualization to the session's WidgetDataSources object.
// Resolved data sources are in the form of a map from visualization ID to its corresponding TableRefMap.
func ResolveWidgetDataSources(session Session, renderers RendererList, dataSources map[string]DataSourcesMap) error {
	for id, ds := range dataSources {
		// Ignore errors from vis data source resolution. We'll just have a nil result.
		// If we were to return an error, then we couldn't resolve the other vis data sources.
		resolvedDataSource, _ := ResolveDataSources(session, ds, renderers)

		err := session.WidgetDataSources().AddDataSources(id, resolvedDataSource)
		if err != nil {
			return err
		}
	}
	return nil
}

// ParseWidgetDataSources parses a WidgetConfigList and returns a map from visualization (or other widget) ID to its DataSourcesMap.
func ParseWidgetDataSources(visConfigs WidgetConfigList) (map[string]DataSourcesMap, error) {
	dataSourcesByVisID := map[string]DataSourcesMap{}
	for _, config := range visConfigs {
		if config.ID == nil {
			continue
		}
		dataSources, err := ParseDataSourcesFromConfig(config.ConfigJSON)
		if err != nil {
			return nil, err
		}
		dataSourcesByVisID[*config.ID] = dataSources
	}
	return dataSourcesByVisID, nil
}
