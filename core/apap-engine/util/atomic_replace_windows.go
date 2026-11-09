// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package util

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const atomicReplaceAttempts = 6

// AtomicReplaceFile replaces dst with src, retrying transient Windows errors.
func AtomicReplaceFile(src, dst string) error {
	var replaceErr error
	// Windows can briefly reject replacement if another process, antivirus, or
	// indexer has the destination open. Retry only those transient sharing/lock
	// errors; other failures are returned immediately.
	for attempt := 0; attempt < atomicReplaceAttempts; attempt++ {
		// os.Rename handles Windows paths that need an extended-length prefix.
		replaceErr = os.Rename(src, dst)
		if replaceErr == nil {
			return nil
		}
		if !isTransientWindowsReplaceError(replaceErr) {
			return replaceErr
		}
		if attempt == atomicReplaceAttempts-1 {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	return replaceErr
}

func isTransientWindowsReplaceError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
