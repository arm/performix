// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"time"

	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpc_recovery "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // Including this registers the gzip compressor allowing transmission of compressed payloads
	"google.golang.org/grpc/reflection"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcerrors"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpclogging"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/atperf-agent/agentconfig"
	"github.com/Arm-Debug/apap-cli/atperf-agent/filelock"
	"github.com/Arm-Debug/apap-cli/atperf-agent/filetransfer"
	"github.com/Arm-Debug/apap-cli/atperf-agent/fsutil"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver/transport"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
	"github.com/Arm-Debug/apap-cli/clients/go/healthproto"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
	"github.com/Arm-Debug/apap-cli/clients/go/tetherproto"
)

const defaultTetherShutdownDeadline = 30 * time.Second
const lockFileName = "lockfile_v2"

type Server interface {
	Start() <-chan error
	Stop(force bool)
}

type GrpcServer struct {
	listener       transport.Listener
	grpcServer     *grpc.Server
	portWriter     PortWriter
	agentServerAPI *AgentServerAPI
}

type TransportConfig interface {
	IsTransportConfig()
}

type TCPTransportConfig struct {
	Host string
	Port int
}

// IsTransportConfig is a marker method for TCPTransportConfig
func (TCPTransportConfig) IsTransportConfig() {}

type FDTransportConfig struct {
	ReadFD         uintptr
	WriteFD        uintptr
	LoggingEnabled bool
}

// IsTransportConfig is a marker method for FDTransportConfig
func (FDTransportConfig) IsTransportConfig() {}

// EstablishedConnTransportConfig is used when the connection is already established through external means
type EstablishedConnTransportConfig struct {
	Conn           net.Conn
	LoggingEnabled bool
}

// IsTransportConfig is a marker method for EstablishedConnTransportConfig
func (EstablishedConnTransportConfig) IsTransportConfig() {}

// GrpcServerConfig is the config for the gRPC server
type GrpcServerConfig struct {
	TransportConfig TransportConfig
	LogBuffer       *LogBuffer
	PortWriter      PortWriter
}

func createListener(config TransportConfig) (transport.Listener, error) {
	switch cfg := config.(type) {
	case TCPTransportConfig:
		return transport.NewTCPListener(cfg.Host, cfg.Port)
	case FDTransportConfig:
		return transport.NewFDListener(cfg.ReadFD, cfg.WriteFD, cfg.LoggingEnabled)
	case EstablishedConnTransportConfig:
		return transport.NewConnListener(cfg.Conn, cfg.LoggingEnabled), nil
	default:
		return nil, fmt.Errorf("unsupported transport config type: %T", config)
	}
}

