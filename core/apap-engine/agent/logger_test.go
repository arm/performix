// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mocks "github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	protomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func newLogger() (*log.Logger, *logtest.Hook) {
	l := log.New()
	l.SetLevel(log.DebugLevel)
	return l, logtest.NewLocal(l)
}

func TestAgentLogger_Start_ClientNil(t *testing.T) {
	logger, _ := newLogger()

	al := newAgentLogger(nil, logger, "target_agent")
	err := al.Start()

	var msg message.Message
	assert.True(t, errors.As(err, &msg))
	assert.Equal(t, message.CommonUnknownError, msg.Code())
	assert.Equal(t, fmt.Errorf("client is not initialized"), msg.Unwrap())
}

func TestAgentLogger_Start_StartupError(t *testing.T) {
	logger, _ := newLogger()

	client := protomocks.NewTargetAgentClient(t)
	client.On("StreamLogs", mock.Anything, mock.Anything).Return(nil, errors.New("boom"))

	al := newAgentLogger(client, logger, "target_agent")
	err := al.Start()

	var msg message.Message
	assert.True(t, errors.As(err, &msg))
	assert.Equal(t, message.CommonUnknownError, msg.Code())
	assert.ErrorContains(t, msg.Unwrap(), "failed to start agent log stream")
}

func TestRunStream_NilEntryAndCancelError(t *testing.T) {
	logger, hook := newLogger()

	al := &agentLogger{id: "target_agent", logger: logger}
	stream := &mocks.StubLogStream{
		Entries: []any{nil},
		Err:     context.Canceled,
	}

	err := al.runStream(stream)
	require.NoError(t, err)

	entries := hook.AllEntries()
	require.NotEmpty(t, entries)
	assert.Equal(t, log.WarnLevel, entries[0].Level)
	assert.Contains(t, strings.ToLower(entries[0].Message), "received nil entry")
	assert.Equal(t, log.InfoLevel, hook.LastEntry().Level)
	assert.Contains(t, hook.LastEntry().Message, "agent log stream closed")
}

func TestRunStream_NonCancelError(t *testing.T) {
	logger, hook := newLogger()

	al := &agentLogger{id: "target_agent", logger: logger}
	stream := &mocks.StubLogStream{Err: errors.New("recv fail")}

	err := al.runStream(stream)
	require.Error(t, err)

	require.NotEmpty(t, hook.AllEntries())
	entry := hook.AllEntries()[0]
	assert.Equal(t, log.ErrorLevel, entry.Level)
	assert.Contains(t, entry.Message, "agent log stream closed unexpectedly")
}

func TestRunStream_HappyPath(t *testing.T) {
	logger, hook := newLogger()

	al := &agentLogger{id: "target_agent", logger: logger}
	stream := &mocks.StubLogStream{
		Entries: []any{
			&targetagentproto.LogEntry{Level: "info", Message: "hello"},
			&targetagentproto.LogEntry{Level: "warn", Message: "world"},
		},
		Err: context.Canceled,
	}

	err := al.runStream(stream)
	require.NoError(t, err)

	// Check logs have correct prefix id field
	found := false
	for _, e := range hook.AllEntries() {
		if e.Data[logFieldID] == "target_agent" &&
			(e.Message == "[Agent] hello" || e.Message == "[Agent] world") {
			found = true
			break
		}
	}
	assert.True(t, found)
}
