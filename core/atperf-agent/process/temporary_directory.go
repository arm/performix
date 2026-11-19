// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	temporaryDirectoryCleanupTimeout = 2 * time.Second
	temporaryDirectoryRetryInterval  = 10 * time.Millisecond
)

// TemporaryDirectory requests a private directory on the execution host. Its path
// is passed through EnvironmentVariable and its contents are removed after exit
// or cancellation. An empty ParentDirectory uses the host's temporary directory.
type TemporaryDirectory struct {
	EnvironmentVariable string
	ParentDirectory     string
}

func (l *launchCommand) prepareTemporaryDirectory() error {
	if l.temporaryDirectory == nil {
		return nil
	}
	parent := l.temporaryDirectory.ParentDirectory
	if parent != "" {
		absolute, err := filepath.Abs(parent)
		if err != nil {
			return err
		}
		parent = absolute
	}
	directory, err := os.MkdirTemp(parent, "apx-script-")
	if err != nil {
		return err
	}
	l.temporaryPath = directory
	environment := l.command.Environ()
	name := l.temporaryDirectory.EnvironmentVariable
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if key == name || (runtime.GOOS == "windows" && strings.EqualFold(key, name)) {
			continue
		}
		filtered = append(filtered, entry)
	}
	l.command.Env = append(filtered, name+"="+directory)
	return nil
}

func (l *launchCommand) removeTemporaryDirectory() error {
	if l.temporaryPath == "" {
		return nil
	}
	// Windows job termination is asynchronous. A descendant can briefly retain a
	// batch-file handle after its parent exits, so retry sharing/access failures.
	// Process exit does not signal that every file handle is released, and these
	// APIs provide no file-unlocked event. Stop on success; bound retries so a
	// permanent lock is reported to the caller rather than blocking indefinitely.
	deadline := time.Now().Add(temporaryDirectoryCleanupTimeout)
	for {
		err := os.RemoveAll(l.temporaryPath)
		if err == nil {
			l.temporaryPath = ""
			return nil
		}
		if runtime.GOOS != "windows" || !temporaryDirectoryBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(temporaryDirectoryRetryInterval)
	}
}

func temporaryDirectoryBusy(err error) bool {
	return errors.Is(err, os.ErrPermission) || isSharingViolation(err)
}
