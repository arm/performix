// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

// Unix process manager tests shared by Linux and Darwin.

package process

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// startDummySleepProcess starts a "sleep 2" process and returns the *os.Process and a channel for Wait() signals
func startDummySleepProcess() (*os.Process, <-chan *os.ProcessState, error) {
	cmd := exec.Command("sleep", "20")

	// Create a new process group
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := cmd.Start()
	if err != nil {
		return nil, nil, err
	}
	proc := cmd.Process

	// Wait for process to exit in a go routine
	done := make(chan *os.ProcessState, 1)
	go func() {
		state, _ := proc.Wait()
		done <- state
	}()
	return proc, done, nil
}

// isProcessGroupAlive returns true if the process group exists.
func isProcessGroupAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(-pid, 0)
	// Alive if no error or EPERM (exists but no permissions).
	return err == nil || err == unix.EPERM
}

func TestProcessManagement(t *testing.T) {
	pm := NewProcessManager()
	pm = NewLoggingProcessManager(pm, nil)

	t.Run("KillProcess successfully terminates a dummy process", func(t *testing.T) {
		// Start a dummy process
		proc, done, err := startDummySleepProcess()
		assert.NoError(t, err)
		assert.NotNil(t, proc)
		defer func() { _ = proc.Kill() }()

		// Kill the process
		err = pm.KillProcess(proc.Pid)
		assert.NoError(t, err)

		// Wait for process exit
		var state *os.ProcessState
		select {
		case state = <-done:
			// success
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timeout waiting for process exit")
		}

		// Inspect why it died
		reason, ok := state.Sys().(syscall.WaitStatus)
		assert.True(t, ok)
		assert.Equal(t, syscall.SIGKILL, reason.Signal())
	})

	t.Run("KillProcess errors when given a pid that does not exist", func(t *testing.T) {
		// Kill an invalid process
		err := pm.KillProcess(999999)
		assert.ErrorContains(t, err, "failed to kill process 999999")
	})

	t.Run("InterruptProcess successfully interrupts a dummy process", func(t *testing.T) {
		// Start a dummy process
		proc, done, err := startDummySleepProcess()
		assert.NoError(t, err)
		assert.NotNil(t, proc)
		defer func() { _ = proc.Kill() }()

		// Interrupt the process
		fmt.Println("PID: ", proc.Pid)
		err = pm.InterruptProcess(proc.Pid)
		assert.NoError(t, err)

		// Wait for process exit
		var state *os.ProcessState
		select {
		case state = <-done:
			// success
		case <-time.After(5000 * time.Millisecond):
			t.Fatal("timeout waiting for process exit")
		}

		// Inspect why it died
		reason, ok := state.Sys().(syscall.WaitStatus)
		assert.True(t, ok)
		assert.Equal(t, syscall.SIGINT, reason.Signal())
	})
	t.Run("InterruptProcess errors when given a pid that does not exist", func(t *testing.T) {
		// Interrupt an invalid process
		err := pm.InterruptProcess(-1)
		assert.ErrorContains(t, err, "failed to interrupt process -1")
	})
	t.Run("ExecCommand successufully runs a command", func(t *testing.T) {
		launchCommand := &LaunchCommand{
			Command: []string{"echo", "test"},
		}
		result, err := pm.ExecCommand(launchCommand)
		assert.NoError(t, err)
		assert.Equal(t, result.Rc, int32(0))
		assert.Contains(t, result.Stdout, "test")
	})
	t.Run("ExecCommand successufully takes an environment variable", func(t *testing.T) {
		launchCommand := &LaunchCommand{
			Command:     []string{"env"},
			Environment: map[string]string{"TEST_ENV_VAR": "success"},
		}
		result, err := pm.ExecCommand(launchCommand)
		assert.NoError(t, err)
		assert.Equal(t, result.Rc, int32(0))
		assert.Contains(t, result.Stdout, "TEST_ENV_VAR=success")
	})
	t.Run("ExecCommand works as expected when specifying the working directory", func(t *testing.T) {
		launchCommand := &LaunchCommand{
			Command:          []string{"pwd"},
			WorkingDirectory: "/tmp",
		}
		result, err := pm.ExecCommand(launchCommand)
		assert.NoError(t, err)
		assert.Equal(t, result.Rc, int32(0))
		assert.Contains(t, result.Stdout, "/tmp")
	})
	t.Run("ExecCommand fails with expected error when the command fails", func(t *testing.T) {
		launchCommand := &LaunchCommand{
			Command: []string{"sh", "-c", "printf 'expected failure\\n' >&2; exit 2"},
		}
		result, err := pm.ExecCommand(launchCommand)
		assert.NoError(t, err)
		assert.Equal(t, result.Rc, int32(2))
		assert.Contains(t, result.Stderr, "expected failure")
	})
	t.Run("WaitProcess with unknown pid returns error", func(t *testing.T) {
		_, err := pm.WaitProcess(context.Background(), -1)
		assert.ErrorContains(t, err, "process not found")
	})

	t.Run("WaitProcess correctly reflects exit code and can be called multiple times", func(t *testing.T) {
		pc, err := pm.StartProcess(&StartProcess{LaunchCommand: LaunchCommand{Command: []string{"ls"}}})
		require.NoError(t, err)
		ec, err := pm.WaitProcess(context.Background(), pc.Pid)
		require.NoError(t, err)
		assert.Equal(t, 0, ec)
		ec, err = pm.WaitProcess(context.Background(), pc.Pid)
		require.NoError(t, err)
		assert.Equal(t, 0, ec)

		pm.ReleaseProcessHandles([]int{pc.Pid})
		_, err = pm.WaitProcess(context.Background(), pc.Pid)
		require.ErrorContains(t, err, "process not found")
	})

	t.Run("WaitProcess returns a cancelled error upon wait cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		pc, err := pm.StartProcess(&StartProcess{LaunchCommand: LaunchCommand{Command: []string{"sleep", "2"}}})
		require.NoError(t, err)
		_, err = pm.WaitProcess(ctx, pc.Pid)
		require.ErrorContains(t, err, "context canceled")
	})
}

