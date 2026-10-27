// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

const (
	environmentKeysVariable = "APX_PROCESS_PROBE_ENVIRONMENT_KEYS"
	exitCodeVariable        = "APX_PROCESS_PROBE_EXIT_CODE"
)

type report struct {
	Arguments        []string          `json:"arguments"`
	Environment      map[string]string `json:"environment"`
	WorkingDirectory string            `json:"workingDirectory"`
	ExitCode         int               `json:"exitCode"`
	PID              int               `json:"pid"`
	CPUAffinity      []int             `json:"cpuAffinity,omitempty"`
}

func main() {
	var environmentKeys []string
	if err := json.Unmarshal([]byte(os.Getenv(environmentKeysVariable)), &environmentKeys); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "decode selected environment keys: %v\n", err)
		os.Exit(2)
	}

	exitCode, err := strconv.Atoi(os.Getenv(exitCodeVariable))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "decode requested exit code: %v\n", err)
		os.Exit(2)
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "read working directory: %v\n", err)
		os.Exit(2)
	}

	environment := make(map[string]string, len(environmentKeys))
	for _, key := range environmentKeys {
		environment[key] = os.Getenv(key)
	}

	result := report{
		Arguments:        os.Args[1:],
		Environment:      environment,
		WorkingDirectory: workingDirectory,
		ExitCode:         exitCode,
		PID:              os.Getpid(),
		CPUAffinity:      currentAffinity(),
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "encode report: %v\n", err)
		os.Exit(2)
	}
	os.Exit(exitCode)
}
