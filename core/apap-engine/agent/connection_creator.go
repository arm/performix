// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
	"github.com/Arm-Debug/apap-cli/clients/go/tetherproto"
)

// ErrAgentDisconnected is the cancellation cause when the agent connection is lost.
var ErrAgentDisconnected = errors.New("agent connection lost")

const defaultConnectionTimeout = 2 * time.Second
const launchScriptName = "verify-and-launch"
const noAgentCtxPanic = "agent.AgentConn invariant violated: nil lifetime context"

// GetAgentPath retrieves the path of the agent tool directory on the target machine.
func GetAgentPath(dstToolDir string, platform conductor.TargetPlatform) string {
	baseDir := dstToolDir
	var cmdRunner conductor.CommandRunner
	if platform.Actions != nil {
		cmdRunner = platform.Actions.CommandRunner()
	}
	resolvedBaseDir, err := conductor.ResolveToolsBaseDir(baseDir, platform.OS, cmdRunner)
	if err != nil {
		log.WithError(err).Warn("unable to resolve temp directory for agent deployment")
		return ""
	}

	return platform.Path.ToOSPath(filepath.Join(resolvedBaseDir, terminology.GetAgentBinaryName(), versions.GetVersion()))
}

// Gets the script command that starts the launch script
func GetAgentLaunchScript(platform conductor.TargetPlatform) string {
	return fmt.Sprintf("%s.%s", launchScriptName, platform.Path.GetScriptExtension())
}

// AgentConn holds connection info for an agent on a target.
// AgentConn is thread-safe as instances are constructed by
// ConnectionProvider while holding its mutex. Fields are not
// mutated after construction.
type AgentConn struct {
	Client        targetagentproto.TargetAgentClient
	dataCc        *grpc.ClientConn
	controlClient targetagentproto.TargetAgentClient
	controlCc     *grpc.ClientConn
	logger        AgentLogger
	tether        AgentTether
	closeOnce     sync.Once
	closeErr      error

	onConnectionLost func(error)
	ctx              context.Context
	cancel           context.CancelCauseFunc

	abortOnce sync.Once
}

// NewAgentConn returns an AgentConn with its lifetime context initialized.
func NewAgentConn(client targetagentproto.TargetAgentClient) *AgentConn {
	ac := &AgentConn{Client: client}
	ac.initLifetimeContext()
	return ac
}

// Close closes the agent gRPC connection, and the cached SFTP and SSH connections.
// Any errors encountered during shutdown or closure are returned as a single error.
func (a *AgentConn) Close() error {
	a.closeOnce.Do(func() {
		if a.tether != nil {
			a.tether.Close()
		}

		if a.logger != nil {
			a.logger.Close()
		}

		if a.cancel != nil {
			a.cancel(nil)
		}

		var errs []error
		appendErr := func(label string, err error) {
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", label, err))
			}
		}

		if a.controlClient != nil {
			force := true
			_, err := a.controlClient.Shutdown(context.Background(), &targetagentproto.ShutdownRequest{Force: &force})
			appendErr("grpc shutdown", err)
		} else if a.Client != nil {
			force := true
			_, err := a.Client.Shutdown(context.Background(), &targetagentproto.ShutdownRequest{Force: &force})
			appendErr("grpc shutdown", err)
		}

		if a.dataCc != nil {
			appendErr("grpc data conn close", a.dataCc.Close())
		}

		if a.controlCc != nil {
			appendErr("grpc control conn close", a.controlCc.Close())
		}

		if len(errs) > 0 {
			a.closeErr = fmt.Errorf("error closing agent connection: %w", errors.Join(errs...))
		}
	})
	return a.closeErr
}

// Abort bails out of the agent connection as quickly as possible, without waiting for a graceful shutdown.
// The connection context is cancelled and transport resources are closed, causing subsequent gRPC calls to fail.
func (a *AgentConn) Abort() {
	a.AbortWithCause(nil)
}

// AbortWithCause bails out of the agent connection as quickly as possible, without waiting for a graceful shutdown.
// The connection context is cancelled with the provided cause and transport resources are closed.
func (a *AgentConn) AbortWithCause(err error) {
	go a.abortOnce.Do(func() {
		log.Warn("agent connection abort requested")
		a.cancelAgentLifetime(err)

		if a.onConnectionLost != nil {
			a.onConnectionLost(classifyAbortCause(err))
		}
		a.closeTransportResources()
	})
}

func classifyAbortCause(err error) error {
	if errors.Is(err, errTetherAckTimeout) {
		return message.Wrap(message.EngineAgentConnectionAckTimeout, err)
	}

	transportErr := message.Wrap(message.EngineAgentConnectionTransportError, err)
	if err != nil {
		grpcStatus, ok := status.FromError(err)
		if !ok {
			return transportErr
		}
		transportErr = transportErr.WithMetadata(map[string]string{
			"grpcCode": grpcStatus.Code().String(),
		})
	}
	return transportErr
}

