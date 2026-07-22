// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/reference"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

const (
	sysutilTimelineCanonicalRendererName    = "SysutilTimelineCanonicalRenderer"
	sysutilTimelineCanonicalRendererVersion = "1.0"
	sysutilTimestampColumnName              = "uptime_s"
	sysutilTimestampWallClockColumnName     = "ts_utc"
)

var (
	// sysutil-timeline emits families of columns whose final segment is
	// discovered at runtime, such as cpu core IDs, block device names, and
	// network interfaces. These patterns map those columns back into canonical
	// measurement and series metadata.
	sysutilCPUCorePattern   = regexp.MustCompile(`^cpu(\d+)_percent$`)
	sysutilReadIOPSPattern  = regexp.MustCompile(`^read_iops_(.+)$`)
	sysutilWriteIOPSPattern = regexp.MustCompile(`^write_iops_(.+)$`)
	sysutilReadBpsPattern   = regexp.MustCompile(`^read_bps_(.+)$`)
	sysutilWriteBpsPattern  = regexp.MustCompile(`^write_bps_(.+)$`)
	sysutilRxBpsPattern     = regexp.MustCompile(`^rx_bps_(.+)$`)
	sysutilTxBpsPattern     = regexp.MustCompile(`^tx_bps_(.+)$`)
	sysutilNumaNodePattern  = regexp.MustCompile(`^numa_node(\d+)_allocations_per_s$`)
	sysutilIRQCorePattern   = regexp.MustCompile(`^irq_cpu(\d+)_per_s$`)
)

type SysutilTimelineCanonicalRenderer struct {
	config *render.Config
}

func (r *SysutilTimelineCanonicalRenderer) Name() string {
	return sysutilTimelineCanonicalRendererName
}

func (r *SysutilTimelineCanonicalRenderer) Version() string {
	return sysutilTimelineCanonicalRendererVersion
}

func (r *SysutilTimelineCanonicalRenderer) Configure(config *render.Config) error {
	r.config = config
	return nil
}

func (r *SysutilTimelineCanonicalRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{
		PortList: render.PortList{
			Ports: []render.PortSpec{
				{
					Name:        "samples",
					Cardinality: render.CardinalityPerRun,
					ComponentType: cdf.ComponentType{
						Name:          "flat_table",
						SchemaVersion: "1.0",
					},
				},
			},
		},
	}
}

func (r *SysutilTimelineCanonicalRenderer) GetOutputSpec() render.OutputSpec {
	return render.OutputSpec{
		PortList: render.PortList{
			Ports: []render.PortSpec{
				{
					Name:        "raw_samples",
					Cardinality: render.CardinalityPerRun,
					ComponentType: cdf.ComponentType{
						Name:          TimeseriesRawSamplesComponentName,
						SchemaVersion: TimeseriesCanonicalSchemaVersion,
					},
				},
				{
					Name:        "series_metadata",
					Cardinality: render.CardinalityPerRun,
					ComponentType: cdf.ComponentType{
						Name:          TimeseriesSeriesMetadataComponentName,
						SchemaVersion: TimeseriesCanonicalSchemaVersion,
					},
				},
				{
					Name:        "measurements",
					Cardinality: render.CardinalityOne,
					ComponentType: cdf.ComponentType{
						Name:          TimelineMeasurementsComponentName,
						SchemaVersion: reference.MeasurementsSchemaVersion,
					},
				},
			},
		},
	}
}

func (r *SysutilTimelineCanonicalRenderer) newManifestEntryInfo(
	componentType cdf.ComponentType,
	associatedContent []run.RunID,
) render.ManifestEntryInfo {
	return render.NewManifestEntryInfo(componentType, r.config.Identity, associatedContent)
}

