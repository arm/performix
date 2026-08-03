// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows || linux || darwin

package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
)

// ErrNewProcessInShutdown is returned when new processes are rejected during shutdown
var ErrNewProcessInShutdown = errors.New("cannot start new process while server is shutting down")

type shutdownState int

const (
	stateRunning  shutdownState = iota // accepting new work
	stateGraceful                      // graceful shutdown in progress
	stateForced                        // forced shutdown in progress
)

type redirectHandle struct {
	pr        *os.File
	pw        *os.File
	closeOnce sync.Once
}

// managedProcess holds the stdio handles and process state for a running process.
type managedProcess struct {
	stdout    *streamHandle
	stderr    *streamHandle
	stdin     *redirectHandle
	procState *ProcessState
}

// processCommon provides shared functionality for Linux and Windows process managers.
// It is unexported to prevent accidental direct usage as a ProcessManager; platform
// implementations hold a pointer and delegate explicitly.
type processCommon struct {
	fs afero.Fs

	managedProcesses sync.Map // key=pid, value=*managedProcess

	shutdownWg sync.WaitGroup
	shutdownMu sync.Mutex
	state      shutdownState
}

func newProcessCommon(fs afero.Fs) *processCommon {
	return &processCommon{fs: fs}
}

// streamHandle manages STDIO streams by holding pipe reader/writer and optional file.
type streamHandle struct {
	reader *os.File       // pipe reader for streaming
	writer *os.File       // pipe writer for streaming
	file   io.WriteCloser // file to write output when RedirectMode includes File
	once   sync.Once      // ensures stream is consumed only once
}

// newStreamHandle creates a streamHandle based on RedirectMode, opening a pipe and/or file.
func (pc *processCommon) newStreamHandle(mode RedirectMode, filePath string) (*streamHandle, error) {
	h := &streamHandle{}

	if IsStreamModeEnabled(mode) {
		r, w, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		h.reader = r
		h.writer = w
	}

	if IsFileModeEnabled(mode) {
		f, err := pc.fs.Create(filePath)
		if err != nil {
			h.close()
			return nil, err
		}
		h.file = f
	}

	return h, nil
}

// newRedirectHandle constructs a redirectHandle used for stdin redirection based on the provided StdinMode.
func (*processCommon) newRedirectHandle(mode StdinMode) (*redirectHandle, error) {
	h := &redirectHandle{}

	switch mode {
	case StdinNone:
	case StdinBuffer:
		pr, pw, err := os.Pipe()
		if err != nil {
			return nil, fmt.Errorf("error creating pipe: %w", err)
		}
		h.pr = pr
		h.pw = pw
	default:
		return nil, fmt.Errorf("unsupported redirect mode: %v", mode)
	}

	return h, nil
}

