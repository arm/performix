// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package process

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

// See: https://docs.kernel.org/admin-guide/cgroup-v2.html
const (
	cgroupRoot    = "/sys/fs/cgroup"
	cgroupV2Magic = 0x63677270 // "cgrp"
)

// Posix signals are reported as exit code 128 + signal.
const PosixSignalExitBase = 128

type cgroupManagerFactory interface {
	newCgroupManager() cgroupManager
}

type cgroupManager interface {
	Path() string
	Mounted() bool

	Create(name string) error
	Migrate(pid int) error
	Destroy() error

	Kill() error
}
type cgroupManagerImpl struct {
	path string
}

type childWaitStatus struct {
	status unix.WaitStatus
	pid    int
}

type linuxGroupController struct {
	cfg        GroupControllerConfig
	execPath   string
	workerPGID atomic.Int64
	reapCh     chan os.Signal
	fwSigCh    chan os.Signal
	fwSigs     []os.Signal

	shutdownCalled atomic.Bool
	closeOnce      sync.Once

	// (Optional) cgroupv2
	cgMgr        cgroupManager
	cgMgrFactory cgroupManagerFactory

	childCmd        *exec.Cmd
	childWaitStatus chan childWaitStatus
}

type defaultCgroupManagerFactory struct{}

func (defaultCgroupManagerFactory) newCgroupManager() cgroupManager {
	return &cgroupManagerImpl{}
}

func newGroupController(cfg GroupControllerConfig) (GroupController, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to find exec path: %w", err)
	}

	// Heuristic signals to be forwared to the worker process group
	fwSigs := []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT}

	return &linuxGroupController{
		cfg:          cfg,
		execPath:     path,
		cgMgrFactory: defaultCgroupManagerFactory{},
		fwSigs:       fwSigs,
	}, nil
}

func (cg *cgroupManagerImpl) Path() string {
	return cg.path
}

// Mounted returns whether /sys/fs/cgroup is a cgroupv2 mount.
func (cg *cgroupManagerImpl) Mounted() bool {
	var st unix.Statfs_t
	if err := unix.Statfs(cgroupRoot, &st); err != nil {
		return false
	}
	return st.Type == cgroupV2Magic
}

// Create creates a private cgroupv2 at cg.path.
func (cg *cgroupManagerImpl) Create(name string) error {
	cgPath := filepath.Join(cgroupRoot, name)
	if err := os.MkdirAll(cgPath, perms.LocalDirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", cgPath, err)
	}
	cg.path = cgPath

	return nil
}

// Migrate moves the given pid to the cgroup.
func (cg *cgroupManagerImpl) Migrate(pid int) error {
	migratePath := filepath.Join(cg.path, "cgroup.procs")
	if err := os.WriteFile(migratePath, []byte(strconv.Itoa(pid)), perms.LocalFilePerm); err != nil {
		return fmt.Errorf("write cgroup.procs: %w", err)
	}
	return nil
}

// Destroy kills andremoves the cgroup.
func (cg *cgroupManagerImpl) Destroy() error {
	_ = cg.Kill()
	cg.waitEmpty(5 * time.Second)
	if err := os.Remove(cg.path); err != nil {
		return fmt.Errorf("destroy cgroup: %w", err)
	}
	return nil
}

// Kill sends SIGKILL to all processes in the cgroup via cgroup.kill.
func (cg *cgroupManagerImpl) Kill() error {
	killPath := filepath.Join(cg.path, "cgroup.kill")
	if err := os.WriteFile(killPath, []byte("1"), perms.LocalFilePerm); err != nil {
		return fmt.Errorf("cgroup.kill: %w", err)
	}
	return nil
}