func (r *SysutilTimelineCanonicalRenderer) Initialize(
	session render.Session,
	resolvedDataSources map[string][]render.TableRef,
) error {
	// This renderer is intentionally narrow in scope: it turns the existing
	// wide sysutil CSV table into the canonical raw samples table, then derives
	// series metadata and a renderer-scoped measurements view from it.
	inputTables := resolvedDataSources["samples"]
	if len(inputTables) == 0 {
		return fmt.Errorf("missing required data source 'samples'")
	}

	rawTableNames := make([]string, 0, len(inputTables))
	associatedContentByID := make(map[string]run.RunID)
	for _, inputTable := range inputTables {
		manifestEntry, err := session.Manifest().GetEntry(inputTable.Name)
		if err != nil {
			return fmt.Errorf("failed to get manifest entry for source table '%s': %w", inputTable.Name, err)
		}

		sourceColumns, err := getTableColumns(session, inputTable.Name)
		if err != nil {
			return err
		}

		seriesSpecs, err := buildSysutilSeriesSpecs(sourceColumns)
		if err != nil {
			return err
		}

		rawTableName := session.Manifest().AddEntry(r.newManifestEntryInfo(
			cdf.ComponentType{
				Name:          TimeseriesRawSamplesComponentName,
				SchemaVersion: TimeseriesCanonicalSchemaVersion,
			},
			manifestEntry.Info().AssociatedContent(),
		))
		if err := createCanonicalRawSamplesTable(session, rawTableName, inputTable.Name, sourceColumns); err != nil {
			return err
		}
		rawTableNames = append(rawTableNames, rawTableName)
		for _, contentID := range manifestEntry.Info().AssociatedContent() {
			associatedContentByID[contentID.Value] = contentID
		}

		measurementIDs, err := upsertTimeseriesMeasurements(session, rawTableName, seriesSpecs)
		if err != nil {
			return err
		}

		timeOriginEpoch, err := computeTimeOriginEpoch(session, rawTableName)
		if err != nil {
			return err
		}

		seriesMetadataTableName := session.Manifest().AddEntry(r.newManifestEntryInfo(
			cdf.ComponentType{
				Name:          TimeseriesSeriesMetadataComponentName,
				SchemaVersion: TimeseriesCanonicalSchemaVersion,
			},
			manifestEntry.Info().AssociatedContent(),
		))
		if err := createSeriesMetadataTable(session, seriesMetadataTableName, seriesSpecs, measurementIDs, timeOriginEpoch); err != nil {
			return err
		}
	}

	associatedContent := make([]run.RunID, 0, len(associatedContentByID))
	for _, contentID := range associatedContentByID {
		associatedContent = append(associatedContent, contentID)
	}
	sort.Slice(associatedContent, func(i, j int) bool {
		return associatedContent[i].Value < associatedContent[j].Value
	})

	measurementsViewName := session.Manifest().AddEntry(r.newManifestEntryInfo(
		cdf.ComponentType{
			Name:          TimelineMeasurementsComponentName,
			SchemaVersion: reference.MeasurementsSchemaVersion,
		},
		associatedContent,
	))
	if err := session.Reference().Measurements().CreateViewByTableRefs(
		context.Background(),
		measurementsViewName,
		rawTableNames,
	); err != nil {
		return fmt.Errorf("create timeline measurements view: %w", err)
	}

	return nil
}

func getTableColumns(session render.Session, tableName string) ([]string, error) {
	rows, err := session.Database().Conn.QueryContext(
		context.Background(),
		fmt.Sprintf(`SELECT name FROM pragma_table_info('%s') ORDER BY cid`, tableName),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect table '%s': %w", tableName, err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, fmt.Errorf("failed to scan column metadata for '%s': %w", tableName, err)
		}
		columns = append(columns, columnName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate columns for '%s': %w", tableName, err)
	}

	return columns, nil
}

