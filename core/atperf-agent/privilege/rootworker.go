// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/conversion"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/transport"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/clients/go/healthproto"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type LaunchTiming struct {
	AcceptTimeout   time.Duration
	HealthPollEvery time.Duration
	HealthTimeout   time.Duration
}

var defaultLaunchTiming = LaunchTiming{
	AcceptTimeout:   5 * time.Second,
	HealthPollEvery: 50 * time.Millisecond,
	HealthTimeout:   5 * time.Second,
}

// DefaultLaunchTiming provides default values for the LaunchTiming struct.
func DefaultLaunchTiming() LaunchTiming {
	return defaultLaunchTiming
}

func FillDefaultLaunchTiming(timing *LaunchTiming) {
	if timing.AcceptTimeout == 0 {
		timing.AcceptTimeout = defaultLaunchTiming.AcceptTimeout
	}
	if timing.HealthPollEvery == 0 {
		timing.HealthPollEvery = defaultLaunchTiming.HealthPollEvery
	}
	if timing.HealthTimeout == 0 {
		timing.HealthTimeout = defaultLaunchTiming.HealthTimeout
	}
}

type deps struct {
	buildClient func(transport.Transport) (targetagentproto.TargetAgentClient, *grpc.ClientConn, func(), error)
	newHealth   func(grpc.ClientConnInterface) healthproto.HealthClient
	waitProcess func(proc *os.Process) (any, error)
}

func fillDefaultDeps(d *deps) {
	if d.buildClient == nil {
		d.buildClient = buildClient
	}
	if d.newHealth == nil {
		d.newHealth = healthproto.NewHealthClient
	}
	if d.waitProcess == nil {
		d.waitProcess = func(proc *os.Process) (any, error) {
			return proc.Wait()
		}
	}
}

type RootWorkerProcessConfig struct {
	TransportLoggingEnabled bool // Enable transport logging for debugging
	ProofMechanism          ProofMechanism
	LaunchTiming            LaunchTiming
	deps                    deps
}

// RootWorkerProcess launches and connects to a root-worker subprocess via gRPC-over-transport.
type RootWorkerProcess interface {
	Launch(ctx context.Context) (
		targetagentproto.TargetAgentClient,
		error,
	)
	StartWatchdog(onServerDied func())
	Close()
	StreamLogs(logger *log.Logger) error
	CheckPrivileges(ctx context.Context) (bool, error)
	LogVersion(ctx context.Context) error
}

type rootWorkerProcessImpl struct {
	pm                      process.ProcessManager
	af                      AcceptorFactory
	execPath                string
	proc                    *os.Process
	client                  targetagentproto.TargetAgentClient
	cleanup                 func()
	closeOnce               sync.Once
	RootWorkerProcessConfig // Configuration for the root-worker process
}

type RootWorkerProcessFactory func(
	pm process.ProcessManager,
	acceptorFactory AcceptorFactory,
	config RootWorkerProcessConfig,
) (RootWorkerProcess, error)

// NewRootWorkerProcess constructs a RootWorkerProcess using the given ProcessManager,
// PipeFactory, and TransportFactory.
func NewRootWorkerProcess(
	pm process.ProcessManager,
	acceptorFactory AcceptorFactory,
	config RootWorkerProcessConfig,
) (RootWorkerProcess, error) {
	return newRootWorkerProcessImpl(pm, acceptorFactory, config)
}

func newRootWorkerProcessImpl(
	pm process.ProcessManager,
	acceptorFactory AcceptorFactory,
	config RootWorkerProcessConfig,
) (*rootWorkerProcessImpl, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to find executable path: %w", err)
	}

	FillDefaultLaunchTiming(&config.LaunchTiming)
	fillDefaultDeps(&config.deps)

	return &rootWorkerProcessImpl{
		pm:                      pm,
		af:                      acceptorFactory,
		execPath:                path,
		RootWorkerProcessConfig: config,
	}, nil
}

// Launch starts the root-worker subprocess, waits for its gRPC server to become available,
// and returns a connected TargetAgentClient and a cleanup function.
func (r *rootWorkerProcessImpl) Launch(ctx context.Context) (
	targetagentproto.TargetAgentClient,
	error,
) {
	acc, err := r.beginAccept()
	if err != nil {
		return nil, err
	}

	proc, err := r.startProcess(acc.acceptor.GetIPCAddress())
	if err != nil {
		acc.Close()
		return nil, err
	}
	r.proc = proc
	cleanupOnFailure := r.assembleCleanup(nil, acc, nil)

	// Redirect stderr to logger
	redirectStderrToLogger(proc, r.pm)

	xport, err := r.waitForAccept(ctx, acc)
	if err != nil {
		cleanupOnFailure()
		return nil, err
	}

	if r.TransportLoggingEnabled {
		xport = transport.NewLoggingTransport(xport, nil)
	}

	// Poll until gRPC server is up
	var (
		client    targetagentproto.TargetAgentClient
		conn      *grpc.ClientConn
		closeConn func()
	)

	client, conn, closeConn, err = r.deps.buildClient(xport)
	cleanup := r.assembleCleanup(closeConn, acc, xport)
	if err != nil {
		log.WithError(err).Error("failed to build root-worker client")
		cleanup()
		return nil, fmt.Errorf("failed to build root-worker client: %w", err)
	}

	if err := r.waitHealthy(ctx, cleanup, conn); err != nil {
		return nil, err
	} else {
		r.client = client
		r.cleanup = cleanup
		return client, nil
	}
}

