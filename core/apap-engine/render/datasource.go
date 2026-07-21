// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// TableRef is the result of resolving a DataSource.
type TableRef struct {
	Name    string
	Pending bool
}

// DataSource is used in renderers that depend on various data sources (that may be coming from different
// renderer invocations).
// Currently only tables are supported as data sources, but this could be extended in the future.
type DataSource interface {
	Resolve(Session, RendererList) (TableRef, error)
}

type DataSourcesMap map[string][]DataSource
type TableRefMap map[string][]TableRef

// IsPending returns true if any table referenced in this TableRefMap is marked as pending
func (t TableRefMap) IsPending() bool {
	for _, tables := range t {
		if slices.ContainsFunc(tables, func(ref TableRef) bool {
			return ref.Pending
		}) {
			return true
		}
	}
	return false
}

// OutputTableRef is a DataSource that references a table by the renderer ID, component type, and index.
type OutputTableRef struct {
	RendererID   string
	Output       string
	ContentIndex int
}

// isRunIDInContent checks if the given run ID is in the list of associated content.
func isRunIDInContent(associatedContent []run.RunID, runIDToMatch run.RunID) bool {
	for _, runID := range associatedContent {
		if runID.Value == runIDToMatch.Value {
			return true
		}
	}
	return false
}

func (r *OutputTableRef) Resolve(session Session, renderers RendererList) (TableRef, error) {
	manifest := session.Manifest()
	// Get the run IDs from the session content. The datasource index is used to find the run ID in the list.
	// When multiple tables are produced by the same renderer, we use the runID to match with the associated content for that table.
	runIDToMatch := run.RunID{}
	if entry := session.Content().FindByIndex(r.ContentIndex); entry != nil {
		runIDToMatch = entry.ID
	}
	matchRendererID := false
	matchRunID := false
	foundTables := []TableRef{}
	for _, entry := range manifest.entries {
		if entry.info.RendererIdentity().ID == nil {
			continue
		}
		if *entry.info.RendererIdentity().ID == r.RendererID {
			matchRendererID = true
			if isRunIDInContent(entry.info.AssociatedContent(), runIDToMatch) {
				// Narrowed down the entry to the correct renderer and index (based on associated content).
				// We will then have to match the output component to get the correct table.
				matchRunID = true
				renderer := renderers[entry.info.RendererIndex()]
				if renderer == nil {
					return TableRef{}, fmt.Errorf("renderer with ID '%s' not found", r.RendererID)
				}
				outSpec := renderer.GetOutputSpec()
				out := outSpec.Get(r.Output)
				if out == nil {
					return TableRef{}, fmt.Errorf("failed to get output '%s' from renderer '%s'", r.Output, r.RendererID)
				}
				if out.ComponentType == entry.info.ComponentType() {
					foundTables = append(foundTables, TableRef{Name: entry.TableName(), Pending: entry.info.Pending()})
				}
			}
		}
	}
	if !matchRendererID {
		return TableRef{}, fmt.Errorf("rendererID '%s' not found for output '%s'", r.RendererID, r.Output)
	} else if !matchRunID {
		return TableRef{}, fmt.Errorf("table for runID '%s' not found, for output '%s' and rendererID '%s'", runIDToMatch.Value, r.Output, r.RendererID)
	} else if len(foundTables) == 0 {
		return TableRef{}, fmt.Errorf("no table found with matching component for output '%s', rendererID '%s'", r.Output, r.RendererID)
	} else if len(foundTables) > 1 {
		// Currently this means you cannot do a comparison between 2 same runs, they would have to be different runs.
		return TableRef{}, fmt.Errorf("multiple tables found with matching component for output '%s', rendererID '%s' and runID '%s'", r.Output, r.RendererID, runIDToMatch.Value)
	} else {
		return foundTables[0], nil
	}
}

// TableRefSource is a DataSource that references a table directly by its name.
type TableRefSource struct {
	Name string
}

func (d *TableRefSource) Resolve(session Session, renderers RendererList) (TableRef, error) {
	if d.Name == "" {
		return TableRef{}, fmt.Errorf("TableRefSource has no name set")
	}
	return TableRef{Name: d.Name}, nil
}

