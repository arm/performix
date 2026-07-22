// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"io"
)

const contextCopyBufferSize = 32 * 1024

// CopyWithContext copies from src to dst while periodically checking ctx.
func CopyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (written int64, err error) {
	if err := ctx.Err(); err != nil {
		return written, err
	}

	buf := make([]byte, contextCopyBufferSize)
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}

		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return written, nil
			}
			return written, er
		}
	}
}
