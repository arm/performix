// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"errors"
	"io"
)

// fanoutWriter writes each record to every destination, even if an earlier
// destination fails. The returned byte count is the number written by every
// destination.
type fanoutWriter []io.Writer

func (w fanoutWriter) Write(p []byte) (int, error) {
	written := len(p)
	var errs error
	for _, destination := range w {
		n, err := destination.Write(p)
		written = min(written, n)
		if err == nil && n != len(p) {
			err = io.ErrShortWrite
		}
		errs = errors.Join(errs, err)
	}
	return written, errs
}
