// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetsession

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
)

// TargetSession creates and caches connections and platform info for a target.
type TargetSession interface {
	// Connect returns the connection to the target, establishing a new connection if required.
	// The target platform is identified and cached.
	// Passwords and passphrases are not supported when establishing a connection through this method.
	// PromptProviders are used for secrets and host-key fingerprint confirmations required when establishing a connection.
	// PlatformGate selects which platform-support policy to enforce while connecting.
	Connect(ctx context.Context, opts ...ConnectOptions) (TargetConnection, error)
	// TargetPlatform returns the cached target platform information.
	TargetPlatform() (*conductor.TargetPlatform, error)
	// TargetAgent returns the agent connection for the target, establishing a new connection if required.
	// An underlying target connection must have been established before calling this method.
	TargetAgent(ctx context.Context) (*agent.AgentConn, error)
	// CloseTargetAgent closes the cached agent connection if one exists.
	// It is a no-op if no agent connection has been established.
	CloseTargetAgent() error
	// ResolveToolsDir returns the cached resolved tools base directory for this session.
	ResolveToolsDir() string
	// Close closes the target session and all underlying connections.
	Close() error
}

type sshConnect func(ctx context.Context, sshTgt target.SSHTarget, promptProviders conductor.PromptProviders) (conductor.SecureClient, error)
type androidConnect func(androidTgt target.AndroidTarget) (*conductor.ADBClient, error)
type createTargetPlatform func(cmdRunner conductor.CommandRunner, fs conductor.TargetFilesystem, platformGate conductor.PlatformGate) (*conductor.TargetPlatform, error)
type resolveToolsDir func(baseDir string, platformOS conductor.OS, cmdRunner conductor.CommandRunner, localityName string) (string, error)

type ConnectOptions struct {
	PromptProviders conductor.PromptProviders
	PlatformGate    conductor.PlatformGate
}

// targetSession holds connections and platform info for a target session.
type targetSession struct {
	// Holds the target details
	target target.Target
	// Holds underlying connections to the target
	connection TargetConnection
	// Holds platform-specific functionality
	platform *conductor.TargetPlatform
	// Holds the agent Grpc connection
	agent *agent.AgentConn
	// Holds the resolved tools base directory on the target
	resolvedToolsDir string
	baseToolsDir     string
	toolsDirResolver resolveToolsDir
	// Handles SSH target connection creation
	sshConnect sshConnect
	// Handles Android target connection creation
	androidConnect androidConnect
	// Handles target platform creation
	platformCreator createTargetPlatform
	// Handles agent connection creation
	agentCreator agent.ConnectionCreator
	// Mutex for synchronizing access
	mu sync.Mutex
}

// localityScopedTargetSession wraps a targetSession with a locality.
// The same underlying localhost session can be reused for both target-locality and host-locality sessions.
type localityScopedTargetSession struct {
	base         *targetSession
	localityName string
}

func newTargetSession(tgt target.Target, agentConnCreator agent.ConnectionCreator, baseToolsDir string, adbPath string) *targetSession {
	return &targetSession{
		target:       tgt,
		baseToolsDir: baseToolsDir,
		toolsDirResolver: func(baseDir string, platformOS conductor.OS, cmdRunner conductor.CommandRunner, localityName string) (string, error) {
			return conductor.ResolveToolsBaseDir(baseDir, platformOS, cmdRunner, localityName)
		},
		sshConnect: func(ctx context.Context, sshTgt target.SSHTarget, promptProviders conductor.PromptProviders) (conductor.SecureClient, error) {
			secureConnector := conductor.NewDefaultSecureConnector()
			return secureConnector.SecureConnectNoRetry(ctx, &sshTgt, promptProviders)
		},
		androidConnect: func(androidTgt target.AndroidTarget) (*conductor.ADBClient, error) {
			adbClient := conductor.NewADBClient(androidTgt.SerialNumber, androidTgt.DeviceIPAddress, adbPath)
			if err := adbClient.CheckHealth(); err != nil {
				return nil, err
			}
			return adbClient, nil
		},
		platformCreator: func(cr conductor.CommandRunner, fs conductor.TargetFilesystem, platformGate conductor.PlatformGate) (*conductor.TargetPlatform, error) {
			targetType := ""
			switch tgt.(type) {
			case *target.AndroidTarget:
				targetType = string(target.TargetTypeAndroid)
			case *target.LocalTarget:
				targetType = string(target.TargetTypeLocal)
			}
			_, targetPlatform, err := conductor.GetTargetArchitecture(cr, fs, targetType, platformGate)
			return targetPlatform, err
		},
		agentCreator: agentConnCreator,
	}
}

func (ts *targetSession) Connect(ctx context.Context, opts ...ConnectOptions) (TargetConnection, error) {
	return ts.connect(ctx, locality.Target, opts...)
}

func (ts *targetSession) connect(ctx context.Context, localityName string, opts ...ConnectOptions) (TargetConnection, error) {
	options := ConnectOptions{PlatformGate: conductor.TargetSupported}
	if len(opts) > 0 {
		options = opts[0]
	}
	return ts.connectWithOptions(ctx, options, localityName)
}

