// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

type executionRPCManager struct {
	process.ProcessManager
	waitStarted    chan int
	cleanupFailure error
	afterStart     func(*os.Process)
	mu             sync.Mutex
	killed         []int
}

func (m *executionRPCManager) StartProcess(request *process.StartProcess) (*os.Process, error) {
	// The in-process server is hosted by the test executable, which cannot
	// re-exec the agent's group-controller command. Its own suites and real
	// deployment tests cover that path; this suite exercises RPC and native exec.
	request.UseGroupController = false
	child, err := m.ProcessManager.StartProcess(request)
	if child != nil && m.afterStart != nil {
		m.afterStart(child)
	}
	return child, err
}

func (m *executionRPCManager) WaitProcess(ctx context.Context, pid int) (int, error) {
	m.waitStarted <- pid
	rc, err := m.ProcessManager.WaitProcess(ctx, pid)
	if err == nil && m.cleanupFailure != nil {
		err = &process.CleanupError{Err: m.cleanupFailure}
	}
	return rc, err
}

func (m *executionRPCManager) ExecCommand(request *process.LaunchCommand) (*process.CommandResult, error) {
	result, err := m.ProcessManager.ExecCommand(request)
	if err == nil && m.cleanupFailure != nil {
		err = &process.CleanupError{Err: m.cleanupFailure}
	}
	return result, err
}

func (m *executionRPCManager) KillProcess(pid int) error {
	err := m.ProcessManager.KillProcess(pid)
	m.mu.Lock()
	m.killed = append(m.killed, pid)
	m.mu.Unlock()
	return err
}

func newExecutionRPCFixture(t *testing.T) (*AgentEngine, *executionRPCManager) {
	t.Helper()
	manager := &executionRPCManager{ProcessManager: process.NewProcessManager(), waitStarted: make(chan int, 16)}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(message.ErrorHandlingServerInterceptor()))
	targetagentproto.RegisterTargetAgentServer(server, &grpcserver.AgentServerAPI{Pm: manager})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); _ = manager.Shutdown(true) })
	connection, err := grpc.NewClient("passthrough:///execution-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(message.ErrorHandlingClientInterceptor()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	platform := conductor.Linux
	switch runtime.GOOS {
	case "darwin":
		platform = conductor.Darwin
	case "windows":
		platform = conductor.Win
	}
	engine, cleanup := NewAgentEngine(context.Background(), context.Background(), targetagentproto.NewTargetAgentClient(connection), nil, nil, false, featureflags.Flags{}, conductor.PlatformConfiguration{OS: platform})
	t.Cleanup(cleanup)
	return engine, manager
}

func TestAgentExecutionRPCContract(t *testing.T) {
	// Given
	engine, manager := newExecutionRPCFixture(t)
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	directory := filepath.Join(t.TempDir(), "space ' & 世界")
	require.NoError(t, os.Mkdir(directory, 0700))
	marker := filepath.Join(directory, "unintended")
	args := append(processcontracttest.ArgumentValues(), processcontracttest.MarkerArgument(marker))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, exitCode := range []int{0, 126, 127} {
		t.Run(strconv.Itoa(exitCode), func(t *testing.T) {
			request := &process.LaunchCommand{Command: append([]string{filepath.Base(probe)}, args...), WorkingDirectory: directory,
				Environment: map[string]string{processcontracttest.EnvironmentKeysVariable: `["VALUE","PATH"]`, processcontracttest.ExitCodeVariable: strconv.Itoa(exitCode), "VALUE": "' & 世界\r\n\\", "PATH": filepath.Dir(probe)}}
			// When
			result, err := engine.RunProcess(ctx, request)
			// Then
			require.NoError(t, err)
			manager.mu.Lock()
			killed := append([]int(nil), manager.killed...)
			manager.mu.Unlock()
			require.Empty(t, killed, "completed execution must not kill the process")
			require.EqualValues(t, exitCode, result.Rc)
			report, err := processcontracttest.DecodeReport(result.Stdout)
			require.NoError(t, err)
			require.Equal(t, args, report.Arguments)
			require.Equal(t, request.Environment["VALUE"], report.Environment["VALUE"])
			require.Equal(t, request.Environment["PATH"], report.Environment["PATH"])
			actualDirectory, err := filepath.EvalSymlinks(directory)
			require.NoError(t, err)
			require.Equal(t, actualDirectory, report.WorkingDirectory)
			_, err = manager.ProcessManager.WaitProcess(ctx, report.PID)
			require.ErrorContains(t, err, "process not found", "completed handles must be released")
		})
	}
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err))
	_, err = engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{filepath.Join(directory, "missing")}})
	var failure *conductor.ExecutionError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, conductor.ExecutionLaunch, failure.Kind)
	require.Equal(t, process.FailureNotFound, failure.FailureReason)
	if runtime.GOOS != "windows" {
		program := filepath.Join(directory, "no-shebang")
		require.NoError(t, os.WriteFile(program, []byte("printf unintended > \"$1\"\n"), 0600))
		require.NoError(t, os.Chmod(program, 0700))
		_, err = engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{program, marker}})
		require.ErrorAs(t, err, &failure)
		require.Equal(t, conductor.ExecutionLaunch, failure.Kind)
		require.Equal(t, process.FailureExecutableFormat, failure.FailureReason)
		_, err = os.Stat(marker)
		require.True(t, os.IsNotExist(err), "direct RPC execution must not retry through a shell")
	}
}

