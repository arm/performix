// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-agent/android"
	"github.com/Arm-Debug/apap-cli/atperf-agent/filelock"
	"github.com/Arm-Debug/apap-cli/atperf-agent/filetransfer"
	"github.com/Arm-Debug/apap-cli/atperf-agent/fsutil"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// gRPC metadata headers to pass privilege information
const (
	metadataAsPrivileged        = "apx-as-privileged"
	metadataPrivilegeProofToken = "apx-privilege-proof-token" //nolint:gosec // Metadata header key, not a credential
)

// AgentServerAPI implements the gRPC APIs for communication with the host engine
type AgentServerAPI struct {
	targetagentproto.UnimplementedTargetAgentServer
	Am         AdmissionManager
	ShutdownCb func(force bool, deadline time.Duration)
	Pm         process.ProcessManager
	LogBuffer  *LogBuffer
	Fm         fsutil.FSManager
	Ftm        filetransfer.FileTransferManager
	SystemInfo systeminfo.SystemInfo
	cpl        *filelock.CrossProcessLock

	// Synchronization
	stopCh chan struct{}

	// Privilege
	Elevator
	AcceptorFactory   privilege.AcceptorFactory
	RootWorkerFactory privilege.RootWorkerProcessFactory
	TokenStorage      privilege.TokenStorage
	Checker           privilege.Checker
}

// GetVersion returns the version of the target agent
func (s *AgentServerAPI) GetVersion(ctx context.Context, _ *emptypb.Empty) (*targetagentproto.GetVersionResponse, error) {
	return &targetagentproto.GetVersionResponse{Version: versions.GetVersion()}, nil
}

// Shutdown requests that the server should shut down
func (s *AgentServerAPI) Shutdown(ctx context.Context, req *targetagentproto.ShutdownRequest) (*emptypb.Empty, error) {
	force := req.GetForce()

	// Restrict incoming gRPC calls
	s.Am.Restrict()

	err := s.Pm.Shutdown(force)

	// Wait for in-flight gRPC calls to finish
	if !force {
		s.Am.Wait()
	}

	// Shutdown gRPC server
	s.ShutdownCb(force, defaultTetherShutdownDeadline)

	return &emptypb.Empty{}, err
}

// KillProcess attempts to kill a process referenced by the PID
func (s *AgentServerAPI) KillProcess(ctx context.Context, req *targetagentproto.KillProcessRequest) (*emptypb.Empty, error) {
	var err error

	asPrivileged, token := s.extractMetadata(ctx)
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return nil, err
	}

	if shouldProxy {
		// Privilege path
		log.Info("Proxying KillProcess to the root worker")

		_, err = proxyUnary(s, unaryProxyOptions[*emptypb.Empty]{
			RPCName: "KillProcess",
			Token:   token,
			Invoke: func(client targetagentproto.TargetAgentClient) (*emptypb.Empty, error) {
				return client.KillProcess(ctx, req)
			},
		})
	} else {
		// Non-privilege path
		err = s.Pm.KillProcess(int(req.Pid))
	}

	return &emptypb.Empty{}, err
}

// InterruptProcess attempts to interrupt a process referenced by the PID
func (s *AgentServerAPI) InterruptProcess(ctx context.Context, req *targetagentproto.InterruptProcessRequest) (*emptypb.Empty, error) {
	var err error

	asPrivileged, token := s.extractMetadata(ctx)
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return nil, err
	}

	if shouldProxy {
		// Privilege path
		log.Info("Proxying InterruptProcess to the root worker")

		_, err = proxyUnary(s, unaryProxyOptions[*emptypb.Empty]{
			RPCName: "InterruptProcess",
			Token:   token,
			Invoke: func(client targetagentproto.TargetAgentClient) (*emptypb.Empty, error) {
				return client.InterruptProcess(ctx, req)
			},
		})
	} else {
		// Non-privilege path
		err = s.Pm.InterruptProcess(int(req.Pid))
	}

	return &emptypb.Empty{}, err
}

