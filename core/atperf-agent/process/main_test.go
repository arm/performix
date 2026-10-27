// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/Arm-Debug/apap-cli/atperf-agent/cmd"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestMain(m *testing.M) {
	// When CLI_MODE=1 the test binary acts as if running the agent binary from the command line
	if os.Getenv("CLI_MODE") == "1" {
		rootCmd := cmd.NewRootCmd()
		if _, err := rootCmd.ExecuteC(); err != nil {
			cmd.HandleRootCmdError(err)
		}
		os.Exit(0)
	}

	cleanupProbe, err := processcontracttest.PrepareProbe()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prepare process probe: %v\n", err)
		os.Exit(1)
	}

	exitCode := m.Run()
	if err := cleanupProbe(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "clean up process probe: %v\n", err)
		if exitCode == 0 {
			exitCode = 1
		}
	}
	os.Exit(exitCode)
}
