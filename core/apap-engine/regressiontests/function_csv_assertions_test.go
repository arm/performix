// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package regressiontests

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/render"
)

type functionMetricKey struct {
	image      string
	symbol     string
	identifier string
}

func checkFunctionCSVAssertions(resources *rendererTestResources, session render.Session, t *testing.T) {
	t.Helper()

	for _, assertion := range resources.config.FunctionCSVAssertions {
		prerecordedRun, err := prerecordedRunForAlias(resources.config.ImportedRuns, assertion.ImportedRunAlias)
		if !assert.NoError(t, err) {
			continue
		}

		archivePath, err := resolvePrerecordedRunArchive(prerecordedRun)
		if !assert.NoError(t, err) {
			continue
		}

		expected, err := expectedFunctionCSVValues(archivePath, assertion.CSVPath, assertion.Metrics)
		if !assert.NoError(t, err) {
			continue
		}
		if !assert.NotEmpty(t, expected, "no source values were found in %s", assertion.CSVPath) {
			continue
		}

		actual, err := renderedFunctionValues(session, assertion.OutputTable, assertion.Metrics)
		if !assert.NoError(t, err) {
			continue
		}

		for key, want := range expected {
			got, found := actual[key]
			if !assert.Truef(t, found, "missing rendered value for %s/%s (%s)", key.image, key.symbol, key.identifier) {
				continue
			}
			assert.InDeltaf(t, want, got, 1e-9, "rendered value does not match source CSV for %s/%s (%s)", key.image, key.symbol, key.identifier)
		}
	}
}

func prerecordedRunForAlias(importedRuns []ImportedRunRegressionTestConfiguration, alias string) (*PrerecordedRunRegressionTestConfiguration, error) {
	for _, importedRun := range importedRuns {
		if importedRun.Alias == alias && importedRun.PrerecordedRun != nil {
			return importedRun.PrerecordedRun, nil
		}
	}

	return nil, fmt.Errorf("no prerecorded run configured for alias %q", alias)
}

func expectedFunctionCSVValues(archivePath string, csvPath string, metrics []FunctionCSVMetricAssertion) (map[functionMetricKey]float64, error) {
	contents, err := readArchiveEntry(archivePath, csvPath)
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(strings.NewReader(string(contents)))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read header from %q: %w", csvPath, err)
	}

	columnIndexes := make(map[string]int, len(header))
	for index, column := range header {
		columnIndexes[column] = index
	}

	imageIndex, ok := columnIndexes["image"]
	if !ok {
		return nil, fmt.Errorf("source CSV %q has no image column", csvPath)
	}
	symbolIndex, ok := columnIndexes["symbol"]
	if !ok {
		return nil, fmt.Errorf("source CSV %q has no symbol column", csvPath)
	}

	for _, metric := range metrics {
		if _, ok := columnIndexes[metric.CSVColumn]; !ok {
			return nil, fmt.Errorf("source CSV %q has no metric column %q", csvPath, metric.CSVColumn)
		}
	}

	values := make(map[functionMetricKey]float64)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read source CSV %q: %w", csvPath, err)
		}

		for _, metric := range metrics {
			valueText := strings.TrimSpace(record[columnIndexes[metric.CSVColumn]])
			if valueText == "" {
				continue
			}

			value, err := strconv.ParseFloat(valueText, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse %q for %s/%s in %q: %w", valueText, record[imageIndex], record[symbolIndex], csvPath, err)
			}

			key := functionMetricKey{
				image:      record[imageIndex],
				symbol:     record[symbolIndex],
				identifier: metric.MeasurementIdentifier,
			}
			if _, exists := values[key]; exists {
				return nil, fmt.Errorf("source CSV %q contains duplicate value for %s/%s (%s)", csvPath, key.image, key.symbol, key.identifier)
			}
			values[key] = value
		}
	}

	return values, nil
}

func readArchiveEntry(archivePath string, entryPath string) ([]byte, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open prerecorded run archive %q: %w", archivePath, err)
	}
	defer archive.Close()

	for _, file := range archive.File {
		if !strings.HasSuffix(file.Name, "/"+entryPath) {
			continue
		}

		contents, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open %q in prerecorded run archive %q: %w", entryPath, archivePath, err)
		}
		defer contents.Close()

		data, err := io.ReadAll(contents)
		if err != nil {
			return nil, fmt.Errorf("failed to read %q from prerecorded run archive %q: %w", entryPath, archivePath, err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("prerecorded run archive %q has no entry %q", archivePath, entryPath)
}

var functionValuesQueries = map[string]string{
	"drilldown_1": `
		SELECT images.image_name, symbols.name, measurements.identifier, function_values.measurement_value
		FROM drilldown_1 AS function_values
		JOIN ref_measurements AS measurements ON function_values.measurement_id = measurements.measurement_id
		JOIN symbols ON function_values.symbol_id = symbols.symbol_id
		JOIN images ON symbols.image_id = images.image_id
		WHERE measurements.identifier IN ?`,
}

func renderedFunctionValues(session render.Session, outputTable string, metrics []FunctionCSVMetricAssertion) (map[functionMetricKey]float64, error) {
	query, supported := functionValuesQueries[outputTable]
	if !supported {
		return nil, fmt.Errorf("function CSV assertion does not support output table %q", outputTable)
	}

	identifiers := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		identifiers = append(identifiers, metric.MeasurementIdentifier)
	}

	rows, err := session.Database().Conn.QueryContext(context.Background(), query, identifiers)
	if err != nil {
		return nil, fmt.Errorf("failed to query rendered function values: %w", err)
	}
	defer rows.Close()

	values := make(map[functionMetricKey]float64)
	for rows.Next() {
		var image, symbol, identifier string
		var value float64
		if err := rows.Scan(&image, &symbol, &identifier, &value); err != nil {
			return nil, fmt.Errorf("failed to read rendered function value: %w", err)
		}

		key := functionMetricKey{image: image, symbol: symbol, identifier: identifier}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("rendered function output contains duplicate value for %s/%s (%s)", key.image, key.symbol, key.identifier)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while querying rendered function values: %w", err)
	}

	return values, nil
}