func TestAgentExecutionRPCChild(t *testing.T) {
	switch os.Getenv("APX_EXECUTION_RPC_CHILD") {
	case "binary":
		_, _ = os.Stdout.Write([]byte{0, 255, '\n', '\r', 128})
		_, _ = os.Stderr.Write([]byte{254, 0, '\r', '\n', 129})
		os.Exit(127)
	case "wait":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func TestAgentExecutionRPCBinaryOutput(t *testing.T) {
	engine, _ := newExecutionRPCFixture(t)
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{executable, "-test.run=^TestAgentExecutionRPCChild$"}, Environment: map[string]string{"APX_EXECUTION_RPC_CHILD": "binary"}})
	require.NoError(t, err)
	require.EqualValues(t, 127, result.Rc)
	require.Equal(t, []byte{0, 255, '\n', '\r', 128}, []byte(result.Stdout))
	require.Equal(t, []byte{254, 0, '\r', '\n', 129}, []byte(result.Stderr))
}

func TestAgentExecutionRPCCancellation(t *testing.T) {
	engine, manager := newExecutionRPCFixture(t)
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{executable, "-test.run=^TestAgentExecutionRPCChild$"}, Environment: map[string]string{"APX_EXECUTION_RPC_CHILD": "wait"}})
		finished <- err
	}()
	var pid int
	select {
	case pid = <-manager.waitStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("process did not reach WaitProcess")
	}
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled process did not finish cleanup")
	}
	manager.mu.Lock()
	killed := append([]int(nil), manager.killed...)
	manager.mu.Unlock()
	require.Equal(t, []int{pid}, killed)
	_, err = manager.ProcessManager.WaitProcess(context.Background(), pid)
	require.ErrorContains(t, err, "process not found", "cancelled handles must be released")
}

func TestAgentExecutionRPCCancellationDuringStart(t *testing.T) {
	engine, manager := newExecutionRPCFixture(t)
	executable, err := os.Executable()
	require.NoError(t, err)
	started := make(chan int, 1)
	replyAllowed := make(chan struct{})
	var allowOnce sync.Once
	allowReply := func() { allowOnce.Do(func() { close(replyAllowed) }) }
	t.Cleanup(allowReply)
	manager.afterStart = func(child *os.Process) {
		started <- child.Pid
		<-replyAllowed
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{executable, "-test.run=^TestAgentExecutionRPCChild$"}, Environment: map[string]string{"APX_EXECUTION_RPC_CHILD": "wait"}})
		finished <- err
	}()
	var pid int
	select {
	case pid = <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not start the child")
	}
	cancel()
	select {
	case err := <-finished:
		t.Fatalf("execution returned before receiving the PID needed for cleanup: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	allowReply()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("execution did not clean up after the start reply")
	}
	manager.mu.Lock()
	killed := append([]int(nil), manager.killed...)
	manager.mu.Unlock()
	require.Equal(t, []int{pid}, killed)
	_, err = manager.ProcessManager.WaitProcess(context.Background(), pid)
	require.ErrorContains(t, err, "process not found", "startup cancellation must release the returned PID")
}

