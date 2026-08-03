// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCmdValidation(t *testing.T) {
	originalConvertFunc := convertFunc
	originalAtomicFunc := convertToFileAtomicallyFunc
	convertFunc = func(context.Context, string, io.Writer) error {
		return nil
	}
	convertToFileAtomicallyFunc = func(context.Context, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		convertFunc = originalConvertFunc
		convertToFileAtomicallyFunc = originalAtomicFunc
	})

	tempDir := t.TempDir()

	tests := []struct {
		name                string
		inputFileNames      []string
		flags               []string
		expectedErrContains string
	}{
		{
			name:                "rejects output directory and stdout together",
			inputFileNames:      []string{"a.parquet"},
			flags:               []string{"--output-dir", "/b", "--stdout"},
			expectedErrContains: `the "--output-dir" and "--stdout" flags are mutually exclusive`,
		},
		{
			name:           "accepts stdout with one input path",
			inputFileNames: []string{"a.parquet"},
			flags:          []string{"--stdout"},
		},
		{
			name:                "rejects stdout with multiple input paths",
			inputFileNames:      []string{"a.parquet", "b.parquet"},
			flags:               []string{"--stdout"},
			expectedErrContains: `the "--stdout" flag can only be used with one file to convert; received 2`,
		},
		{
			name:           "accepts explicitly disabled stdout with multiple input paths",
			inputFileNames: []string{"a.parquet", "b.parquet"},
			flags:          []string{"--stdout=false"},
		},
		{
			name:                "rejects missing input path",
			expectedErrContains: "requires at least 1 arg(s), only received 0",
		},
		{
			name:           "accepts multiple input paths",
			inputFileNames: []string{"a.parquet", "b.parquet"},
		},
		{
			name:                "rejects multiple input paths with the same name",
			inputFileNames:      []string{"a.parquet", "b.parquet", "b.pq"},
			expectedErrContains: `multiple input parquet files resolve to the same output path`,
		},
		{
			name:                "rejects multiple input paths with the same name, with custom output directory",
			inputFileNames:      []string{"a.parquet", "b.parquet", "b.parquet"},
			flags:               []string{"--output-dir", tempDir},
			expectedErrContains: `multiple input parquet files resolve to the same output path`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := make([]string, 0, len(tt.inputFileNames)+len(tt.flags))
			for _, inputFileName := range tt.inputFileNames {
				inputPath := filepath.Join(tempDir, inputFileName)
				assert.NoError(t, os.WriteFile(inputPath, nil, 0o600))
				args = append(args, inputPath)
			}
			args = append(args, tt.flags...)

			cmd := newRootCmd()
			cmdBuf := &bytes.Buffer{}
			cmd.SetOut(cmdBuf)
			cmd.SetArgs(args)

			err := cmd.Execute()
			if tt.expectedErrContains != "" {
				assert.ErrorContains(t, err, tt.expectedErrContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOutputWriterSelection(t *testing.T) {
	originalConvertFunc := convertFunc
	t.Cleanup(func() {
		convertFunc = originalConvertFunc
	})

	toPtr := func(a bool) *bool {
		return &a
	}

	tests := []struct {
		name             string
		useOutputDirFlag bool
		stdoutFlagValue  *bool
	}{
		{
			name: "uses adjacent file by default",
		},
		{
			name:             "uses file in output directory",
			useOutputDirFlag: true,
		},
		{
			name:            "uses stdout",
			stdoutFlagValue: toPtr(true),
		},
		{
			name:            "doesn't use stdout when explicitly marked as false",
			stdoutFlagValue: toPtr(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputDir := t.TempDir()
			inputPath := filepath.Join(inputDir, "input.parquet")
			assert.NoError(t, os.WriteFile(inputPath, nil, 0o600))

			args := []string{inputPath}
			expectedOutputPath := filepath.Join(inputDir, "input.json")
			if tt.useOutputDirFlag {
				outputDir := filepath.Join(t.TempDir(), "new-output-dir")
				args = append(args, "--output-dir", outputDir)
				expectedOutputPath = filepath.Join(outputDir, "input.json")
			}
			if tt.stdoutFlagValue != nil {
				if *tt.stdoutFlagValue {
					args = append(args, "--stdout")
				} else {
					args = append(args, "--stdout=false")
				}
			}

			var receivedOutputFile *os.File
			convertFunc = func(_ context.Context, receivedInputPath string, writer io.Writer) error {
				assert.Equal(t, inputPath, receivedInputPath)
				if tt.stdoutFlagValue != nil && *tt.stdoutFlagValue {
					assert.Same(t, os.Stdout, writer)
					assert.NoFileExists(t, expectedOutputPath)
				} else {
					outputFile, ok := writer.(*os.File)
					assert.True(t, ok)
					assert.Equal(t, filepath.Dir(expectedOutputPath), filepath.Dir(outputFile.Name()))
					assert.NotEqual(t, expectedOutputPath, outputFile.Name())
					receivedOutputFile = outputFile
				}
				return nil
			}

			cmd := newRootCmd()
			cmd.SetArgs(args)
			assert.NoError(t, cmd.Execute())
			// Verify that output file handle was closed
			if receivedOutputFile != nil {
				assert.ErrorIs(t, receivedOutputFile.Close(), fs.ErrClosed)
				assert.FileExists(t, expectedOutputPath)
			}
		})
	}
}

func TestPrepareOutputDir(t *testing.T) {
	t.Run("creates a specified relative directory", func(t *testing.T) {
		t.Chdir(t.TempDir())
		outputDir := filepath.Join("relative", "output")

		assert.NoError(t, prepareOutputDir(outputDir, true))
		assert.DirExists(t, outputDir)

		entries, err := os.ReadDir(outputDir)
		assert.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("accepts an existing writable directory", func(t *testing.T) {
		outputDir := t.TempDir()

		assert.NoError(t, prepareOutputDir(outputDir, false))

		entries, err := os.ReadDir(outputDir)
		assert.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("does not create a missing default directory", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "missing")

		err := prepareOutputDir(outputDir, false)
		assert.ErrorContains(t, err, "does not exist")
		assert.ErrorContains(t, err, fmt.Sprintf("%q", outputDir))
		assert.NoDirExists(t, outputDir)
	})

	t.Run("rejects a path to a file", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "output")
		assert.NoError(t, os.WriteFile(outputPath, nil, 0o600))

		err := prepareOutputDir(outputPath, true)
		assert.EqualError(t, err, fmt.Sprintf("output path %q is not a directory", outputPath))
	})

	t.Run("rejects an unwritable directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("file mode permissions are not enforced on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("privileged processes can bypass file mode permissions")
		}

		outputDir := t.TempDir()
		assert.NoError(t, os.Chmod(outputDir, 0o500))
		t.Cleanup(func() {
			assert.NoError(t, os.Chmod(outputDir, 0o700))
		})

		err := prepareOutputDir(outputDir, false)
		assert.ErrorContains(t, err, "exists but is not writable")
		assert.ErrorContains(t, err, fmt.Sprintf("%q", outputDir))
	})
}

func TestConvertToFileAtomically(t *testing.T) {
	originalConvertFunc := convertFunc
	t.Cleanup(func() {
		convertFunc = originalConvertFunc
	})

	assertNoTempFiles := func(t *testing.T, outputPath string) {
		matches, err := filepath.Glob(filepath.Join(
			filepath.Dir(outputPath),
			filepath.Base(outputPath)+"-*.tmp",
		))
		assert.NoError(t, err)
		assert.Empty(t, matches)
	}

	t.Run("publishes complete output", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "output.json")
		convertFunc = func(_ context.Context, _ string, writer io.Writer) error {
			_, err := io.WriteString(writer, "complete")
			return err
		}

		err := convertToFileAtomically(context.Background(), "input.parquet", outputPath)
		assert.NoError(t, err)

		contents, err := os.ReadFile(outputPath)
		assert.NoError(t, err)
		assert.Equal(t, "complete", string(contents))
		assertNoTempFiles(t, outputPath)
	})

	t.Run("removes temporary output after conversion failure", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "output.json")
		conversionErr := errors.New("conversion failed")
		convertFunc = func(_ context.Context, _ string, writer io.Writer) error {
			_, err := io.WriteString(writer, "partial")
			assert.NoError(t, err)
			return conversionErr
		}

		err := convertToFileAtomically(context.Background(), "input.parquet", outputPath)
		assert.ErrorIs(t, err, conversionErr)
		assert.NoFileExists(t, outputPath)
		assertNoTempFiles(t, outputPath)
	})

	t.Run("best-effort preserves existing output", func(t *testing.T) {
		outputPath := filepath.Join(t.TempDir(), "output.json")
		assert.NoError(t, os.WriteFile(outputPath, []byte("existing"), 0o600))
		convertFunc = func(_ context.Context, _ string, writer io.Writer) error {
			_, err := io.WriteString(writer, "replacement")
			return err
		}

		err := convertToFileAtomically(context.Background(), "input.parquet", outputPath)
		assert.ErrorContains(t, err, "already exists")
		assert.ErrorContains(t, err, fmt.Sprintf("%q", outputPath))

		contents, err := os.ReadFile(outputPath)
		assert.NoError(t, err)
		assert.Equal(t, "existing", string(contents))
		assertNoTempFiles(t, outputPath)
	})
}