// Init performs one-time group controller process setup so that it:
//
//   - Exits if parent dies (PR_SET_PDEATHSIG)
//   - Becomes a subreaper for children (PR_SET_CHILD_SUBREAPER)
//   - Starts a reaper goroutine to avoid zombies.
//   - Prepares signal forwarding to the worker process group
//   - Sets up Linux cgroupv2 if available.
//     They allow us to reliably contain, track and kill all descendants (child processes).
//     Only a process with write access to /sys/fs/cgroup can escape it.
//     Without cgroups, a descendant could escape its PGID, and our control, by
//     forking a child and have it setsid() to create a new session and PGID.
func (c *linuxGroupController) Init() error {
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGHUP), 0, 0, 0); err != nil {
		return fmt.Errorf("set pdeathsig: %w", err)
	}
	if os.Getppid() == 1 {
		return fmt.Errorf("orphaned at start (ppid=1). aborting")
	}

	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set child subreaper: %w", err)
	}

	// Reap children
	c.reapCh = make(chan os.Signal, 1)
	c.childWaitStatus = make(chan childWaitStatus, 1)
	signal.Notify(c.reapCh, syscall.SIGCHLD)
	go func() {
		defer close(c.childWaitStatus)
		for range c.reapCh {
			for {
				var status unix.WaitStatus
				var rusage unix.Rusage

				pid, err := unix.Wait4(-1, &status, unix.WNOHANG, &rusage)
				if pid <= 0 {
					if err != nil && err != unix.ECHILD {
						log.WithError(err).Warn("wait4 failed while reaping child")
					}
					break
				}

				if c.cfg.SendChildWaitStatus {
					c.childWaitStatus <- childWaitStatus{status: status, pid: pid}
				}
			}
		}
	}()

	// Forward signals
	c.fwSigCh = make(chan os.Signal, 4)
	signal.Notify(c.fwSigCh, c.fwSigs...)

	go func() {
		for s := range c.fwSigCh {
			pgid := int(c.workerPGID.Load())
			if pgid <= 0 {
				continue
			}
			if err := unix.Kill(-pgid, s.(syscall.Signal)); err != nil && err != unix.ESRCH {
				log.WithError(err).WithField("pgid", pgid).Warn("failed to forward signal to worker process group")
			} else {
				log.WithField("sig", s).WithField("pgid", pgid).Info("forwarded signal to worker process group")
			}
		}
	}()

	// Setup cgroupv2 if available
	cg := c.cgMgrFactory.newCgroupManager()
	if cg.Mounted() {
		cgName := fmt.Sprintf("%v-gc-%d", terminology.GetAgentBinaryName(), os.Getpid())
		if err := cg.Create(cgName); err != nil {
			log.WithError(err).Warn("failed to create cgroup; continuing without cgroup")
		} else {
			c.cgMgr = cg
		}
	} else {
		log.Warn("cgroup manager not initialized")
	}

	return nil
}

// Close stops the reaper and signal forwarder goroutines.
func (c *linuxGroupController) Close() {
	c.closeOnce.Do(func() {
		if c.reapCh != nil {
			signal.Stop(c.reapCh)
			close(c.reapCh)
		}
		if c.fwSigCh != nil {
			signal.Stop(c.fwSigCh)
			close(c.fwSigCh)
		}

		signal.Reset(c.fwSigs...)
	})
}

// Shutdown sends SIGTERM, optionally cgroup.kill, and finally SIGKILL to the worker process group.
func (c *linuxGroupController) Shutdown() {
	if c.shutdownCalled.Swap(true) {
		return
	}

	defer func() {
		if c.cgMgr != nil {
			if err := c.cgMgr.Destroy(); err != nil {
				log.WithError(err).Warn("failed to destroy cgroup")
			}
		}
	}()

	pgid := int(c.workerPGID.Load())

	if pgid <= 0 {
		log.Warn("shutdown requested but workerPGID was not set; nothing to do")
		return
	}

	// Phase 1: SIGTERM
	if err := unix.Kill(-pgid, syscall.SIGTERM); err != nil && err != unix.ESRCH {
		log.WithError(err).WithField("pgid", pgid).Warn("failed to send SIGTERM to worker group")
	} else {
		log.WithField("pgid", pgid).Info("sent SIGTERM to worker process group")
	}

	if c.waitGroupExit(pgid) {
		return
	}

	// Phase 2: cgroup.kill
	if c.cgMgr != nil {
		if err := c.cgMgr.Kill(); err != nil {
			log.WithError(err).Warn("cgroup.kill failed")
		}

		if c.waitGroupExit(pgid) {
			return
		}
	}

	// Phase 3: SIGKILL
	if err := unix.Kill(-pgid, syscall.SIGKILL); err != nil && err != unix.ESRCH {
		log.WithError(err).WithField("pgid", pgid).Warn("failed to send SIGKILL to worker group")
	}
}

// WaitUntilEOF blocks until a signal arrives or stdin hits EOF/error.
// It always attempts a graceful shutdown before returning.
func (c *linuxGroupController) WaitUntilEOF() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(sigCh)
	defer c.Shutdown()

	done := make(chan struct{})
	go func() {
		var b [1]byte
		for {
			n, err := unix.Read(unix.Stdin, b[:])
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				log.WithError(err).Warn("stdin read error")
				break
			}

			if n == 0 {
				log.Info("stdin closed; initiating shutdown")
				break
			}
		}
		close(done)
	}()

	select {
	case <-sigCh:
	case <-done:
	}
}

