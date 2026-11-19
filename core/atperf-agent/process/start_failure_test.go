// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type affinityFailureManager struct {
	ProcessManager
	launch  *launchCommand
	failure error
}

func (m *affinityFailureManager) buildCmd(request *LaunchCommand) (*launchCommand, error) {
	launch, err := m.ProcessManager.buildCmd(request)
	if err == nil {
		m.launch = launch
		if m.failure != nil {
			// Exercise the post-start failure on every host, without native affinity.
			launch.affinity = nil
		}
	}
	return launch, err
}

func (m *affinityFailureManager) setCPUAffinityAfterStart(pid int, affinity []string) error {
	if m.failure != nil {
		return m.failure
	}
	return m.ProcessManager.setCPUAffinityAfterStart(pid, affinity)
}

func TestAffinityFailureChild(t *testing.T) {
	if os.Getenv("APX_AFFINITY_FAILURE_CHILD") != "1" {
		return
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestStartProcessReapsChildAfterAffinityFailure(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "injected post-start failure"
		if native {
			name = "Windows invalid affinity"
		}
		t.Run(name, func(t *testing.T) {
			if native && runtime.GOOS != "windows" {
				t.Skip("requires Windows affinity API")
			}
			// Given
			executable, err := os.Executable()
			require.NoError(t, err)
			manager := &affinityFailureManager{ProcessManager: NewProcessManager()}
			if !native {
				manager.failure = errors.New("injected affinity failure")
			}
			common := newProcessCommon(afero.NewMemMapFs())
			t.Cleanup(func() {
				if manager.launch != nil && manager.launch.command.Process != nil && manager.launch.command.ProcessState == nil {
					_ = manager.launch.command.Process.Kill()
					_ = manager.launch.command.Wait()
				}
			})
			// When
			proc, err := common.StartProcessCommon(&StartProcess{
				LaunchCommand: LaunchCommand{Command: []string{executable, "-test.run=^TestAffinityFailureChild$"},
					Environment: map[string]string{"APX_AFFINITY_FAILURE_CHILD": "1"}, Affinity: []string{"64"}},
				Stdout: StreamRedirect{Mode: Stream}, Stderr: StreamRedirect{Mode: Stream}, Stdin: StdinBuffer,
			}, manager)
			// Then
			require.Nil(t, proc)
			require.ErrorContains(t, err, "failed to set CPU affinity")
			if !native {
				require.ErrorIs(t, err, manager.failure)
			}
			cmd := manager.launch.command
			require.NotNil(t, cmd.Process, "failure must happen after starting a real child")
			require.NotNil(t, cmd.ProcessState, "child must be reaped before returning")
			require.False(t, cmd.ProcessState.Success())
			// Waiting twice must fail: cleanup has already called Wait.
			require.Error(t, cmd.Wait())
			for _, file := range []*os.File{cmd.Stdout.(*os.File), cmd.Stderr.(*os.File), cmd.Stdin.(*os.File)} {
				require.ErrorIs(t, file.Close(), os.ErrClosed, "stream file must already be closed")
			}
			common.managedProcesses.Range(func(_, _ any) bool { t.Error("failed child was registered"); return true })
			done := make(chan struct{})
			go func() { common.shutdownWg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("failed start left shutdown waiting")
			}
		})
	}
}
