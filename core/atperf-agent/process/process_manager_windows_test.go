// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package process

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

var (
	modKernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procGetProcessAffinityMask = modKernel32.NewProc("GetProcessAffinityMask")
)

// startDummySleepProcess starts a powershell sleep process with its own process group and console.
func startDummySleepProcess(sleepTime int) (*os.Process, error) {
	cmd := exec.Command("powershell", "-Command", fmt.Sprintf("Start-Sleep %d", sleepTime))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NEW_CONSOLE}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd.Process, nil
}

// waitForExit waits for process completion or times out.
func waitForExit(p *os.Process, timeout time.Duration) error {
	done := make(chan struct{})
	go func() { _, _ = p.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for process %d", p.Pid)
	}
}

// TestProcessManagement mirrors the Linux ordering for portable behavior tests.
func TestProcessManagement(t *testing.T) {
	pm := NewLoggingProcessManager(NewProcessManager(), nil)

	t.Run("KillProcess successfully terminates a dummy process", func(t *testing.T) {
		proc, err := startDummySleepProcess(20)
		require.NoError(t, err)
		require.NoError(t, pm.KillProcess(proc.Pid))
		_, err = proc.Wait()
		require.NoError(t, err)
	})

	t.Run("KillProcess errors when given a pid that does not exist", func(t *testing.T) {
		err := pm.KillProcess(999999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open process")
	})

	t.Run("InterruptProcess successfully interrupts a dummy process", func(t *testing.T) {
		t.Skip("(APAP-4209) Skipping InterruptProcess test on Windows due to unstable behaviour in CI")

		proc, err := startDummySleepProcess(20)
		require.NoError(t, err)
		require.NoError(t, pm.InterruptProcess(proc.Pid))
		_, err = proc.Wait()
		require.NoError(t, err)
	})

	t.Run("InterruptProcess returns no error when the process has already exited", func(t *testing.T) {
		t.Skip("(APAP-4209) Skipping InterruptProcess test on Windows due to unstable behaviour in CI")

		proc, err := startDummySleepProcess(1)
		require.NoError(t, err)
		_, err = proc.Wait()
		require.NoError(t, err)
		require.NoError(t, pm.InterruptProcess(proc.Pid))
	})

	t.Run("InterruptProcess errors when given an invalid pid", func(t *testing.T) {
		err := pm.InterruptProcess(-1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid pid")
	})

	t.Run("ExecCommand successfully runs a command", func(t *testing.T) {
		res, err := pm.ExecCommand(&LaunchCommand{Command: []string{"powershell", "-Command", "Write-Output test"}})
		require.NoError(t, err)
		assert.Equal(t, int32(0), res.Rc)
		assert.Contains(t, res.Stdout, "test")
	})

	t.Run("ExecCommand successfully takes an environment variable", func(t *testing.T) {
		res, err := pm.ExecCommand(&LaunchCommand{Command: []string{"powershell", "-Command", "$env:TEST_ENV_VAR"}, Environment: map[string]string{"TEST_ENV_VAR": "success"}})
		require.NoError(t, err)
		assert.Equal(t, int32(0), res.Rc)
		assert.Contains(t, res.Stdout, "success")
	})

	t.Run("ExecCommand works as expected when specifying the working directory", func(t *testing.T) {
		dir := t.TempDir()
		res, err := pm.ExecCommand(&LaunchCommand{Command: []string{"powershell", "-Command", "(Get-Location).Path"}, WorkingDirectory: dir})
		require.NoError(t, err)
		assert.Equal(t, int32(0), res.Rc)
		assert.Contains(t, res.Stdout, filepath.Base(dir))
	})

	t.Run("ExecCommand fails with expected error when the command fails", func(t *testing.T) {
		res, err := pm.ExecCommand(&LaunchCommand{Command: []string{"powershell", "-Command", "throw 'boom'"}})
		require.NoError(t, err)
		assert.NotZero(t, res.Rc)
		assert.Contains(t, res.Stderr, "boom")
	})

	t.Run("StartProcess successfully starts and streams stdout", func(t *testing.T) {
		sp := &StartProcess{LaunchCommand: LaunchCommand{Command: []string{"powershell", "-Command", "Write-Output mystdout"}}, Stdout: StreamRedirect{Mode: Stream}}
		proc, err := pm.StartProcess(sp)
		require.NoError(t, err)
		sender := &fakeSender{ctx: context.Background()}
		require.NoError(t, pm.StreamStdout(proc.Pid, sender))
		joined := bytes.Join(sender.chunks, nil)
		assert.Contains(t, string(joined), "mystdout")
	})

	t.Run("WaitProcess with unknown pid returns error", func(t *testing.T) {
		_, err := pm.WaitProcess(context.Background(), -1)
		assert.ErrorContains(t, err, "process not found")
	})

	t.Run("WaitProcess correctly reflects exit code and can be called multiple times", func(t *testing.T) {
		proc, err := pm.StartProcess(&StartProcess{LaunchCommand: LaunchCommand{Command: []string{"powershell", "-Command", "exit 0"}}})
		require.NoError(t, err)
		ec, err := pm.WaitProcess(context.Background(), proc.Pid)
		require.NoError(t, err)
		assert.Equal(t, 0, ec)
		ec, err = pm.WaitProcess(context.Background(), proc.Pid)
		require.NoError(t, err)
		assert.Equal(t, 0, ec)

		pm.ReleaseProcessHandles([]int{proc.Pid})
		_, err = pm.WaitProcess(context.Background(), proc.Pid)
		require.ErrorContains(t, err, "process not found")
	})

	t.Run("WaitProcess returns a cancelled error upon wait cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		proc, err := pm.StartProcess(&StartProcess{LaunchCommand: LaunchCommand{Command: []string{"powershell", "-Command", "Start-Sleep 2"}}})
		require.NoError(t, err)
		_, err = pm.WaitProcess(ctx, proc.Pid)
		require.ErrorContains(t, err, "context canceled")
	})
}

func TestIsProcessRunning(t *testing.T) {
	// Current test process should always exist.
	assert.True(t, isProcessRunning(uint32(os.Getpid())))

	proc, err := startDummySleepProcess(2)
	require.NoError(t, err)
	assert.True(t, isProcessRunning(uint32(proc.Pid)))

	_, err = proc.Wait()
	require.NoError(t, err)
	assert.False(t, isProcessRunning(uint32(proc.Pid)))
}

func TestShutdownKillsProcessesGracefully(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())
	sp := &StartProcess{LaunchCommand: LaunchCommand{Command: []string{"powershell", "-Command", "Start-Sleep 3"}}, Stdout: StreamRedirect{Mode: Stream}}
	p1, err := pm.StartProcess(sp)
	require.NoError(t, err)
	p2, err := pm.StartProcess(sp)
	require.NoError(t, err)
	require.NoError(t, pm.Shutdown(true)) // forceful termination on Windows
	_, err = pm.WaitProcess(context.Background(), p1.Pid)
	require.NoError(t, err)
	_, err = pm.WaitProcess(context.Background(), p2.Pid)
	require.NoError(t, err)
}

