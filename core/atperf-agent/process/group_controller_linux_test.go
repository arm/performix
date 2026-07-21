// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package process

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type mockCgroupManager struct {
	m mock.Mock
}

func (m *mockCgroupManager) newCgroupManager() cgroupManager { return m }
func (m *mockCgroupManager) Path() string                    { args := m.m.Called(); return args.String(0) }
func (m *mockCgroupManager) Mounted() bool                   { args := m.m.Called(); return args.Bool(0) }
func (m *mockCgroupManager) Create(name string) error        { args := m.m.Called(name); return args.Error(0) }
func (m *mockCgroupManager) Migrate(pid int) error           { args := m.m.Called(pid); return args.Error(0) }
func (m *mockCgroupManager) Destroy() error                  { args := m.m.Called(); return args.Error(0) }
func (m *mockCgroupManager) Kill() error                     { args := m.m.Called(); return args.Error(0) }

// runInSubprocess runs the given test body in a subprocess, re-execing the current test binary.
// GroupController, by its nature, modifies the current processes state (signal forwards, subreaping)
// This creates a problem when other tests are also modifying the same process state (e.g., TestShutdownEscalation)
// So, to avoid this collision (and future ones), we simply run `Init()` tests with the helper below
func runInSubprocess(t *testing.T, body func(t *testing.T)) {
	t.Helper()

	// Child: Run the test body
	if os.Getenv("GC_SUBPROC") == "1" {
		body(t)
		return
	}

	// Parent: Re-exec the test process in a subprocess
	// #nosec G204 -- this just re-execs the current test binary. no need to panic
	cmd := exec.Command(os.Args[0], "-test.run", "^"+t.Name()+"$", "-test.v", "-test.timeout", "30s")
	cmd.Env = append(os.Environ(), "GC_SUBPROC=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGKILL}

	out, err := cmd.CombinedOutput()
	if err != nil || testing.Verbose() {
		t.Logf("subprocess output:\n%s", out)
	}

	// Bubble up the test results
	if ee, ok := err.(*exec.ExitError); ok && !ee.Success() {
		t.Fatalf("subprocess failed: %v", err)
	}
	if err != nil {
		t.Fatalf("could not run subprocess: %v", err)
	}
}

func newController(cfg GroupControllerConfig, fac cgroupManagerFactory) *linuxGroupController {
	return &linuxGroupController{
		execPath:     os.Args[0],
		cfg:          cfg,
		cgMgrFactory: fac,
	}
}

func TestGroupController_Init(t *testing.T) {
	runInSubprocess(t, func(t *testing.T) {
		t.Run("succeeds under normal parent (ppid != 1)", func(t *testing.T) {
			c, err := NewGroupController(GroupControllerConfig{GraceTimeout: 1 * time.Second})
			require.NoError(t, err)

			err = c.Init()
			defer c.Close()
			assert.NoError(t, err)

			// We don't check cgroupv2 here because the CI might not have it and/or have different permissions in place.
			// Important thing is that Init() doesn't fail EVEN if cgroupv2 is not available.
		})

		t.Run("succeeds under normal parent (ppid != 1) with cgroupv2", func(t *testing.T) {
			cgMgr := &mockCgroupManager{}
			cgMgr.m.On("Mounted").Return(true)
			cgMgr.m.On("Create", mock.Anything).Return(nil)

			c := newController(GroupControllerConfig{
				GraceTimeout: 1 * time.Second,
			}, cgMgr)

			err := c.Init()
			defer c.Close()
			require.NoError(t, err)

			assert.NotNil(t, c.cgMgr)
			cgMgr.m.AssertExpectations(t)
		})

		t.Run("succeeds under normal parent (ppid != 1) with no cgroupv2", func(t *testing.T) {
			cgMgr := &mockCgroupManager{}
			cgMgr.m.On("Mounted").Return(false)

			c := newController(GroupControllerConfig{
				GraceTimeout: 1 * time.Second,
			}, cgMgr)

			err := c.Init()
			defer c.Close()
			require.NoError(t, err)

			assert.Nil(t, c.cgMgr)
		})

		for _, sendChildWaitStatus := range []bool{false, true} {
			t.Run(fmt.Sprintf("successfully reaps children when SendChildWaitStatus is %v", sendChildWaitStatus), func(t *testing.T) {
				if sendChildWaitStatus {
					// APAP-4709: this case is flaky in CI. Keep the SendChildWaitStatus=false case enabled so
					// the subreaper path is still covered while the wait-status delivery race is investigated.
					t.Skip("test disabled")
				}

				cgMgr := &mockCgroupManager{}
				cgMgr.m.On("Mounted").Return(false)

				c := newController(GroupControllerConfig{
					GraceTimeout:        1 * time.Second,
					SendChildWaitStatus: sendChildWaitStatus,
				}, cgMgr)

				err := c.Init()
				defer c.Close()
				require.NoError(t, err)

				cmd := exec.Command("/bin/true")
				require.NoError(t, cmd.Start())
				pid := cmd.Process.Pid

				require.NoError(t, cmd.Process.Release())

				assert.Eventually(t, func() bool {
					var status syscall.WaitStatus
					wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
					if wpid == 0 {
						return false
					}
					var errno syscall.Errno
					return err != nil && errors.As(err, &errno) && errno == syscall.ECHILD
				}, 3*time.Second, 50*time.Millisecond, "child was not reaped in time")

				assert.Eventually(t, func() bool {
					select {
					case <-c.childWaitStatus:
						return c.cfg.SendChildWaitStatus
					default:
						return !c.cfg.SendChildWaitStatus
					}
				}, 3*time.Second, 50*time.Millisecond, "wait status should only be sent when SendChildWaitStatus is true")
			})
		}

		t.Run("successfully forwards signals to worker", func(t *testing.T) {
			startSignalEchoChild := func(t *testing.T) (*exec.Cmd, *bytes.Buffer, *os.File) {
				t.Helper()
				var buf bytes.Buffer

				// Create a dedicated readiness pipe; child will write to READY into it
				readyR, readyW, err := os.Pipe()
				require.NoError(t, err)

				script := `trap 'echo TERM; exit 0' TERM; trap 'echo INT; exit 0' INT; trap 'echo HUP; exit 0' HUP; trap 'echo QUIT; exit 0' QUIT; echo READY >&3; read x`
				cmd := exec.Command("/bin/sh", "-c", script)
				cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
				cmd.Stdout = &buf
				cmd.Stderr = &buf

				// Pass the readiness pipe to the child
				cmd.ExtraFiles = []*os.File{readyW}

				stdin, err := cmd.StdinPipe()
				if err != nil {
					t.Fatalf("stdin pipe: %v", err)
				}
				if err := cmd.Start(); err != nil {
					t.Fatalf("start child: %v", err)
				}

				_ = readyW.Close()
				_ = stdin // keep open so the child blocks
				return cmd, &buf, readyR
			}

			cgMgr := &mockCgroupManager{}
			cgMgr.m.On("Mounted").Return(false)

			c := newController(GroupControllerConfig{
				GraceTimeout: 1 * time.Second,
			}, cgMgr)

			err := c.Init()
			defer c.Close()
			require.NoError(t, err)

			child, out, readyR := startSignalEchoChild(t)
			defer readyR.Close()
			defer func() { _ = child.Process.Kill() }()

			// Wait for child to become READY
			readyCh := make(chan error, 1)
			go func() {
				_, err := bufio.NewReader(readyR).ReadString('\n')
				readyCh <- err
			}()

			// Confirm child is READY
			select {
			case err := <-readyCh:
				require.NoError(t, err, "reading readiness from child")
			case <-time.After(5 * time.Second):
				t.Fatalf("child did not become READY in time; output so far: %q", out.String())
			}

			c.workerPGID.Store(int64(child.Process.Pid))

			// SIGTERM this process; the forwarder should relay to the worker group
			require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM), "sending SIGTERM to self")

			// Confirm child exits with TERM
			waitCh := make(chan error, 1)
			go func() {
				waitCh <- child.Wait()
			}()

			select {
			case err := <-waitCh:
				// We ignore ECHILD here because the group controller may have reaped it already
				require.True(t, err == nil || errors.Is(err, syscall.ECHILD), "child exited unexpectedly: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for child to exit after SIGTERM")
			}
			assert.Contains(t, out.String(), "TERM", "did not see TERM in output: %q", out.String())
		})
	})
}

