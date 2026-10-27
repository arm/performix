// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Package processcontracttest provides a process probe and shared values for
// testing direct command execution without relying on shell-generated text.
package processcontracttest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	EnvironmentKeysVariable = "APX_PROCESS_PROBE_ENVIRONMENT_KEYS"
	ExitCodeVariable        = "APX_PROCESS_PROBE_EXIT_CODE"
)

var probePath string

// Report is emitted as JSON by the process probe before it exits.
type Report struct {
	Arguments        []string          `json:"arguments"`
	Environment      map[string]string `json:"environment"`
	WorkingDirectory string            `json:"workingDirectory"`
	ExitCode         int               `json:"exitCode"`
	PID              int               `json:"pid"`
	CPUAffinity      []int             `json:"cpuAffinity,omitempty"`
}

// ArgumentValues returns values which commonly expose lost argument boundaries
// or accidental shell evaluation.
func ArgumentValues() []string {
	return []string{
		"",
		"with spaces",
		"with\ttab",
		"'single quotes'",
		`"double quotes"`,
		"& | ; $ ( ) < > * ?",
		"Unicode: λ 世界 🚀",
		"carriage\rreturn",
		"line1\nline2",
		`trailing\`,
		// Argument and command injection patterns, passed as literal arguments.
		"--help",
		"&& echo unexpected",
		"|| echo unexpected",
		"$(echo unexpected)",
		"`echo unexpected`",
		"%PATH%",
		// Backslash/quote boundaries from the Windows command-line parsing rules.
		`a\"b`,
		`a\\"b`,
		`a\\\"b`,
		`C:\path with spaces\`,
	}
}

// EnvironmentSelector returns the JSON value used to select environment keys
// for the process probe's report.
func EnvironmentSelector(keys ...string) (string, error) {
	encoded, err := json.Marshal(keys)
	return string(encoded), err
}

// MarkerArgument returns harmless text which would create markerPath if a
// direct-process argument were accidentally interpreted by a common shell.
func MarkerArgument(markerPath string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("& type nul > \"%s\"", markerPath)
	}
	return fmt.Sprintf("; touch %q", markerPath)
}

// TestingT is the subset of testing.T used to access the probe.
type TestingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// PrepareProbe builds the standalone process probe once for the current test
// run. The caller must invoke the returned cleanup function after all tests.
func PrepareProbe() (func() error, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("resolve process probe source directory")
	}
	packageDirectory := filepath.Dir(sourceFile)
	outputName := "process-probe"
	if runtime.GOOS == "windows" {
		outputName += ".exe"
	}
	outputDirectory, err := os.MkdirTemp("", "performix-process-probe-")
	if err != nil {
		return nil, fmt.Errorf("create process probe directory: %w", err)
	}
	outputPath := filepath.Join(outputDirectory, outputName)
	command := exec.Command("go", "build", "-o", outputPath, "./testdata/processprobe") //nolint:gosec // Fixed test helper package and temporary output.
	command.Dir = packageDirectory
	if output, err := command.CombinedOutput(); err != nil {
		_ = os.RemoveAll(outputDirectory)
		return nil, fmt.Errorf("build process probe: %w\n%s", err, output)
	}
	probePath = outputPath
	return func() error {
		probePath = ""
		return os.RemoveAll(outputDirectory)
	}, nil
}

// ProbePath returns the process probe built by PrepareProbe.
func ProbePath(t TestingT) string {
	t.Helper()
	if probePath == "" {
		t.Fatalf("process probe has not been prepared")
		return ""
	}
	return probePath
}

// DecodeReport decodes a process probe report.
func DecodeReport(output string) (Report, error) {
	var report Report
	err := json.Unmarshal([]byte(output), &report)
	return report, err
}
