// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestDirectCommandRejectsInvalidRequestsBeforeLaunch(t *testing.T) {
	for _, test := range []struct {
		name    string
		request *LaunchCommand
		cause   error
	}{
		{"missing request", nil, ErrMissingLaunchCommand},
		{"missing command", &LaunchCommand{}, ErrMissingExecutable},
		{"elevation", &LaunchCommand{Command: []string{"unused"}, AsPrivileged: true}, ErrUnsupportedLaunch},
		{"affinity", &LaunchCommand{Command: []string{"unused"}, Affinity: []string{"0"}}, ErrUnsupportedLaunch},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given an invalid or unsupported request.
			request := test.request

			// When a caller asks for direct execution.
			result, err := RunDirectCommand(context.Background(), request)

			// Then no command result is produced and the original reason is retained.
			require.Nil(t, result)
			require.ErrorIs(t, err, test.cause)
		})
	}
}

func TestDirectCommandReportsLaunchFailure(t *testing.T) {
	// Given a program that does not exist.
	request := &LaunchCommand{Command: []string{filepath.Join(t.TempDir(), "missing.exe")}}

	// When the program is launched.
	result, err := RunDirectCommand(context.Background(), request)

	// Then the caller can distinguish a launch failure from a child exit code.
	require.ErrorIs(t, err, os.ErrNotExist)
	require.EqualValues(t, -1, result.Rc)
	require.Empty(t, result.Stdout)
	require.Empty(t, result.Stderr)
}

func TestDirectCommandPreservesNonzeroExitAndOutput(t *testing.T) {
	// Given a direct program returning a normal nonzero exit status.
	probe := processcontracttest.ProbePath(t)
	request := &LaunchCommand{Command: []string{probe, "literal argument"}, Environment: map[string]string{
		processcontracttest.EnvironmentKeysVariable: "[]", processcontracttest.ExitCodeVariable: "23",
	}}

	// When the program completes.
	result, err := RunDirectCommand(context.Background(), request)

	// Then its exit status and complete output are retained without cancellation.
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, FailureProcessExit, FailureReasonForError(err))
	require.False(t, errors.Is(err, context.Canceled))
	require.EqualValues(t, 23, result.Rc)
	report, decodeErr := processcontracttest.DecodeReport(result.Stdout)
	require.NoError(t, decodeErr)
	require.Equal(t, []string{"literal argument"}, report.Arguments)
}

func TestDirectCommandRejectsMissingWorkingDirectory(t *testing.T) {
	// Given a valid program with a working directory that does not exist.
	request := &LaunchCommand{Command: []string{processcontracttest.ProbePath(t)}, WorkingDirectory: filepath.Join(t.TempDir(), "missing")}

	// When the request is prepared.
	result, err := RunDirectCommand(context.Background(), request)

	// Then the directory failure is reported before launch.
	require.Nil(t, result)
	require.Equal(t, FailureDirectoryMissing, FailureReasonForError(err))
}