func TestGroupController_Shutdown(t *testing.T) {
	// Helper: start a child that echoes on TERM and exits.
	startTermEchoChild := func(t *testing.T) (*exec.Cmd, *bytes.Buffer) {
		t.Helper()
		var buf bytes.Buffer

		// Create a dedicated readiness pipe; child will write READY into it
		readyR, readyW, err := os.Pipe()
		require.NoError(t, err)

		// On TERM print "TERM" and exit 0; otherwise block on stdin.
		script := `trap 'echo TERM; exit 0' TERM; echo READY >&3; read x`
		cmd := exec.Command("/bin/sh", "-c", script)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdout = &buf
		cmd.Stderr = &buf

		// Pass the readiness pipe to the child
		cmd.ExtraFiles = []*os.File{readyW}

		stdin, err := cmd.StdinPipe()
		require.NoError(t, err)
		require.NoError(t, cmd.Start())

		_ = readyW.Close()
		readyCh := make(chan error, 1)
		go func() {
			_, err := bufio.NewReader(readyR).ReadString('\n')
			readyCh <- err
		}()

		select {
		case err := <-readyCh:
			require.NoError(t, err, "reading readiness from child")
		case <-time.After(5 * time.Second):
			t.Fatalf("child did not become READY in time")
		}
		_ = readyR.Close()

		_ = stdin // keep open so the child blocks
		return cmd, &buf
	}

	// Helper: start a child that ignores TERM and blocks.
	startIgnoreTermChild := func(t *testing.T) *exec.Cmd {
		t.Helper()

		// Create a dedicated readiness pipe; child will write READY into it
		readyR, readyW, err := os.Pipe()
		require.NoError(t, err)

		// Ignore TERM; block on stdin.
		script := `trap '' TERM; echo READY >&3; read x`
		cmd := exec.Command("/bin/sh", "-c", script)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

		// Pass the readiness pipe to the child
		cmd.ExtraFiles = []*os.File{readyW}

		stdin, err := cmd.StdinPipe()
		require.NoError(t, err)
		require.NoError(t, cmd.Start())

		_ = readyW.Close()
		readyCh := make(chan error, 1)
		go func() {
			_, err := bufio.NewReader(readyR).ReadString('\n')
			readyCh <- err
		}()

		select {
		case err := <-readyCh:
			require.NoError(t, err, "reading readiness from child")
		case <-time.After(5 * time.Second):
			t.Fatalf("child did not become READY in time")
		}
		_ = readyR.Close()

		_ = stdin // keep open so the child blocks
		return cmd
	}

	t.Run("destroys cgroup", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Destroy").Return(nil)

		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)

		c.cgMgr = cgMgr
		c.workerPGID.Store(0)

		c.Shutdown()

		cgMgr.m.AssertExpectations(t)
	})

	t.Run("sends SIGTERM to child", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Destroy").Return(nil)

		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)

		child, out := startTermEchoChild(t)
		defer func() { _ = child.Process.Kill() }()

		c.workerPGID.Store(int64(child.Process.Pid))
		require.Eventually(t, func() bool {
			return c.workerPGID.Load() == int64(child.Process.Pid)
		}, 3*time.Second, 100*time.Millisecond, "child PGID not set in time")

		// Shutdown the group controller; the child should exit
		c.Shutdown()

		// Confirm child exits with TERM
		waitCh := make(chan error, 1)
		go func() {
			waitCh <- child.Wait()
		}()

		select {
		case err := <-waitCh:
			// We ignore ECHILD here because the group controller may have reaped it already
			require.True(t, err == nil || errors.Is(err, syscall.ECHILD), "child exited unexpectedly: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for child to exit after Shutdown")
		}
		assert.Contains(t, out.String(), "TERM", "did not see TERM in child output: %q", out.String())
	})

	t.Run("sends cgroup Kill to child", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Kill").Return(nil)
		cgMgr.m.On("Destroy").Return(nil)

		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)
		c.cgMgr = cgMgr

		child := startIgnoreTermChild(t)
		defer func() {
			_ = child.Process.Kill()
			_ = child.Wait()
		}()

		c.workerPGID.Store(int64(child.Process.Pid))
		require.Eventually(t, func() bool {
			return c.workerPGID.Load() == int64(child.Process.Pid)
		}, 3*time.Second, 50*time.Millisecond, "child PGID not set in time")

		c.Shutdown()

		cgMgr.m.AssertCalled(t, "Kill")
		cgMgr.m.AssertExpectations(t)
	})

	t.Run("sends SIGKILL to child", func(t *testing.T) {
		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, &mockCgroupManager{})

		child := startIgnoreTermChild(t)

		c.workerPGID.Store(int64(child.Process.Pid))
		require.Eventually(t, func() bool {
			return c.workerPGID.Load() == int64(child.Process.Pid)
		}, 3*time.Second, 50*time.Millisecond, "child PGID not set in time")

		c.Shutdown()
		err := child.Wait()

		// The child should have been killed by SIGKILL
		require.Error(t, err, "expected non-zero/signal termination")
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)

		ws, ok := exitErr.Sys().(syscall.WaitStatus)
		require.True(t, ok, "expected WaitStatus")
		assert.Equal(t, syscall.SIGKILL, ws.Signal(), "expected SIGKILL to terminate the child")
	})

	t.Run("Shutdown is idempotent", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Destroy").Return(nil).Once()

		c := newController(GroupControllerConfig{GraceTimeout: 3 * time.Second}, cgMgr)
		c.cgMgr = cgMgr

		c.Shutdown()
		c.Shutdown()

		cgMgr.m.AssertExpectations(t)
	})
}