func (ts *targetSession) connectWithOptions(ctx context.Context, options ConnectOptions, localityName string) (TargetConnection, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.connection != nil {
		err := ts.connection.CheckHealth()
		if err == nil && ts.platform != nil {
			if err := conductor.ValidatePlatformGateSupport(ts.platform.PlatformConfiguration, options.PlatformGate); err != nil {
				return nil, err
			}
			return ts.connection, nil
		}
		logx.FromContext(ctx).WithFields(logrus.Fields{"err": err, "target": ts.target.String()}).Warn(
			"Existing target connection is unhealthy, re-establishing")
		// Close existing connection
		_ = ts.close()
	}

	switch tgt := ts.target.(type) {
	case *target.SSHTarget:
		// Establish connection
		sshClient, err := ts.sshConnect(ctx, *tgt, options.PromptProviders)
		if err != nil {
			return nil, err
		}
		// Initialise SFTP client
		sftpClient, err := sshClient.SFTPClient()
		if err != nil {
			_ = sshClient.Close()
			return nil, message.New(message.EngineCommonCreateSftpClient).WithCause(err)
		}

		ts.connection = &sshTargetConnection{sshConn: sshClient, sftpCache: newSftpCache(sftpClient)}
	case *target.LocalTarget:
		ts.connection = &localTargetConnection{}
	case *target.AndroidTarget:
		adbClient, err := ts.androidConnect(*tgt)
		if err != nil {
			return nil, err
		}
		ts.connection = &androidTargetConnection{client: adbClient}
	default:
		targetType := reflect.TypeOf(tgt).String()
		return nil, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": targetType})
	}

	platform, err := ts.platformCreator(ts.connection.CommandRunner(), ts.connection.Filesystem(), options.PlatformGate)
	if err != nil {
		_ = ts.close()
		return nil, err
	}
	ts.platform = platform

	resolvedDir, err := ts.toolsDirResolver(ts.baseToolsDir, platform.OS, ts.connection.CommandRunner(), localityName)
	if err != nil {
		_ = ts.close()
		return nil, err
	}
	ts.resolvedToolsDir = resolvedDir

	return ts.connection, nil
}

func (ts *targetSession) TargetPlatform() (*conductor.TargetPlatform, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.platform == nil {
		return nil, message.New(message.EngineTargetSessionConnectionNotEstablished)
	}
	return ts.platform, nil
}

func (ts *targetSession) TargetAgent(ctx context.Context) (*agent.AgentConn, error) {
	return (&localityScopedTargetSession{base: ts, localityName: locality.Target}).TargetAgent(ctx)
}

func (ts *targetSession) targetAgent(ctx context.Context, localityName string) (*agent.AgentConn, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.connection == nil || ts.platform == nil {
		return nil, message.New(message.EngineTargetSessionConnectionNotEstablished)
	}

	if ts.agent != nil {
		err := agent.CheckAgentClientConn(ctx, ts.agent)
		if err == nil {
			logx.FromContext(ctx).WithField("target", ts.target.String()).Info("Existing agent connection found")

			return ts.agent, nil
		}

		logx.FromContext(ctx).WithFields(logrus.Fields{"err": err, "target": ts.target.String()}).Warn(
			"Existing agent connection is unhealthy, re-establishing")

		// Close existing connection
		err = ts.agent.Close()
		if err != nil {
			logx.FromContext(ctx).WithField("target", ts.target.String()).WithError(err).Warn(
				"Error closing unhealthy agent connection",
			)
		}
		ts.agent = nil
	}
	conn, err := ts.agentCreator.NewConnection(ts.target.DisplayHost(), localityName, *ts.platform, ts.connection.Dialer())
	if err != nil {
		return nil, err
	}

	logx.FromContext(ctx).WithField("target", ts.target.String()).Info("New agent connection created")
	ts.agent = conn
	return conn, nil
}

func (ts *targetSession) CloseTargetAgent() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.agent == nil {
		return nil
	}
	err := ts.agent.Close()
	ts.agent = nil
	return err
}

func (ls *localityScopedTargetSession) ResolveToolsDir() string {
	ls.base.mu.Lock()
	defer ls.base.mu.Unlock()
	return ls.base.resolvedToolsDir
}

func (ts *targetSession) Close() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.close()
}

func (ts *targetSession) close() error {
	var joined error
	if ts.agent != nil {
		if err := ts.agent.Close(); err != nil {
			joined = errors.Join(joined, err)
		}
		ts.agent = nil
	}
	ts.platform = nil
	if ts.connection != nil {
		if err := ts.connection.Close(); err != nil {
			joined = errors.Join(joined, err)
		}
		ts.connection = nil
	}
	if joined == nil {
		return nil
	}
	return fmt.Errorf("error closing target session: %w", joined)
}

func (ls *localityScopedTargetSession) Connect(ctx context.Context, opts ...ConnectOptions) (TargetConnection, error) {
	return ls.base.connect(ctx, ls.localityName, opts...)
}

func (ls *localityScopedTargetSession) TargetAgent(ctx context.Context) (*agent.AgentConn, error) {
	return ls.base.targetAgent(ctx, ls.localityName)
}

func (ts *targetSession) ResolveToolsDir() string {
	ls := &localityScopedTargetSession{base: ts, localityName: locality.Target}
	return ls.ResolveToolsDir()
}

func (ls *localityScopedTargetSession) TargetPlatform() (*conductor.TargetPlatform, error) {
	return ls.base.TargetPlatform()
}

func (ls *localityScopedTargetSession) CloseTargetAgent() error {
	return ls.base.CloseTargetAgent()
}

func (ls *localityScopedTargetSession) Close() error {
	return ls.base.Close()
}
