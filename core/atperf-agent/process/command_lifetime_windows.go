// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// commandKilledExitCode is the Windows status for an interrupted process.
const commandKilledExitCode = uint32(windows.STATUS_CONTROL_C_EXIT)

// commandLifetime owns the complete Windows process tree. The job handle is
// never inherited, and closing it terminates every remaining descendant.
type commandLifetime struct {
	mu  sync.Mutex
	job windows.Handle
}

func (l *commandLifetime) configureCancellation(cmd *exec.Cmd) {
	if cmd.Cancel != nil {
		// Only context-bound direct execution uses the cancellation marker.
		cmd.Cancel = func() error { return l.terminate(commandKilledExitCode) }
	}
}

func (l *commandLifetime) start(cmd *exec.Cmd, _ []string) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create process job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("configure process job: %w", err)
	}

	// os/exec closes the initial thread handle and has no job-list attribute.
	// Start suspended so the child cannot spawn descendants before assignment.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	l.mu.Lock()
	l.job = job
	err = cmd.Start()
	if err != nil {
		l.mu.Unlock()
		l.close()
		return err
	}

	// The mutex also holds off CommandContext cancellation until assignment.
	// #nosec G115 -- Windows process IDs are unsigned 32-bit values.
	pid := uint32(cmd.Process.Pid)
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if err == nil {
		err = windows.AssignProcessToJobObject(job, handle)
	}
	if err == nil {
		err = resumeInitialThread(pid)
	}
	l.mu.Unlock()
	if err != nil {
		// Assignment can fail before the job owns the suspended child.
		_ = cmd.Process.Kill()
		l.close()
		_ = cmd.Wait()
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return fmt.Errorf("start process in job: %w", err)
	}

	go func() {
		defer func() { _ = windows.CloseHandle(handle) }()
		_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
		// Close at parent exit, before os/exec waits for inherited output pipes.
		// Descendants must not keep those pipes, or the operation, alive.
		l.close()
	}()
	return nil
}

// Managed kills and forced shutdowns retain their public exit status.
func (l *commandLifetime) kill(_ *os.Process) error { return l.terminate(1) }

func (l *commandLifetime) terminate(exitCode uint32) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.job == 0 {
		return os.ErrProcessDone
	}
	return windows.TerminateJobObject(l.job, exitCode)
}

func (l *commandLifetime) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.job != 0 {
		_ = windows.CloseHandle(l.job)
		l.job = 0
	}
}

func resumeInitialThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		defer func() { _ = windows.CloseHandle(thread) }()
		_, err = windows.ResumeThread(thread)
		return err
	}
	if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return errors.New("suspended process has no initial thread")
	}
	return err
}