func TestGroupController_WaitUntilEOF(t *testing.T) {
	cgMgr := &mockCgroupManager{}
	cgMgr.m.On("Destroy").Return(nil)

	c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)
	c.cgMgr = cgMgr

	// Create a pipe and duplicate its read end onto fd 0 so WaitUntilEOF reads from it.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()

	old0, err := unix.Dup(unix.Stdin)
	require.NoError(t, err)
	defer func() {
		_ = unix.Dup2(old0, unix.Stdin)
		_ = unix.Close(old0)
	}()

	require.NoError(t, unix.Dup2(int(r.Fd()), unix.Stdin))

	done := make(chan struct{})
	go func() {
		c.WaitUntilEOF()
		close(done)
	}()

	// Trigger EOF
	require.NoError(t, w.Close())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for WaitUntilEOF to return after stdin closed")
	}

	assert.True(t, c.shutdownCalled.Load(), "expected Shutdown to be called when stdin closed")
	cgMgr.m.AssertExpectations(t)
}

func TestNewGroupController_SpawnProcess(t *testing.T) {
	t.Run("attempts to use CLONE_INTO_CGROUP if available", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Path").Return(t.TempDir())

		c := &linuxGroupController{
			cfg:   GroupControllerConfig{GraceTimeout: time.Second},
			cgMgr: cgMgr,
		}

		args := []string{"/bin/true"}
		_ = c.SpawnProcess(args)

		cgMgr.m.AssertCalled(t, "Path")
	})

	t.Run("attempts to start the process", func(t *testing.T) {
		c := &linuxGroupController{
			cfg: GroupControllerConfig{GraceTimeout: time.Second},
		}

		args := []string{"/nonexistent-binary"}
		err := c.SpawnProcess(args)
		assert.ErrorContains(t, err, "start process")
	})
}

