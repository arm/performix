// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"net"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
)

func NewStartRootWorkerCmd(
	serverStartedChan chan struct{},
) *cobra.Command {
	var ipcSocket string
	var transportLoggingEnabled = false

	var rootWorkerCmd = &cobra.Command{
		Use:   "start-root-worker",
		Short: fmt.Sprintf("Start the root worker for the %v agent.", terminology.GetProductFullName()),
		Long:  fmt.Sprintf("Start the root worker for the %v agent, communicating via gRPC-over-pipe. This command must be run as root.", terminology.GetProductFullName()),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Windows is not currently supported
			if runtime.GOOS == "windows" {
				return message.New(message.AgentLifecycleRootWorkerUnsupportedPlatform).
					WithMetadata(map[string]string{
						"os":   runtime.GOOS,
						"arch": runtime.GOARCH,
					})
			}

			// Root worker always logs to the console; the caller, group controller,
			// streams logs from it and relays them back to the agent controller.
			return setupLogging(true, cmd.OutOrStdout())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			//// We run startRootWorkerAgent inside a goroutine to allow unit testing this.
			//// On Windows, we cannot simply fire an os.Interrupt signal to stop the grpc server. So instead we use the cobra
			//// command execution framework to cancel the test. Outside of unit tests it should work as expected (Ctrl + C to cancel).
			errCh := make(chan error, 1)
			go func() {
				errCh <- startRootWorkerAgent(ipcSocket, serverStartedChan, transportLoggingEnabled)
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

	rootWorkerCmd.Flags().StringVar(&ipcSocket, "ipc-socket", "", "Path to the IPC socket for communication.")
	rootWorkerCmd.Flags().BoolVar(&transportLoggingEnabled, "transport-logging", transportLoggingEnabled, "Enable logging for the transport layer.")

	return rootWorkerCmd
}

// connectToParentSocket dials the given Unix domain socket path and returns the connection.
// The socket must be owned and created by the parent controller with strict permissions (e.g., 0600).
func connectToParentSocket(socketPath string) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to dial parent socket at %s: %w", socketPath, err)
	}
	return conn, nil
}

// startRootWorkerAgent starts the root worker server in the background.
func startRootWorkerAgent(
	ipcSocket string,
	serverStartedChan chan struct{},
	transportLoggingEnabled bool,
) error {
	var serverFactory = func(config grpcserver.GrpcServerConfig) (grpcserver.Server, error) {
		return grpcserver.NewServer(config)
	}

	// The model here is that the root worker connects to the parent controller, instead of opening a listener
	// in the child. This is better for security because it avoids exposing a socket that could be misused by other
	// processes. Additionally, it allows the parent to verify that the child is indeed the root worker via
	// the use of Unix domain sockets with strict permissions and UID checks: Only a process running as root
	// can connect to the socket and dispatch requests from the parent controller.
	conn, err := connectToParentSocket(ipcSocket)
	if err != nil {
		log.WithField("error", err).Error("Failed to connect to parent socket")
		return err
	}

	// Once established, we have a full duplex connection to the parent controller and we invert the model:
	// the root worker is the gRPC server and the parent controller is the client. This allows the root worker
	// to appear as a regular target agent, with the same API as the main controller.
	transportConfig := grpcserver.EstablishedConnTransportConfig{
		Conn:           conn,
		LoggingEnabled: transportLoggingEnabled,
	}

	agent, err := NewAgent(transportConfig, serverFactory, "root-worker", globalLogBuffer, grpcserver.NewNullPortWriter())
	if err != nil {
		log.WithField("error", err).Error("Failed to initialize server")
		return err
	}

	if serverStartedChan != nil {
		// Poll every few ms until the server is started
		go func() {
			for !agent.IsStarted() {
				time.Sleep(10 * time.Millisecond)
			}
			log.Info("Root worker server started successfully")
			serverStartedChan <- struct{}{}
			close(serverStartedChan)
		}()
	}

	err = agent.Run()
	if err != nil {
		log.WithError(err).Error("Root worker server failed to run")
		return err
	}
	return nil
}
