// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

const JSONExtension = ".json"

// Set as variables to allow mocking in tests
var convertFunc = Convert
var convertToFileAtomicallyFunc = convertToFileAtomically

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

var outputDir string
var outputToStdout bool

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "parquet-to-json <input_file_path...>",
		SilenceUsage: true,
		Short: `Convert one or more Parquet data files to JSON.

By default, the converted files are written next to the originals; for example, the conversion of
  /a/b.parquet
would be written to 
  /a/b.json

Use the --output-dir flag to control where the converted files are written.
Alternatively, if only one parquet file is being converted, use the --stdout flag to write the converted JSON to stdout instead.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateFlags(cmd, args); err != nil {
				return err
			}

			if outputToStdout {
				for _, inputFilePath := range args {
					if err := convertFunc(context.Background(), inputFilePath, cmd.OutOrStdout()); err != nil {
						return err
					}
				}
				return nil
			}

			// Validate input file paths
			for _, inputFilePath := range args {
				if err := validateInputFilePath(inputFilePath); err != nil {
					return err
				}
			}

			// Compute, prepare and validate output paths
			outputDirSpecified := cmd.Flags().Changed("output-dir")
			if outputDirSpecified {
				if err := prepareOutputDir(outputDir, true); err != nil {
					return err
				}
			}
			outputFilePaths := make([]string, len(args))
			for i, inputFilePath := range args {
				var resolvedOutputDir string
				if outputDirSpecified {
					resolvedOutputDir = outputDir
				} else {
					resolvedOutputDir = filepath.Dir(inputFilePath)
					if err := prepareOutputDir(resolvedOutputDir, false); err != nil {
						return err
					}
				}

				fileName := retrieveFileName(inputFilePath)
				outputPath := filepath.Join(resolvedOutputDir, fileName+JSONExtension)
				if slices.Contains(outputFilePaths, outputPath) {
					return fmt.Errorf("multiple input parquet files resolve to the same output path (%q)", outputPath)
				}
				if err := verifyOutputPathDoesNotExist(outputPath); err != nil {
					return err
				}
				outputFilePaths[i] = outputPath
			}

			// Convert each file and output to the correct output path
			for i, inputFilePath := range args {
				if err := convertToFileAtomicallyFunc(context.Background(), inputFilePath, outputFilePaths[i]); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", "", "Specify the path to the directory in which the output JSON files will be written.")
	cmd.Flags().BoolVarP(&outputToStdout, "stdout", "s", false, "Output the converted JSON directly to stdout, rather than writing to a file. This can only be used if only one file is to be converted.")

	return cmd
}

// validateFlags validates that the specified flag set is a valid permutation
func validateFlags(cmd *cobra.Command, args []string) error {
	if cmd.Flags().Changed("output-dir") && outputToStdout {
		return fmt.Errorf("the %q and %q flags are mutually exclusive", "--output-dir", "--stdout")
	}
	if outputToStdout && len(args) > 1 {
		return fmt.Errorf("the %q flag can only be used with one file to convert; received %v", "--stdout", len(args))
	}
	return nil
}

// validateInputFilePath validates that the provided file path exists, is a file, and is readable
func validateInputFilePath(inputPath string) error {
	file, err := os.Open(inputPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("input parquet file %q does not exist", inputPath)
	} else if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("input parquet file %q exists but is not readable", inputPath)
	} else if err != nil {
		return fmt.Errorf("error opening input parquet file %q: %w", inputPath, err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("error checking if input parquet file %q is a directory: %w", inputPath, err)
	}
	if fileInfo.IsDir() {
		return fmt.Errorf("input path %q is a directory", inputPath)
	}

	return nil
}

// prepareOutputDir creates outputDir when requested, then verifies that it is a writable directory.
func prepareOutputDir(outputDir string, createIfMissing bool) error {
	fileInfo, err := os.Stat(outputDir)
	if errors.Is(err, os.ErrNotExist) && !createIfMissing {
		return fmt.Errorf("output directory %q does not exist", outputDir)
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("output directory %q cannot be created because a parent directory is not writable", outputDir)
			}
			return fmt.Errorf("error creating output directory %q: %w", outputDir, err)
		}
	} else if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("output directory %q exists but is not accessible", outputDir)
	} else if err != nil {
		return fmt.Errorf("error inspecting output directory %q: %w", outputDir, err)
	} else if !fileInfo.IsDir() {
		return fmt.Errorf("output path %q is not a directory", outputDir)
	}

	tempFile, err := os.CreateTemp(outputDir, ".parquet-to-json-write-test-*.tmp")
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("output directory %q exists but is not writable", outputDir)
	} else if err != nil {
		return fmt.Errorf("error testing if output directory %q is writable: %w", outputDir, err)
	}

	closeErr := tempFile.Close()
	removeErr := os.Remove(tempFile.Name())
	if err := errors.Join(closeErr, removeErr); err != nil {
		return fmt.Errorf("error cleaning up output directory writability test: %w", err)
	}

	return nil
}

// retrieveFileName take a file path and returns the name of that file (excluding the file extension)
func retrieveFileName(inputPath string) string {
	fileName := filepath.Base(inputPath)
	return strings.TrimSuffix(fileName, filepath.Ext(fileName))
}

// convertToFileAtomically calls convertFunc to convert the input parquet file to an output JSON file.
// It outputs the converted JSON to a temporary file, and then atomically renames this to the specified
// output path if no errors occurred.
func convertToFileAtomically(ctx context.Context, inputPath, outputPath string) (err error) {
	tempFile, err := os.CreateTemp(
		filepath.Dir(outputPath),
		filepath.Base(outputPath)+"-*.tmp",
	)
	if err != nil {
		return fmt.Errorf("error creating a temporary file for output JSON file %q: %w", outputPath, err)
	}

	tempPath := tempFile.Name()
	defer func() {
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			cleanupErr := fmt.Errorf("error removing the temporary file for output JSON file %q: %w", outputPath, removeErr)
			err = errors.Join(err, cleanupErr)
		}
	}()

	convertErr := convertFunc(ctx, inputPath, tempFile)
	closeErr := tempFile.Close()
	if convertErr != nil && closeErr != nil {
		return fmt.Errorf("error converting input parquet file %q to JSON and closing its temporary output file: %w", inputPath, errors.Join(convertErr, closeErr))
	} else if convertErr != nil {
		return fmt.Errorf("error converting input parquet file %q to JSON: %w", inputPath, convertErr)
	} else if closeErr != nil {
		return fmt.Errorf("error closing the temporary file for output JSON file %q: %w", outputPath, closeErr)
	}

	if err = verifyOutputPathDoesNotExist(outputPath); err != nil {
		return err
	}

	if err = os.Rename(tempPath, outputPath); errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("output JSON file %q cannot be created because the output directory is not writable", outputPath)
	} else if err != nil {
		return fmt.Errorf("error creating output JSON file %q: %w", outputPath, err)
	}

	return nil
}

func verifyOutputPathDoesNotExist(outputPath string) error {
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("output path %q already exists", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("error checking output path %q: %w", outputPath, err)
	}
	return nil
}
