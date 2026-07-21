// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process_test

import (
	"os"
	"testing"

	"github.com/Arm-Debug/apap-cli/atperf-agent/cmd"
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

	os.Exit(m.Run())
}