func createCanonicalRawSamplesTable(
	session render.Session,
	outputTableName string,
	inputTableName string,
	sourceColumns []string,
) error {
	// Keep the wide source shape intact and only normalize the value types so the
	// rest of the canonical pipeline can treat the table consistently.
	selectColumns := make([]string, 0, len(sourceColumns))
	for _, column := range sourceColumns {
		quotedColumn := QuoteColumnName(column)
		switch column {
		case sysutilTimestampWallClockColumnName:
			selectColumns = append(selectColumns, fmt.Sprintf(`%s AS %s`, quotedColumn, quotedColumn))
		default:
			selectColumns = append(selectColumns, fmt.Sprintf(`TRY_CAST(%s AS DOUBLE) AS %s`, quotedColumn, quotedColumn))
		}
	}

	//nolint:gosec // output/input table names and selected columns are derived from manifest-generated identifiers and fixed canonical column mappings.
	query := fmt.Sprintf(
		`CREATE TABLE %s AS SELECT %s FROM %s`,
		outputTableName,
		strings.Join(selectColumns, ", "),
		inputTableName,
	)
	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("failed to create canonical raw samples table '%s': %w", outputTableName, err)
	}

	return nil
}

func buildSysutilSeriesSpecs(sourceColumns []string) ([]CanonicalTimeseriesSeriesSpec, error) {
	var specs []CanonicalTimeseriesSeriesSpec
	for _, column := range sourceColumns {
		switch column {
		case sysutilTimestampColumnName, sysutilTimestampWallClockColumnName:
			continue
		}

		spec, ok := resolveSysutilSeriesSpec(column)
		if !ok {
			return nil, fmt.Errorf("unsupported sysutil timeline column '%s'", column)
		}
		spec.SeriesOrder = len(specs)
		specs = append(specs, spec)
	}

	sort.SliceStable(specs, func(i, j int) bool {
		return specs[i].SeriesOrder < specs[j].SeriesOrder
	})
	return specs, nil
}