func (r *rootWorkerProcessImpl) startProcess(ipcAddress string) (*os.Process, error) {
	workerCommand := []string{
		r.execPath,
		"start-group-controller",
		"--",
		r.execPath,
		"start-root-worker",
		"--ipc-socket", ipcAddress,
	}
	if r.TransportLoggingEnabled {
		workerCommand = append(workerCommand, "--transport-logging")
	}

	cmdLine, err := r.buildLaunchCommand(workerCommand)
	if err != nil {
		return nil, err
	}

	sp := &process.StartProcess{
		LaunchCommand: process.LaunchCommand{Command: cmdLine},
		Stdin:         process.StdinBuffer,
		Stderr: process.StreamRedirect{
			Mode: process.Stream,
		},
	}
	proc, err := r.pm.StartProcess(sp)
	if err != nil {
		return nil, fmt.Errorf("failed to start root-worker: %w", err)
	}

	log.Info("Root-worker started with PID: ", proc.Pid)

	return proc, nil
}

func (r *rootWorkerProcessImpl) buildLaunchCommand(workerCommand []string) ([]string, error) {
	switch r.ProofMechanism {
	case NoPasswdSudo:
		return append([]string{"sudo", "-E", "-n"}, workerCommand...), nil
	case AndroidSu:
		return buildAndroidSuCommand(workerCommand)
	default:
		return nil, fmt.Errorf("unsupported root-worker proof mechanism: %d", r.ProofMechanism)
	}
}

// TODO: implement Android su support
func buildAndroidSuCommand(_ []string) ([]string, error) {
	return nil, message.New(message.AgentElevatePrivilegesMechanismAndroidSuNotImplemented)
}

type acceptState struct {
	acceptor        Acceptor
	closeOnce       sync.Once
	acceptorChan    chan transport.Transport
	acceptorErrChan chan error
}

func (r *acceptState) Close() {
	r.closeOnce.Do(func() {
		r.acceptor.Close()
	})
}

func (r *rootWorkerProcessImpl) beginAccept() (*acceptState, error) {
	// Create acceptor for communication.
	acceptor, err := r.af.NewAcceptor()
	if err != nil {
		return nil, fmt.Errorf("failed to accept connection: %w", err)
	}

	state := &acceptState{
		acceptor:        acceptor,
		acceptorChan:    make(chan transport.Transport, 1),
		acceptorErrChan: make(chan error, 1),
	}

	// Start a goroutine to accept the transport connection
	go func() {
		// Close the acceptor after accepting the connection. On Unix, this will remove the socket file.
		// This is important to avoid leaving stale sockets around. While it's not a security risk, it avoids
		// confusion - nobody could launch a second root-worker subprocess that connects to the same socket.
		// If they were to do so, it wouldn't cause a security issue, but it would lead to confusion.
		defer state.Close()

		xport, err := acceptor.Accept()
		if err != nil {
			state.acceptorErrChan <- err
			return
		}

		state.acceptorChan <- xport
	}()

	return state, nil
}

func (r *rootWorkerProcessImpl) waitForAccept(ctx context.Context, state *acceptState) (transport.Transport, error) {
	// Poll until the acceptor accepts a connection; fail if it times out. Resources from the acceptor will be cleaned up
	// by the cleanup function.
	ticker := time.NewTicker(r.LaunchTiming.AcceptTimeout)
	defer ticker.Stop()
	// Wait for the acceptor to accept a connection
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("failed to accept connection from root-worker: %w", ctx.Err())
	case <-ticker.C:
		return nil, fmt.Errorf("failed to accept connection from root-worker: %w", context.DeadlineExceeded)
	case xport := <-state.acceptorChan:
		log.Info("Accepted transport connection from root-worker subprocess")
		return xport, nil
	case err := <-state.acceptorErrChan:
		return nil, fmt.Errorf("failed to accept transport connection: %w", err)
	}
}

// assembleCleanup creates a cleanup function that releases all resources associated
// with the root-worker.
func (r *rootWorkerProcessImpl) assembleCleanup(
	closeConn func(),
	acceptState *acceptState,
	xport transport.Transport,
) func() {
	return func() {
		log.Info("Cleaning up root-worker process resources")

		if closeConn != nil {
			closeConn()
		}

		if r.proc != nil {
			if err := r.pm.WriteToStdin(r.proc.Pid, []byte{}); err != nil {
				log.WithError(err).Error("failed to kill the root-worker")
			}
		}

		if acceptState != nil {
			acceptState.Close()
		}

		if xport != nil {
			xport.Close()
		}
	}
}

