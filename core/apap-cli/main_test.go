// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd"
)

/*
This benchmark allows the profiler to be used to examine the performance
of the server when exercised from a suitable client. Typically, this would be started using the
built-in profiling tool, and the CLI would then call the API method that was of interest.

Note that profiling results will only appear once the benchmark has finished (a forced
termination using ^C will stop the profiler and produce no results). Use the CLI command
`atperf daemon stop` instead.
*/

func BenchmarkServer(b *testing.B) {
	// Use full timestamp in log messages
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	// Log infos and above.
	log.SetLevel(log.InfoLevel)

	rootCmd := cmd.NewRootCmd()
	rootCmd.SetArgs([]string{"daemon", "start", "-b", "--preload"})
	cmd.Execute(rootCmd)
}
