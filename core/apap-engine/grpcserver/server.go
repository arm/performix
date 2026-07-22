// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
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

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcerrors"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpclogging"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/authproto"
	"github.com/Arm-Debug/apap-cli/clients/go/healthproto"
)

type GrpcServerConfig struct {
	Host                      string
	Port                      int
	AuthPort                  int
	HttpPort                  int
	HttpChunkBytes            int
	ParallelJobs              uint
	DataDirectory             string
	LogLevel                  string
	LogPath                   string
	SrcToolsDirectory         string
	DeploymentToolsDir        string
	IsRootWorkerEnabled       bool
	EnableFullCaptureSupport  bool
	EnableRerendering         bool
	EnableExperimentalRecipes bool
	EnableSecondaryRunPaths   bool
	EnableTransferManager     bool
	EnableRenderDBSandbox     bool
	EnableNeoprofTimeline     bool
	ConfigDirectory           string
	RunContext                func() context.Context
}

type GrpcServer struct {
	Config    GrpcServerConfig
	Listen    func(network string, address string) (net.Listener, error)
	ListenTLS func(network string, address string) (net.Listener, error)
}

func (s *GrpcServer) deletePidFile() {
	pidfiles.DeletePid(s.Config.Host, s.Config.Port)
}

func onInterrupt(ctx context.Context, f func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	select {
	case <-c:
	case <-ctx.Done():
	}
	f()
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

func (s *GrpcServer) authAddress() string {
	return fmt.Sprintf("%s:%d", s.Config.Host, s.Config.AuthPort)
}

// Create and run the gRPC server.
func (s *GrpcServer) runServer() error {
	log.WithField("version", versions.GetVersion()).Infof("Launching %v daemon", terminology.GetProductFullName())
	log.Infof("gRPC server configuration: %+v", s.Config)

	if err := s.makePidFile(); err != nil {
		return err
	}
	defer s.deletePidFile()

	if s.Config.AuthPort <= 0 {
		return fmt.Errorf("auth port must be configured")
	}
	if s.Config.AuthPort == s.Config.Port {
		return fmt.Errorf("auth port must differ from main gRPC port")
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if s.Config.RunContext != nil {
		ctx, cancel = context.WithCancel(s.Config.RunContext())
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}

	logOpts := []grpc_logrus.Option{
		grpc_logrus.WithMessageProducer(grpcerrors.GRPCLogErrorProducer),
	}

	grpcServer := grpc.NewServer(
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
		ParallelJobs:              s.Config.ParallelJobs,
		DataDirectory:             s.Config.DataDirectory,
		IsRootWorkerEnabled:       s.Config.IsRootWorkerEnabled,
		EnableFullCaptureSupport:  s.Config.EnableFullCaptureSupport,
		EnableRerendering:         s.Config.EnableRerendering,
		EnableExperimentalRecipes: s.Config.EnableExperimentalRecipes,
		EnableSecondaryRunPaths:   s.Config.EnableSecondaryRunPaths,
		EnableTransferManager:     s.Config.EnableTransferManager,
		EnableRenderDBSandbox:     s.Config.EnableRenderDBSandbox,
		EnableNeoprofTimeline:     s.Config.EnableNeoprofTimeline,
		ServerHostname:            s.Config.Host,
		ServerGRPCPort:            s.Config.Port,
		ServerAuthPort:            s.Config.AuthPort,
		ServerHTTPPort:            s.Config.HttpPort,
		ServerHTTPChunkBytes:      s.Config.HttpChunkBytes,
		LogLevel:                  s.Config.LogLevel,
		LogFile:                   s.Config.LogPath,
		SourceToolsDir:            s.Config.SrcToolsDirectory,
		ConfigDirectory:           s.Config.ConfigDirectory,
	}

	deploymentPaths := deployer.BaseToolDeploymentPaths{
		DeployedToolsDirectory: s.Config.DeploymentToolsDir,
	}
	apapServer, err := NewApapServer(ctx, packsServerConfig, deploymentPaths, cancel)
	if err != nil {
		return err
	}
	apapproto.RegisterApapServer(grpcServer, apapServer)

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
			s.Listen,
		)
		if err := httpServer.Start(ctx, cancel); err != nil {
			return err
		}
	}

	// Register reflection service on the main gRPC server, but not for the auth server.
	reflection.Register(grpcServer)

	configDir := s.Config.ConfigDirectory
	if configDir == "" {
		configDir, err = userdirs.ConfigDir()
		if err != nil {
			return message.New(message.EngineLifecycleStartupFailed).WithCause(err)
		}
	}

	// Configure TLS credentials for the auth service.
	tlsManager, err := tlsconfig.NewManager(configDir)
	if err != nil {
		return err
	}
	authCreds, err := tlsManager.GenerateServerCredentials(s.Config.Host)
	if err != nil {
		return err
	}

	authServer := grpc.NewServer(
		grpc.Creds(authCreds),
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

	authproto.RegisterAuthServer(authServer, NewAuthServer(
		ctx,
		apapServer.targetSessions,
		&apapServer.targetAccess,
	))

	// As soon as we start listening, clients will be able to connect and start issuing RPCs.
	lis, err := s.Listen("tcp", s.address())
	if err != nil {
		listenErr := fmt.Errorf("main server failed to listen: %w", err)
		return message.New(message.EngineLifecycleStartupFailed).WithCause(listenErr)
	}
	log.WithFields(log.Fields{"address": lis.Addr()}).Info("Main server is listening with configuration")

	listenTLS := s.ListenTLS
	if listenTLS == nil {
		listenTLS = net.Listen
	}
	authLis, err := listenTLS("tcp", s.authAddress())
	if err != nil {
		listenErr := fmt.Errorf("auth server failed to listen: %w", err)
		return message.New(message.EngineLifecycleStartupFailed).WithCause(listenErr)
	}
	log.WithFields(log.Fields{"address": authLis.Addr()}).Info("Auth server is listening with configuration")

	serve(ctx, grpcServer, lis, cancel)
	defer grpcServer.GracefulStop()

	serve(ctx, authServer, authLis, cancel)
	defer authServer.GracefulStop()

	<-ctx.Done()

	log.Debug("Attempting graceful shutdown")

	return nil
}

// serve runs the gRPC server in a goroutine until the context is cancelled.
func serve(ctx context.Context, server *grpc.Server, lis net.Listener, cancel context.CancelFunc) {
	go func() {
		if err := server.Serve(lis); err != nil {
			log.WithFields(log.Fields{"error": err}).Error("Failed to serve")
			cancel()
		}
	}()

	go onInterrupt(ctx, cancel)
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
	return &GrpcServer{Config: config, Listen: net.Listen, ListenTLS: net.Listen}
}