// StartProcess will start a process and return its pid
func (s *AgentServerAPI) StartProcess(ctx context.Context, req *targetagentproto.StartProcessRequest) (*targetagentproto.StartProcessResponse, error) {
	var resp *targetagentproto.StartProcessResponse
	var err error

	startProcess, err := StartProcessFromProto(req)
	if err != nil {
		return nil, err
	}

	asPrivileged, token := s.extractMetadata(ctx)
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return nil, err
	}

	if shouldProxy {
		// Privilege path
		log.Info("Proxying StartProcess to the root worker")

		resp, err = proxyUnary(s, unaryProxyOptions[*targetagentproto.StartProcessResponse]{
			RPCName: "StartProcess",
			Token:   token,
			Invoke: func(client targetagentproto.TargetAgentClient) (*targetagentproto.StartProcessResponse, error) {
				return client.StartProcess(ctx, req)
			},
		})
	} else {
		// Non-privilege path
		platform, platformErr := systeminfo.GetPlatformType()
		startProcess.UseGroupController = (platformErr == nil && platform.OS == systeminfo.LinuxType) && startProcess.UseGroupController

		proc, startErr := s.Pm.StartProcess(startProcess)
		pid := int32(-1)
		if proc != nil {
			// this avoids a panic when proc is nil (for example, if validateFilePath in StartProcessCommon fails)
			pid = int32(proc.Pid) // #nosec G115
		}
		//nolint:gosec // pid is in valid range for int32
		resp = &targetagentproto.StartProcessResponse{Pid: pid}
		err = startErr
	}

	return resp, err
}

func (s *AgentServerAPI) ReleaseProcessHandles(ctx context.Context, req *targetagentproto.ReleaseProcessHandlesRequest) (*emptypb.Empty, error) {
	s.Pm.ReleaseProcessHandles(util.Map(req.Pids, func(pid int32) int { return int(pid) }))
	return &emptypb.Empty{}, nil
}

// ExecCommand launches a command and returns the result (rc, stdout, stderr)
func (s *AgentServerAPI) ExecCommand(ctx context.Context, req *targetagentproto.ExecCommandRequest) (*targetagentproto.CommandResult, error) {
	var resp *targetagentproto.CommandResult
	var err error

	launchCommand, err := LaunchCommandFromProto(req)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	asPrivileged, token := s.extractMetadata(ctx)
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return nil, err
	}

	if shouldProxy {
		log.Info("Proxying ExecCommand to the root worker")

		resp, err = proxyUnary(s, unaryProxyOptions[*targetagentproto.CommandResult]{
			RPCName: "ExecCommand",
			Token:   token,
			Invoke: func(client targetagentproto.TargetAgentClient) (*targetagentproto.CommandResult, error) {
				return client.ExecCommand(ctx, req)
			},
		})
	} else {
		// Non-privilege path
		result, execErr := s.Pm.ExecCommand(launchCommand)

		resp = CommandResultToProto(result)
		err = execErr
	}

	return resp, err
}

// StreamStdout streams the contents of stdout from a running/ran process to the client.
// The client sends a ProcessStreamRequest with the PID, and the server pushes each StreamChunk over the stream.
func (s *AgentServerAPI) StreamStdout(req *targetagentproto.ProcessStreamRequest, stream targetagentproto.TargetAgent_StreamStdoutServer) error {
	var err error

	asPrivileged, token := s.extractMetadata(stream.Context())
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return err
	}

	if shouldProxy {
		// Privilege path
		log.Info("Proxying StreamStdout to the root worker")

		err = proxyServerStream(s, serverStreamProxyOptions[targetagentproto.TargetAgent_StreamStdoutServer]{
			RPCName: "StreamStdout",
			Token:   token,
			Stream:  stream,
			Invoke: func(client targetagentproto.TargetAgentClient, serverStream targetagentproto.TargetAgent_StreamStdoutServer) error {
				clientStream, err := client.StreamStdout(serverStream.Context(), req)
				if err != nil {
					return err
				}
				for {
					chunk, err := clientStream.Recv()
					if errors.Is(err, io.EOF) {
						return nil
					}
					if err != nil {
						return err
					}
					if err := serverStream.Send(chunk); err != nil {
						return err
					}
				}
			},
		})
	} else {
		// Non-privilege path
		err = s.Pm.StreamStdout(int(req.Pid), &grpcStreamAdapter{stream: stream})
	}

	return err
}