func resolveSysutilSeriesSpec(columnName string) (CanonicalTimeseriesSeriesSpec, bool) {
	// For sysutil-timeline we own the source schema, so canonical measurement and
	// series mappings are intentionally hard-coded here instead of discovered from
	// collector-provided metadata.
	if match := sysutilCPUCorePattern.FindStringSubmatch(columnName); match != nil {
		coreID := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.cpu.utilisation",
			"CPU Utilisation",
			"Per-core or aggregate CPU utilisation percentage.",
			"percent",
			fmt.Sprintf("Core %s", coreID),
		), true
	}
	if match := sysutilReadIOPSPattern.FindStringSubmatch(columnName); match != nil {
		device := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.disk.read.iops",
			"Disk Read IOPS",
			"Read input/output operations per second.",
			"iops",
			fmt.Sprintf("Read IOPS %s", device),
		), true
	}
	if match := sysutilWriteIOPSPattern.FindStringSubmatch(columnName); match != nil {
		device := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.disk.write.iops",
			"Disk Write IOPS",
			"Write input/output operations per second.",
			"iops",
			fmt.Sprintf("Write IOPS %s", device),
		), true
	}
	if match := sysutilReadBpsPattern.FindStringSubmatch(columnName); match != nil {
		device := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.disk.read.bytes_per_second",
			"Disk Read Bandwidth",
			"Disk read throughput in bytes per second.",
			"bytes_per_s",
			fmt.Sprintf("Read Bps %s", device),
		), true
	}
	if match := sysutilWriteBpsPattern.FindStringSubmatch(columnName); match != nil {
		device := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.disk.write.bytes_per_second",
			"Disk Write Bandwidth",
			"Disk write throughput in bytes per second.",
			"bytes_per_s",
			fmt.Sprintf("Write Bps %s", device),
		), true
	}
	if match := sysutilRxBpsPattern.FindStringSubmatch(columnName); match != nil {
		iface := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.network.rx.bytes_per_second",
			"Network Receive Bandwidth",
			"Network receive throughput in bytes per second.",
			"bytes_per_s",
			fmt.Sprintf("RX %s", iface),
		), true
	}
	if match := sysutilTxBpsPattern.FindStringSubmatch(columnName); match != nil {
		iface := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.network.tx.bytes_per_second",
			"Network Transmit Bandwidth",
			"Network transmit throughput in bytes per second.",
			"bytes_per_s",
			fmt.Sprintf("TX %s", iface),
		), true
	}
	if match := sysutilNumaNodePattern.FindStringSubmatch(columnName); match != nil {
		nodeID := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.numa.node.allocations.rate",
			"NUMA Node Pages/s",
			"Rate of memory pages allocated from each NUMA node.",
			"pages_per_s",
			fmt.Sprintf("Node %s Pages/s", nodeID),
		), true
	}
	if match := sysutilIRQCorePattern.FindStringSubmatch(columnName); match != nil {
		coreID := match[1]
		return newSeriesSpec(
			columnName,
			"timeline.interrupts.per_core.rate",
			"Per-Core IRQ Rate",
			"Hardware interrupt rate for each individual core, summed across all IRQ sources.",
			"count_per_s",
			fmt.Sprintf("Core %s", coreID),
		), true
	}

	switch columnName {
	case "cpu_total_percent":
		return newSeriesSpec(
			columnName,
			"timeline.cpu.utilisation",
			"CPU Utilisation",
			"Per-core or aggregate CPU utilisation percentage.",
			"percent",
			"CPU Total",
		), true
	case "load1":
		return newSeriesSpec(columnName, "timeline.load.1", "Load Average (1m)", "System load average over 1 minute.", "count", "Load 1"), true
	case "load5":
		return newSeriesSpec(columnName, "timeline.load.5", "Load Average (5m)", "System load average over 5 minutes.", "count", "Load 5"), true
	case "load15":
		return newSeriesSpec(columnName, "timeline.load.15", "Load Average (15m)", "System load average over 15 minutes.", "count", "Load 15"), true
	case "mem_total_kb":
		return newSeriesSpec(columnName, "timeline.memory.total", "Memory Total", "Total system memory.", "kB", "Mem Total"), true
	case "mem_available_kb":
		return newSeriesSpec(columnName, "timeline.memory.available", "Memory Available", "Available system memory.", "kB", "Mem Available"), true
	case "mem_used_kb":
		return newSeriesSpec(columnName, "timeline.memory.used", "Memory Used", "Used system memory.", "kB", "Mem Used"), true
	case "mem_used_percent":
		return newSeriesSpec(columnName, "timeline.memory.utilisation", "Memory Utilisation", "Used system memory as a percentage.", "percent", "Mem Use"), true
	case "swap_total_kb":
		return newSeriesSpec(columnName, "timeline.swap.total", "Swap Total", "Total swap space.", "kB", "Swap Total"), true
	case "swap_used_kb":
		return newSeriesSpec(columnName, "timeline.swap.used", "Swap Used", "Used swap space.", "kB", "Swap Used"), true
	case "procs_running":
		return newSeriesSpec(columnName, "timeline.processes.running", "Processes Running", "Number of runnable processes.", "count", "Procs Running"), true
	case "procs_blocked":
		return newSeriesSpec(columnName, "timeline.processes.blocked", "Processes Blocked", "Number of blocked processes.", "count", "Procs Blocked"), true
	case "ctxt_per_s":
		return newSeriesSpec(columnName, "timeline.context_switches.rate", "Context Switches/s", "Context switches per second.", "count_per_s", "Context Switches/s"), true
	case "intr_per_s":
		return newSeriesSpec(columnName, "timeline.interrupts.rate", "Interrupts/s", "Interrupts per second.", "count_per_s", "IRQs/s"), true
	case "page_faults_per_s":
		return newSeriesSpec(columnName, "timeline.page_faults.rate", "Page Faults/s", "Page faults per second.", "count_per_s", "Page Faults/s"), true
	case "pgmajfaults_per_s":
		return newSeriesSpec(columnName, "timeline.page_faults.major.rate", "Major Faults/s", "Major page faults per second.", "count_per_s", "Major Faults/s"), true
	case "threads_total":
		return newSeriesSpec(columnName, "timeline.threads.total", "Threads", "Total thread count.", "count", "Threads"), true
	case "numa_hit_per_s":
		return newSeriesSpec(columnName, "timeline.numa.hit.rate", "NUMA Hit Pages/s", "Rate of memory pages allocated from the preferred NUMA node.", "pages_per_s", "NUMA Hit Pages/s"), true
	case "numa_miss_per_s":
		return newSeriesSpec(columnName, "timeline.numa.miss.rate", "Preferred Node Miss Pages/s", "Rate of memory pages allocated from a NUMA node other than the preferred NUMA node, summed across all NUMA nodes.", "pages_per_s", "Preferred Node Miss Pages/s"), true
	case "numa_interleave_hit_per_s":
		return newSeriesSpec(columnName, "timeline.numa.interleave_hit.rate", "NUMA Interleave Hit Pages/s", "Rate of interleave policy memory pages allocated from the intended NUMA node.", "pages_per_s", "NUMA Interleave Hit Pages/s"), true
	case "numa_local_node_per_s":
		return newSeriesSpec(columnName, "timeline.numa.local_node.rate", "NUMA Local Node Pages/s", "Rate of memory pages allocated from the same NUMA node as the running process.", "pages_per_s", "NUMA Local Node Pages/s"), true
	case "numa_other_node_per_s":
		return newSeriesSpec(columnName, "timeline.numa.other_node.rate", "NUMA Other Node Pages/s", "Rate of memory pages allocated from a different NUMA node than the running process.", "pages_per_s", "NUMA Other Node Pages/s"), true
	case "numa_local_percent":
		return newSeriesSpec(columnName, "timeline.numa.local.percent", "NUMA Local", "Percentage of memory pages allocated from the same NUMA node as the running process in the sample interval.", "percent", "NUMA Local (%)"), true
	case "numa_remote_percent":
		return newSeriesSpec(columnName, "timeline.numa.remote.percent", "NUMA Remote", "Percentage of memory pages allocated from a different NUMA node than the running process in the sample interval.", "percent", "NUMA Remote (%)"), true
	default:
		return CanonicalTimeseriesSeriesSpec{}, false
	}
}