func sendWaitStatus(t *testing.T, c *linuxGroupController, pid int) {
	err := c.childCmd.Wait()
	var status unix.WaitStatus
	if err == nil {
		if c.childCmd.ProcessState != nil {
			if sys, ok := c.childCmd.ProcessState.Sys().(syscall.WaitStatus); ok {
				status = unix.WaitStatus(sys)
			}
		}
	} else if exitErr, ok := err.(*exec.ExitError); ok {
		if sys, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			status = unix.WaitStatus(sys)
		}
	}
	c.childWaitStatus <- childWaitStatus{status: status, pid: pid}
	close(c.childWaitStatus)
}

type waitForChildLogHook struct {
	ready chan struct{}
	once  sync.Once
}

func (h *waitForChildLogHook) Levels() []log.Level { return log.AllLevels }
func (h *waitForChildLogHook) Fire(e *log.Entry) error {
	if strings.Contains(e.Message, "waiting for child process") {
		h.once.Do(func() { close(h.ready) })
	}
	return nil
}

func TestGroupController_WaitForChildProcess(t *testing.T) {
	t.Run("returns nil when child exits cleanly", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Destroy").Return(nil)

		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)
		c.cgMgr = cgMgr

		c.childCmd = exec.Command("ls")
		err := c.childCmd.Start()
		require.NoError(t, err)

		childPid := c.childCmd.Process.Pid
		c.childWaitStatus = make(chan childWaitStatus, 1)
		go sendWaitStatus(t, c, childPid)

		done := make(chan struct{})
		go func() {
			err := c.WaitForChildProcess()
			assert.NoError(t, err)
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for WaitForChildProcess to return")
		}

		assert.True(t, c.shutdownCalled.Load(), "expected Shutdown to be called by WaitForChildProcess")
		cgMgr.m.AssertExpectations(t)
	})

	t.Run("returns ChildProcessExitError when child is signaled", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Destroy").Return(nil)

		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)
		c.cgMgr = cgMgr

		c.childCmd = exec.Command("sleep", "5")
		err := c.childCmd.Start()
		require.NoError(t, err)

		childPid := c.childCmd.Process.Pid
		c.childWaitStatus = make(chan childWaitStatus, 1)
		_ = c.childCmd.Process.Signal(syscall.SIGKILL)
		go sendWaitStatus(t, c, childPid)

		err = c.WaitForChildProcess()
		require.Error(t, err)
		exitErr, ok := err.(*ChildProcessExitError)
		require.True(t, ok)
		assert.Equal(t, childPid, exitErr.Pid)
		assert.Equal(t, PosixSignalExitBase+int(syscall.SIGKILL), exitErr.ExitCode)
	})

	t.Run("returns error for unknown wait status", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Destroy").Return(nil)

		const pid = 1234

		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)
		c.cgMgr = cgMgr
		c.childCmd = &exec.Cmd{Process: &os.Process{Pid: pid}}
		c.childWaitStatus = make(chan childWaitStatus, 1)
		c.childWaitStatus <- childWaitStatus{pid: pid, status: 0x7f}
		close(c.childWaitStatus)

		err := c.WaitForChildProcess()
		require.EqualError(t, err, fmt.Sprintf("child process %d exited with unknown status", pid))
	})

	t.Run("initiates shutdown when signal received while waiting", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Destroy").Return(nil)

		const pid = 5678

		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)
		c.cgMgr = cgMgr
		c.childCmd = &exec.Cmd{Process: &os.Process{Pid: pid}}
		c.childWaitStatus = make(chan childWaitStatus)

		errCh := make(chan error, 1)
		go func() {
			errCh <- c.WaitForChildProcess()
		}()

		readyCh := make(chan struct{})
		hook := &waitForChildLogHook{ready: readyCh}
		logger := log.StandardLogger()
		logger.AddHook(hook)

		select {
		case <-readyCh:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for WaitForChildProcess to set up signal handler")
		}

		require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))

		require.Eventually(t, func() bool {
			return c.shutdownCalled.Load()
		}, 3*time.Second, 50*time.Millisecond, "expected shutdown to be triggered by signal")

		c.childWaitStatus <- childWaitStatus{pid: pid, status: 0}
		close(c.childWaitStatus)

		select {
		case err := <-errCh:
			assert.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for WaitForChildProcess to return after signal")
		}
	})

	t.Run("drains wait statuses after child returns", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		cgMgr.m.On("Destroy").Return(nil)

		const pid = 9123

		c := newController(GroupControllerConfig{GraceTimeout: 100 * time.Millisecond}, cgMgr)
		c.cgMgr = cgMgr
		c.childCmd = &exec.Cmd{Process: &os.Process{Pid: pid}}
		c.workerPGID.Store(int64(pid))
		c.childWaitStatus = make(chan childWaitStatus)

		errCh := make(chan error, 1)
		go func() {
			errCh <- c.WaitForChildProcess()
		}()

		firstSent := make(chan struct{})
		go func() {
			c.childWaitStatus <- childWaitStatus{pid: pid, status: 0}
			close(firstSent)
		}()

		select {
		case <-firstSent:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out sending initial child wait status")
		}

		select {
		case err := <-errCh:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("WaitForChildProcess did not return after child exit")
		}

		secondSent := make(chan struct{})
		go func() {
			c.childWaitStatus <- childWaitStatus{pid: pid + 1, status: 0}
			close(secondSent)
		}()

		select {
		case <-secondSent:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out sending additional wait status after child exit")
		}

		close(c.childWaitStatus)
	})
}