func TestShutdownKillsProcessesGracefully(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())

	sp := &StartProcess{
		LaunchCommand: LaunchCommand{
			Command: []string{"sleep", "3"},
		},
		Stdout: StreamRedirect{Mode: Stream},
	}
	p1, err := pm.StartProcess(sp)
	require.NoError(t, err)
	p2, err := pm.StartProcess(sp)
	require.NoError(t, err)

	assert.True(t, isProcessGroupAlive(p1.Pid) && isProcessGroupAlive(p2.Pid))

	err = pm.Shutdown(true)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return !isProcessGroupAlive(p1.Pid) && !isProcessGroupAlive(p2.Pid)
	}, time.Second, 10*time.Millisecond)
}

func TestShutdownKillsExecCommandProcessGracefully(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())

	// Run ExecCommand in a goroutine so we can shut down while it's running.
	var (
		res *CommandResult
		err error
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, err = pm.ExecCommand(&LaunchCommand{Command: []string{"sleep", "4"}})
	}()
	// Give the command time to start and register its PID
	time.Sleep(40 * time.Millisecond)

	require.NoError(t, pm.Shutdown(false))

	// ExecCommand must return promptly after shutdown
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ExecCommand did not return after Shutdown")
	}

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotZero(t, res.Rc)
}

// TestShutdownRejectsNewWorkUnix is similar to the common TestShutdownRejectsNewWork, but adds
// an extra guarantee by using isProcessGroupAlive + require.Eventually to confirm the entire process
// group actually terminates.
func TestShutdownRejectsNewWorkUnix(t *testing.T) {
	pm := NewProcessManagerWithFs(afero.NewMemMapFs())

	sp := &StartProcess{
		LaunchCommand: LaunchCommand{
			Command: []string{"sleep", "1"},
		},
		Stdout: StreamRedirect{Mode: Stream},
		Stderr: StreamRedirect{Mode: Stream},
	}

	p, err := pm.StartProcess(sp)
	require.NoError(t, err)
	require.True(t, isProcessGroupAlive(p.Pid))

	require.NoError(t, pm.Shutdown(false))

	_, err = pm.StartProcess(sp)
	require.ErrorIs(t, err, ErrNewProcessInShutdown)

	_, err = pm.ExecCommand(&LaunchCommand{Command: []string{"echo", "hello"}})
	require.ErrorIs(t, err, ErrNewProcessInShutdown)

	require.Eventually(t, func() bool { return !isProcessGroupAlive(p.Pid) },
		time.Second, 10*time.Millisecond, "process group still alive")
}

func TestShutdownEscalation(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewProcessManagerWithFs(fs)
	const outPath = "stdout.txt"

	// process prints READY when trap has been set up
	proc, err := pm.StartProcess(&StartProcess{
		LaunchCommand: LaunchCommand{
			Command: []string{"sh", "-c", `trap '' INT; echo READY; while :; do sleep 1; done`},
		},
		Stdout: StreamRedirect{Mode: File, FilePath: outPath},
		Stderr: StreamRedirect{Mode: None},
		Stdin:  StdinNone,
	})
	require.NoError(t, err)
	require.True(t, isProcessGroupAlive(proc.Pid))

	require.Eventually(t, func() bool {
		data, err := afero.ReadFile(fs, outPath)
		return err == nil && bytes.Contains(data, []byte("READY"))
	}, 1*time.Second, 10*time.Millisecond)

	// Request graceful shutdown
	done := make(chan struct{})
	var gracefulErr error
	go func() {
		defer close(done)
		gracefulErr = pm.Shutdown(false)
	}()

	// Graceful shutdown should block as the interrupt is ignored
	require.Never(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 40*time.Millisecond, 10*time.Millisecond)

	// Request forceful shutdown
	require.NoError(t, pm.Shutdown(true))

	// This should cause graceful shutdown to unblock when the process is killed
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("graceful shutdown did not return after escalation")
	}
	require.NoError(t, gracefulErr)
	require.Eventually(t, func() bool { return !isProcessGroupAlive(proc.Pid) },
		time.Second, 10*time.Millisecond)
}

func TestReadGroupControllerChildPidFailed(t *testing.T) {
	pc := newProcessCommon(afero.NewMemMapFs())

	stdoutHandle := &streamHandle{}
	stderrHandle := &streamHandle{}
	stdinRedirect := &redirectHandle{}

	childPidReader, childPidWriter, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = childPidReader.Close()
		_ = childPidWriter.Close()
	})

	require.NoError(t, pc.incrementWaitGroup())

	cmd := &exec.Cmd{Process: &os.Process{Pid: 999999}}

	_, _, err = pc.readGroupControllerChildPid(
		cmd,
		stdoutHandle,
		stderrHandle,
		stdinRedirect,
		childPidReader,
		childPidWriter,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read child pid")
}
