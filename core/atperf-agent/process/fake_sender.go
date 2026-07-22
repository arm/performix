// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import "context"

// fakeSender implements StreamChunkSender by collecting chunks.
type fakeSender struct {
	ctx    context.Context
	chunks [][]byte
}

func (f *fakeSender) Context() context.Context {
	return f.ctx
}

func (f *fakeSender) Send(chunk *StreamChunk) error {
	// Copy data
	data := make([]byte, len(chunk.Data))
	copy(data, chunk.Data)
	f.chunks = append(f.chunks, data)
	return nil
}