// StreamStderr streams the contents of stdout from a running/ran process to the client.
// The client sends a ProcessStreamRequest with the PID, and the server pushes each StreamChunk over the stream.
func (s *AgentServerAPI) StreamStderr(req *targetagentproto.ProcessStreamRequest, stream targetagentproto.TargetAgent_StreamStderrServer) error {
	var err error

	asPrivileged, token := s.extractMetadata(stream.Context())
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return err
	}

	if shouldProxy {
		// Privilege path
		log.Info("Proxying StreamStderr to the root worker")

		err = proxyServerStream(s, serverStreamProxyOptions[targetagentproto.TargetAgent_StreamStderrServer]{
			RPCName: "StreamStderr",
			Token:   token,
			Stream:  stream,
			Invoke: func(client targetagentproto.TargetAgentClient, serverStream targetagentproto.TargetAgent_StreamStderrServer) error {
				clientStream, err := client.StreamStderr(serverStream.Context(), req)
				if err != nil {
					return err
				}
				for {
					chunk, err := clientStream.Recv()
					if errors.Is(err, io.EOF) {
						return nil
					}
					if err != nil {
						return err
					}
					if err := serverStream.Send(chunk); err != nil {
						return err
					}
				}
			},
		})
	} else {
		// Non-privilege path
		err = s.Pm.StreamStderr(int(req.Pid), &grpcStreamAdapter{stream: stream})
	}

	return err
}

func (s *AgentServerAPI) WriteToStdin(ctx context.Context, in *targetagentproto.StdinChunk) (*emptypb.Empty, error) {
	var err error

	asPrivileged, token := s.extractMetadata(ctx)
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return nil, err
	}

	if shouldProxy {
		// Privilege path
		log.Info("Proxying WriteToStdin to the root worker")

		_, err = proxyUnary(s, unaryProxyOptions[*emptypb.Empty]{
			RPCName: "WriteToStdin",
			Token:   token,
			Invoke: func(client targetagentproto.TargetAgentClient) (*emptypb.Empty, error) {
				return client.WriteToStdin(ctx, in)
			},
		})
	} else {
		// Non-privilege path
		err = s.Pm.WriteToStdin(int(in.Pid), in.Data)
	}

	return &emptypb.Empty{}, err
}

// CreateTempDir creates a temporary directory and returns its path
func (s *AgentServerAPI) CreateTempDir(ctx context.Context, _ *emptypb.Empty) (*targetagentproto.TempDir, error) {
	path, err := s.Fm.CreateTempDir()
	if err != nil {
		return nil, err
	}

	return &targetagentproto.TempDir{Path: path}, err
}

// Mkdir creates a directory at the provided path
func (s *AgentServerAPI) Mkdir(ctx context.Context, req *targetagentproto.MkdirRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Fm.Mkdir(req.Path)
}

// Rm removes the file or directory at the provided path
func (s *AgentServerAPI) Rm(ctx context.Context, req *targetagentproto.RmRequest) (*emptypb.Empty, error) {
	var err error

	asPrivileged, token := s.extractMetadata(ctx)
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return nil, err
	}

	if shouldProxy {
		log.Info("Proxying Rm to the root worker")

		_, err = proxyUnary(s, unaryProxyOptions[*emptypb.Empty]{
			RPCName: "Rm",
			Token:   token,
			Invoke: func(client targetagentproto.TargetAgentClient) (*emptypb.Empty, error) {
				return client.Rm(ctx, req)
			},
		})
	} else {
		err = s.Fm.Rm(req.Path, req.Recursive, req.Force)
	}

	return &emptypb.Empty{}, err
}

// MakeWritable sets the write permissions of the file or directory at the provided path
func (s *AgentServerAPI) MakeWritable(ctx context.Context, req *targetagentproto.MakeWritableRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Fm.MakeWritable(req.Path, req.Recursive)
}

// Chown changes the owner of the file or directory at the provided path
func (s *AgentServerAPI) Chown(ctx context.Context, req *targetagentproto.ChownRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.Fm.Chown(req.Path, req.Owner, req.Recursive)
}

// ListFiles lists all of the files in the provided paths
// Each path can be exact path or glob (e.g. "/tmp/*.log")
// Errors are associated with each path individually
func (s *AgentServerAPI) ListFiles(ctx context.Context, req *targetagentproto.ListFilesRequest) (*targetagentproto.ListFilesResponse, error) {
	perPathInfos := mapSlice(req.Paths, func(path string) []fsutil.FileInfo { return s.Fm.ListFiles(path) })
	return &targetagentproto.ListFilesResponse{Responses: FileInfosToProto(perPathInfos)}, nil
}

// StoreFile receives a streamed file transfer from the client.
// The first message must be an OpenFileRequest specifying the path and mode (append or truncate),
// followed by one or more Content chunks.
func (s *AgentServerAPI) StoreFile(stream targetagentproto.TargetAgent_StoreFileServer) error {
	return s.Ftm.StoreFile(stream)
}

