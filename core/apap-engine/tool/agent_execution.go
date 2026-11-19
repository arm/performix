// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

var _ conductor.ExecutionRunner = (*AgentEngine)(nil)

const agentExecutionStartupTimeout = 5 * time.Second

// Protobuf string fields require UTF-8 even when a POSIX target accepts raw
// bytes. Check this transport boundary without restricting local POSIX launches.
func validateAgentExecutionEncoding(request *process.LaunchCommand) error {
	if request == nil || !utf8.ValidString(request.WorkingDirectory) {
		return conductor.NewExecutionError(conductor.ExecutionInvalid)
	}
	if directory := request.TemporaryDirectory; directory != nil {
		if !utf8.ValidString(directory.EnvironmentVariable) || !utf8.ValidString(directory.ParentDirectory) {
			return conductor.NewExecutionError(conductor.ExecutionInvalid)
		}
	}
	for _, values := range [][]string{request.Command, request.Affinity} {
		for _, value := range values {
			if !utf8.ValidString(value) {
				return conductor.NewExecutionError(conductor.ExecutionInvalid)
			}
		}
	}
	for name, value := range request.Environment {
		if !utf8.ValidString(name) || !utf8.ValidString(value) {
			return conductor.NewExecutionError(conductor.ExecutionInvalid)
		}
	}
	return nil
}

