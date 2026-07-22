// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql/driver"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	applicationsComponentDefault = "applications.xml"
	applicationsDefaultEntity    = "tool/neoprof/0/"
	ProcessesTableName           = "processes"
	ThreadsTableName             = "threads"
)

type applicationsFile struct {
	XMLName   xml.Name              `xml:"applications"`
	Processes []applicationsProcess `xml:"process"`
}

type applicationsProcess struct {
	UID     string               `xml:"uid,attr"`
	PID     string               `xml:"pid,attr"`
	VMUID   string               `xml:"vmUID,attr"`
	Start   string               `xml:"start,attr"`
	End     string               `xml:"end,attr"`
	Name    string               `xml:"name,attr"`
	Threads []applicationsThread `xml:"thread"`
}

type applicationsThread struct {
	UID    string `xml:"uid,attr"`
	TID    string `xml:"tid,attr"`
	Start  string `xml:"start,attr"`
	End    string `xml:"end,attr"`
	Kernel string `xml:"kernel,attr"`
	Idle   string `xml:"idle,attr"`
	Name   string `xml:"name,attr"`
}

// processRow represents a single row in the processes table.
type processRow struct {
	processUID int64
	pid        int64
	vmUID      int64
	name       string
	startTime  *int64
	endTime    *int64
}

// threadRow represents a single row in the threads table.
type threadRow struct {
	threadUID  int64
	tid        int64
	pid        int64
	processUID int64
	name       string
	kernel     bool
	idle       bool
	startTime  *int64
	endTime    *int64
}

// ProcessesAndThreadsConfig holds renderer-level configuration options.
type ProcessesAndThreadsConfig struct {
	Entity string `json:"entity"`
}

// ProcessesAndThreadsRenderer parses applications.xml and emits DuckDB tables for processes and threads.
type ProcessesAndThreadsRenderer struct {
	config         *render.Config
	specificConfig *ProcessesAndThreadsConfig
}

// Name returns the renderer identifier.
func (renderer *ProcessesAndThreadsRenderer) Name() string {
	return "ProcessesAndThreadsParser"
}

// Version returns the renderer schema version.
func (renderer *ProcessesAndThreadsRenderer) Version() string {
	return "0.1.0"
}

// Configure stores the renderer configuration and parses renderer-specific JSON.
func (renderer *ProcessesAndThreadsRenderer) Configure(config *render.Config) error {
	renderer.config = config

	parsed, err := util.DecodeJSON[ProcessesAndThreadsConfig]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}

	renderer.specificConfig = parsed
	return nil
}

// getEntity returns the entity root used to locate applications.xml.
func (renderer *ProcessesAndThreadsRenderer) getEntity() string {
	if renderer.specificConfig != nil && renderer.specificConfig.Entity != "" {
		return renderer.specificConfig.Entity
	}
	return applicationsDefaultEntity
}

// manifestEntry builds the manifest entry info for a given output component type.
func (renderer *ProcessesAndThreadsRenderer) manifestEntry(
	componentTypeName string,
	associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(
		cdf.ComponentType{Name: componentTypeName, SchemaVersion: renderer.Version()},
		renderer.config.Identity,
		associatedContent,
	)
}

// loadApplications opens and parses applications.xml into process and thread rows.
func loadApplications(path string) ([]processRow, []threadRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open applications xml: %w", err)
	}
	defer file.Close()

	return parseApplications(file)
}

