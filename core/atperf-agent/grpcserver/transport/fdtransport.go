// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package transport

import (
	"fmt"
	"os"
	"sync/atomic"
)

type fdTransport struct {
	read      *os.File
	write     *os.File
	hasClosed atomic.Bool
}

func NewFDTransport(readFD, writeFD uintptr) (Transport, error) {
	read := os.NewFile(readFD, fmt.Sprintf("fd-%d-read", readFD))
	write := os.NewFile(writeFD, fmt.Sprintf("fd-%d-write", writeFD))

	if read == nil || write == nil {
		return nil, fmt.Errorf("could not open file descriptors")
	}

	return &fdTransport{
		read:  read,
		write: write,
	}, nil
}

func (t *fdTransport) Read(p []byte) (int, error)  { return t.read.Read(p) }
func (t *fdTransport) Write(p []byte) (int, error) { return t.write.Write(p) }
func (t *fdTransport) Close() error {
	if t.hasClosed.Swap(true) {
		return nil // already closed
	}

	err1 := t.read.Close()
	err2 := t.write.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func (t *fdTransport) Name() string {
	return fmt.Sprintf("fdTransport(readFD=%s, writeFD=%s)", t.read.Name(), t.write.Name())
}