func newSeriesSpec(
	columnName string,
	identifier string,
	measurementName string,
	description string,
	units string,
	seriesName string,
) CanonicalTimeseriesSeriesSpec {
	measurementSpec := render.MeasurementSpec{
		Identifier:       render.SlugIdentifier(identifier),
		Name:             measurementName,
		ShortDescription: description,
		Description:      description,
		Units:            units,
	}

	return CanonicalTimeseriesSeriesSpec{
		ColumnName:  columnName,
		Measurement: measurementSpec,
		SeriesName:  seriesName,
		SeriesKind:  TimeseriesSeriesKindLine,
		Description: description,
		Unit:        units,
	}
}

func upsertTimeseriesMeasurements(
	session render.Session,
	rawTableName string,
	seriesSpecs []CanonicalTimeseriesSeriesSpec,
) (map[string]render.MeasurementID, error) {
	// Multiple plotted series can legitimately map to the same measurement
	// definition, such as aggregate CPU utilisation and per-core CPU
	// utilisation. Upsert measurement definitions once, then link series
	// instances to those shared IDs in timeseries_series_metadata.
	measurementSpecsByIdentifier := map[render.SlugIdentifier]render.MeasurementSpec{}
	for _, spec := range seriesSpecs {
		measurementSpec := spec.Measurement
		measurementSpec.ColumnRefs = append(measurementSpec.ColumnRefs, render.ColumnRef{
			Table:  rawTableName,
			Column: spec.ColumnName,
		})
		existing, found := measurementSpecsByIdentifier[measurementSpec.Identifier]
		if found {
			existing.ColumnRefs = append(existing.ColumnRefs, measurementSpec.ColumnRefs...)
			measurementSpecsByIdentifier[measurementSpec.Identifier] = existing
			continue
		}
		measurementSpecsByIdentifier[measurementSpec.Identifier] = measurementSpec
	}

	specs := make([]render.MeasurementSpec, 0, len(measurementSpecsByIdentifier))
	for _, spec := range measurementSpecsByIdentifier {
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Identifier < specs[j].Identifier
	})

	ids, err := session.Reference().Measurements().Upsert(context.Background(), specs)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert timeline measurements: %w", err)
	}

	result := make(map[string]render.MeasurementID, len(specs))
	for i, spec := range specs {
		result[string(spec.Identifier)] = ids[i]
	}
	return result, nil
}

