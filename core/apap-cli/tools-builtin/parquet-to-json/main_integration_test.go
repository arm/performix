// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
)

func TestParquetToJSONIntegration(t *testing.T) {
	t.Run("converts fixture files to a custom output directory", func(t *testing.T) {
		originalConvertFunc := convertFunc
		convertFunc = Convert
		t.Cleanup(func() {
			convertFunc = originalConvertFunc
		})

		fixtureNames := []string{
			"capture_metadata",
			"counter_series_metadata",
		}
		inputPaths := make([]string, 0, len(fixtureNames))
		for _, fixtureName := range fixtureNames {
			inputPaths = append(inputPaths, filepath.Join(
				"integration-test-data",
				fixtureName+".parquet",
			))
		}

		outputDir := filepath.Join(t.TempDir(), "output")
		cmdBuf := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetArgs(append(inputPaths, "--output-dir", outputDir))
		cmd.SetOut(cmdBuf)
		if !assert.NoError(t, cmd.Execute()) {
			return
		}

		assert.Empty(t, cmdBuf.String())
		for _, fixtureName := range fixtureNames {
			expected, err := os.ReadFile(filepath.Join(
				"integration-test-data",
				fixtureName+".expected.json",
			))
			if !assert.NoError(t, err) {
				continue
			}

			actual, err := os.ReadFile(filepath.Join(outputDir, fixtureName+".json"))
			if !assert.NoError(t, err) {
				continue
			}
			assert.JSONEq(t, string(expected), string(actual))
		}
	})

	t.Run("converts a variety of data types to stdout", func(t *testing.T) {
		originalConvertFunc := convertFunc
		convertFunc = Convert
		t.Cleanup(func() {
			convertFunc = originalConvertFunc
		})

		schema := arrow.NewSchema([]arrow.Field{
			{Name: "enabled", Type: arrow.FixedWidthTypes.Boolean},
			{Name: "signed", Type: arrow.PrimitiveTypes.Int64},
			{Name: "unsigned", Type: arrow.PrimitiveTypes.Uint32},
			{Name: "ratio", Type: arrow.PrimitiveTypes.Float64},
			{Name: "label", Type: arrow.BinaryTypes.String},
			{Name: "payload", Type: arrow.BinaryTypes.Binary},
			{Name: "recorded_at", Type: arrow.FixedWidthTypes.Timestamp_ms},
			{Name: "optional", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil)
		recordedAt := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
		inputPath := writeParquetFile(t, schema, func(builder *array.RecordBuilder) {
			builder.Field(0).(*array.BooleanBuilder).Append(true)
			builder.Field(1).(*array.Int64Builder).Append(-42)
			builder.Field(2).(*array.Uint32Builder).Append(42)
			builder.Field(3).(*array.Float64Builder).Append(3.5)
			builder.Field(4).(*array.StringBuilder).Append("smoke test")
			builder.Field(5).(*array.BinaryBuilder).Append([]byte{1, 2, 3})
			builder.Field(6).(*array.TimestampBuilder).Append(arrow.Timestamp(recordedAt.UnixMilli()))
			builder.Field(7).(*array.StringBuilder).AppendNull()
		})

		cmdBuf := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetArgs([]string{inputPath, "--stdout"})
		cmd.SetOut(cmdBuf)
		assert.NoError(t, cmd.Execute())

		assert.JSONEq(t, `[{"enabled":true,"signed":-42,"unsigned":42,"ratio":3.5,"label":"smoke test","payload":"AQID","recorded_at":"2024-01-02T03:04:05Z","optional":null}]`, cmdBuf.String())
		assert.NoFileExists(t, filepath.Join(filepath.Dir(inputPath), "input.json"))
	})

	t.Run("handles multiple input files in different directories, using default output", func(t *testing.T) {
		originalConvertFunc := convertFunc
		convertFunc = Convert
		t.Cleanup(func() {
			convertFunc = originalConvertFunc
		})

		schema := arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "name", Type: arrow.BinaryTypes.String},
		}, nil)
		testCases := []struct {
			id           int64
			name         string
			expectedJSON string
		}{
			{id: 1, name: "first", expectedJSON: `[{"id":1,"name":"first"}]`},
			{id: 2, name: "second", expectedJSON: `[{"id":2,"name":"second"}]`},
			{id: 3, name: "third", expectedJSON: `[{"id":3,"name":"third"}]`},
		}

		inputPaths := make([]string, 0, len(testCases))
		for _, testCase := range testCases {
			inputPath := writeParquetFile(t, schema, func(builder *array.RecordBuilder) {
				builder.Field(0).(*array.Int64Builder).Append(testCase.id)
				builder.Field(1).(*array.StringBuilder).Append(testCase.name)
			})
			inputPaths = append(inputPaths, inputPath)
		}

		cmdBuf := &bytes.Buffer{}
		cmd := newRootCmd()
		cmd.SetArgs(inputPaths)
		cmd.SetOut(cmdBuf)
		assert.NoError(t, cmd.Execute())

		assert.Empty(t, cmdBuf.String())

		// Assert expected files contents
		for index, inputPath := range inputPaths {
			outputPath := filepath.Join(filepath.Dir(inputPath), "input.json")
			output, err := os.ReadFile(outputPath)
			assert.NoError(t, err)
			assert.JSONEq(t, testCases[index].expectedJSON, string(output))
		}
	})
}