// parseApplications decodes applications.xml content into process and thread rows.
func parseApplications(reader io.Reader) ([]processRow, []threadRow, error) {
	var parsed applicationsFile
	if err := xml.NewDecoder(reader).Decode(&parsed); err != nil {
		return nil, nil, fmt.Errorf("failed to decode applications xml: %w", err)
	}

	processes := make([]processRow, 0, len(parsed.Processes))
	threads := make([]threadRow, 0)

	for _, proc := range parsed.Processes {
		pid, err := parseRequiredInt(proc.PID, "pid", proc.Name)
		if err != nil {
			return nil, nil, err
		}

		processUID, err := parseRequiredInt(proc.UID, "uid", proc.Name)
		if err != nil {
			return nil, nil, err
		}

		vmUID, err := parseRequiredInt(proc.VMUID, "vmUID", proc.Name)
		if err != nil {
			return nil, nil, err
		}

		start, err := parseOptionalInt(proc.Start, "start", fmt.Sprint("process pid ", pid))
		if err != nil {
			return nil, nil, err
		}

		end, err := parseOptionalInt(proc.End, "end", fmt.Sprint("process pid ", pid))
		if err != nil {
			return nil, nil, err
		}

		processes = append(processes, processRow{
			processUID: processUID,
			pid:        pid,
			vmUID:      vmUID,
			name:       proc.Name,
			startTime:  start,
			endTime:    end,
		})

		for _, th := range proc.Threads {
			tid, err := parseRequiredInt(th.TID, "tid", th.Name)
			if err != nil {
				return nil, nil, err
			}

			threadUID, err := parseRequiredInt(th.UID, "uid", th.Name)
			if err != nil {
				return nil, nil, err
			}

			threadStart, err := parseOptionalInt(th.Start, "start", fmt.Sprint("thread tid ", tid))
			if err != nil {
				return nil, nil, err
			}

			threadEnd, err := parseOptionalInt(th.End, "end", fmt.Sprint("thread tid ", tid))
			if err != nil {
				return nil, nil, err
			}

			kernel, err := parseYesNo(th.Kernel, "kernel", tid)
			if err != nil {
				return nil, nil, err
			}

			idle, err := parseYesNo(th.Idle, "idle", tid)
			if err != nil {
				return nil, nil, err
			}

			threads = append(threads, threadRow{
				threadUID:  threadUID,
				tid:        tid,
				pid:        pid,
				processUID: processUID,
				name:       th.Name,
				kernel:     kernel,
				idle:       idle,
				startTime:  threadStart,
				endTime:    threadEnd,
			})
		}
	}

	return processes, threads, nil
}

// parseRequiredInt converts a required string attribute into an int64, returning an error on failure.
// raw: the attribute text value, field: attribute name (for error message), context: human-readable owner (e.g., process/thread).
func parseRequiredInt(raw, field, context string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s '%s' for %s: %w", field, raw, context, err)
	}
	return value, nil
}

// parseOptionalInt converts an optional string attribute into an *int64, returning nil if empty.
// raw: the attribute text value, field: attribute name (for error message), context: human-readable owner (e.g., process/thread).
func parseOptionalInt(raw, field, context string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid %s '%s' for %s: %w", field, raw, context, err)
	}
	return &value, nil
}

// parseYesNo converts common yes/no flags into booleans, defaulting to false when empty.
// raw: the attribute text value, field: attribute name (for error message), context: human-readable owner (e.g., tid or pid).
func parseYesNo(raw, field string, context interface{}) (bool, error) {
	if raw == "" {
		return false, nil
	}

	switch strings.ToLower(raw) {
	case "yes":
		return true, nil
	case "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s flag '%s' for %v: expected yes/no", field, raw, context)
	}
}

// createProcessTable builds the processes table and populates it using DuckDB's appender.
func (renderer *ProcessesAndThreadsRenderer) createProcessTable(
	session render.Session,
	tableName string,
	processes []processRow,
) error {
	query := fmt.Sprintf(`CREATE TABLE %s (
		process_uid BIGINT,
		pid BIGINT,
		vm_uid BIGINT,
		name VARCHAR,
		start_time BIGINT,
		end_time BIGINT
	)`, tableName)

	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("failed to create processes table: %w", err)
	}

	return session.Database().Conn.Raw(func(dc any) error {
		duckdbConn, err := render.GetRawDuckDBConn(dc.(driver.Conn))
		if err != nil {
			return err
		}
		appender, err := duckdb.NewAppenderFromConn(duckdbConn, "", tableName)
		if err != nil {
			return err
		}
		defer appender.Close()

		for _, proc := range processes {
			if err := appender.AppendRow(
				proc.processUID,
				proc.pid,
				proc.vmUID,
				proc.name,
				optionalInt64(proc.startTime),
				optionalInt64(proc.endTime),
			); err != nil {
				return fmt.Errorf("failed to append process row: %w", err)
			}
		}

		return nil
	})
}