// RunProcess uses the existing asynchronous RPC lifecycle so cancellation can
// stop the process as well as the RPC. Existing ExecCommand callers are unchanged.
func (g *AgentEngine) RunProcess(ctx context.Context, request *process.LaunchCommand) (result *process.CommandResult, resultErr error) {
	if err := validateAgentExecutionEncoding(request); err != nil {
		return nil, err
	}
	if err := conductor.ValidateProcessRequest(request, g.targetPlatform.OS); err != nil {
		return nil, err
	}
	if err := g.execCtx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(g.execCtx, cancel)
	defer stop()
	defer cancel()
	// AfterFunc runs asynchronously, even when the engine is already cancelled.
	if g.execCtx.Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	invoke := func(callCtx context.Context, name string, fn func(context.Context) error) error {
		if request.AsPrivileged {
			return g.privilegeSession.Invoke(callCtx, name, fn)
		}
		return fn(callCtx)
	}
	var response *targetagentproto.StartProcessResponse
	// Cancellation must not discard the PID of a process that has just started.
	// Keep startup bounded but let its reply arrive before honoring cancellation.
	// A transport failure before that reply still requires agent-session cleanup.
	startupCtx, cancelStartup := context.WithTimeout(context.WithoutCancel(ctx), agentExecutionStartupTimeout)
	err := invoke(startupCtx, "StartProcess", func(callCtx context.Context) error {
		var err error
		response, err = g.client.StartProcess(callCtx, &targetagentproto.StartProcessRequest{
			Command: request.Command, Environment: request.Environment, WorkingDirectory: request.WorkingDirectory,
			Affinity: request.Affinity, AsPrivileged: request.AsPrivileged, TemporaryDirectory: temporaryDirectoryToProto(request.TemporaryDirectory),
			Stdout: &targetagentproto.StreamRedirect{Redirect: targetagentproto.StreamRedirect_STREAM},
			Stderr: &targetagentproto.StreamRedirect{Redirect: targetagentproto.StreamRedirect_STREAM},
		})
		return err
	})
	cancelStartup()
	completed := false
	defer func() {
		// Only a confirmed PID can be cleaned up through these process RPCs.
		if response == nil || response.Pid <= 0 {
			return
		}
		if !completed {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), processCleanupTimeout)
			err := invoke(cleanupCtx, "KillProcess", func(c context.Context) error {
				_, err := g.client.KillProcess(c, &targetagentproto.KillProcessRequest{Pid: response.Pid})
				return err
			})
			cleanupCancel()
			if err != nil {
				resultErr = errors.Join(resultErr, conductor.NewExecutionFailure(conductor.ExecutionTransport, err))
				// Keep the process tracked so agent shutdown can retry termination.
				return
			}
		}
		// Handle release gets its own budget after termination completes.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), processCleanupTimeout)
		defer cleanupCancel()
		if request.TemporaryDirectory != nil && !completed {
			// Wait for target-side file cleanup before releasing the process state.
			var waited *targetagentproto.WaitProcessResponse
			err := invoke(cleanupCtx, "WaitProcess", func(c context.Context) error {
				var err error
				waited, err = g.client.WaitProcess(c, &targetagentproto.WaitProcessRequest{Pid: response.Pid})
				return err
			})
			if err != nil {
				resultErr = errors.Join(resultErr, conductor.NewExecutionFailure(conductor.ExecutionTransport, err))
				return
			}
			if waited != nil && waited.CleanupFailureReason != "" {
				resultErr = errors.Join(resultErr, cleanupFailureFromProto(waited.CleanupFailureReason))
			}
		}
		err := invoke(cleanupCtx, "ReleaseProcessHandles", func(c context.Context) error {
			_, err := g.client.ReleaseProcessHandles(c, &targetagentproto.ReleaseProcessHandlesRequest{Pids: []int32{response.Pid}})
			return err
		})
		if err != nil {
			resultErr = errors.Join(resultErr, conductor.NewExecutionFailure(conductor.ExecutionTransport, err))
		}
	}()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if errors.Is(err, message.New(message.EngineToolServiceElevatePrivilegesFailed)) {
			return nil, err
		}
		_, launchFailure := conductor.ExecutionLaunchFailureReason(err)
		if !launchFailure {
			return nil, conductor.NewExecutionFailure(conductor.ExecutionTransport, err)
		}
		failure := conductor.NewExecutionFailure(conductor.ExecutionLaunch, err)
		var executionErr *conductor.ExecutionError
		if errors.As(failure, &executionErr) && executionErr.FailureReason == process.FailureUnsupported {
			return nil, conductor.NewExecutionErrorReason(conductor.ExecutionUnsupported, process.FailureUnsupported)
		}
		return nil, failure
	}
	// Stop the wait and sibling stream after an I/O failure without changing
	// the execution context, which records caller/engine cancellation.
	streamCtx, cancelStreams := context.WithCancel(ctx)
	defer cancelStreams()
	handle, err := NewAgentProcessHandle(streamCtx, response.Pid, g.client,
		process.StreamRedirect{Mode: process.Stream}, process.StreamRedirect{Mode: process.Stream},
		request.AsPrivileged, g.privilegeSession)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, conductor.NewExecutionFailure(conductor.ExecutionTransport, err)
	}
	var stdout, stderr []byte
	var firstFailure error
	var failureOnce sync.Once
	recordFailure := func(err error) {
		if err != nil {
			failureOnce.Do(func() {
				firstFailure = err
				cancelStreams()
			})
		}
	}
	var streams sync.WaitGroup
	streams.Add(2)
	go func() {
		defer streams.Done()
		var err error
		stdout, err = io.ReadAll(handle.Stdout())
		recordFailure(err)
	}()
	go func() {
		defer streams.Done()
		var err error
		stderr, err = io.ReadAll(handle.Stderr())
		recordFailure(err)
	}()
	rc, waitErr := handle.Wait()
	var cleanup *process.CleanupError
	completedCleanupFailure := errors.As(waitErr, &cleanup)
	if completedCleanupFailure {
		// The process has exited. Drain its output instead of cancelling the streams.
		failureOnce.Do(func() { firstFailure = waitErr })
	} else {
		recordFailure(waitErr)
	}
	streams.Wait()
	// #nosec G115 -- AgentProcessHandle.Wait widens the RPC int32 exit code to int (or returns zero).
	result = &process.CommandResult{Rc: int32(rc), Stdout: string(stdout), Stderr: string(stderr)}
	completed = waitErr == nil || completedCleanupFailure
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if firstFailure != nil {
		return result, conductor.NewExecutionFailure(conductor.ExecutionTransport, firstFailure)
	}
	return result, nil
}

func (g *AgentEngine) RunScript(ctx context.Context, request *conductor.ScriptRequest) (*process.CommandResult, error) {
	command, err := conductor.ScriptCommand(request, g.targetPlatform.OS)
	if err != nil {
		return nil, err
	}
	return g.RunProcess(ctx, command)
}

// The agent must create and remove staging files on the execution host. Sending
// the directory request (rather than an engine-local path) gives its process
// owner enough information to clean up after normal exit or cancellation.
func temporaryDirectoryToProto(directory *process.TemporaryDirectory) *targetagentproto.TemporaryDirectory {
	if directory == nil {
		return nil
	}
	return &targetagentproto.TemporaryDirectory{EnvironmentVariable: directory.EnvironmentVariable, ParentDirectory: directory.ParentDirectory}
}
