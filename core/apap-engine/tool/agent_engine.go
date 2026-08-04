// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/notifiers"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/privilege"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// NewAgentEngine creates a new AgentEngine instance.
func NewAgentEngine(
	execCtx context.Context,
	cleanupCtx context.Context,
	client targetagentproto.TargetAgentClient,
	notifier notifiers.StageNotifier,
	userMessageWriter run.UserMessageWriter,
	preserveTempDirs bool,
	rootWorkerEnabled bool,
	targetPlatform conductor.PlatformConfiguration,
) (*AgentEngine, func()) {
	ps := privilege.NewPrivilegeSession(client, targetPlatform.OS)

	ae := &AgentEngine{
		client:                client,
		notifier:              notifier,
		execCtx:               execCtx,
		cleanupCtx:            cleanupCtx,
		userMessageWriter:     userMessageWriter,
		preserveTemporaryDirs: preserveTempDirs,
		rootWorkerEnabled:     rootWorkerEnabled,
		privilegeSession:      ps,
		targetPlatform:        targetPlatform,
	}
	return ae, ae.Cleanup
}

// Cleanup removes any temporary directories, completes unfinished stages
func (g *AgentEngine) Cleanup() {
	g.CleanupStages()

	// Close any open host files
	g.closeHostFiles()

	cleanupCtx := g.selectCleanupContext()
	if cleanupCtx == nil {
		return
	}

	wg := sync.WaitGroup{}
	if !g.preserveTemporaryDirs {
		wg.Add(len(g.tempDirs))
		// Remove any directories created by the engine
		for _, d := range g.tempDirs {
			go func(dirToRemove string) {
				defer wg.Done()
				if err := g.rmTempDir(cleanupCtx, dirToRemove); err != nil {
					logx.FromContext(cleanupCtx).Warnf("failed to recursively force remove temporary directory %s: %v", dirToRemove, err)
				}
			}(d)
		}
	}

	// Release processes created by the engine
	if len(g.procsToRelease) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &targetagentproto.ReleaseProcessHandlesRequest{Pids: g.procsToRelease}
			_, err := g.client.ReleaseProcessHandles(cleanupCtx, req)
			if err != nil {
				logx.FromContext(cleanupCtx).Warnf("failed to release processes with pids %v: %v", g.procsToRelease, err)
			}
		}()
	}

	if len(g.privilegeProcsToRelease) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := g.privilegeSession.Invoke(cleanupCtx, "Release process handle", func(ctx context.Context) error {
				req := &targetagentproto.ReleaseProcessHandlesRequest{Pids: g.privilegeProcsToRelease}
				_, err := g.client.ReleaseProcessHandles(ctx, req)
				return err
			})
			if err != nil {
				logx.FromContext(cleanupCtx).Warnf("failed to release processes with pids %v: %v", g.privilegeProcsToRelease, err)
			}
		}()
	}

	wg.Wait()
}

func (g *AgentEngine) CleanupStages() {
	for _, id := range g.stagesInFlight {
		logx.FromContext(g.execCtx).Warnf("cleaning up unfinished stage: %s", id)
		g.notifier.OnStageEnd(notifiers.StageInfo{Name: id}, errors.New("stage did not complete"))
	}
}

// selectCleanupContext returns the engine context for cleanup unless the agent
// connection was lost, in which case RPC-based cleanup is skipped using a nil context.
func (g *AgentEngine) selectCleanupContext() context.Context {
	cause := context.Cause(g.cleanupCtx)
	if errors.Is(cause, agent.ErrAgentDisconnected) {
		return nil
	}
	return g.cleanupCtx
}

type TransferOptions struct {
	ImmediateRetrieval bool
	Exclude            []string
	BackgroundTransfer bool
}

type FileCollector interface {
	QueueFileRetrieval(outputEntityDir string, targetPath string, destRelativePath string, componentType cdf.ComponentType, transferOptions TransferOptions) error
	AddComponent(outputEntityDir string, componentType cdf.ComponentType, relativePath string) (string, error)
}

// AgentEngine is a TargetAgent implementation of the engine
type AgentEngine struct {
	client                  targetagentproto.TargetAgentClient
	notifier                notifiers.StageNotifier
	execCtx                 context.Context
	cleanupCtx              context.Context
	stagesInFlight          []string
	tempDirs                []string
	preserveTemporaryDirs   bool
	hostFiles               []*hostFileHandle
	userMessageWriter       run.UserMessageWriter
	procsToRelease          []int32
	privilegeProcsToRelease []int32
	targetPlatform          conductor.PlatformConfiguration

	// Privilege
	rootWorkerEnabled bool
	privilegeSession  privilege.PrivilegeSession
}

