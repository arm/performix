// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/privilege"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

// AgentProcessHandle manages a remote process via the target agent.
// It supports lifecycle operations (kill, interrupt, wait) and I/O streams.
type AgentProcessHandle struct {
	ctx              context.Context
	pid              int32
	client           targetagentproto.TargetAgentClient
	stderrPipeReader *io.PipeReader
	stdoutPipeReader *io.PipeReader

	// Privilege
	asPrivileged     bool
	privilegeSession privilege.PrivilegeSession
}

// pipeStream continuously reads from a gRPC process stream (stdout or stderr)
// and writes data into the provided PipeWriter. When the stream ends (EOF) or
// encounters an error, the pipe is closed accordingly.
func pipeStream(ctx context.Context, streamName string, stream targetagentproto.TargetAgent_StreamStdoutClient, pw *io.PipeWriter) {
	for {
		stdinResp, err := stream.Recv()
		if err == io.EOF {
			pw.Close()
			break
		} else if err != nil {
			logx.FromContext(ctx).WithFields(logrus.Fields{"err": err, "stream": streamName}).Errorf("stream receive error")
			pw.CloseWithError(classifyStreamReceiveError(ctx, err))
			return
		}

		if _, err := pw.Write([]byte(stdinResp.GetData())); err != nil {
			pw.Close()
			logx.FromContext(ctx).WithFields(logrus.Fields{"err": err, "stream": streamName}).Errorf("pipe writer error")
			return
		}
	}
}

// classifyStreamReceiveError checks and classifies user cancellation, preserves
// messages returned by the agent, identifies transport failures, and wraps
// remaining errors as process stream failures.
func classifyStreamReceiveError(ctx context.Context, err error) error {
	if errors.Is(context.Cause(ctx), context.Canceled) {
		return message.New(message.EngineCommonUserCanceled).WithCause(err)
	}

	grpcStatus, isGRPCStatus := status.FromError(err)
	if isGRPCStatus {
		decodedErr := message.FromGRPCStatus(err)
		if message.IsMessage(decodedErr) != nil {
			return decodedErr
		}
	}

	if errors.Is(context.Cause(ctx), agent.ErrAgentDisconnected) ||
		(isGRPCStatus && grpcStatus.Code() == codes.Unavailable) {
		return message.New(message.EngineAgentConnectionTransportError).WithCause(err)
	}

	return message.Wrap(message.EngineAgentProcessStreamReceiveFailed, err)
}

// NewAgentProcessHandle creates a handle for a process with optional stdout/stderr streaming.
func NewAgentProcessHandle(
	ctx context.Context,
	pid int32,
	client targetagentproto.TargetAgentClient,
	stdout process.StreamRedirect,
	stderr process.StreamRedirect,
	asPrivileged bool,
	privilegeSession privilege.PrivilegeSession) (*AgentProcessHandle, error) {

	streamCtx, cancelStreams := context.WithCancel(ctx)
	var stdoutStream targetagentproto.TargetAgent_StreamStdoutClient
	var stderrStream targetagentproto.TargetAgent_StreamStderrClient
	openStream := func(name string) error {
		invoke := func(callCtx context.Context) error {
			request := &targetagentproto.ProcessStreamRequest{Pid: pid}
			var err error
			if name == "StreamStdout" {
				stdoutStream, err = client.StreamStdout(callCtx, request)
			} else {
				stderrStream, err = client.StreamStderr(callCtx, request)
			}
			return err
		}
		if asPrivileged {
			return privilegeSession.Invoke(streamCtx, name, invoke)
		}
		return invoke(streamCtx)
	}
	if process.IsStreamModeEnabled(stdout.Mode) {
		if err := openStream("StreamStdout"); err != nil {
			cancelStreams()
			return nil, err
		}
	}
	if process.IsStreamModeEnabled(stderr.Mode) {
		if err := openStream("StreamStderr"); err != nil {
			cancelStreams()
			return nil, err
		}
	}

	// Do not start pipe writers until every stream is established. On setup
	// failure there is no handle whose caller could drain those pipes.
	var stdoutPipeReader, stderrPipeReader *io.PipeReader
	var drains sync.WaitGroup
	startDrain := func(name string, stream targetagentproto.TargetAgent_StreamStdoutClient) *io.PipeReader {
		reader, writer := io.Pipe()
		drains.Add(1)
		go func() {
			defer drains.Done()
			pipeStream(streamCtx, name, stream, writer)
		}()
		return reader
	}
	if process.IsStreamModeEnabled(stdout.Mode) {
		stdoutPipeReader = startDrain("stdout", stdoutStream)
	}
	if process.IsStreamModeEnabled(stderr.Mode) {
		stderrPipeReader = startDrain("stderr", stderrStream)
	}
	go func() {
		drains.Wait()
		cancelStreams()
	}()

	return &AgentProcessHandle{
		ctx:              ctx,
		pid:              pid,
		client:           client,
		stderrPipeReader: stderrPipeReader,
		stdoutPipeReader: stdoutPipeReader,
		asPrivileged:     asPrivileged,
		privilegeSession: privilegeSession,
	}, nil
}

