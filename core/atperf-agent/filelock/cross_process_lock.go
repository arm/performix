// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

// CrossProcessLock coordinates in-process and cross-process lock acquisition.
//
// CrossProcessLock must be constructed with NewCrossProcessLock. Its zero value
// is not ready for use because the Flock's file path is required.
type CrossProcessLock struct {
	inProcLoc      sync.Mutex
	fileLock       *flock.Flock
	lockRetryDelay time.Duration
}

type LockPathError struct {
	Path string
	Err  error
}

func (e *LockPathError) Error() string {
	return fmt.Sprintf("failed to prepare lock path %q: %v", e.Path, e.Err)
}

func (e *LockPathError) Unwrap() error {
	return e.Err
}

func NewCrossProcessLock(fileLockPath string, lockRetryDelay time.Duration) (*CrossProcessLock, error) {
	if err := ensureLockPath(fileLockPath); err != nil {
		return nil, err
	}

	return &CrossProcessLock{
		fileLock:       flock.New(fileLockPath),
		lockRetryDelay: lockRetryDelay,
	}, nil
}

func ensureLockFile(fileLockPath string) error {
	// Create with desired permissions
	f, err := os.OpenFile(fileLockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0660)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return &LockPathError{Path: fileLockPath, Err: fmt.Errorf("create lock file: %w", err)}
		}
		// File already exists, which is expected if not the first agent invocation. All good!
	} else {
		var pathErr error
		if runtime.GOOS != "windows" {
			// On Unix, ensure the file is world writable so that any user can acquire the lock.
			// user umask may interfere with the permissions set on file creation, so we explicitly set them here.
			if err := f.Chmod(0666); err != nil {
				pathErr = errors.Join(pathErr, fmt.Errorf("set lock file permissions: %w", err))
			}
		}
		if err := f.Close(); err != nil {
			pathErr = errors.Join(pathErr, fmt.Errorf("close lock file: %w", err))
		}
		if pathErr != nil {
			return &LockPathError{Path: fileLockPath, Err: pathErr}
		}
	}

	return nil
}

func ensureLockPath(fileLockPath string) error {
	lockDir := filepath.Dir(fileLockPath)
	info, err := os.Stat(lockDir)
	if err == nil {
		if !info.IsDir() {
			return &LockPathError{Path: lockDir, Err: errors.New("validate lock directory: path is not a directory")}
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(lockDir, perms.TargetDirPerm); err != nil {
			return &LockPathError{Path: lockDir, Err: fmt.Errorf("create lock directory: %w", err)}
		}
		if runtime.GOOS != "windows" {
			// Dir may not be created with correct permissions due to umasks - manually chmod after
			// creation to address this
			if err := os.Chmod(lockDir, perms.TargetDirPerm); err != nil {
				return &LockPathError{Path: lockDir, Err: fmt.Errorf("set lock directory permissions: %w", err)}
			}
		}
	} else {
		return &LockPathError{Path: lockDir, Err: fmt.Errorf("stat lock directory: %w", err)}
	}

	return ensureLockFile(fileLockPath)
}

// HoldLock acquires an in-process lock AND a cross-process file lock,
// calls onLockGranted(), then holds both until ctx is done.
func (cpl *CrossProcessLock) HoldLock(
	ctx context.Context,
	onLockGranted func(),
) error {

	for !cpl.inProcLoc.TryLock() {

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cpl.lockRetryDelay):
		}
	}
	defer cpl.inProcLoc.Unlock()

	if err := ensureLockPath(cpl.fileLock.Path()); err != nil {
		return err
	}

	// File lock next to guard against processes lock next
	ok, err := cpl.fileLock.TryLockContext(ctx, cpl.lockRetryDelay)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		// !ok with no err typically means ctx expired/cancelled.
		return ctx.Err()
	}

	defer func() { _ = cpl.fileLock.Unlock() }()
	onLockGranted()

	// Hold until cancellation
	<-ctx.Done()
	return nil
}