func (g *AgentEngine) GetPlatform() conductor.PlatformConfiguration {
	return g.targetPlatform
}

type hostFileHandle struct {
	path     string
	file     *os.File
	closeErr error
}

func (h *hostFileHandle) isClosed() bool {
	return h.file == nil
}

func (g *AgentEngine) ExecCommand(opts *process.LaunchCommand) (*process.CommandResult, error) {
	req := &targetagentproto.ExecCommandRequest{
		Command:          opts.Command,
		AsPrivileged:     opts.AsPrivileged,
		Affinity:         opts.Affinity,
		Environment:      opts.Environment,
		WorkingDirectory: opts.WorkingDirectory,
	}

	var resp *targetagentproto.CommandResult
	wantsPrivilege := g.rootWorkerEnabled && opts.AsPrivileged
	if wantsPrivilege {
		// Privilege path
		err := g.privilegeSession.Invoke(g.execCtx, "ExecCommand", func(ctx context.Context) error {
			var callErr error
			resp, callErr = g.client.ExecCommand(ctx, req)
			return callErr
		})

		if err != nil {
			return nil, err
		}
	} else {
		// Non-privilege path
		var err error
		resp, err = g.client.ExecCommand(g.execCtx, req)
		if err != nil {
			return nil, err
		}
	}

	return &process.CommandResult{
		Rc:     resp.Rc,
		Stdout: resp.Stdout,
		Stderr: resp.Stderr,
	}, nil
}

func (g *AgentEngine) StartProcess(opts *process.StartProcess) (ProcessHandle, error) {
	req := &targetagentproto.StartProcessRequest{
		Command:          opts.LaunchCommand.Command,
		AsPrivileged:     opts.LaunchCommand.AsPrivileged,
		Affinity:         opts.LaunchCommand.Affinity,
		Environment:      opts.LaunchCommand.Environment,
		WorkingDirectory: opts.LaunchCommand.WorkingDirectory,
		Stdout: &targetagentproto.StreamRedirect{
			//nolint:gosec
			Redirect: targetagentproto.StreamRedirect_Mode(opts.Stdout.Mode), Path: opts.Stdout.FilePath,
		},
		Stderr: &targetagentproto.StreamRedirect{
			//nolint:gosec
			Redirect: targetagentproto.StreamRedirect_Mode(opts.Stderr.Mode), Path: opts.Stderr.FilePath,
		},
		UseGroupController: viper.GetBool("agent-use-group-controller"),
	}

	var resp *targetagentproto.StartProcessResponse

	wantsPrivilege := g.rootWorkerEnabled && opts.LaunchCommand.AsPrivileged
	if wantsPrivilege {
		// Privilege path
		err := g.privilegeSession.Invoke(g.execCtx, "StartProcess", func(ctx context.Context) error {
			var err error
			resp, err = g.client.StartProcess(ctx, req)
			return err
		})

		if err != nil {
			return nil, err
		}
		g.privilegeProcsToRelease = append(g.privilegeProcsToRelease, resp.Pid)
	} else {
		// Non-privilege path
		var err error
		resp, err = g.client.StartProcess(g.execCtx, req)
		if err != nil {
			return nil, err
		}
		g.procsToRelease = append(g.procsToRelease, resp.Pid)
	}

	return NewAgentProcessHandle(
		opts.Ctx,
		resp.Pid,
		g.client,
		opts.Stdout,
		opts.Stderr,

		// Privilege
		wantsPrivilege,
		g.privilegeSession,
	)
}

func (g *AgentEngine) CreateTempDir() (string, error) {
	resp, err := g.client.CreateTempDir(g.execCtx, &emptypb.Empty{})
	if err != nil {
		return "", err
	}
	g.tempDirs = append(g.tempDirs, resp.Path)
	return resp.Path, nil
}

func (g *AgentEngine) Mkdir(path string) error {
	_, err := g.client.Mkdir(g.execCtx, &targetagentproto.MkdirRequest{
		Path: path,
	})
	return err
}

func (g *AgentEngine) Rm(path string, recursive, force bool) error {
	_, err := g.client.Rm(g.execCtx, &targetagentproto.RmRequest{
		Path:      path,
		Recursive: recursive,
		Force:     force,
	})
	return err
}

