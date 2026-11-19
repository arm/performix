// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"errors"

	"golang.org/x/sys/windows"
)

func platformFailureReason(err error) FailureReason {
	switch {
	case errors.Is(err, windows.ERROR_BAD_EXE_FORMAT):
		return FailureExecutableFormat
	case errors.Is(err, windows.ERROR_DIRECTORY):
		return FailureNotDirectory
	default:
		return FailureUnknown
	}
}