func TestShutdownKillsExecCommandProcessGracefully(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())
	var (
		res *CommandResult
		err error
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err = pm.ExecCommand(&LaunchCommand{Command: []string{"powershell", "-Command", "Start-Sleep 4"}})
	}()
	// Give process time to start
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, pm.Shutdown(false))
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ExecCommand did not return after Shutdown")
	}
	require.NoError(t, err)
	require.NotNil(t, res)
	// Do not check for RC. On Windows a CTRL+C during PowerShell Start-Sleep may return 0 or non-0.
	// The non-0 exit code may be large statuses (0xC0000142) which will show up as negative in int32 type.
}

func TestExpandAffinityTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []int
	}{
		{"single cpus", []string{"0", "2"}, []int{0, 2}},
		{"comma list", []string{"0,1,3"}, []int{0, 1, 3}},
		{"range", []string{"2-4"}, []int{2, 3, 4}},
		{"mixed", []string{"0-2,4", "6"}, []int{0, 1, 2, 4, 6}},
		{"whitespace", []string{" 1 - 3 "}, []int{1, 2, 3}},
		{"descending range", []string{"3-1"}, []int{1, 2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandAffinityTokens(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSetCPUAffinityAfterStart(t *testing.T) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 5")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	pm := &WindowsProcessManager{common: newProcessCommon(afero.NewMemMapFs())}
	require.NoError(t, pm.setCPUAffinityAfterStart(cmd.Process.Pid, []string{"0"}))

	mask, err := getProcessAffinityMask(cmd.Process.Pid)
	require.NoError(t, err)
	assert.Equal(t, uintptr(1), mask)
}

func getProcessAffinityMask(pid int) (uintptr, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(handle)

	var processMask uintptr
	var systemMask uintptr
	r1, _, e1 := procGetProcessAffinityMask.Call(uintptr(handle), uintptr(unsafe.Pointer(&processMask)), uintptr(unsafe.Pointer(&systemMask)))
	if r1 == 0 {
		if e1 != nil {
			return 0, e1
		}
		return 0, fmt.Errorf("GetProcessAffinityMask failed")
	}
	return processMask, nil
}
