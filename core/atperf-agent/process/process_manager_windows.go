// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"golang.org/x/sys/windows"
)

// WindowsProcessManager is an implementation of ProcessManager for Windows targets.
type WindowsProcessManager struct {
	common *processCommon
}

// Windows API constants
const (
	ATTACH_PARENT_PROCESS = 0xFFFFFFFF
)

// Windows API functions
var (
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procSetProcessAffinityMask = kernel32.NewProc("SetProcessAffinityMask")
	procFreeConsole            = kernel32.NewProc("FreeConsole")
	procAttachConsole          = kernel32.NewProc("AttachConsole")
	procSetConsoleCtrlHandler  = kernel32.NewProc("SetConsoleCtrlHandler")
)

// Time to wait after interrupting a process for it to exit (allow for post processing)
var InterruptProcessWaitForExitTimeout = 30 * time.Second

// freeConsole detaches the calling process from its console.
func freeConsole() error {
	ret, _, err := procFreeConsole.Call()
	if ret == 0 {
		return err
	}
	return nil
}

// attachConsole attaches the calling process to the console of the specified process.
func attachConsole(dwProcessId uint32) error {
	ret, _, err := procAttachConsole.Call(uintptr(dwProcessId))
	if ret == 0 {
		return err
	}
	return nil
}

// setConsoleIgnoreCtrlEvents toggles whether the console attached to the calling process ignores console control events.
func setConsoleIgnoreCtrlEvents(ignore bool) error {
	addVal := uintptr(0)
	nullptr := uintptr(0)
	if ignore {
		addVal = 1
	}
	ret, _, err := procSetConsoleCtrlHandler.Call(nullptr, addVal)
	if ret == 0 {
		return err
	}
	return nil
}

// isProcessRunning returns true if the PID appears to still exist.
// It returns false when Windows reports the PID is invalid/non-existent.
// For access-denied scenarios, it conservatively returns true.
func isProcessRunning(pid uint32) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)

	status, waitErr := windows.WaitForSingleObject(h, 0)
	if waitErr != nil {
		return true
	}

	return status == uint32(windows.WAIT_TIMEOUT)
}

// waitForProcessExit waits for the specified process to exit within the given timeout.
func (w *WindowsProcessManager) WaitProcessWithTimeout(pid uint32, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	val, ok := w.common.managedProcesses.Load(int(pid))
	if !ok {
		return nil
	}
	if _, err := val.(*managedProcess).procState.WaitForExitCode(ctx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("failed waiting for process %d to exit", pid)
	}

	return nil
}

// setProcessAffinityMask sets the processor affinity mask for the specified process.
func setProcessAffinityMask(hProcess windows.Handle, dwProcessAffinityMask uintptr) error {
	ret, _, err := procSetProcessAffinityMask.Call(uintptr(hProcess), dwProcessAffinityMask)
	if ret == 0 {
		return err
	}
	return nil
}

// KillProcess sends a termination signal to the given PID on Windows using OpenProcess/TerminateProcess.
func (w *WindowsProcessManager) KillProcess(pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("failed to open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("failed to terminate process %d: %w", pid, err)
	}
	return nil
}

// InterruptProcess sends CTRL+C to all the processes attached to the console of the given PID.
// We make sure that each PID gets its own console. This is the only way to reliably send CTRL+C.
func (w *WindowsProcessManager) InterruptProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}

	targetPID := uint32(pid)

	if err := freeConsole(); err != nil {
		log.WithError(err).Error("Failed to release current console")
	}

	// According to ChatGPT:
	// "Calling cmd.Start() with for a process created with CREATE_NEW_CONSOLE does not guarantee that the new console exists
	// by the time you call AttachConsole(). The console is created// asynchronously, so AttachConsole() can fail with
	// ERROR_INVALID_HANDLE or ERROR_ACCESS_DENIED even though the process has started successfully [...] there is a
	// real race condition, and it’s a known quirk of how Windows creates consoles"

	// Retrying attaching a few times with increasing delays
	attachRetryCount := 5

	var attachError error
	for retry := range attachRetryCount {
		attachError = attachConsole(targetPID)
		if attachError == nil {
			break
		}
		if !isProcessRunning(targetPID) {
			// Couldn't attach because process exited, nothing to do
			return nil
		}
		time.Sleep(time.Duration(200*(retry+1)) * time.Millisecond)
	}

	if attachError != nil {
		return fmt.Errorf("failed to attach to console of process %d after %d attempts: %w", targetPID, attachRetryCount, attachError)
	}

	if err := setConsoleIgnoreCtrlEvents(true); err != nil {
		return fmt.Errorf("failed to disable console control handling for current process: %w", err)
	}

	defer func() {
		if err := freeConsole(); err != nil {
			log.WithError(err).Error("Failed to free console after sending Ctrl+C")
		}
		if err := attachConsole(ATTACH_PARENT_PROCESS); err != nil {
			log.WithError(err).Error("Failed to re-attach to parent console after sending Ctrl+C")
		}
		if err := setConsoleIgnoreCtrlEvents(false); err != nil {
			log.WithError(err).Error("Failed to re-enable console control handling for current process")
		}
	}()

	// Send Ctrl+C to the child's console (affects all processes in that console)
	err := windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, 0)
	if err != nil {
		return fmt.Errorf("failed to send CTRL+C to console of process %d: %w", targetPID, err)
	}

	// Wait for process to exit before reattaching the console or the Ctrl+C might be received
	// by the atperf-agent process resulting in its exit.
	if err := w.WaitProcessWithTimeout(targetPID, InterruptProcessWaitForExitTimeout); err != nil {
		return err
	}

	return nil
}