// WaitForChildProcess blocks until the child process exits.
// It always attempts a graceful shutdown before returning.
func (c *linuxGroupController) WaitForChildProcess() error {
	defer c.Shutdown()

	if c.childCmd == nil {
		return fmt.Errorf("no child process to wait on")
	}
	if c.childWaitStatus == nil {
		return fmt.Errorf("wait result channel not initialised")
	}

	statusCh := c.childWaitStatus

	// Drain any further child wait statuses so the reaper goroutine doesn't block
	defer func() {
		go func() {
			for range statusCh {
			}
		}()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(sigCh)

	pid := c.childCmd.Process.Pid

	log.Infof("waiting for child process %d", pid)
	for {
		select {
		case child, ok := <-statusCh:
			if !ok {
				return fmt.Errorf("wait status channel closed before child %d exited", pid)
			}
			if child.pid != pid {
				continue
			}

			status := child.status
			if status.Exited() {
				exitCode := status.ExitStatus()
				if exitCode == 0 {
					log.Infof("child process %d exited normally", pid)
					return nil
				}
				log.Infof("child process %d exited with code %d", pid, exitCode)
				return &ChildProcessExitError{Pid: pid, ExitCode: exitCode}
			}

			if status.Signaled() {
				sig := status.Signal()
				exitCode := PosixSignalExitBase + int(sig)
				log.WithField("sig", sig).Infof("child process %d exited with code %d", pid, exitCode)
				return &ChildProcessExitError{Pid: pid, ExitCode: exitCode}
			}

			return fmt.Errorf("child process %d exited with unknown status", pid)
		case sig := <-sigCh:
			log.WithField("sig", sig).Info("signal received while waiting for child; initiating shutdown")
			c.Shutdown()
		}
	}
}

// SpawnProcess starts an arbitrary command as a new session + process group
// All descendants will inherit that PGID. If cgroup is available, the
// worker group is moved into a dedicated cgroup as a best-effort attempt.
func (c *linuxGroupController) SpawnProcess(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command provided")
	}

	cmdName := args[0]
	cmdArgs := args[1:]

	// #nosec G204 -- command and arguments are supplied and validated by the caller
	c.childCmd = exec.Command(cmdName, cmdArgs...)
	c.childCmd.Stdout = os.Stdout
	c.childCmd.Stderr = os.Stderr

	sysProcAttr := &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGHUP}

	// (Optional) Linux 5.7+ CLONE_INTO_CGROUP
	// This removes the window where a process could fork before we migrate it into the cgroup.
	// See: https://lwn.net/Articles/807728/
	if c.cgMgr != nil {
		fd, err := unix.Open(c.cgMgr.Path(), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err == nil {
			sysProcAttr.UseCgroupFD = true
			sysProcAttr.CgroupFD = fd
			defer func() { _ = unix.Close(fd) }()
		} else {
			log.WithError(err).Warn("failed to open cgroup FD; will fall back to migrate-after-start")
		}
	}

	c.childCmd.SysProcAttr = sysProcAttr

	childPidFd := c.cfg.ChildPidFd
	if childPidFd > 0 {
		if _, err := unix.FcntlInt(uintptr(childPidFd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
			log.WithError(err).Warn("failed to set FD_CLOEXEC on child pid pipe")
		}
	}

	if err := c.childCmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}
	c.workerPGID.Store(int64(c.childCmd.Process.Pid))

	if childPidFd > 0 {
		if err := c.writeChildPid(childPidFd, c.childCmd.Process.Pid); err != nil {
			log.WithError(err).Warn("failed to write child pid to pipe")
		}
	}

	// (Optional) Fallback migration §path if CLONE_INTO_CGROUP wasn't used.
	// WARNING: This is racy if the worker forks quickly, but it's better than nothing.
	if c.cgMgr != nil && !sysProcAttr.UseCgroupFD {
		if err := c.cgMgr.Migrate(c.childCmd.Process.Pid); err != nil {
			log.WithError(err).Warn("failed to migrate worker process group into cgroup; continuing without cgroup")
		} else {
			log.WithField("pgid", c.childCmd.Process.Pid).WithField("path", c.cgMgr.Path()).
				Info("migrated worker process group into cgroup (fallback)")
		}
	}

	return nil
}

func (c *linuxGroupController) writeChildPid(fd int, pid int) error {
	log.Infof("writing child pid %d to fd %d", pid, fd)

	f := os.NewFile(uintptr(fd), "child-pid-pipe")
	if f == nil {
		return fmt.Errorf("failed to create new file with fd %d", fd)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
		return fmt.Errorf("failed to write pid %d: %w", pid, err)
	}

	return nil
}

func (c *linuxGroupController) waitGroupExit(pgid int) bool {
	deadline := time.Now().Add(c.cfg.GraceTimeout)
	for time.Now().Before(deadline) {
		if err := unix.Kill(-pgid, 0); err == unix.ESRCH {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func (cg *cgroupManagerImpl) waitEmpty(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	events := filepath.Join(cg.path, "cgroup.events")
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(events)
		if err != nil {
			break
		}
		if bytes.Contains(b, []byte("populated 0")) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