func TestGroupController_WriteChildPid(t *testing.T) {
	t.Run("write succeeds", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		c := newController(GroupControllerConfig{}, cgMgr)
		c.cgMgr = cgMgr

		r, w, err := os.Pipe()
		require.NoError(t, err)
		defer r.Close()
		defer w.Close()

		fd, err := unix.Dup(int(w.Fd()))
		require.NoError(t, err)
		defer unix.Close(fd)

		const pid = 1234
		require.NoError(t, c.writeChildPid(fd, pid))
		require.NoError(t, w.Close())

		got, err := bufio.NewReader(r).ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("%d\n", pid), got)
	})

	t.Run("write fails", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		c := newController(GroupControllerConfig{}, cgMgr)
		c.cgMgr = cgMgr

		r, w, err := os.Pipe()
		require.NoError(t, err)
		defer r.Close()
		defer w.Close()

		fd, err := unix.Dup(int(r.Fd())) // Incorrectly use fd of read end
		require.NoError(t, err)
		defer unix.Close(fd)

		const pid = 1234
		err = c.writeChildPid(fd, pid)
		require.ErrorContains(t, err, "failed to write pid")
	})

	t.Run("write fails, invalid fd", func(t *testing.T) {
		cgMgr := &mockCgroupManager{}
		c := newController(GroupControllerConfig{}, cgMgr)
		c.cgMgr = cgMgr

		const fd = -1
		const pid = 1234
		err := c.writeChildPid(fd, pid)
		require.ErrorContains(t, err, "failed to create new file with fd")
	})
}