// NewServer initialises a new GrpcServer, registered to AgentServerAPI
func NewServer(config GrpcServerConfig) (*GrpcServer, error) {
	lis, err := createListener(config.TransportConfig)
	if err != nil {
		log.WithField("error", err).Errorf("failed to create listener")
		return nil, message.New(message.AgentLifecycleStartupFailed).
			WithCause(err)
	}

	logOpts := []grpc_logrus.Option{
		grpc_logrus.WithMessageProducer(grpcerrors.GRPCLogErrorProducer),
	}

	admissionInterceptor := NewAdmissionInterceptor(ExemptOnlyShutdown)

	s := grpc.NewServer(
		grpc.ChainStreamInterceptor(
			admissionInterceptor.Stream(),
			grpclogging.StreamRPCStartInterceptor(log.NewEntry(log.StandardLogger())),
			grpc_logrus.StreamServerInterceptor(log.NewEntry(log.StandardLogger()), logOpts...),
			grpc_recovery.StreamServerInterceptor(grpc_recovery.WithRecoveryHandler(grpclogging.LogPanicCallStackFunc)),
			message.ErrorHandlingServerStreamInterceptor(),
		),
		grpc.ChainUnaryInterceptor(
			admissionInterceptor.Unary(),
			grpclogging.UnaryRPCStartInterceptor(log.NewEntry(log.StandardLogger())),
			grpc_logrus.UnaryServerInterceptor(log.NewEntry(log.StandardLogger()), logOpts...),
			grpc_recovery.UnaryServerInterceptor(grpc_recovery.WithRecoveryHandler(grpclogging.LogPanicCallStackFunc)),
			message.ErrorHandlingServerInterceptor(),
		),
	)

	processManager := process.NewLoggingProcessManager(process.NewProcessManager(), nil)

	lockRootPath := agentconfig.GetDefaultLockRootDirectory(runtime.GOOS)
	log.WithField("path", lockRootPath).Info("Using lock root path")

	lockPath := filepath.Join(lockRootPath, lockFileName)
	processLock, err := filelock.NewCrossProcessLock(lockPath, 5*time.Millisecond)
	if err != nil {
		log.WithError(err).Error("failed to create cross-process lock")
		return nil, message.New(message.CommonPathCreationFailed).WithMetadata(map[string]string{
			"path":        lockPath,
			"permissions": fmt.Sprintf("%o", perms.TargetDirPerm),
		}).WithCause(err)
	}

	agentServerAPI := &AgentServerAPI{
		Am:              admissionInterceptor,
		Pm:              processManager,
		LogBuffer:       config.LogBuffer,
		AcceptorFactory: privilege.NewUDSAcceptorFactory(""),
		Fm:              fsutil.NewFSManager(),
		Ftm:             filetransfer.NewFileTransferManager(),
		SystemInfo:      systeminfo.NewSystemInfo(),
		Checker:         privilege.NewChecker(),
		stopCh:          make(chan struct{}),
		cpl:             processLock,
	}

	// Create token storage
	// We'll cleanup root worker whenever storage is empty
	tsCfg := privilege.WithDefaultTokenStorageConfig()
	tsCfg.OnEmpty = func() { agentServerAPI.CleanupRootWorker() }
	ts, err := privilege.NewTokenStorage(tsCfg)
	if err != nil {
		log.WithError(err).Error("failed to create token storage")
		return nil, message.New(message.AgentLifecycleStartupFailed).
			WithCause(err)
	}
	agentServerAPI.TokenStorage = ts

	gs := &GrpcServer{grpcServer: s, listener: lis, portWriter: config.PortWriter, agentServerAPI: agentServerAPI}

	agentServerAPI.ShutdownCb = func(force bool, deadline time.Duration) {
		log.WithField("transport", gs.listener.Name()).Info("gRPC server shutting down")

		if force {
			log.Info("Force stopping gRPC server")
			go func() {
				agentServerAPI.Close()
				gs.Stop(true)
			}()
		} else {
			done := make(chan struct{})

			// Attempt specified shutdown option
			go func() {
				agentServerAPI.Close()
				gs.Stop(false)
				close(done)
			}()

			go func() {
				// If normal shutdown failed, force shutdown after timeout
				select {
				case <-done:
					// Graceful shutdown was successful - don't do anything
				case <-time.After(deadline):
					// Graceful shutdown timed out - force shutdown
					go func() {
						gs.Stop(true)
					}()
				}
			}()
		}
	}
	targetagentproto.RegisterTargetAgentServer(s, agentServerAPI)
	healthproto.RegisterHealthServer(s, NewHealthServer())
	tetherproto.RegisterTetherServer(s, NewTetherServer(defaultTetherDeadline, func(reason string) {
		log.WithField("reason", reason).Info("Tether expired")
		agentServerAPI.ShutdownCb(false, defaultTetherShutdownDeadline)
	}))
	reflection.Register(s)

	return gs, nil
}

func (s *GrpcServer) Start() <-chan error {
	errChan := make(chan error, 1)
	log.WithField("transport", s.listener.Name()).Info("gRPC server started")
	go func() {
		s.writePortFile()
		defer s.removePortFile()

		if err := s.grpcServer.Serve(s.listener); err != nil {
			errChan <- fmt.Errorf("failed to serve: %v", err)
		} else {
			errChan <- nil
		}
	}()
	return errChan
}

func (g *GrpcServer) writePortFile() {
	if g.portWriter != nil {
		if tcp, ok := g.listener.Addr().(*net.TCPAddr); ok {
			if err := g.portWriter.Write(tcp.Port); err != nil {
				log.WithError(err).Warn("failed to write port file")
			}
		}
	}
}

func (g *GrpcServer) removePortFile() {
	if g.portWriter != nil {
		if err := g.portWriter.Remove(); err != nil {
			log.WithError(err).Error("failed to remove port file")
		}
	}
}

func (g *GrpcServer) Stop(force bool) {
	g.removePortFile()
	if force {
		g.grpcServer.Stop()
	} else {
		g.grpcServer.GracefulStop()
	}
	g.agentServerAPI.CleanupRootWorker()
}
