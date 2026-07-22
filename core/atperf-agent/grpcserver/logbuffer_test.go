// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestLogBuffer_BasicSendReceive(t *testing.T) {
	buf := NewLogBuffer(2)
	entry := &targetagentproto.LogEntry{Message: "test"}
	select {
	case buf.ch <- entry:
	case <-time.After(time.Second):
		t.Fatal("timeout sending to buffer")
	}
	select {
	case got := <-buf.Channel():
		assert.Equal(t, entry, got)
	case <-time.After(time.Second):
		t.Fatal("timeout receiving from buffer")
	}
}

func TestLogBuffer_Close(t *testing.T) {
	buf := NewLogBuffer(1)
	buf.Close()
	_, ok := <-buf.Channel()
	assert.False(t, ok, "channel should be closed")
}

func TestLogBufferHook_Fire_DropsOldest(t *testing.T) {
	buf := NewLogBuffer(2)
	hook := &LogBufferHook{Buffer: buf}

	// Fill buffer
	for i := 0; i < 2; i++ {
		entry := &logrus.Entry{
			Level:   logrus.InfoLevel,
			Message: "msg",
			Time:    time.Now(),
			Data:    logrus.Fields{"i": i},
		}
		assert.NoError(t, hook.Fire(entry))
	}

	// Add one more, should drop the oldest
	entryNew := &logrus.Entry{
		Level:   logrus.WarnLevel,
		Message: "newest",
		Time:    time.Now(),
	}
	assert.NoError(t, hook.Fire(entryNew))

	// Only the last two should remain
	got := []*targetagentproto.LogEntry{<-buf.Channel(), <-buf.Channel()}
	assert.Len(t, got, 2)
	assert.Equal(t, "msg", got[0].Message)
	assert.Equal(t, "newest", got[1].Message)
}

func TestLogBufferHook_Fire_NonBlocking(t *testing.T) {
	buf := NewLogBuffer(1)
	hook := &LogBufferHook{Buffer: buf}

	// Fill buffer
	entry := &logrus.Entry{
		Level:   logrus.InfoLevel,
		Message: "first",
		Time:    time.Now(),
	}
	assert.NoError(t, hook.Fire(entry))

	// Fire again, should not block
	entry2 := &logrus.Entry{
		Level:   logrus.InfoLevel,
		Message: "second",
		Time:    time.Now(),
	}
	done := make(chan struct{})
	go func() {
		_ = hook.Fire(entry2)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Fire blocked, should be non-blocking")
	}
}
