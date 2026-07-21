// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"io"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// FileHandle represents a handle to a file opened by the engine.
type FileHandle interface {
	Append(data string) error
	Close() error
	Path() string
}

// ProcessHandle wraps a process on the target started
// through the target agent
type ProcessHandle interface {
	PID() int
	Kill() error
	Interrupt() error
	Wait() (exitCode int, err error)
	Stdout() io.Reader
	Stderr() io.Reader
	WriteStdin(data string) error
}

// Engine defines the interface exposed to a tool integration.
// It provides an interface to the target agent process manager
// and directory system as well as methods for controlling
// stage progress.
type Engine interface {
	ExecCommand(opts *process.LaunchCommand) (*process.CommandResult, error)
	StartProcess(opts *process.StartProcess) (ProcessHandle, error)
	CreateTempDir() (string, error)
	Mkdir(path string) error
	Rm(path string, recursive, force bool) error
	MakeWritable(path string, recursive bool) error
	Chown(path, owner string, recursive bool) error
	Log(level string, message string)
	WriteUserMessage(level string, message string)
	CreateRunFile(path string) (FileHandle, error)
	ReadHostFile(path string) (string, error)
	StartProgressTracker(id string) error
	UpdateProgress(id, message string, percent float64) error
	EndProgress(id string) error
}