// PID returns the process ID of the target process
func (p *AgentProcessHandle) PID() int {
	return int(p.pid)
}

const processCleanupTimeout = 5 * time.Second

// Kill sends a process kill request even after the operation has been canceled.
func (p *AgentProcessHandle) Kill() error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(p.ctx), processCleanupTimeout)
	defer cancel()
	return killAgentProcess(ctx, p.pid, p.client, p.asPrivileged, p.privilegeSession)
}

func killAgentProcess(ctx context.Context, pid int32, client targetagentproto.TargetAgentClient, asPrivileged bool, session privilege.PrivilegeSession) error {
	var err error

	if asPrivileged {
		// Privilege path
		err = session.Invoke(ctx, "KillProcess", func(ctx context.Context) error {
			_, err = client.KillProcess(ctx, &targetagentproto.KillProcessRequest{
				Pid: pid,
			})
			return err
		})
	} else {
		// Non-privilege path
		_, err = client.KillProcess(ctx, &targetagentproto.KillProcessRequest{
			Pid: pid,
		})
	}
	return err
}

// Interrupt sends an interrupt signal to the target process
func (p *AgentProcessHandle) Interrupt() error {
	var err error

	if p.asPrivileged {
		// Privilege path
		err = p.privilegeSession.Invoke(p.ctx, "InterruptProcess", func(privCtx context.Context) error {
			_, err = p.client.InterruptProcess(privCtx, &targetagentproto.InterruptProcessRequest{
				Pid: p.pid,
			})
			return err
		})
	} else {
		// Non-privilege path
		_, err = p.client.InterruptProcess(p.ctx, &targetagentproto.InterruptProcessRequest{
			Pid: p.pid,
		})
	}
	return err
}

// Wait blocks until the process exits and returns its exit code.
func (p *AgentProcessHandle) Wait() (int, error) {
	var resp *targetagentproto.WaitProcessResponse
	var err error

	if p.asPrivileged {
		// Privilege path
		err = p.privilegeSession.Invoke(p.ctx, "WaitProcess", func(privCtx context.Context) error {
			var err error
			resp, err = p.client.WaitProcess(privCtx, &targetagentproto.WaitProcessRequest{Pid: p.pid})
			return err
		})
	} else {
		// Non-privilege path
		resp, err = p.client.WaitProcess(p.ctx, &targetagentproto.WaitProcessRequest{Pid: p.pid})
	}

	if resp != nil {
		return int(resp.ExitCode), err
	}
	return 0, err
}

// WriteStdin writes data to stdin.
func (p *AgentProcessHandle) WriteStdin(data string) error {
	var err error

	if p.asPrivileged {
		// Privilege path
		err = p.privilegeSession.Invoke(p.ctx, "WriteToStdin", func(privCtx context.Context) error {
			var err error
			_, err = p.client.WriteToStdin(privCtx, &targetagentproto.StdinChunk{
				Pid:  p.pid,
				Data: []byte(data),
			})
			return err
		})
	} else {
		// Non-privilege path
		_, err = p.client.WriteToStdin(p.ctx, &targetagentproto.StdinChunk{
			Pid:  p.pid,
			Data: []byte(data),
		})
	}
	return err
}

func (p *AgentProcessHandle) Stdout() io.Reader { return p.stdoutPipeReader }
func (p *AgentProcessHandle) Stderr() io.Reader { return p.stderrPipeReader }