func (pc *processCommon) validateFilePath(filePath string) error {
	if filePath == "" || filePath == "." {
		return fmt.Errorf("file path must be non-empty: %s", filePath)
	}

	dir := filepath.Dir(filePath)
	info, err := pc.fs.Stat(dir)
	if err != nil {
		return fmt.Errorf("directory does not exist: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}

	if _, err := pc.fs.Stat(filePath); err == nil {
		return fmt.Errorf("file already exists: %s", filePath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("error checking file: %w", err)
	}

	return nil
}

// Releases any pipe endpoints held by the redirectHandle, while collecting all errors.
// Safe to call multiple times concurrently (first call closes pipe endpoints, subsequent calls are no-ops).
func (r *redirectHandle) close() error {
	var errs []error
	r.closeOnce.Do(func() {
		if r.pw != nil {
			if err := r.pw.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if r.pr != nil {
			if err := r.pr.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	})

	if len(errs) > 0 {
		return fmt.Errorf("error closing redirect handle: %v", errs)
	}

	return nil
}

// writerTarget returns an io.Writer that writes to file, pipe, or both according to streamHandle.
func (s *streamHandle) writerTarget() io.Writer {
	switch {
	case s.file != nil && s.writer != nil:
		return io.MultiWriter(s.file, s.writer)
	case s.file != nil:
		return s.file
	case s.writer != nil:
		return s.writer
	default:
		return io.Discard
	}
}

// markUsed ensures the stream is only consumed once; returns error on subsequent calls.
func (s *streamHandle) markUsed() error {
	var first bool
	s.once.Do(func() { first = true })
	if first {
		return nil
	}
	return errors.New("stream already consumed")
}

// closeWriters closes underlying writer and file writer if present.
func (s *streamHandle) closeWriters() {
	if s.writer != nil {
		_ = s.writer.Close()
	}
	if s.file != nil {
		_ = s.file.Close()
	}
}

// closeReader closes the pipe reader if present.
func (s *streamHandle) closeReader() {
	if s.reader != nil {
		_ = s.reader.Close()
	}
}

// close cleans up both writers and reader.
func (s *streamHandle) close() {
	s.closeWriters()
	s.closeReader()
}

// mergeEnv merges the current inheritted env with the overrides.
// When setting the command's env, it replaces the previously inherited env.
func mergeEnv(overrides map[string]string) []string {
	env := os.Environ()
	for k, v := range overrides {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

// drainStream reads from the registered pipe and sends chunks until EOF or error.
func (*processCommon) drainStream(sh *streamHandle, name string, stream StreamChunkSender) error {
	// Enforce exactly once
	if err := sh.markUsed(); err != nil {
		return err
	}

	defer func() {
		sh.closeReader()
	}()

	buf := make([]byte, 4096)
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		default:
			n, err := sh.reader.Read(buf)
			if n > 0 {
				if err2 := stream.Send(&StreamChunk{Data: buf[:n]}); err2 != nil {
					return err2
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				if stream.Context().Err() != nil {
					return stream.Context().Err()
				}
				return err
			}
		}
	}
}

// StreamStdout begins streaming stdout chunks for a given pid.
func (pc *processCommon) StreamStdout(pid int, stream StreamChunkSender) error {
	v, ok := pc.managedProcesses.Load(pid)
	if ok {
		stdOut := v.(*managedProcess).stdout
		if stdOut != nil {
			return pc.drainStream(stdOut, "stdout", stream)
		}
	}
	return fmt.Errorf("stdout stream not available for pid %d", pid)
}

// StreamStderr begins streaming stderr chunks for a given pid.
func (pc *processCommon) StreamStderr(pid int, stream StreamChunkSender) error {
	v, ok := pc.managedProcesses.Load(pid)
	if ok {
		stderr := v.(*managedProcess).stderr
		if stderr != nil {
			return pc.drainStream(stderr, "stderr", stream)
		}
	}
	return fmt.Errorf("stderr stream not available for pid %d", pid)
}

// WriteToStdin writes data to the stdin of a running process.
func (pc *processCommon) WriteToStdin(pid int, data []byte) error {
	v, ok := pc.managedProcesses.Load(pid)
	if !ok {
		return fmt.Errorf("pid not found %d", pid)
	}

	procAccess := v.(*managedProcess)
	stdin := procAccess.stdin

	if stdin == nil || stdin.pw == nil {
		return fmt.Errorf("stdin redirection not available for pid %d", pid)
	}

	defer func() { _ = stdin.close() }()

	if _, err := stdin.pw.Write(data); err != nil {
		return fmt.Errorf("error writing to stdin for pid %d: %w", pid, err)
	}

	return nil
}

func (pc *processCommon) WaitProcess(ctx context.Context, pid int) (int, error) {
	val, ok := pc.managedProcesses.Load(pid)
	if !ok {
		return -1, errors.New("process not found")
	}
	return val.(*managedProcess).procState.WaitForExitCode(ctx)
}

// incrementWaitGroup adds a process to the shutdown wait group.
// It should be called before starting the process so that Shutdown knows what
// processes it needs to wait for.
// It should always be paired with a decrementWaitGroup.
func (pc *processCommon) incrementWaitGroup() error {
	pc.shutdownMu.Lock()
	defer pc.shutdownMu.Unlock()
	if pc.state != stateRunning {
		return ErrNewProcessInShutdown
	}
	pc.shutdownWg.Add(1)
	return nil
}

// decrementWaitGroup releases a process from the shutdown wait group.
// It should always be paired with an incrementWaitGroup.
func (pc *processCommon) decrementWaitGroup() {
	pc.shutdownWg.Done()
}

// registerProcess stores process handles for shutdown management
func (pc *processCommon) registerProcess(pid int, stdoutHandle, stderrHandle *streamHandle, stdinRedirect *redirectHandle, processState *ProcessState, sp *StartProcess) {

	ps := &managedProcess{procState: processState}

	// Maintain the stdio redirect mappings
	if IsStreamModeEnabled(sp.Stdout.Mode) {
		ps.stdout = stdoutHandle
	}
	if IsStreamModeEnabled(sp.Stderr.Mode) {
		ps.stderr = stderrHandle
	}
	if sp.Stdin != StdinNone {
		ps.stdin = stdinRedirect
	}

	pc.managedProcesses.Store(pid, ps)
}

func (pc *processCommon) ReleaseProcessHandles(pids []int) {
	for _, pid := range pids {
		pc.managedProcesses.Delete(pid)
	}
}

// applyInFlightShutdown delivers the appropriate signal to a process that was
// registered in the shutdown wait group but did not register its PID process state
// in time for Shutdown to signal it.
func (pc *processCommon) applyInFlightShutdown(pid int, mgr ProcessManager) {
	pc.shutdownMu.Lock()
	st := pc.state
	pc.shutdownMu.Unlock()

	switch st {
	case stateGraceful:
		_ = mgr.InterruptProcess(pid)
	case stateForced:
		_ = mgr.KillProcess(pid)
	}
}

// shutdownProcesses handles the shutdown logic for all registered processes
func (pc *processCommon) shutdownProcesses(force bool, mgr ProcessManager) error {
	pc.shutdownMu.Lock()
	prev := pc.state
	next := prev

	if force && prev != stateForced {
		next = stateForced
	} else if !force && prev == stateRunning {
		next = stateGraceful
	}

	pc.state = next
	pc.shutdownMu.Unlock()

	// Don't send signals if we haven't transitioned states
	didTransition := prev != next

	switch next {
	case stateGraceful:
		if didTransition {
			pc.managedProcesses.Range(func(key, _ any) bool {
				if pid, ok := key.(int); ok {
					_ = mgr.InterruptProcess(pid)
				}
				return true
			})
		}
		pc.shutdownWg.Wait()
	case stateForced:
		if didTransition {
			pc.managedProcesses.Range(func(key, _ any) bool {
				if pid, ok := key.(int); ok {
					_ = mgr.KillProcess(pid)
				}
				return true
			})
		}
	}
	return nil
}

func (pc *processCommon) startProcessWithGroupController(sp *StartProcess) (*os.File, *os.File, func(), error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to find executable path: %w", err)
	}

	cmdLine := []string{
		execPath,
		"start-group-controller",
		"--wait-for-child",
		"--child-pid-fd=3", // File descriptor 3 is implied by appending the write pipe to Cmd.ExtraFiles (0=stdin, 1=stdout, 2=stderr)
		"--",
	}
	sp.LaunchCommand.Command = append(cmdLine, sp.LaunchCommand.Command...)

	childPidReader, childPidWriter, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create child pid pipe: %w", err)
	}

	closePipe := func() {
		if childPidReader != nil {
			_ = childPidReader.Close()
		}
		if childPidWriter != nil {
			_ = childPidWriter.Close()
		}
	}

	return childPidReader, childPidWriter, closePipe, nil
}

func (pc *processCommon) readGroupControllerChildPid(
	cmd *exec.Cmd,
	stdoutHandle *streamHandle,
	stderrHandle *streamHandle,
	stdinRedirect *redirectHandle,
	childPidReader *os.File,
	childPidWriter *os.File,
) (*os.Process, int, error) {
	log.Debugf("Started group controller process with pid %d", cmd.Process.Pid)

	cleanup := func() {
		stdoutHandle.close()
		stderrHandle.close()
		_ = stdinRedirect.close()
		if cmd.Process != nil {
			if killErr := cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				log.WithError(killErr).Warn("failed to terminate group controller process after failure to read child pid")
			}
			_ = cmd.Wait()
		}
		pc.decrementWaitGroup()
	}

	_ = childPidWriter.Close()
	childPidWriter = nil

	childPid, err := readChildPID(childPidReader)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("failed to read child pid: %w", err)
	}

	_ = childPidReader.Close()
	childPidReader = nil

	childProc, err := os.FindProcess(childPid)
	if err != nil {
		cleanup()
		return nil, 0, fmt.Errorf("failed to find child process %d: %w", childPid, err)
	}

	log.Debugf("Group controller started child process with pid %d", childPid)

	return childProc, childPid, nil
}

func readChildPID(r io.Reader) (int, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("read child pid: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return 0, errors.New("received empty child pid from group controller")
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("parse child pid %q: %w", pidStr, err)
	}

	if pid <= 0 {
		return 0, fmt.Errorf("invalid child pid %d", pid)
	}

	return pid, nil
}

// StartProcessCommon provides the common StartProcess implementation for platform-specific managers
func (pc *processCommon) StartProcessCommon(sp *StartProcess, builder ProcessManager) (*os.Process, error) {
	if IsFileModeEnabled(sp.Stdout.Mode) {
		sp.Stdout.FilePath = filepath.Clean(sp.Stdout.FilePath)
		if err := pc.validateFilePath(sp.Stdout.FilePath); err != nil {
			return nil, fmt.Errorf("invalid stdout file path: %w", err)
		}
	}
	if IsFileModeEnabled(sp.Stderr.Mode) {
		sp.Stderr.FilePath = filepath.Clean(sp.Stderr.FilePath)
		if err := pc.validateFilePath(sp.Stderr.FilePath); err != nil {
			return nil, fmt.Errorf("invalid stderr file path: %w", err)
		}
	}
	if IsFileModeEnabled(sp.Stdout.Mode) &&
		IsFileModeEnabled(sp.Stderr.Mode) &&
		sp.Stdout.FilePath == sp.Stderr.FilePath {
		return nil, fmt.Errorf("stdout and stderr file paths must be different: %s", sp.Stdout.FilePath)
	}

	var childPidReader *os.File
	var childPidWriter *os.File

	if sp.UseGroupController {
		var err error
		var closePipe func()
		childPidReader, childPidWriter, closePipe, err = pc.startProcessWithGroupController(sp)
		if err != nil {
			return nil, err
		}
		defer closePipe()
	}

	cmd, err := builder.buildCmd(&sp.LaunchCommand)
	if err != nil {
		return nil, fmt.Errorf("failed to build command: %w", err)
	}

	if childPidWriter != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, childPidWriter)
	}

	stdoutHandle, err := pc.newStreamHandle(sp.Stdout.Mode, sp.Stdout.FilePath)
	if err != nil {
		return nil, fmt.Errorf("stdout setup error: %w", err)
	}
	stderrHandle, err := pc.newStreamHandle(sp.Stderr.Mode, sp.Stderr.FilePath)
	if err != nil {
		stdoutHandle.close()
		return nil, fmt.Errorf("stderr setup error: %w", err)
	}
	stdinRedirect, err := pc.newRedirectHandle(sp.Stdin)
	if err != nil {
		stdoutHandle.close()
		stderrHandle.close()
		return nil, fmt.Errorf("stdin setup error: %w", err)
	}

	cmd.Stdout = stdoutHandle.writerTarget()
	cmd.Stderr = stderrHandle.writerTarget()
	cmd.Stdin = stdinRedirect.pr

	if err := pc.incrementWaitGroup(); err != nil {
		stdoutHandle.close()
		stderrHandle.close()
		_ = stdinRedirect.close()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		stdoutHandle.close()
		stderrHandle.close()
		_ = stdinRedirect.close()
		pc.decrementWaitGroup()
		return nil, fmt.Errorf("error running command: %w", err)
	}

	pid := cmd.Process.Pid
	proc := cmd.Process

	if sp.UseGroupController {
		childProc, childPid, err := pc.readGroupControllerChildPid(
			cmd,
			stdoutHandle,
			stderrHandle,
			stdinRedirect,
			childPidReader,
			childPidWriter,
		)
		if err != nil {
			return nil, err
		}

		// We now want to appear as if there is no group controller involved.
		// Note: when the controller process exits after waiting on the child process
		//       it should exit with the same status as the child process.
		pid = childPid
		proc = childProc
	}

	// Set CPU affinity if specified (platform-specific)
	if len(sp.LaunchCommand.Affinity) > 0 {
		if err := builder.setCPUAffinityAfterStart(pid, sp.LaunchCommand.Affinity); err != nil {
			return nil, fmt.Errorf("failed to set CPU affinity: %w", err)
		}
	}

	processState := NewProcessState(cmd)
	pc.registerProcess(pid, stdoutHandle, stderrHandle, stdinRedirect, processState, sp)

	// Send shutdown signal to process if shutdown began while process was starting
	pc.applyInFlightShutdown(pid, builder)

	// Wait for the process to exit and clean up resources
	go func() {
		_ = cmd.Wait()
		processState.ProcessExited()
		stdoutHandle.closeWriters()
		stderrHandle.closeWriters()
		_ = stdinRedirect.close()
		pc.decrementWaitGroup()
	}()

	return proc, nil
}

