// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/conversion"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type LogBuffer struct {
	ch   chan *targetagentproto.LogEntry
	once sync.Once
}

func NewLogBuffer(size int) *LogBuffer {
	return &LogBuffer{
		ch: make(chan *targetagentproto.LogEntry, size),
	}
}

func (b *LogBuffer) Channel() <-chan *targetagentproto.LogEntry {
	return b.ch
}

func (b *LogBuffer) Close() {
	b.once.Do(func() { close(b.ch) })
}

// LogBufferHook is a Logrus hook to push logs into the buffer
type LogBufferHook struct {
	Buffer *LogBuffer
}

func (h *LogBufferHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *LogBufferHook) Fire(entry *logrus.Entry) error {
	logEntry := conversion.ProtoFromLogEntry(entry)
	for {
		select {
		case h.Buffer.ch <- logEntry:
			// Successfully sent
			return nil
		default:
			// Buffer full, remove oldest and try again
			select {
			case <-h.Buffer.ch:
				// Oldest dropped
			default:
				// Should not happen, but break to avoid infinite loop
				return nil
			}
		}
	}
}