// RetrieveFile streams the contents of a file to the client.
// The request must specify the file path, and the server will send the file
// back in a series of FileContent chunks.
func (s *AgentServerAPI) RetrieveFile(req *targetagentproto.FileRequest, stream targetagentproto.TargetAgent_RetrieveFileServer) error {
	return s.Ftm.RetrieveFile(req, stream)
}

// ListProcesses retrieves a list of currently running processes on the target system
func (s *AgentServerAPI) ListProcesses(ctx context.Context, _ *emptypb.Empty) (*targetagentproto.ProcessList, error) {
	processes, err := s.SystemInfo.ListProcesses()
	if err != nil {
		return nil, err
	}

	return ProcessInfoToProto(processes)
}

// ListAndroidPackages lists packages installed on an Android target and their MAIN activities.
func (s *AgentServerAPI) ListAndroidPackages(ctx context.Context, _ *emptypb.Empty) (*targetagentproto.AndroidPackageList, error) {
	return android.ListPackages(s.Pm)
}

// GetTargetInfo retrieves information about the target system
func (s *AgentServerAPI) GetTargetInfo(ctx context.Context, _ *emptypb.Empty) (*targetagentproto.TargetInfo, error) {
	targetInfo, err := systeminfo.GetTargetInfo(s.SystemInfo, s.Checker)
	if err != nil {
		return nil, err
	}

	return TargetInfoToProto(targetInfo)
}

// StreamLogs streams log entries to the client as they arrive.
func (a *AgentServerAPI) StreamLogs(_ *emptypb.Empty, stream targetagentproto.TargetAgent_StreamLogsServer) error {
	for {
		select {
		case <-a.stopCh:
			return nil
		case <-stream.Context().Done():
			return nil
		case logEntry, ok := <-a.LogBuffer.Channel():
			if !ok {
				return nil
			}

			if err := stream.Send(logEntry); err != nil {
				return err
			}
		}
	}
}

// Close closes the stopCh and cleans up any internal resources.
func (a *AgentServerAPI) Close() {
	close(a.stopCh)
}

// WaitProcess blocks until a process to finished and returns its exit code.
func (s *AgentServerAPI) WaitProcess(ctx context.Context, req *targetagentproto.WaitProcessRequest) (*targetagentproto.WaitProcessResponse, error) {
	var resp *targetagentproto.WaitProcessResponse
	var err error

	asPrivileged, token := s.extractMetadata(ctx)
	err, shouldProxy := s.shouldProxyToRootWorker(asPrivileged, token)
	if err != nil {
		return nil, err
	}

	if shouldProxy {
		// Privilege path
		log.Info("Proxying WaitProcess to the root worker")

		resp, err = proxyUnary(s, unaryProxyOptions[*targetagentproto.WaitProcessResponse]{
			RPCName: "WaitProcess",
			Token:   token,
			Invoke: func(client targetagentproto.TargetAgentClient) (*targetagentproto.WaitProcessResponse, error) {
				return client.WaitProcess(ctx, req)
			},
		})
	} else {
		// Non-privilege path
		rc, waitErr := s.Pm.WaitProcess(ctx, int(req.Pid))

		//nolint:gosec // rc is in valid range for int32
		resp = &targetagentproto.WaitProcessResponse{ExitCode: int32(rc)}
		err = waitErr
	}

	return resp, err
}