func (r *rootWorkerProcessImpl) waitHealthy(ctx context.Context, cleanup func(), conn *grpc.ClientConn) error {
	ticker := time.NewTicker(r.LaunchTiming.HealthPollEvery)
	defer ticker.Stop()

	healthClient := r.deps.newHealth(conn)

	// Wait for the root-worker to become healthy
	healthDeadline := time.Now().Add(r.LaunchTiming.HealthTimeout)

	doHealthCheck := func() (bool, error) {
		hCtx, hCancel := context.WithTimeout(context.Background(), time.Second)
		resp, herr := healthClient.Check(hCtx, &healthproto.HealthCheckRequest{Service: ""})
		hCancel()
		if herr != nil || resp.GetStatus() != healthproto.HealthCheckResponse_SERVING {
			if time.Now().After(healthDeadline) {
				cleanup()
				return false, fmt.Errorf("timeout waiting for root-worker healthy: %w", context.DeadlineExceeded)
			}
			return false, nil
		}
		return true, nil
	}

	// One-off health check before entering the loop in case the server is already healthy
	if _, err := doHealthCheck(); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			cleanup()
			return ctx.Err()
		case <-ticker.C:
			if ok, err := doHealthCheck(); err != nil {
				return err
			} else if ok {
				return nil
			}
		}
	}
}

type stderrToLogAdapter struct {
}

func (*stderrToLogAdapter) Send(chunk *process.StreamChunk) error {
	log.Errorf("[RootWorker stderr] %s", string(chunk.Data))
	return nil
}

func (*stderrToLogAdapter) Context() context.Context {
	return context.Background()
}

func redirectStderrToLogger(proc *os.Process, pm process.ProcessManager) {
	go func() {
		err := pm.StreamStderr(proc.Pid, &stderrToLogAdapter{})
		if err != nil {
			log.WithError(err).Error("Failed to stream stderr from root-worker process")
		}
	}()
}

// buildClient constructs a gRPC client over the provided transport endpoints.
func buildClient(xport transport.Transport) (
	targetagentproto.TargetAgentClient,
	*grpc.ClientConn,
	func(),
	error,
) {
	connAdapter := transport.NewIOToConnAdapter(xport)

	grpcConn, err := grpc.NewClient(
		"passthrough:///ignored", // target is still required but unused in this custom dialer setup
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return connAdapter, nil
		}),
	)
	if err != nil {
		return nil, nil, func() {}, fmt.Errorf("failed to dial gRPC: %w", err)
	}
	client := targetagentproto.NewTargetAgentClient(grpcConn)
	return client, grpcConn, func() { grpcConn.Close() }, nil
}

// StartWatchdog launches a goroutine that waits for the root-worker process to exit
// and sends a notification on the provided channel when it does.
func (r *rootWorkerProcessImpl) StartWatchdog(onServerDied func()) {
	go func() {
		state, err := r.deps.waitProcess(r.proc)
		if err != nil {
			log.Errorf("root-worker watchdog wait error: %v", err)
		} else {
			log.Warnf("root-worker process exited: %v", state)
		}
		onServerDied()
	}()
}

// Close kills the root-worker and cleans up associated resources
func (r *rootWorkerProcessImpl) Close() {
	r.closeOnce.Do(func() {
		log.Info("Closing the root-worker process")

		if r.cleanup != nil {
			r.cleanup()
			r.cleanup = nil
		}
		if r.proc != nil {
			r.proc = nil
		}
		if r.client != nil {
			r.client = nil
		}
		log.Info("Root worker process cleaned up")
	})
}

// StreamLogs streams logs from the root-worker process back to the parent process.
func (r *rootWorkerProcessImpl) StreamLogs(logger *log.Logger) error {
	if r.client == nil {
		return fmt.Errorf("root-worker client is not initialized")
	}

	stream, err := r.client.StreamLogs(context.Background(), &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to start log stream: %w", err)
	}

	entry := log.NewEntry(logger)

	for {
		logEntry, err := stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled || errors.Is(err, context.Canceled) {
				log.Info("Log stream closed")
				return nil
			}
			return fmt.Errorf("error receiving log entry: %w", err)
		}

		if logEntry == nil {
			log.Warn("Received nil log entry from root-worker")
			continue
		}

		level, msg := conversion.LogEntryFromProto(logEntry, entry)

		msg = "[RootWorker] " + msg // Prefix log messages for clarity

		entry.Log(level, msg)
	}
}

// CheckPrivileges checks if the root-worker has elevated privileges.
func (r *rootWorkerProcessImpl) CheckPrivileges(ctx context.Context) (bool, error) {
	if r.client == nil {
		return false, fmt.Errorf("root-worker client is not initialized")
	}

	resp, err := r.client.GetPrivilegeInfo(ctx, &emptypb.Empty{})
	if err != nil {
		return false, fmt.Errorf("failed to check privileges: %w", err)
	}

	return resp.GetHasAdmin(), nil
}

// LogVersion logs the root-worker version.
func (r *rootWorkerProcessImpl) LogVersion(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("root-worker client is not initialized")
	}

	resp, err := r.client.GetVersion(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to get root-worker version: %w", err)
	}

	log.Infof("Root-worker version: %s", resp.GetVersion())
	return nil
}