func (g *AgentEngine) MakeWritable(path string, recursive bool) error {
	_, err := g.client.MakeWritable(g.execCtx, &targetagentproto.MakeWritableRequest{
		Path:      path,
		Recursive: recursive,
	})
	return err
}

func (g *AgentEngine) Chown(path, owner string, recursive bool) error {
	_, err := g.client.Chown(g.execCtx, &targetagentproto.ChownRequest{
		Path:      path,
		Owner:     owner,
		Recursive: recursive,
	})
	return err
}

func (g *AgentEngine) Log(level string, message string) {
	logCtx := logx.FromContext(g.execCtx)
	switch level {
	case "error":
		logCtx.Error(message)
		return
	case "warn":
		logCtx.Warn(message)
		return
	case "info":
		logCtx.Info(message)
		return
	case "debug":
		logCtx.Debug(message)
		return
	}
	logCtx.Error("unknown log level, defaulting to error: " + message)
}

func (g *AgentEngine) WriteUserMessage(level string, message string) {
	loggerWithFields := logx.FromContext(g.execCtx).WithFields(log.Fields{
		"messageLevel": level,
		"message":      message,
	})
	if g.userMessageWriter == nil {
		loggerWithFields.Error("user message requested with no user message writer configured")
		return
	}
	loggerWithFields.Info("writing user message")
	g.userMessageWriter.Write(level, message)
}

func (g *AgentEngine) StartProgressTracker(id string) error {
	g.stagesInFlight = append(g.stagesInFlight, id)
	g.notifier.OnStageStart(notifiers.StageInfo{Name: id})
	return nil
}

func (g *AgentEngine) UpdateProgress(id, message string, percent float64) error {
	g.notifier.OnStageProgress(notifiers.StageInfo{Name: id}, notifiers.StageProgress{
		Sent:    int64(percent),
		Max:     100,
		Unit:    notifiers.UnitPercent,
		Message: message,
	})
	return nil
}

func (g *AgentEngine) EndProgress(id string) error {
	g.stagesInFlight = util.RemoveFirst(g.stagesInFlight, func(i int) bool { return g.stagesInFlight[i] == id })
	g.notifier.OnStageEnd(notifiers.StageInfo{Name: id}, nil)
	return nil
}

func (g *AgentEngine) CreateRunFile(path string) (FileHandle, error) {
	if err := os.MkdirAll(filepath.Dir(path), perms.LocalDirPerm); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perms.LocalFilePerm)
	if err != nil {
		return nil, err
	}

	handle := &hostFileHandle{
		path: path,
		file: file,
	}

	g.hostFiles = append(g.hostFiles, handle)

	return handle, nil
}

func (g *AgentEngine) ReadHostFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (g *AgentEngine) closeHostFiles() {
	for _, handle := range g.hostFiles {
		if err := handle.Close(); err != nil {
			logx.FromContext(g.execCtx).Warnf("failed to close host file %s: %v", handle.Path(), err)
		}
	}
}

// rmTempDir removes the temporary directories created by the engine.
// It's a best effort where we first try to remove it normally (non-privilege path),
// and if that fails, we try again with elevated privileges (privilege path).
func (g *AgentEngine) rmTempDir(ctx context.Context, path string) error {
	req := &targetagentproto.RmRequest{
		Path:      path,
		Recursive: true,
		Force:     true,
	}

	// Attempt 1. Non-privilege path
	_, err := g.client.Rm(ctx, req)
	if err == nil {
		return err
	}

	// Attempt 2. Privilege path
	if g.rootWorkerEnabled {
		commonPermissionErr := message.New(message.AgentFsutilCommonPermissionError)
		if errors.Is(message.FromGRPCStatus(err), commonPermissionErr) {
			err = g.privilegeSession.Invoke(ctx, "Rm", func(privCtx context.Context) error {
				_, callErr := g.client.Rm(privCtx, req)
				return callErr
			})
		}
	}
	return err
}

func (h *hostFileHandle) Append(data string) error {
	if h.isClosed() {
		return errors.New("host file handle is closed")
	}
	_, err := h.file.WriteString(data)
	return err
}

func (h *hostFileHandle) Close() error {
	if h.isClosed() {
		return h.closeErr
	}
	h.closeErr = h.file.Close()
	h.file = nil
	return h.closeErr
}

func (h *hostFileHandle) Path() string {
	return h.path
}