// ElevatePrivileges attempts to elevate the agent's privileges by spawning a
// root worker process and then having a gRPC client connection back to it.
// This is no-op if the agent is already running with elevated privileges.
func (a *AgentServerAPI) ElevatePrivileges(ctx context.Context, req *targetagentproto.ElevatePrivilegesRequest) (*targetagentproto.ElevatePrivilegesResponse, error) {
	log.Info("ElevatePrivileges API called")

	if req == nil || req.GetProof() == nil {
		return nil, message.New(message.AgentElevatePrivilegesProofMechanismUnknown).
			WithCause(errors.New("missing request or proof mechanism"))
	}

	mech, err := PrivilegeProofProtoToMech(req.GetProof())
	if err != nil {
		return nil, err
	}

	// 1. Generate the privilege proof token
	// Just return a generic error to avoid leaking info - no matter how innocent they seem
	if a.TokenStorage == nil {
		return nil, message.New(message.AgentApiInternalError).
			WithMetadata(map[string]string{"request": "ElevatePrivileges"}).
			WithCause(errors.New("token storage is not initialised"))
	}

	token, err := a.TokenStorage.Generate()
	if err != nil {
		return nil, message.New(message.AgentApiInternalError).
			WithMetadata(map[string]string{"request": "ElevatePrivileges"}).
			WithCause(err)
	}

	// 2. No-op if already elevated
	privileged, err := a.Checker.IsPrivileged()
	if err != nil {
		log.WithError(err).Error("Privilege check failed during ElevatePrivileges")
		return nil, message.New(message.AgentApiInternalError).
			WithMetadata(map[string]string{"request": "ElevatePrivileges"}).
			WithCause(err)
	}

	if privileged {
		log.Info("Privilege elevation not needed; already have privileges")
		return &targetagentproto.ElevatePrivilegesResponse{
			Token: &targetagentproto.PrivilegeProofToken{Value: token},
		}, nil
	}

	// 3. Try to elevate privileges
	err = a.Elevator.ElevatePrivileges(ctx, ElevatorConfig{
		Pm:                a.Pm,
		AcceptorFactory:   a.AcceptorFactory,
		RootWorkerFactory: a.RootWorkerFactory,
		Mech:              mech,
	})

	if err != nil {
		a.TokenStorage.Revoke(token) // revoke the token if we fail to elevate
		return nil, err
	}

	return &targetagentproto.ElevatePrivilegesResponse{
		Token: &targetagentproto.PrivilegeProofToken{Value: token},
	}, nil
}

func (a *AgentServerAPI) CleanupRootWorker() {
	a.Elevator.CleanupRootWorker()

	if a.TokenStorage != nil {
		log.Info("Revoking all privilege proof tokens")
		a.TokenStorage.RevokeAll()
	}
}

func (a *AgentServerAPI) GetPrivilegeInfo(ctx context.Context, _ *emptypb.Empty) (*targetagentproto.GetPrivilegeInfoResponse, error) {
	log.Info("GetPrivilegeInfo API called")

	checker := privilege.NewChecker()
	privileged, err := checker.IsPrivileged()
	if err != nil {
		log.WithError(err).Error("Privilege check failed")
		return nil, err
	}
	return &targetagentproto.GetPrivilegeInfoResponse{HasAdmin: privileged}, nil
}

// shouldProxyToRootWorker check whether the request should be proxied to the root worker.
func (a *AgentServerAPI) shouldProxyToRootWorker(asPrivileged bool, token string) (error, bool) {
	if !asPrivileged || token == "" {
		return nil, false
	}

	valid := a.TokenStorage.Validate(token)
	if !valid {
		log.Error("Invalid privilege proof token provided")
		return message.New(message.AgentElevatePrivilegesInvalidToken), false
	}

	privileged, err := a.Checker.IsPrivileged()
	if err != nil {
		log.WithError(err).Error("Privilege check failed in shouldProxyToRootWorker")
		return message.New(message.AgentApiInternalError).
			WithMetadata(map[string]string{"function": "shouldProxyToRootWorker"}).
			WithCause(err), false
	}

	if !privileged {
		return nil, true
	}
	return nil, false
}

func (a *AgentServerAPI) extractMetadata(ctx context.Context) (bool, string) {
	// Extract gRPC metadata: token
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		md = metadata.MD{}
	}

	var asPrivileged bool
	asPrivilegedValues := md.Get(metadataAsPrivileged)
	if len(asPrivilegedValues) > 0 && asPrivilegedValues[0] == "true" {
		asPrivileged = true
	}

	var token string
	tokens := md.Get(metadataPrivilegeProofToken)
	if len(tokens) > 0 {
		token = tokens[0]
	}

	return asPrivileged, token
}

// HoldLock acquires an exclusive lock on the target agent.
// It sends a LockGranted message to the client once the lock is acquired,
// and holds the lock until the client closes the stream or the context is cancelled.
func (a *AgentServerAPI) HoldLock(_ *emptypb.Empty, stream targetagentproto.TargetAgent_HoldLockServer) error {
	err := a.cpl.HoldLock(stream.Context(), func() {
		_ = stream.Send(&targetagentproto.LockGranted{})
	})
	if err != nil {
		log.WithError(err).Error("Failed to acquire exclusive lock on target agent")
		var lockPathErr *filelock.LockPathError
		if errors.As(err, &lockPathErr) {
			return message.New(message.CommonPathCreationFailed).WithMetadata(map[string]string{
				"path":        lockPathErr.Path,
				"permissions": fmt.Sprintf("%o", perms.TargetDirPerm),
			}).WithCause(err)
		}
	}
	return err
}
