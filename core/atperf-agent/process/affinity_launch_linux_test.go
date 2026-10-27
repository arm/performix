// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package process

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestAffinityLaunchDoesNotAffectLaterLaunches(t *testing.T) {
	manager := NewProcessManager()
	probe := processcontracttest.ProbePath(t)
	launch := func(affinity []string) []int {
		t.Helper()
		result, err := manager.ExecCommand(&LaunchCommand{
			Command:  []string{probe},
			Affinity: affinity,
			Environment: map[string]string{
				processcontracttest.EnvironmentKeysVariable: "[]",
				processcontracttest.ExitCodeVariable:        "0",
			},
		})
		require.NoError(t, err)
		require.Zero(t, result.Rc, result.Stderr)
		report, err := processcontracttest.DecodeReport(result.Stdout)
		require.NoError(t, err)
		return report.CPUAffinity
	}
	original := launch(nil)
	if len(original) < 2 {
		t.Skip("affinity isolation requires at least two available CPUs")
	}
	for i := 0; i < 16; i++ {
		cpu := original[i%2]
		require.Equal(t, []int{cpu}, launch([]string{strconv.Itoa(cpu)}))
		require.Equal(t, original, launch(nil))
	}
}
