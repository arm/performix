// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
)

var setupLogHookMutex sync.Mutex
var isLogHookSetup bool
var globalLogBuffer = grpcserver.NewLogBuffer(512)

// NewStartCmd starts the agent
func NewStartCmd() *cobra.Command {
	var host string
	var port int
	var consoleLog bool
	var portFileDir string

	cmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Start the %v agent on the selected port", terminology.GetProductFullName()),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return setupLogging(consoleLog, cmd.OutOrStdout())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// We run the startControllerAgent(port) inside a goroutine to allow unit testing this (TestStartCmd).
			// On Windows, we cannot simply fire an os.Interrupt signal to stop the grpc server. So instead we use the cobra
			// command execution framework to cancel the test. Outside of unit tests it should work as expected (Ctrl + C to cancel).
			errCh := make(chan error, 1)
			go func() {
				errCh <- startControllerAgent(host, port, portFileDir)
			}()

			select {
			case <-cmd.Context().Done():
				log.Info("Server stopped")
				return nil
			case err := <-errCh:
				return err
			}
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "gRPC server host")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "gRPC server port")
	cmd.Flags().BoolVar(&consoleLog, "console-log", false, "Write logs to console instead of a file")
	cmd.Flags().StringVar(&portFileDir, "port-file-dir", "", "Directory to write the port number file to (default: system temp directory)")

	return cmd
}

// setupLogging creates a logfile named with current date and time. If console is set to true,
// we log to onsole (stdout), otherwise we log to file.
func setupLogging(console bool, out io.Writer) error {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	if console {
		log.SetOutput(out)
	} else {
		logFilename := fmt.Sprintf("%v_%s.log", terminology.GetAgentBinaryName(), timestamp)
		file, err := os.OpenFile(logFilename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, perms.LocalFilePerm)
		if err != nil {
			fmt.Fprintf(out, "Could not open log file %s: %v\n", logFilename, err)
			return err
		}
		log.SetOutput(file)
	}

	// Using mutex/flag rather than sync.Once to allow for error return
	setupLogHookMutex.Lock()
	defer setupLogHookMutex.Unlock()
	if !isLogHookSetup {
		log.SetFormatter(&log.TextFormatter{FullTimestamp: true, TimestampFormat: time.RFC3339})

		// Set up log streaming
		log.AddHook(&grpcserver.LogBufferHook{Buffer: globalLogBuffer})

		isLogHookSetup = true
	}

	return nil
}

type Agent struct {
	server    grpcserver.Server
	sigChan   chan os.Signal
	isStarted atomic.Bool
	modeName  string
}

type ServerFactory func(config grpcserver.GrpcServerConfig) (grpcserver.Server, error)

// NewAgent takes a port and a serverFactory functions, returning an Agent server
func NewAgent(
	transportConfig grpcserver.TransportConfig,
	serverFactory ServerFactory,
	modeName string,
	logBuffer *grpcserver.LogBuffer,
	portWriter grpcserver.PortWriter,
) (*Agent, error) {
	server, err := serverFactory(grpcserver.GrpcServerConfig{
		TransportConfig: transportConfig,
		LogBuffer:       logBuffer,
		PortWriter:      grpcserver.WrapPortWriter(portWriter),
	})
	if err != nil {
		return nil, err
	}
	// Basic handling of SIGTERM to stop the grpcServer.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	return &Agent{server: server, sigChan: sigChan, modeName: modeName}, nil
}

// Run starts the agent's grpc server and listens for SIGTERM or for any errors.
func (a *Agent) Run() error {
	log.Infof("Starting %v agent in mode: %s", terminology.GetProductFullName(), a.modeName)
	errChan := a.server.Start()

	a.isStarted.Store(true)

	select {
	case <-a.sigChan:
		log.Info("Shutdown signal received")
		a.server.Stop(false)
		log.Info("Server stopped")
	case err := <-errChan:
		if err != nil {
			log.WithField("error", err).Error("Server died with error")
			return err
		}
	}
	return nil
}

func (a *Agent) IsStarted() bool {
	return a.isStarted.Load()
}

// startControllerAgent starts the agent server in the background.
func startControllerAgent(host string, port int, portFileDir string) error {
	var serverFactory = func(config grpcserver.GrpcServerConfig) (grpcserver.Server, error) {
		return grpcserver.NewServer(config)
	}

	transportConfig := grpcserver.TCPTransportConfig{
		Host: host,
		Port: port,
	}
	portWriter := &grpcserver.CompositePortWriter{Writers: []grpcserver.PortWriter{
		grpcserver.NewFilePortWriter(portFileDir),
		grpcserver.NewLoggingPortWriter(),
		grpcserver.NewTerminalPortWriter(),
	}}

	agent, err := NewAgent(transportConfig, serverFactory, "controller", globalLogBuffer, portWriter)
	if err != nil {
		log.WithField("error", err).Error("Failed to initialize server")
		return err
	}
	err = agent.Run()
	if err != nil {
		return err
	}
	return nil
}