func TestValidateInputFilePath(t *testing.T) {
	t.Run("accepts readable file", func(t *testing.T) {
		inputPath := filepath.Join(t.TempDir(), "input.parquet")
		assert.NoError(t, os.WriteFile(inputPath, nil, 0o600))

		assert.NoError(t, validateInputFilePath(inputPath))
	})

	t.Run("rejects missing file", func(t *testing.T) {
		inputPath := filepath.Join(t.TempDir(), "missing.parquet")

		err := validateInputFilePath(inputPath)
		assert.ErrorContains(t, err, "does not exist")
		assert.ErrorContains(t, err, fmt.Sprintf("%q", inputPath))
	})

	t.Run("rejects unreadable file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("file mode permissions are not enforced on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("privileged processes can bypass file mode permissions")
		}

		inputPath := filepath.Join(t.TempDir(), "unreadable.parquet")
		assert.NoError(t, os.WriteFile(inputPath, nil, 0o600))
		assert.NoError(t, os.Chmod(inputPath, 0o200))
		t.Cleanup(func() {
			assert.NoError(t, os.Chmod(inputPath, 0o600))
		})

		err := validateInputFilePath(inputPath)
		assert.ErrorContains(t, err, "exists but is not readable")
		assert.ErrorContains(t, err, fmt.Sprintf("%q", inputPath))
	})

	t.Run("rejects directory", func(t *testing.T) {
		inputPath := t.TempDir()

		err := validateInputFilePath(inputPath)
		assert.EqualError(t, err, fmt.Sprintf("input path %q is a directory", inputPath))
	})
}

func TestRetrieveFileName(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "removes file extension",
			path:     "/a/b.parquet",
			expected: "b",
		},
		{
			name:     "removes only final extension",
			path:     "/a/b.data.parquet",
			expected: "b.data",
		},
		{
			name:     "handles file without extension",
			path:     "/a/b",
			expected: "b",
		},
		{
			name:     "handles hidden file with extension",
			path:     "/a/.b.parquet",
			expected: ".b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, retrieveFileName(tt.path))
		})
	}
}
