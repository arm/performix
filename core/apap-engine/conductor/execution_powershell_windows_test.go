// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"runtime"
	"testing"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
	"github.com/stretchr/testify/require"
)

// RunScript selects powershell.exe (Windows PowerShell 5.1), not pwsh.exe.
// Its implicit status and return behaviour differ from newer PowerShell, so
// these tests deliberately retain the 5.1 outcomes.
func TestWindowsPowerShell51Execution(t *testing.T) {
	// Given a native command probe for the Windows PowerShell 5.1 contract.
	probe, cleanup, err := processcontracttest.BuildProbe("windows", runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	t.Run("local scripts", func(t *testing.T) {
		// When local scripts finish through the Windows wrapper.
		// Then arguments, streams and exit statuses follow the 5.1 contract.
		testWindowsScriptOutcomes(t, &LocalCommandRunner{}, probe)
	})
}
