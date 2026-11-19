// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestExecutionUTF8ValidationScope(t *testing.T) {
	for _, test := range []struct {
		name    string
		request *process.LaunchCommand
	}{
		{"executable", &process.LaunchCommand{Command: []string{"bad\xff"}}},
		{"argument", &process.LaunchCommand{Command: []string{"program", "bad\xff"}}},
		{"directory", &process.LaunchCommand{Command: []string{"program"}, WorkingDirectory: "bad\xff"}},
		{"environment name", &process.LaunchCommand{Command: []string{"program"}, Environment: map[string]string{"bad\xff": "value"}}},
		{"environment value", &process.LaunchCommand{Command: []string{"program"}, Environment: map[string]string{"NAME": "bad\xff"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, ValidateProcessRequest(test.request, Linux), "local POSIX byte strings remain valid")
			var failure *ExecutionError
			require.ErrorAs(t, ValidateProcessRequest(test.request, Win), &failure)
			require.Equal(t, ExecutionUnsupported, failure.Kind)

		})
	}
	t.Run("valid Unicode", func(t *testing.T) {
		request := &process.LaunchCommand{Command: []string{"program", "世界"}, WorkingDirectory: "世界", Environment: map[string]string{"世界": "λ"}}
		require.NoError(t, validateUTF8Request(request))
		require.NoError(t, ValidateProcessRequest(request, Win))
	})
}
