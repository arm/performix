// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package jfr generates a small synthetic execution-sample Run for tests.
package jfr

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// ParquetRoot is the directory containing the JFR files within the generated Run.
const ParquetRoot = "tool/neoprof/0/java/parquet"

// ObservationPattern is a file glob matching execution-sample Parquet files in every recording.
const ObservationPattern = "observations/event=jdk.ExecutionSample/recording=*/*.parquet"

//go:embed generate.sql
var fixtureSQL string

// ExecutionSamples generates the synthetic JFR execution-sample fixture.
type ExecutionSamples struct{}

// Name identifies this fixture in regression test configurations.
func (ExecutionSamples) Name() string { return "jfr_execution_samples" }

// GenerateRun generates the default JFR execution-sample fixture.
func GenerateRun(rootDir string) error {
	return (ExecutionSamples{}).GenerateRun(rootDir)
}

// GenerateRun writes fresh execution samples and a Run manifest to rootDir.
// The caller owns the directory and should share the generated files across tests.
func (ExecutionSamples) GenerateRun(rootDir string) error {
	parquetRoot := filepath.Join(rootDir, ParquetRoot)
	if err := os.MkdirAll(filepath.Join(parquetRoot, "observations"), 0700); err != nil {
		return err
	}

	db, err := (&render.DuckDBFactory{}).Connect(rootDir + "_fixture_writer")
	if err != nil {
		return err
	}
	defer db.Close()
	query := fixtureSQL
	for _, file := range []string{"symbols.parquet", "paths.parquet", "observations"} {
		query = strings.ReplaceAll(query, util.SQLQuoteStringLiteral(file), util.SQLQuoteStringLiteral(filepath.Join(parquetRoot, file)))
	}
	_, err = db.Conn.ExecContext(context.Background(), query)
	if err != nil {
		return fmt.Errorf("generate JFR Parquet files: %w", err)
	}

	// Keep all observation partitions in one component, like capture_apc directories.
	manifest := cdf.Manifest{}
	for _, path := range []string{"symbols.parquet", "paths.parquet", "observations/**/*"} {
		manifest.Entries = append(manifest.Entries, cdf.ManifestEntry{
			Path:          ParquetRoot + "/" + path,
			ComponentType: cdf.ComponentType{Name: "jfr-parquet", SchemaVersion: "1.0"},
		})
	}

	metadata := cdf.Metadata{Name: "Synthetic JFR execution samples", RecipeName: "code_hotspots"}
	for name, value := range map[string]any{"manifest.json": manifest, "metadata.json": metadata} {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(rootDir, name), data, 0600); err != nil {
			return err
		}
	}
	return nil
}