// buildCmd creates the executable command for Windows, handling sudo-like behavior (or lack thereof) and affinity.
func (w *WindowsProcessManager) buildCmd(lc *LaunchCommand) (*exec.Cmd, error) {
	var parts []string

	parts = append(parts, lc.Command...)

	if lc.AsPrivileged {
		// Not available under windows without user interaction
		return nil, fmt.Errorf("AsPrivileged flag is not supported on Windows without interactive elevation")
	}

	//nolint:gosec
	c := exec.Command(parts[0], parts[1:]...)

	if len(lc.Environment) > 0 {
		c.Env = mergeEnv(lc.Environment)
	}
	if lc.WorkingDirectory != "" {
		c.Dir = lc.WorkingDirectory
	}

	// Always create a process group and a dedicated console for CTRL+C handling
	creationFlags := uint32(syscall.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NEW_CONSOLE)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: creationFlags}

	log.WithField("cmd", strings.Join(c.Args, " ")).Info("Launch command")

	return c, nil
}

// setCPUAffinityAfterStart sets CPU affinity for a Windows process after it has started
func (w *WindowsProcessManager) setCPUAffinityAfterStart(pid int, affinity []string) error {
	if len(affinity) == 0 {
		return nil
	}

	var affinityMask uintptr
	for _, cpu := range expandAffinityTokens(affinity) {
		if cpu >= 0 && cpu < 64 {
			affinityMask |= 1 << uint(cpu)
		}
	}

	if affinityMask == 0 {
		return fmt.Errorf("invalid CPU affinity specification")
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_SET_INFORMATION, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("failed to open process %d for affinity setting: %w", pid, err)
	}
	defer windows.CloseHandle(handle)

	if err := setProcessAffinityMask(handle, affinityMask); err != nil {
		return fmt.Errorf("failed to set CPU affinity for process %d: %w", pid, err)
	}

	return nil
}

// StartProcess launches the command asynchronously, returning the PID.
func (w *WindowsProcessManager) StartProcess(sp *StartProcess) (*os.Process, error) {
	return w.common.StartProcessCommon(sp, w)
}

func (p *WindowsProcessManager) ReleaseProcessHandles(pids []int) {
	p.common.ReleaseProcessHandles(pids)
}

// ExecCommand runs the command to completion, capturing stdout, stderr, and exit code.
func (w *WindowsProcessManager) ExecCommand(lc *LaunchCommand) (*CommandResult, error) {
	return w.common.ExecCommandCommon(lc, w)
}

// expandAffinityTokens expands CPU affinity tokens into a list of individual CPU indices.
// Supports individual indices (e.g., "0", "2") and ranges (e.g., "1-3").
func expandAffinityTokens(tokens []string) []int {
	var cpus []int
	for _, token := range tokens {
		for _, part := range strings.Split(token, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.Contains(part, "-") {
				bounds := strings.SplitN(part, "-", 2)
				start, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
				if err1 != nil || err2 != nil {
					continue
				}
				if start > end {
					start, end = end, start
				}
				for cpu := start; cpu <= end; cpu++ {
					cpus = append(cpus, cpu)
				}
				continue
			}
			cpu, err := strconv.Atoi(part)
			if err != nil {
				continue
			}
			cpus = append(cpus, cpu)
		}
	}
	return cpus
}

func (w *WindowsProcessManager) StreamStdout(pid int, stream StreamChunkSender) error {
	return w.common.StreamStdout(pid, stream)
}

func (w *WindowsProcessManager) StreamStderr(pid int, stream StreamChunkSender) error {
	return w.common.StreamStderr(pid, stream)
}

func (w *WindowsProcessManager) WriteToStdin(pid int, data []byte) error {
	return w.common.WriteToStdin(pid, data)
}

func (w *WindowsProcessManager) WaitProcess(ctx context.Context, pid int) (int, error) {
	return w.common.WaitProcess(ctx, pid)
}

// Shutdown stops accepting new processes and handles process termination.
//   - force=true: Terminate all known processes and return immediately.
//   - force=false: Send CTRL+C to all known processes and wait for them to exit.
func (w *WindowsProcessManager) Shutdown(force bool) error {
	return w.common.shutdownProcesses(force, w)
}

// NewProcessManager constructs a WindowsProcessManager with the default OS Fs.
func NewProcessManager() ProcessManager {
	return NewProcessManagerWithFs(afero.NewOsFs())
}

// NewProcessManagerWithFs constructs a WindowsProcessManager with the given Fs.
func NewProcessManagerWithFs(fs afero.Fs) ProcessManager {
	return &WindowsProcessManager{common: newProcessCommon(fs)}
}
