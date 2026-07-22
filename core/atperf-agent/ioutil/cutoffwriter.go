// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ioutil

import (
	"bytes"
	"io"
	"sync"
)

// CutoffWriter is a thread-safe writer that buffers data until Cutoff() is called.
// After Cutoff() is called, Write operations will return an error.
// The buffered data can be retrieved as a string using the String() method.
// This is useful for capturing logs or other output that should be finalized
// at a certain point, such as when a server is shutting down.
type CutoffWriter struct {
	mu     sync.Mutex
	buf    *bytes.Buffer
	closed bool
}

func NewCutoffWriter() *CutoffWriter {
	return &CutoffWriter{
		buf: &bytes.Buffer{},
	}
}

func (w *CutoffWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, io.ErrClosedPipe
	}
	return w.buf.Write(p)
}

func (w *CutoffWriter) Cutoff() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
}

func (w *CutoffWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
