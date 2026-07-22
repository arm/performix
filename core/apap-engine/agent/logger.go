// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"fmt"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/conversion"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

const logFieldID = "target"

type AgentLogger interface {
	Start() error
	Close()
}

type agentLogger struct {
	client targetagentproto.TargetAgentClient
	cancel context.CancelFunc
	id     string
	logger log.FieldLogger
}

func newAgentLogger(client targetagentproto.TargetAgentClient, logger log.FieldLogger, id string) AgentLogger {
	return &agentLogger{
		client: client,
		id:     id,
		logger: logger,
	}
}

// Start opens a server-streaming RPC to the agent and receives log entries.
// The log entries are included in the engine logs with the agent ID as a field.
func (a *agentLogger) Start() error {
	if a.client == nil {
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("client is not initialized"))
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := a.client.StreamLogs(ctx, &emptypb.Empty{})
	if err != nil {
		cancel()
		log.Errorf("failed to start agent log stream: %v", err)
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("failed to start agent log stream: %w", err))
	}

	a.cancel = cancel

	go func() {
		_ = a.runStream(stream)
	}()

	return nil
}

// runStream runs the receive loop for the log stream.
func (a *agentLogger) runStream(stream targetagentproto.TargetAgent_StreamLogsClient) error {
	defer a.Close()

	baseLog := a.logger.WithField(logFieldID, a.id)

	for {
		entry, err := stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled || errors.Is(err, context.Canceled) {
				baseLog.Info("agent log stream closed")
				return nil
			}
			baseLog.WithError(err).Error("agent log stream closed unexpectedly")
			return err
		}
		if entry == nil {
			baseLog.Warn("received nil entry from agent log stream")
			continue
		}

		agentLog := a.logger.WithField(logFieldID, a.id)
		level, msg := conversion.LogEntryFromProto(entry, agentLog)
		agentLog.Log(level, "[Agent] "+msg)
	}
}

// Close cancels the log stream.
func (a *agentLogger) Close() {
	if a.cancel != nil {
		a.cancel()
	}
}