func TestLinuxBuildCmdUsesTasksetForAffinity(t *testing.T) {
	pm := NewProcessManager()
	cmd, err := pm.buildCmd(&LaunchCommand{
		Command:  []string{"echo", "test"},
		Affinity: []string{"1", "2"},
	})
	require.NoError(t, err)
	require.NotNil(t, cmd)
	assert.Equal(t, []string{"taskset", "-c", "1,2", "echo", "test"}, cmd.Args)
}

func TestLinuxStartProcessIsVisibleInProcfs(t *testing.T) {
	pm := NewProcessManager()
	proc, err := pm.StartProcess(&StartProcess{
		LaunchCommand: LaunchCommand{Command: []string{"sleep", "5"}},
	})
	require.NoError(t, err)
	defer func() { _ = proc.Kill() }()

	_, err = os.Stat(fmt.Sprintf("/proc/%d", proc.Pid))
	require.NoError(t, err)
	require.NoError(t, pm.KillProcess(proc.Pid))

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(fmt.Sprintf("/proc/%d", proc.Pid))
		return os.IsNotExist(statErr)
	}, time.Second, 10*time.Millisecond)
}

func startProcessWithGroupController(t *testing.T, pm ProcessManager, command []string) *os.Process {
	sp := &StartProcess{
		LaunchCommand: LaunchCommand{
			Command:     command,
			Environment: map[string]string{"CLI_MODE": "1"},
		},
		Stdout:             StreamRedirect{Mode: Stream},
		Stderr:             StreamRedirect{Mode: Stream},
		UseGroupController: true,
	}
	proc, err := pm.StartProcess(sp)
	require.NoError(t, err)
	return proc
}

