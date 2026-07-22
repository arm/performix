// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	stateComponentDefault = "state.xml"
	stateDefaultEntity    = "tool/neoprof/0/"
	timeLimitsTableName   = "time_limits"
)

// TimeRangeConfig holds renderer-level configuration options.
type TimeRangeConfig struct {
	Entity string `json:"entity"`
}

// TimeRangeRenderer parses state.xml and emits DuckDB tables for profiling time ranges.
type TimeRangeRenderer struct {
	config         *render.Config
	specificConfig *TimeRangeConfig
}

// Name returns the renderer identifier.
func (renderer *TimeRangeRenderer) Name() string {
	return "TimeRangeParser"
}

// Version returns the renderer schema version.
func (renderer *TimeRangeRenderer) Version() string {
	return "0.1.0"
}

// Configure stores the renderer configuration and parses renderer-specific JSON.
func (renderer *TimeRangeRenderer) Configure(config *render.Config) error {
	renderer.config = config

	parsed, err := util.DecodeJSON[TimeRangeConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}

	renderer.specificConfig = parsed
	return nil
}

// getEntity returns the entity root used to locate state.xml.
func (renderer *TimeRangeRenderer) getEntity() string {
	if renderer.specificConfig != nil && renderer.specificConfig.Entity != "" {
		return renderer.specificConfig.Entity
	}
	return stateDefaultEntity
}

// manifestEntry builds the manifest entry info for a given output component type.
func (renderer *TimeRangeRenderer) manifestEntry(
	componentTypeName string,
	associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

// getProfilingTimeRange reads the profiling state file and returns the profiling duration in nanoseconds and the absolute start time in microseconds.
func getProfilingTimeRange(stateFile string) (int64, int64, error) {
	profilingState, err := util.ReadXMLFile[ProfilingState](stateFile)
	if err != nil {
		return 0, 0, err
	}

	switch profilingState.TimeUnit {
	case "nanoseconds":
		return profilingState.StopTime, profilingState.Created, nil
	default:
		return 0, 0, fmt.Errorf("unable to convert time unit to ns: unsupported time unit %v", profilingState.TimeUnit)
	}
}

// createTimeRangeTable builds the time range table and populates it with a single row containing the profiling duration.
func (renderer *TimeRangeRenderer) createTimeRangeTable(
	session render.Session,
	tableName string,
	endTimeNS int64,
	absStartTimeUS int64,
) error {

	query := fmt.Sprintf(`CREATE TABLE %s (
		start_time_ns BIGINT,
		end_time_ns BIGINT,
		abs_start_time_us BIGINT
	)`, tableName)

	_, err := session.Database().Conn.ExecContext(
		context.Background(),
		query,
	)

	if err != nil {
		return fmt.Errorf("failed to create time range table: %w", err)
	}

	_, err = session.Database().Conn.ExecContext(
		context.Background(),
		fmt.Sprintf(`INSERT INTO %s (start_time_ns, end_time_ns, abs_start_time_us) VALUES (?, ?, ?)`, tableName),
		int64(0), endTimeNS, absStartTimeUS,
	)

	if err != nil {
		return fmt.Errorf("failed to insert into time range table: %w", err)
	}
	return nil
}

// Initialize creates the time range table for each run and populates it with a single row containing the profiling duration.
func (renderer *TimeRangeRenderer) Initialize(
	session render.Session,
	_ map[string][]render.TableRef,
) error {
	for _, entry := range session.Content().Entries {
		tableName := session.Manifest().AddEntry(
			renderer.manifestEntry(timeLimitsTableName, []run.RunID{entry.ID}),
		)

		component, err := entry.Model.ResolveComponent(
			filepath.Join(renderer.getEntity(), stateComponentDefault),
		)
		if err != nil {
			return err
		}

		endTimeNS, absStartTimeUS, err := getProfilingTimeRange(component.AbsolutePath)
		if err != nil {
			return err
		}

		if err := renderer.createTimeRangeTable(session, tableName, endTimeNS, absStartTimeUS); err != nil {
			return err
		}
	}

	return nil
}

// GetInputSpec declares that the renderer has no dependencies.
func (renderer *TimeRangeRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

// GetOutputSpec describes the per-run outputs of the renderer.
func (renderer *TimeRangeRenderer) GetOutputSpec() render.OutputSpec {
	return render.OutputSpec{
		PortList: render.PortList{
			Ports: []render.PortSpec{
				{
					Name:        "time_limits",
					Cardinality: render.CardinalityPerRun,
					ComponentType: cdf.ComponentType{
						Name:          timeLimitsTableName,
						SchemaVersion: renderer.Version(),
					},
				},
			},
		},
	}
}