func computeTimeOriginEpoch(session render.Session, rawTableName string) (int64, error) {
	// The canonical model stores timestamps relative to uptime_s, but later LOD
	// tables need a stable absolute anchor. Derive that anchor from the earliest
	// ts_utc/uptime_s pair present in the raw samples table.
	row := session.Database().Conn.QueryRowContext(
		context.Background(),
		fmt.Sprintf(
			`SELECT %s, %s FROM %s WHERE %s IS NOT NULL AND %s IS NOT NULL ORDER BY %s ASC LIMIT 1`,
			QuoteColumnName(sysutilTimestampWallClockColumnName),
			QuoteColumnName(sysutilTimestampColumnName),
			rawTableName,
			QuoteColumnName(sysutilTimestampWallClockColumnName),
			QuoteColumnName(sysutilTimestampColumnName),
			QuoteColumnName(sysutilTimestampColumnName),
		),
	)

	var tsUTC sql.NullString
	var uptimeSeconds sql.NullFloat64
	if err := row.Scan(&tsUTC, &uptimeSeconds); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to compute time origin for '%s': %w", rawTableName, err)
	}
	if !tsUTC.Valid || !uptimeSeconds.Valid {
		return 0, nil
	}

	parsed, err := time.Parse(time.RFC3339, tsUTC.String)
	if err != nil {
		return 0, fmt.Errorf("failed to parse ts_utc '%s': %w", tsUTC.String, err)
	}

	return parsed.Add(-time.Duration(uptimeSeconds.Float64 * float64(time.Second))).UnixMilli(), nil
}

func createSeriesMetadataTable(
	session render.Session,
	tableName string,
	seriesSpecs []CanonicalTimeseriesSeriesSpec,
	measurementIDs map[string]render.MeasurementID,
	timeOriginEpoch int64,
) error {
	// Series metadata stays separate from measurement metadata so the UI can
	// distinguish between metric definitions and concrete plotted series such as
	// "CPU Total" versus "Core 0".
	query := fmt.Sprintf(`CREATE TABLE %s (
		series_id VARCHAR,
		column_name VARCHAR,
		measurement_id INTEGER,
		series_name VARCHAR,
		series_kind VARCHAR,
		series_order INTEGER,
		description VARCHAR,
		unit VARCHAR,
		time_origin_epoch BIGINT
	)`, tableName)
	if _, err := session.Database().Conn.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("failed to create series metadata table '%s': %w", tableName, err)
	}

	valueRows := make([]string, 0, len(seriesSpecs))
	for _, spec := range seriesSpecs {
		measurementID, ok := measurementIDs[string(spec.Measurement.Identifier)]
		if !ok {
			return fmt.Errorf("missing measurement ID for identifier '%s'", spec.Measurement.Identifier)
		}
		valueRows = append(valueRows, fmt.Sprintf(
			"(%s, %s, %d, %s, %s, %d, %s, %s, %d)",
			sqlQuoteString(spec.ColumnName),
			sqlQuoteString(spec.ColumnName),
			measurementID,
			sqlQuoteString(spec.SeriesName),
			sqlQuoteString(spec.SeriesKind),
			spec.SeriesOrder,
			sqlQuoteString(spec.Description),
			sqlQuoteString(spec.Unit),
			timeOriginEpoch,
		))
	}
	if len(valueRows) == 0 {
		return nil
	}

	//nolint:gosec // table name and VALUES rows are constructed from manifest-generated table names and renderer-defined canonical metadata.
	insertQuery := fmt.Sprintf(
		`INSERT INTO %s (
			series_id,
			column_name,
			measurement_id,
			series_name,
			series_kind,
			series_order,
			description,
			unit,
			time_origin_epoch
		) VALUES %s`,
		tableName,
		strings.Join(valueRows, ", "),
	)
	if _, err := session.Database().Conn.ExecContext(context.Background(), insertQuery); err != nil {
		return fmt.Errorf("failed to populate series metadata table '%s': %w", tableName, err)
	}

	return nil
}
