// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcerrors"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpclogging"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	"github.com/Arm-Debug/apap-cli/api/authproto"
	"github.com/Arm-Debug/apap-cli/api/healthproto"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

type GrpcServerConfig struct {
	Host               string
	Port               int
	HttpPort           int
	HttpChunkBytes     int
	ParallelJobs       uint
	DataDirectory      string
	LogLevel           string
	LogPath            string
	SrcToolsDirectory  string
	DeploymentToolsDir string
	ADBPath            string
	FeatureFlags       featureflags.Flags
	ConfigDirectory    string
	RunContext         func() context.Context
}

type GrpcServer struct {
	Config GrpcServerConfig
	Listen func(network string, address string) (net.Listener, error)
}

func (s *GrpcServer) deletePidFile() {
	pidfiles.DeletePid(s.Config.Host, s.Config.Port)
}

func (s *GrpcServer) makePidFile() error {
	pidfile, err := pidfiles.ConstructPidFilePath(s.Config.Host, s.Config.Port)
	if err != nil {
		return fmt.Errorf("server failed to construct PID file path: %w", err)
	}

	// If the PID file exists then it may be that the user crashed the server rather than
	// shutting it down. Warn the user and attempt to start the server anyway. If the PID file
	// had been deleted but the server remains active then the flow of control will not reach here.
	if pid, err := pidfiles.GetPid(pidfile); err == nil {
		log.WithFields(log.Fields{
			"pid": pid,
		}).Warn("server not shut down correctly; PID file already exists")
	}

	err = pidfiles.SavePid(os.Getpid(), s.Config.Host, s.Config.Port)
	if err != nil {
		return fmt.Errorf("failed to save PID file: %w", err)
	}

	return nil
}

func (s *GrpcServer) address() string {
	return fmt.Sprintf("%s:%d", s.Config.Host, s.Config.Port)
}