// ResolveDataSources resolves a map of DataSource values to a map of TableRef values.
func ResolveDataSources(session Session, dataSources map[string][]DataSource, renderers RendererList) (TableRefMap, error) {
	newDataSources := TableRefMap{}

	for key, values := range dataSources {
		for _, v := range values {
			directTable, err := v.Resolve(session, renderers)
			if err != nil {
				return nil, err
			}
			newDataSources[key] = append(newDataSources[key], directTable)
		}
	}
	return newDataSources, nil
}

// ValidateDataSources checks if the provided data sources match the input specification, returning an error if they do not.
func ValidateDataSources(manifest *Manifest, dataSources TableRefMap, inputSpec InputSpec) error {
	inputSet := map[string]struct{}{}
	for name, tables := range dataSources {
		if len(tables) == 0 {
			return fmt.Errorf("data source '%s' has no tables", name)
		}

		// Check that all tables have matching components
		tableComponentType, err := manifest.GetComponentTypeFromTable(tables[0].Name)
		if err != nil {
			return err
		}
		for _, table := range tables[1:] {
			ct, err := manifest.GetComponentTypeFromTable(table.Name)
			if err != nil {
				return err
			}
			if ct.Name != tableComponentType.Name || ct.SchemaVersion != tableComponentType.SchemaVersion {
				return fmt.Errorf("data source '%s' has tables with different component types: '%s:%s' and '%s:%s'",
					name, tableComponentType.Name, tableComponentType.SchemaVersion, ct.Name, ct.SchemaVersion,
				)
			}
		}

		// Check that the component type matches the input spec
		input := inputSpec.Get(name)
		if input == nil {
			return fmt.Errorf("data source '%s' not found in input spec", name)
		}
		if tableComponentType.Name == input.ComponentType.Name &&
			tableComponentType.SchemaVersion == input.ComponentType.SchemaVersion {
			// Match
			inputSet[name] = struct{}{}
		} else {
			return fmt.Errorf("data source '%s' has component type '%s:%s', expected '%s:%s'",
				name, tableComponentType.Name, tableComponentType.SchemaVersion, input.ComponentType.Name, input.ComponentType.SchemaVersion,
			)
		}
	}
	for _, input := range inputSpec.Ports {
		if _, ok := inputSet[input.Name]; !ok {
			return fmt.Errorf("required input '%s' not provided in data sources", input.Name)
		}
	}
	return nil
}

// DataSourceDecodeHook is a custom decode hook for converting data source to an implementation of render.DataSource.
func DataSourceDecodeHook(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
	// Only handle conversion to render.DataSource
	if to == reflect.TypeOf((*DataSource)(nil)).Elem() {
		if m, ok := data.(map[string]interface{}); ok {
			// Detect TableRefSource
			if name, ok := m["name"].(string); ok && len(m) == 1 {
				return &TableRefSource{Name: name}, nil
			}
			// Detect OutputTableRef
			id, idOk := m["renderer_id"].(string)
			ct, ctOk := m["output"].(string)
			idx, idxOk := m["content_index"].(float64)
			if idOk && ctOk {
				if !idxOk {
					// Default to 0 if content_index is not provided
					idx = 0
				}
				return &OutputTableRef{
					RendererID:   id,
					Output:       ct,
					ContentIndex: int(idx),
				}, nil
			}
			return nil, fmt.Errorf("invalid DataSource format: %+v", m)
		}
	}
	return data, nil
}

// DecodedDataSourceConfig is a struct used to decode the data source configuration from the renderer config JSON.
type DecodedDataSourceConfig struct {
	DataSources struct {
		Tables map[string][]DataSource `json:"tables"`
	} `json:"data_source"`
}

// ParseDataSourcesFromConfig parses the data sources from a JSON configuration string, returning a map of data sources.
func ParseDataSourcesFromConfig(configJSON string) (map[string][]DataSource, error) {
	if len(configJSON) == 0 {
		return nil, nil
	}
	result, err := util.DecodeJSONWithHook[DecodedDataSourceConfig]([]byte(configJSON), DataSourceDecodeHook)
	if err != nil {
		return nil, fmt.Errorf("failed to parse data sources from config: %w", err)
	}

	return result.DataSources.Tables, nil
}