func TestAgentExecutionRPCCancellationAfterProcessExit(t *testing.T) {
	// Given a process that exits before its startup reply reaches the engine.
	engine, manager := newExecutionRPCFixture(t)
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var pid int
	manager.afterStart = func(child *os.Process) {
		pid = child.Pid
		_, waitErr := manager.ProcessManager.WaitProcess(context.Background(), pid)
		require.NoError(t, waitErr)
		cancel()
	}

	// When cancellation tries to stop the already-finished process.
	_, err = engine.RunProcess(ctx, &process.LaunchCommand{Command: []string{executable, "-test.run=^TestAgentExecutionRPCChild$"}})

	// Then cancellation releases its record without adding a cleanup error.
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, context.Canceled, err)
	_, err = manager.ProcessManager.WaitProcess(context.Background(), pid)
	require.ErrorContains(t, err, "process not found")
}

func TestAgentExecutionOwnsTemporaryDirectory(t *testing.T) {
	// Given a real RPC runner and a probe that reports its temporary directory.
	engine, _ := newExecutionRPCFixture(t)
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	parent := t.TempDir()
	request := &process.LaunchCommand{Command: []string{probe},
		Environment:        map[string]string{processcontracttest.EnvironmentKeysVariable: `["SCRIPT_DIRECTORY"]`, processcontracttest.ExitCodeVariable: "0"},
		TemporaryDirectory: &process.TemporaryDirectory{EnvironmentVariable: "SCRIPT_DIRECTORY", ParentDirectory: parent}}
	// When the remote process runs to completion.
	result, err := engine.RunProcess(context.Background(), request)
	// Then the directory crosses the RPC boundary and is removed before return.
	require.NoError(t, err)
	require.Zero(t, result.Rc, result.Stderr)
	report, err := processcontracttest.DecodeReport(result.Stdout)
	require.NoError(t, err)
	directory := report.Environment["SCRIPT_DIRECTORY"]
	require.Equal(t, parent, filepath.Dir(directory))
	_, err = os.Stat(directory)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAgentExecutionRPCKeepsExitCodeWhenCleanupFails(t *testing.T) {
	// Given a real agent RPC server with cleanup failure injected after child exit.
	engine, manager := newExecutionRPCFixture(t)
	manager.cleanupFailure = fmt.Errorf("private cleanup path: %w", os.ErrPermission)
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	for _, code := range []int{0, 19} {
		for _, mode := range []string{"RunProcess", "ExecCommand"} {
			t.Run(mode+"/"+strconv.Itoa(code), func(t *testing.T) {
				request := &process.LaunchCommand{Command: []string{probe}, Environment: map[string]string{
					processcontracttest.EnvironmentKeysVariable: `[]`, processcontracttest.ExitCodeVariable: strconv.Itoa(code),
				}}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				// When the completed result and cleanup diagnostic cross the RPC boundary.
				var result *process.CommandResult
				var err error
				if mode == "RunProcess" {
					result, err = engine.RunProcess(ctx, request)
				} else {
					result, err = engine.ExecCommand(request)
				}
				// Then both exit status and the safe cleanup diagnostic reach the caller.
				require.NotNil(t, result)
				require.EqualValues(t, code, result.Rc)
				var failure *conductor.ExecutionError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, process.FailurePermission, failure.FailureReason)
				require.False(t, strings.Contains(err.Error(), "private cleanup path"))
				report, decodeErr := processcontracttest.DecodeReport(result.Stdout)
				require.NoError(t, decodeErr)
				require.Equal(t, code, report.ExitCode)
			})
		}
	}
}
