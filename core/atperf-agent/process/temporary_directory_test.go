// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestTemporaryDirectoryChild(t *testing.T) {
	if os.Getenv("APX_TEMP_TEST_CHILD") != "1" {
		return
	}
	directory := os.Getenv("APX_TEMP_TEST_DIRECTORY")
	// #nosec G703 -- The parent test supplies its own temporary directory to this child fixture.
	if err := os.WriteFile(filepath.Join(directory, "ready"), []byte("ready"), 0600); err != nil {
		os.Exit(2)
	}
	if os.Getenv("APX_TEMP_TEST_WAIT") == "1" {
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func TestExecutionRemovesTemporaryDirectory(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"direct", "managed"} {
		for _, outcome := range []string{"success", "launch failure", "cancel"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				// Given a child that writes into its process-owned temporary directory.
				parent := t.TempDir()
				request := &LaunchCommand{Command: []string{executable, "-test.run=^TestTemporaryDirectoryChild$"},
					Environment:        map[string]string{"APX_TEMP_TEST_CHILD": "1", "APX_TEMP_TEST_DIRECTORY": "must be replaced"},
					TemporaryDirectory: &TemporaryDirectory{EnvironmentVariable: "APX_TEMP_TEST_DIRECTORY", ParentDirectory: parent}}
				if outcome == "launch failure" {
					request.Command[0] = filepath.Join(parent, "missing.exe")
				}
				if outcome == "cancel" {
					request.Environment["APX_TEMP_TEST_WAIT"] = "1"
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				ready := func() {
					require.Eventually(t, func() bool {
						matches, err := filepath.Glob(filepath.Join(parent, "apx-script-*", "ready"))
						return err == nil && len(matches) == 1
					}, 5*time.Second, 10*time.Millisecond)
				}
				// When execution completes, fails to launch, or is cancelled.
				if mode == "direct" {
					done := make(chan error, 1)
					go func() { _, err := RunDirectCommand(ctx, request); done <- err }()
					if outcome == "cancel" {
						ready()
						cancel()
					}
					select {
					case err := <-done:
						if outcome == "success" {
							require.NoError(t, err)
						} else {
							require.Error(t, err)
						}
					case <-time.After(10 * time.Second):
						t.Fatal("execution did not finish")
					}
				} else {
					manager := NewProcessManager()
					child, err := manager.StartProcess(&StartProcess{LaunchCommand: *request})
					if outcome == "launch failure" {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
						t.Cleanup(func() { _ = manager.KillProcess(child.Pid); manager.ReleaseProcessHandles([]int{child.Pid}) })
						if outcome == "cancel" {
							ready()
							require.NoError(t, manager.KillProcess(child.Pid))
						}
						rc, err := manager.WaitProcess(ctx, child.Pid)
						require.NoError(t, err)
						if outcome == "success" {
							require.Zero(t, rc)
						}
					}
				}
				// Then the directory and all child-created files are gone before return.
				files, err := os.ReadDir(parent)
				require.NoError(t, err)
				require.Empty(t, files)
			})
		}
	}
}

func TestTemporaryDirectoryRejectsInvalidRequests(t *testing.T) {
	for _, directory := range []TemporaryDirectory{
		{}, {EnvironmentVariable: "BAD=NAME"}, {EnvironmentVariable: "BAD\x00NAME"},
		{EnvironmentVariable: "DIRECTORY", ParentDirectory: "bad\x00parent"},
	} {
		// Given an invalid temporary directory request.
		request := &LaunchCommand{Command: []string{"program"}, TemporaryDirectory: &directory}
		// When validating it before any filesystem access or launch.
		err := ValidateLaunchCommand(request)
		// Then it fails at the shared request boundary.
		require.Error(t, err)
	}
}

func TestCleanupFailureChild(t *testing.T) {
	if value := os.Getenv("APX_CLEANUP_FAILURE_EXIT"); value != "" {
		code, err := strconv.Atoi(value)
		if err != nil {
			os.Exit(2)
		}
		os.Exit(code)
	}
}

type failedTemporaryCleanupManager struct{ ProcessManager }

func (m *failedTemporaryCleanupManager) buildCmd(request *LaunchCommand) (*launchCommand, error) {
	launch, err := m.ProcessManager.buildCmd(request)
	if err != nil {
		return nil, err
	}
	// Inject an invalid removal path without depending on filesystem permissions.
	launch.temporaryPath = "invalid\x00cleanup"
	return launch, nil
}

func TestExecutionReportsTemporaryCleanupFailure(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, code := range []int{0, 19} {
		for _, mode := range []string{"direct", "managed", "exec"} {
			t.Run(mode+"/"+strconv.Itoa(code), func(t *testing.T) {
				// Given a real child and a deterministic filesystem cleanup failure.
				request := &LaunchCommand{Command: []string{executable, "-test.run=^TestCleanupFailureChild$"},
					Environment: map[string]string{"APX_CLEANUP_FAILURE_EXIT": strconv.Itoa(code)}}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				// When execution completes successfully or with a nonzero exit.
				var rc int
				var cleanupErr error
				if mode == "direct" {
					cmd, err := NewDirectCommand(ctx, request)
					require.NoError(t, err)
					result, err := runDirectCommand(ctx, &launchCommand{command: cmd, temporaryPath: "invalid\x00cleanup"})
					require.NotNil(t, result)
					rc, cleanupErr = int(result.Rc), err
				} else {
					common := newProcessCommon(afero.NewOsFs())
					builder := &failedTemporaryCleanupManager{ProcessManager: NewProcessManager()}
					if mode == "exec" {
						result, err := common.ExecCommandCommon(request, builder)
						require.NotNil(t, result)
						rc, cleanupErr = int(result.Rc), err
					} else {
						child, err := common.StartProcessCommon(&StartProcess{LaunchCommand: *request}, builder)
						require.NoError(t, err)
						t.Cleanup(func() { common.ReleaseProcessHandles([]int{child.Pid}) })
						rc, cleanupErr = common.WaitProcess(ctx, child.Pid)
					}
				}
				// Then callers receive the cleanup failure independently of exit status.
				require.Equal(t, code, rc)
				var pathErr *os.PathError
				require.ErrorAs(t, cleanupErr, &pathErr)
			})
		}
	}
}