func (a *AgentConn) cancelAgentLifetime(err error) {
	if a.cancel == nil {
		return
	}
	if err != nil {
		a.cancel(fmt.Errorf("%w: %v", ErrAgentDisconnected, err))
		return
	}
	a.cancel(ErrAgentDisconnected)
}

func (a *AgentConn) closeTransportResources() {
	if a.tether != nil {
		a.tether.Close()
	}
	if a.logger != nil {
		a.logger.Close()
	}
	if a.controlCc != nil {
		_ = a.controlCc.Close()
	}
	if a.dataCc != nil {
		_ = a.dataCc.Close()
	}
}

// OnConnectionLost registers a callback that runs when the agent connection is lost.
func (a *AgentConn) OnConnectionLost(fn func(error)) {
	a.onConnectionLost = fn
}

func (a *AgentConn) initLifetimeContext() {
	if a.ctx != nil && a.cancel != nil {
		return
	}
	a.ctx, a.cancel = context.WithCancelCause(context.Background())
}

// AgentContext returns a context for the lifetime of the agent.
func (a *AgentConn) AgentContext() context.Context {
	if a.ctx == nil {
		panic(noAgentCtxPanic)
	}
	return a.ctx
}

// BindContext returns a child context that is cancelled when either the parent
// context is cancelled or the agent connection is lost.
func (a *AgentConn) BindContext(parent context.Context) (context.Context, context.CancelCauseFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	agentCtx := a.AgentContext()
	go func() {
		select {
		case <-agentCtx.Done():
			cancel(context.Cause(agentCtx))
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// ConnectionCreator creates new agent connections.
type ConnectionCreator interface {
	NewConnection(id string, tp conductor.TargetPlatform, dialer grpcconnection.TCPDialer) (*AgentConn, error)
}

func NewConnectionCreator(toolsDir string, rootWorkerEnabled bool) ConnectionCreator {
	return &agentConnectionCreator{
		toolsDir:          toolsDir,
		rootWorkerEnabled: rootWorkerEnabled,
		launch:            launchAgent,
		connect:           newAgentConnection,
		newTether:         newAgentTether,
		newLogger:         newAgentLogger,
	}
}

// agentConnectionCreator implements ConnectionCreator.
type agentConnectionCreator struct {
	toolsDir          string
	rootWorkerEnabled bool
	launch            func(tp conductor.TargetPlatform, agentPath string, rootWorkerEnabled bool) (int, error)
	connect           func(conn grpcconnection.GRPCConnector, port int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error)
	newTether         func(tether tetherproto.TetherClient, pulseEvery time.Duration) AgentTether
	newLogger         func(client targetagentproto.TargetAgentClient, logger log.FieldLogger, id string) AgentLogger
}

// NewConnection launches a new target agent gRPC server on the target platform
// and returns a client connection to it via the connector.
// Tethering and logging streams are started and stored.
func (p *agentConnectionCreator) NewConnection(
	id string,
	tp conductor.TargetPlatform,
	dialer grpcconnection.TCPDialer,
) (*AgentConn, error) {
	agentPath := GetAgentPath(p.toolsDir, tp)
	port, err := p.launch(tp, agentPath, p.rootWorkerEnabled)
	if err != nil {
		return nil, err
	}

	// Create two agent connections: one for data, one for control (to avoid large data transfers interfering
	// with control flow traffic)
	dataClient, dataCc, err := p.connect(grpcconnection.NewConnector(dialer), port)
	if err != nil {
		return nil, err
	}
	ac := NewAgentConn(dataClient)
	ac.dataCc = dataCc

	cleaner := util.ScopeCleaner{}
	defer cleaner.MaybeCleanup(func() { _ = ac.Close() })

	controlClient, controlCc, err := p.connect(grpcconnection.NewConnector(dialer), port)
	if err != nil {
		return nil, err
	}
	ac.controlClient = controlClient
	ac.controlCc = controlCc

	// Route tether and log connections via the control conn rather than the data conn
	tether := p.newTether(tetherproto.NewTetherClient(controlCc), defaultPulseInterval)
	tether.OnTransportFailure(ac.AbortWithCause)
	if err := tether.Start(); err != nil {
		return nil, err
	}
	ac.tether = tether

	// Agent logs are not yet included in the run logs. In future, AgentLogger
	// could be extended with methods for adding/removing loggers. These new
	// loggers would take a user-provided context (e.g a run context so that
	// the logs would be included in the run logs).
	// A possible implementation may be to:
	// 1. Tag the context of a new logger with a unique tag.
	// 2. Store the new logger in a map in the AgentLogger keyed by the tag.
	// 3. Pass the tag on through the gRPC calls to the agent. Have the agent
	//    pass the tag back when sending log messages (all via interceptors).
	// 4. The tag can be used to look up the correct logger in the map.
	logger := p.newLogger(controlClient, logx.FromContext(context.Background()), id)
	if err := logger.Start(); err != nil {
		return nil, err
	}
	ac.logger = logger

	cleaner.CancelCleanup()

	return ac, nil
}

// launchAgent launches a new target agent server via the verify-and-launch script.
// The server's port number is returned.
func launchAgent(
	tp conductor.TargetPlatform,
	agentPath string,
	rootWorkerEnabled bool,
) (int, error) {
	scriptFile := GetAgentLaunchScript(tp)
	cmd := tp.Path.GenerateRunScriptCommand(scriptFile, agentPath)

	// We have two ways to launch the agent:
	// 1. non-root: Part of target agent security model. The agent will act as a
	// 	controller and handle the normal (user) operations itself.
	// 	If a privileged operation (root) is required, it will launch the root
	//	worker agent and proxy the request to it.
	// 2. root (default): The current default behaviour. The agent will handle
	//	both normal and privileged operations itself.

	var out conductor.RunCommandOutput
	var err error
	if rootWorkerEnabled {
		// Non-root launch
		out, err = tp.Actions.RunCommand(cmd)
	} else {
		// Root launch (default)
		out, err = tp.Actions.RunCommandAsAdmin(cmd)
	}

	if err != nil || out.ReturnCode != 0 {
		return 0, classifyLaunchFailure(tp, agentPath, out, err, !rootWorkerEnabled)
	}

	portString := strings.TrimSpace(out.Stdout)
	p, err := strconv.Atoi(portString)
	if err != nil {
		return 0, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("verify-and-launch produced non-numeric port=%s: %w", portString, err))
	}
	if p < 1 || p > 65535 {
		return 0, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("verify-and-launch produced out-of-range port=%d", p))
	}

	return p, nil
}

// classifyLaunchFailure classifies the failure to launch a new target agent
// and returns an appropriate error message.
func classifyLaunchFailure(
	tp conductor.TargetPlatform,
	agentPath string,
	out conductor.RunCommandOutput,
	runErr error,
	requiresAdmin bool,
) error {
	scriptFile := GetAgentLaunchScript(tp)
	metadata := map[string]string{
		"cmd":        scriptFile,
		"workingDir": filepath.ToSlash(agentPath),
	}

	log.Warnf("working dir: %v", agentPath)

	if requiresAdmin {
		hasAdmin, adminErr := tp.Actions.HasAdminPerms()
		if !hasAdmin || adminErr != nil {
			log.Errorf("failed to launch new target agent due to lack of admin or root privileges")
			msg := message.New(message.EngineAgentConnectionCreatorInsufficientPrivileges)
			if adminErr != nil {
				msg = msg.WithCause(adminErr)
			}
			return msg
		}
	}

	if _, statErr := tp.Actions.Stat(filepath.Join(agentPath, scriptFile)); statErr != nil {
		log.Errorf("failed to find verify-and-launch script when launching new target agent at %s", agentPath)
		return message.New(message.EngineAgentConnectionCreatorAgentNotDeployed).WithCause(statErr).WithMetadata(metadata)
	}

	if out.ReturnCode != 0 {
		log.Errorf("failed to launch new target agent (exit code %v); stderr: '%v'", out.ReturnCode, out.Stderr)
		metadata["exitCode"] = fmt.Sprintf("%v", out.ReturnCode)
		return message.New(message.EngineAgentConnectionCreatorLaunchErrorCode).WithMetadata(metadata)
	}

	log.Errorf("failed to run launch script for new target agent; stderr: '%v'", out.Stderr)
	return message.New(message.EngineAgentConnectionCreatorRunLaunchCommand).WithCause(runErr).WithMetadata(metadata)
}

// newAgentConnection creates a new connection to the target agent server
func newAgentConnection(
	conn grpcconnection.GRPCConnector,
	port int,
) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
	cc, err := conn.Connect("127.0.0.1", port, defaultConnectionTimeout)
	if err != nil {
		metadata := map[string]string{
			"portNum": strconv.Itoa(port),
		}
		return nil, nil, message.New(message.EngineAgentConnectionCreatorNewAgentConnection).WithCause(err).WithMetadata(metadata)
	}
	return targetagentproto.NewTargetAgentClient(cc), cc, nil
}

// CheckAgentClientConn checks the health of agent client gRPC connection
func CheckAgentClientConn(ctx context.Context, agent *AgentConn) error {
	_, err := agent.Client.GetVersion(ctx, &emptypb.Empty{})
	if err != nil {
		return fmt.Errorf("health check for agent gRPC connection failed: %w", err)
	}
	return nil
}
