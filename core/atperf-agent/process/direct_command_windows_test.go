// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectCommandRejectsResolvedBatchFiles(t *testing.T) {
	for _, extension := range []string{".cmd", ".BAT"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			program := filepath.Join(directory, "batch"+extension)
			require.NoError(t, os.WriteFile(program, []byte("@echo unintended\r\n"), 0600))
			request := &LaunchCommand{Command: []string{"batch"}, Environment: map[string]string{"PATH": directory, "PATHEXT": extension}}
			_, err := NewDirectCommand(context.Background(), request)
			require.ErrorIs(t, err, ErrUnsupportedLaunch)
			_, err = NewProcessManager().StartProcess(&StartProcess{LaunchCommand: *request})
			require.ErrorIs(t, err, ErrUnsupportedLaunch)
		})
	}
}

func TestDirectCommandPreservesRequestPathLookupFailure(t *testing.T) {
	ambientDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ambientDirectory, "batch.cmd"), []byte("@echo unintended\r\n"), 0600))
	t.Setenv("PATH", ambientDirectory)
	t.Setenv("PATHEXT", ".CMD")
	request := &LaunchCommand{Command: []string{"batch"}, Environment: map[string]string{"PATH": t.TempDir(), "PATHEXT": ".CMD"}}
	command, err := NewDirectCommand(context.Background(), request)
	require.NoError(t, err)
	require.ErrorIs(t, command.Run(), exec.ErrNotFound)
	_, err = NewProcessManager().StartProcess(&StartProcess{LaunchCommand: *request})
	require.ErrorIs(t, err, exec.ErrNotFound)
	require.Equal(t, FailureNotFound, FailureReasonForError(err))
}
