// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateLaunchCommand(t *testing.T) {
	tests := []struct {
		name    string
		command *LaunchCommand
		want    error
		field   string
	}{
		{name: "missing request", want: ErrMissingLaunchCommand},
		{name: "missing command", command: &LaunchCommand{}, want: ErrMissingExecutable},
		{name: "empty executable", command: &LaunchCommand{Command: []string{""}}, want: ErrMissingExecutable},
		{name: "NUL executable", command: &LaunchCommand{Command: []string{"bad\x00program"}}, field: "executable"},
		{name: "NUL argument", command: &LaunchCommand{Command: []string{"program", "bad\x00argument"}}, field: "argument"},
		{name: "empty environment name", command: &LaunchCommand{Command: []string{"program"}, Environment: map[string]string{"": "value"}}, field: "environment name"},
		{name: "equals in environment name", command: &LaunchCommand{Command: []string{"program"}, Environment: map[string]string{"A=B": "value"}}, field: "environment name"},
		{name: "NUL environment name", command: &LaunchCommand{Command: []string{"program"}, Environment: map[string]string{"BAD\x00NAME": "value"}}, field: "environment name"},
		{name: "NUL environment value", command: &LaunchCommand{Command: []string{"program"}, Environment: map[string]string{"NAME": "bad\x00value"}}, field: "environment value"},
		{name: "NUL working directory", command: &LaunchCommand{Command: []string{"program"}, WorkingDirectory: "bad\x00directory"}, field: "working directory"},
		{name: "empty argument is valid", command: &LaunchCommand{Command: []string{"program", ""}}},
		{name: "empty working directory is valid", command: &LaunchCommand{Command: []string{"program"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLaunchCommand(tt.command)
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
				return
			}
			if tt.field != "" {
				var validationError *launchCommandValidationError
				require.ErrorAs(t, err, &validationError)
				assert.Equal(t, tt.field, validationError.field)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestProcessManagerRejectsInvalidLaunchBeforeBuilding(t *testing.T) {
	manager := NewProcessManager()

	result, err := manager.ExecCommand(&LaunchCommand{Command: []string{"program", "bad\x00argument"}})
	require.Nil(t, result)
	var validationError *launchCommandValidationError
	require.True(t, errors.As(err, &validationError))

	started, err := manager.StartProcess(&StartProcess{LaunchCommand: LaunchCommand{Command: []string{""}}})
	require.Nil(t, started)
	require.ErrorIs(t, err, ErrMissingExecutable)
}

func TestMergeEnvForOS(t *testing.T) {
	t.Run("preserves inherited values and replaces an override", func(t *testing.T) {
		merged := mergeEnvForOS(
			[]string{"KEEP=inherited", "REPLACE=old"},
			map[string]string{"REPLACE": "new", "ADD": "added"},
			"linux",
		)

		assert.Equal(t, []string{"KEEP=inherited", "REPLACE=new", "ADD=added"}, merged)
	})

	t.Run("replaces Windows names without regard to case", func(t *testing.T) {
		merged := mergeEnvForOS(
			[]string{"Path=old", "KEEP=inherited"},
			map[string]string{"PATH": "new"},
			"windows",
		)

		assert.Equal(t, []string{"PATH=new", "KEEP=inherited"}, merged)
	})
}