func readStdout(t *testing.T, pm ProcessManager, pid int) string {
	sender := &fakeSender{ctx: context.Background()}
	require.NoError(t, pm.StreamStdout(pid, sender))
	return string(bytes.Join(sender.chunks, nil))
}

func waitForExitCode(t *testing.T, pm ProcessManager, pid int, expectedExitCode int, logbuf bytes.Buffer) {
	exitCode, err := pm.WaitProcess(context.Background(), pid)
	require.NoError(t, err)
	require.Equal(t, expectedExitCode, exitCode)
	require.Contains(t, logbuf.String(), "start-group-controller --wait-for-child --child-pid-fd=3")
}

func TestStartProcess_UseProcessGroupController(t *testing.T) {
	var logbuf bytes.Buffer
	defer log.SetOutput(log.StandardLogger().Out)
	log.SetOutput(&logbuf)

	pm := NewProcessManagerWithFs(afero.NewMemMapFs())
	command := []string{"bash", "-c", "echo pid is $$"}
	proc := startProcessWithGroupController(t, pm, command)

	stdout := readStdout(t, pm, proc.Pid)
	require.Contains(t, stdout, fmt.Sprintf("Spawned process: [%s]", strings.Join(command, " ")))
	require.Contains(t, stdout, fmt.Sprintf("pid is %d\n", proc.Pid))
	waitForExitCode(t, pm, proc.Pid, 0, logbuf)
}

func TestStartProcess_UseProcessGroupController_ExitError(t *testing.T) {
	var logbuf bytes.Buffer
	defer log.SetOutput(log.StandardLogger().Out)
	log.SetOutput(&logbuf)

	pm := NewProcessManagerWithFs(afero.NewMemMapFs())
	command := []string{"bash", "-c", "exit 66"}
	proc := startProcessWithGroupController(t, pm, command)

	stdout := readStdout(t, pm, proc.Pid)
	require.Contains(t, stdout, fmt.Sprintf("Spawned process: [%s]", strings.Join(command, " ")))
	require.Contains(t, stdout, fmt.Sprintf("child process %d exited with code 66\n", proc.Pid))
	waitForExitCode(t, pm, proc.Pid, 66, logbuf)
}

func TestStartProcess_UseProcessGroupController_KillChild(t *testing.T) {
	var logbuf bytes.Buffer
	defer log.SetOutput(log.StandardLogger().Out)
	log.SetOutput(&logbuf)

	pm := NewProcessManagerWithFs(afero.NewMemMapFs())
	command := []string{"bash", "-c", "sleep 60"}
	proc := startProcessWithGroupController(t, pm, command)
	require.NoError(t, pm.KillProcess(proc.Pid))

	stdout := readStdout(t, pm, proc.Pid)
	require.Contains(t, stdout, fmt.Sprintf("Spawned process: [%s]", strings.Join(command, " ")))
	require.Contains(t, stdout, fmt.Sprintf("child process %d exited with code %d\n", proc.Pid, PosixSignalExitBase+int(syscall.SIGKILL)))
	waitForExitCode(t, pm, proc.Pid, PosixSignalExitBase+int(syscall.SIGKILL), logbuf)
}

func TestStartProcess_UseProcessGroupController_InterruptChild(t *testing.T) {
	var logbuf bytes.Buffer
	defer log.SetOutput(log.StandardLogger().Out)
	log.SetOutput(&logbuf)

	pm := NewProcessManagerWithFs(afero.NewMemMapFs())
	command := []string{"bash", "-c", "sleep 60"}
	proc := startProcessWithGroupController(t, pm, command)
	require.NoError(t, pm.InterruptProcess(proc.Pid))

	stdout := readStdout(t, pm, proc.Pid)
	require.Contains(t, stdout, fmt.Sprintf("Spawned process: [%s]", strings.Join(command, " ")))
	require.Contains(t, stdout, fmt.Sprintf("child process %d exited with code %d\n", proc.Pid, PosixSignalExitBase+int(syscall.SIGINT)))
	waitForExitCode(t, pm, proc.Pid, PosixSignalExitBase+int(syscall.SIGINT), logbuf)
}