func generateArrowToken() (string, error) {
	const tokenBytes = 32
	token := make([]byte, tokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// Create and run the gRPC server.
func (s *GrpcServer) runServer() error {
	log.WithField("version", versions.GetVersion()).Infof("Launching %v daemon", terminology.GetProductFullName())
	log.Infof("gRPC server configuration: %+v", s.Config)

	var ctx context.Context
	var cancel context.CancelFunc
	if s.Config.RunContext != nil {
		ctx, cancel = context.WithCancel(s.Config.RunContext())
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	logOpts := []grpc_logrus.Option{
		grpc_logrus.WithMessageProducer(grpcerrors.GRPCLogErrorProducer),
	}
	configDir := s.Config.ConfigDirectory
	if configDir == "" {
		var err error
		configDir, err = userdirs.ConfigDir()
		if err != nil {
			return message.New(message.EngineLifecycleStartupFailed).WithCause(err)
		}
	}
	tlsManager, err := tlsconfig.NewManager(configDir)
	if err != nil {
		return err
	}
	serverCredentials, err := tlsManager.GenerateServerCredentials(s.Config.Host)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(
		grpc.Creds(serverCredentials),
		grpc.ChainStreamInterceptor(
			grpclogging.StreamRPCStartInterceptor(log.NewEntry(log.StandardLogger())),
			grpc_logrus.StreamServerInterceptor(log.NewEntry(log.StandardLogger()), logOpts...),
			grpc_recovery.StreamServerInterceptor(grpc_recovery.WithRecoveryHandler(grpclogging.LogPanicCallStackFunc)),
			message.ErrorHandlingServerStreamInterceptor(),
		),
		grpc.ChainUnaryInterceptor(
			grpclogging.UnaryRPCStartInterceptor(log.NewEntry(log.StandardLogger())),
			grpc_logrus.UnaryServerInterceptor(log.NewEntry(log.StandardLogger()), logOpts...),
			grpc_recovery.UnaryServerInterceptor(grpc_recovery.WithRecoveryHandler(grpclogging.LogPanicCallStackFunc)),
			message.ErrorHandlingServerInterceptor(),
		),
	)

	// Create the APAP service
	packsServerConfig := ApapServerConfig{
		ParallelJobs:         s.Config.ParallelJobs,
		DataDirectory:        s.Config.DataDirectory,
		FeatureFlags:         s.Config.FeatureFlags,
		ServerHostname:       s.Config.Host,
		ServerGRPCPort:       s.Config.Port,
		ServerHTTPPort:       s.Config.HttpPort,
		ServerHTTPChunkBytes: s.Config.HttpChunkBytes,
		LogLevel:             s.Config.LogLevel,
		LogFile:              s.Config.LogPath,
		SourceToolsDir:       s.Config.SrcToolsDirectory,
		ConfigDirectory:      s.Config.ConfigDirectory,
		ADBPath:              s.Config.ADBPath,
	}

	deploymentPaths := deployer.BaseToolDeploymentPaths{
		DeployedToolsDirectory: s.Config.DeploymentToolsDir,
	}
	apapServer, err := NewApapServer(ctx, packsServerConfig, deploymentPaths, cancel)
	if err != nil {
		return err
	}
	arrowToken, err := generateArrowToken()
	if err != nil {
		return err
	}
	apapproto.RegisterApapServer(grpcServer, apapServer)
	authproto.RegisterAuthServer(grpcServer, NewAuthServer(
		ctx,
		apapServer.targetSessions,
		&apapServer.targetAccess,
		arrowToken,
	))

	// Create the health service
	healthServer := NewHealthServer()
	healthproto.RegisterHealthServer(grpcServer, healthServer)

	// Start the HTTP query server if configured
	if s.Config.HttpPort > 0 {
		httpServer := NewHTTPQueryServer(
			s.Config.Host,
			s.Config.HttpPort,
			s.Config.HttpChunkBytes,
			apapServer,
			arrowToken,
			s.Listen,
		)
		if err := httpServer.Start(ctx, cancel); err != nil {
			return err
		}
	}

	// Register reflection service on the gRPC server.
	reflection.Register(grpcServer)

	// As soon as we start listening, clients will be able to connect and start issuing RPCs.
	lis, err := s.Listen("tcp", s.address())
	if err != nil {
		listenErr := fmt.Errorf("gRPC server failed to listen: %w", err)
		return message.New(message.EngineLifecycleStartupFailed).WithCause(listenErr)
	}
	closeListener := true
	defer func() {
		if closeListener {
			_ = lis.Close()
		}
	}()
	log.WithFields(log.Fields{"address": lis.Addr()}).Info("gRPC server is listening with configuration")

	if err := s.makePidFile(); err != nil {
		return err
	}
	defer s.deletePidFile()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	stopWatcher := make(chan struct{})
	// Register this before the server cleanup below so signal handling remains
	// active until the deferred GracefulStop call has completed. Stop signal
	// delivery before releasing a watcher that has not received a signal.
	defer func() {
		signal.Stop(signals)
		close(stopWatcher)
	}()
	go func() {
		select {
		case received := <-signals:
			log.WithFields(log.Fields{
				"component": "engine",
				"pid":       os.Getpid(),
				"ppid":      os.Getppid(),
				"signal":    received.String(),
			}).Debug("gRPC server received shutdown signal")
			cancel()
		case <-stopWatcher:
		}
	}()

	serve(grpcServer, lis, cancel)
	closeListener = false
	defer grpcServer.GracefulStop()

	<-ctx.Done()

	log.Debug("Attempting graceful shutdown")

	return nil
}

// serve runs the gRPC server in a goroutine until the context is cancelled.
func serve(server *grpc.Server, lis net.Listener, cancel context.CancelFunc) {
	go func() {
		if err := server.Serve(lis); err != nil {
			log.WithFields(log.Fields{"error": err}).Error("Failed to serve")
			cancel()
		}
	}()
}

// RunBlocking runs the gRPC server and blocks until it is shutdown.
func (s *GrpcServer) RunBlocking() error {
	err := logging.SetLogLevel(s.Config.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning. Non-fatal error: Could not set log level: %v\nContinuing...\n", err)
	}

	if s.Config.LogPath != "stdout" {
		logFile, err := logging.SetLogOutputFile(s.Config.LogPath)
		if err != nil {
			fmt.Fprintf(
				os.Stderr, "Warning. Non-fatal error: Could not set log file. Error: %v\nContinuing...\n", err)
		} else {
			defer logFile.Close()
		}
	}

	serverError := s.runServer()
	if serverError != nil {
		log.Error(serverError)
	}
	return serverError
}

func NewGrpcServer(config GrpcServerConfig) *GrpcServer {
	return &GrpcServer{Config: config, Listen: net.Listen}
}
