// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package transport

import "errors"

// NewFDListener creates an fdListener from an fdTransport
func NewFDListener(readFD, writeFD uintptr, loggingEnabled bool) (Listener, error) {
	return nil, errors.New("FDListener is not supported on Windows")
}