// createThreadTable builds the threads table and populates it using DuckDB's appender.
func (renderer *ProcessesAndThreadsRenderer) createThreadTable(
	session render.Session,
	tableName string,
	threads []threadRow,
) error {
	query := fmt.Sprintf(`CREATE TABLE %s (
		thread_uid BIGINT,
		tid BIGINT,
		pid BIGINT,
		process_uid BIGINT,
		name VARCHAR,
		kernel BOOLEAN,
		idle BOOLEAN,
		start_time BIGINT,
		end_time BIGINT
	)`, tableName)

	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("failed to create threads table: %w", err)
	}

	return session.Database().Conn.Raw(func(dc any) error {
		duckdbConn, err := render.GetRawDuckDBConn(dc.(driver.Conn))
		if err != nil {
			return err
		}
		appender, err := duckdb.NewAppenderFromConn(duckdbConn, "", tableName)
		if err != nil {
			return err
		}
		defer appender.Close()

		for _, thread := range threads {
			if err := appender.AppendRow(
				thread.threadUID,
				thread.tid,
				thread.pid,
				thread.processUID,
				thread.name,
				thread.kernel,
				thread.idle,
				optionalInt64(thread.startTime),
				optionalInt64(thread.endTime),
			); err != nil {
				return fmt.Errorf("failed to append thread row: %w", err)
			}
		}

		return nil
	})
}

// optionalInt64 converts a *int64 to either nil or the underlying value for appender insertion.
func optionalInt64(value *int64) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

// Initialize reads applications.xml for each run and creates DuckDB tables for processes and threads.
func (renderer *ProcessesAndThreadsRenderer) Initialize(
	session render.Session,
	resolvedDataSources map[string][]render.TableRef,
) error {
	for _, entry := range session.Content().Entries {
		processTable := session.Manifest().AddEntry(renderer.manifestEntry(ProcessesTableName, []run.RunID{entry.ID}))
		threadTable := session.Manifest().AddEntry(renderer.manifestEntry(ThreadsTableName, []run.RunID{entry.ID}))

		componentPath := filepath.Join(renderer.getEntity(), applicationsComponentDefault)
		component, err := entry.Model.ResolveComponent(componentPath)
		if err != nil {
			// Treat missing component as optional; older runs won't have applications.xml.
			// Create empty tables in this case.
			if err := renderer.createProcessTable(session, processTable, nil); err != nil {
				return fmt.Errorf("failed to create empty processes table: %w", err)
			}
			if err := renderer.createThreadTable(session, threadTable, nil); err != nil {
				return fmt.Errorf("failed to create empty threads table: %w", err)
			}
			continue
		}

		processes, threads, err := loadApplications(component.AbsolutePath)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", component.AbsolutePath, err)
		}
		if err := renderer.createProcessTable(session, processTable, processes); err != nil {
			return err
		}
		if err := renderer.createThreadTable(session, threadTable, threads); err != nil {
			return err
		}
	}

	return nil
}

// GetInputSpec declares that the renderer has no dependencies.
func (renderer *ProcessesAndThreadsRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

// GetOutputSpec describes the per-run outputs of the renderer.
func (renderer *ProcessesAndThreadsRenderer) GetOutputSpec() render.OutputSpec {
	return render.OutputSpec{
		PortList: render.PortList{
			Ports: []render.PortSpec{
				{Name: "processes", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: ProcessesTableName, SchemaVersion: renderer.Version()}},
				{Name: "threads", Cardinality: render.CardinalityPerRun, ComponentType: cdf.ComponentType{Name: ThreadsTableName, SchemaVersion: renderer.Version()}},
			},
		},
	}
}