// ExecCommandCommon provides the common ExecCommand implementation for platform-specific managers
func (pc *processCommon) ExecCommandCommon(lc *LaunchCommand, builder ProcessManager) (*CommandResult, error) {
	cmd, err := builder.buildCmd(lc)
	if err != nil {
		return nil, fmt.Errorf("failed to build command: %w", err)
	}

	// Treat a command that cannot be resolved through PATH like any other
	// non-zero command result. This lets callers probe optional executables
	// without requiring a shell to translate the lookup failure into an exit
	// code. Other launch failures remain errors.
	if errors.Is(cmd.Err, exec.ErrNotFound) {
		return &CommandResult{
			Rc:     CommandNotFoundExitCode,
			Stderr: cmd.Err.Error(),
		}, nil
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := pc.incrementWaitGroup(); err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		pc.decrementWaitGroup()
		return nil, fmt.Errorf("error starting command: %w", err)
	}

	pid := cmd.Process.Pid

	// Set CPU affinity if specified (platform-specific)
	if len(lc.Affinity) > 0 {
		if err := builder.setCPUAffinityAfterStart(pid, lc.Affinity); err != nil {
			return nil, err
		}
	}

	processState := NewProcessState(cmd)
	pc.managedProcesses.Store(pid, &managedProcess{procState: processState})

	// Send shutdown signal to process if shutdown began while process was starting
	pc.applyInFlightShutdown(pid, builder)

	waitCh := make(chan error, 1)

	go func() {
		waitCh <- cmd.Wait()
		processState.ProcessExited()
		pc.decrementWaitGroup()
		pc.managedProcesses.Delete(pid)
	}()

	err = <-waitCh

	// Extract return code (platform-neutral)
	var rc int32
	if cmd.ProcessState != nil {
		// #nosec G115 -- safe conversion
		rc = int32(cmd.ProcessState.ExitCode())
	} else if err != nil {
		return nil, err
	}

	return &CommandResult{
		Rc:     rc,
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}, nil
}
