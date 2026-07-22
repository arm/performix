// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"io"

	"github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/privilege"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
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
			pw.CloseWithError(err)
			return
		}

		if _, err := pw.Write([]byte(stdinResp.GetData())); err != nil {
			pw.Close()
			logx.FromContext(ctx).WithFields(logrus.Fields{"err": err, "stream": streamName}).Errorf("pipe writer error")
			return
		}
	}
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

	var stdoutPipeReader, stderrPipeReader *io.PipeReader
	var stdoutPipeWriter, stderrPipeWriter *io.PipeWriter

	if process.IsStreamModeEnabled(stdout.Mode) {
		var stdOutStream targetagentproto.TargetAgent_StreamStdoutClient
		req := &targetagentproto.ProcessStreamRequest{Pid: pid}
		stdoutPipeReader, stdoutPipeWriter = io.Pipe()

		if asPrivileged {
			// Privilege path
			err := privilegeSession.Invoke(ctx, "StreamStdout", func(privCtx context.Context) error {
				var err error
				stdOutStream, err = client.StreamStdout(privCtx, req)
				return err
			})

			if err != nil {
				return nil, err
			}
		} else {
			// Non-privilege path
			var err error
			stdOutStream, err = client.StreamStdout(ctx, req)
			if err != nil {
				return nil, err
			}
		}
		go pipeStream(ctx, "stdout", stdOutStream, stdoutPipeWriter)
	}

	if process.IsStreamModeEnabled(stderr.Mode) {
		var stdErrStream targetagentproto.TargetAgent_StreamStderrClient
		req := &targetagentproto.ProcessStreamRequest{Pid: pid}
		stderrPipeReader, stderrPipeWriter = io.Pipe()

		if asPrivileged {
			// Privilege path
			err := privilegeSession.Invoke(ctx, "StreamStderr", func(privCtx context.Context) error {
				var err error
				stdErrStream, err = client.StreamStderr(privCtx, req)
				return err
			})

			if err != nil {
				return nil, err
			}
		} else {
			// Non-privilege path
			var err error
			stdErrStream, err = client.StreamStderr(ctx, req)
			if err != nil {
				return nil, err
			}
		}
		go pipeStream(ctx, "stderr", stdErrStream, stderrPipeWriter)
	}

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

// Kill sends a process kill request to the target process
func (p *AgentProcessHandle) Kill() error {
	var err error

	if p.asPrivileged {
		// Privilege path
		err = p.privilegeSession.Invoke(p.ctx, "KillProcess", func(ctx context.Context) error {
			_, err = p.client.KillProcess(ctx, &targetagentproto.KillProcessRequest{
				Pid: p.pid,
			})
			return err
		})
	} else {
		// Non-privilege path
		_, err = p.client.KillProcess(p.ctx, &targetagentproto.KillProcessRequest{
			Pid: p.pid,
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
