// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"errors"

	"golang.org/x/sys/windows"
)

// A sharing violation means another open handle prevents deletion. Windows can
// report it briefly while terminated descendants release their batch-file handles.
// Keep this check in a Windows file: a runtime GOOS check cannot make the
// golang.org/x/sys/windows import compile on other platforms.
func isSharingViolation(err error) bool { return errors.Is(err, windows.ERROR_SHARING_VIOLATION) }
