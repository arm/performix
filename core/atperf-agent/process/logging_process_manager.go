// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"context"
	"os"
	"os/exec"

	"github.com/sirupsen/logrus"
)

// LoggingProcessManager wraps another ProcessManager and logs every call.
type LoggingProcessManager struct {
	Delegate ProcessManager
	Logger   *logrus.Entry
}

// NewLoggingProcessManager returns a LoggingProcessManager that logs to the specified logger.
// If logger is nil, it defaults to the standard logger.
func NewLoggingProcessManager(inner ProcessManager, logger *logrus.Logger) *LoggingProcessManager {
	entry := logrus.NewEntry(logrus.StandardLogger())
	if logger != nil {
		entry = logrus.NewEntry(logger)
	}
	return &LoggingProcessManager{
		Delegate: inner,
		Logger:   entry,
	}
}

func (l *LoggingProcessManager) InterruptProcess(pid int) error {
	l.Logger.WithField("pid", pid).Info("InterruptProcess called")
	err := l.Delegate.InterruptProcess(pid)
	if err != nil {
		l.Logger.WithError(err).WithField("pid", pid).Error("InterruptProcess failed")
	} else {
		l.Logger.WithField("pid", pid).Info("InterruptProcess succeeded")
	}
	return err
}

func (l *LoggingProcessManager) StartProcess(cmd *StartProcess) (*os.Process, error) {
	l.Logger.WithField("cmd", cmd).Info("StartProcess called")
	proc, err := l.Delegate.StartProcess(cmd)
	if err != nil {
		l.Logger.WithError(err).WithField("cmd", cmd).Error("StartProcess failed")
	} else {
		l.Logger.WithFields(logrus.Fields{
			"cmd": cmd,
			"pid": proc.Pid,
		}).Info("StartProcess succeeded")
	}
	return proc, err
}

func (l *LoggingProcessManager) ReleaseProcessHandles(pids []int) {
	l.Logger.WithField("pids", pids).Info("ReleaseProcessHandles called")
	l.Delegate.ReleaseProcessHandles(pids)
	l.Logger.WithField("pids", pids).Info("ReleaseProcessHandles completed")
}

func (l *LoggingProcessManager) ExecCommand(cmd *LaunchCommand) (*CommandResult, error) {
	l.Logger.WithField("cmd", cmd).Info("ExecCommand called")
	result, err := l.Delegate.ExecCommand(cmd)
	if err != nil {
		l.Logger.
			WithError(err).
			WithField("cmd", cmd).
			Error("ExecCommand failed")
	} else {
		l.Logger.
			WithField("cmd", cmd).
			WithField("exitCode", result.Rc).
			Info("ExecCommand succeeded")
	}
	return result, err
}

func (l *LoggingProcessManager) KillProcess(pid int) error {
	l.Logger.WithField("pid", pid).Info("KillProcess called")
	err := l.Delegate.KillProcess(pid)
	if err != nil {
		l.Logger.WithError(err).WithField("pid", pid).Error("KillProcess failed")
	} else {
		l.Logger.WithField("pid", pid).Info("KillProcess succeeded")
	}
	return err
}

func (l *LoggingProcessManager) WaitProcess(ctx context.Context, pid int) (int, error) {
	l.Logger.WithField("pid", pid).Info("WaitProcess called")
	ec, err := l.Delegate.WaitProcess(ctx, pid)
	if err != nil {
		l.Logger.WithError(err).WithField("pid", pid).Error("WaitProcess failed")
	} else {
		l.Logger.WithField("pid", pid).WithField("exitCode", ec).Info("WaitProcess succeeded")
	}
	return ec, err
}

func (l *LoggingProcessManager) StreamStdout(pid int, stream StreamChunkSender) error {
	l.Logger.WithField("pid", pid).Info("StreamStdout called")
	err := l.Delegate.StreamStdout(pid, stream)
	if err != nil {
		l.Logger.WithError(err).WithField("pid", pid).Error("StreamStdout failed")
	} else {
		l.Logger.WithField("pid", pid).Info("StreamStdout succeeded")
	}
	return err
}

func (l *LoggingProcessManager) StreamStderr(pid int, stream StreamChunkSender) error {
	l.Logger.WithField("pid", pid).Info("StreamStderr called")
	err := l.Delegate.StreamStderr(pid, stream)
	if err != nil {
		l.Logger.WithError(err).WithField("pid", pid).Error("StreamStderr failed")
	} else {
		l.Logger.WithField("pid", pid).Info("StreamStderr succeeded")
	}
	return err
}

func (l *LoggingProcessManager) WriteToStdin(pid int, data []byte) error {
	l.Logger.WithField("pid", pid).WithField("data", data).Info("WriteToStdin called")
	err := l.Delegate.WriteToStdin(pid, data)
	if err != nil {
		l.Logger.WithError(err).WithField("pid", pid).Error("WriteToStdin failed")
	} else {
		l.Logger.WithField("pid", pid).Info("WriteToStdin succeeded")
	}
	return err
}

func (l *LoggingProcessManager) Shutdown(force bool) error {
	l.Logger.WithField("force", force).Info("Shutdown called")
	err := l.Delegate.Shutdown(force)
	if err != nil {
		l.Logger.WithError(err).WithField("force", force).Error("Shutdown failed")
	} else {
		l.Logger.WithField("force", force).Info("Shutdown succeeded")
	}
	return err
}

func (l *LoggingProcessManager) buildCmd(lc *LaunchCommand) (*exec.Cmd, error) {
	return l.Delegate.buildCmd(lc)
}

func (l *LoggingProcessManager) setCPUAffinityAfterStart(pid int, affinity []string) error {
	return l.Delegate.setCPUAffinityAfterStart(pid, affinity)
}
