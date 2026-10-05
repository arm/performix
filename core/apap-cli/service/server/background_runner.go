// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/daemon"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcconnection"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const daemonReadinessTimeout = 10 * time.Second

type startedProcess interface {
	PID() int
	KillAndWait() error
}

type commandRunner func(command string, args ...string) (startedProcess, error)

type backgroundRunner struct {
	runCommand commandRunner
	output     io.Writer
}

func (s backgroundRunner) Run(config grpcserver.GrpcServerConfig) error {
	apapExe := os.Args[0]
	args := append([]string{"daemon", "start", "--block"}, serverConfigToArgs(config)...)
	process, err := s.runCommand(apapExe, args...)
	if err != nil {
		return message.New(message.CliServiceServerCannotStartNewServer).WithCause(err)
	}
	if err := waitForDaemonReady(config); err != nil {
		return message.Join(message.CliServiceServerCannotStartNewServer, append([]error{err}, cleanupStartedDaemon(process, config)...)...)
	}
	if err := checkStartedDaemonPidFile(process.PID(), config); err != nil {
		return message.Join(message.CliServiceServerCannotStartNewServer, append([]error{err}, cleanupStartedDaemon(process, config)...)...)
	}
	if s.output != nil && !viper.GetBool("json") {
		fmt.Fprintf(s.output, "Daemon process started; to stop call `%v daemon stop`.\n", terminology.GetProductBinaryName())
	}
	return nil
}

func cleanupStartedDaemon(process startedProcess, config grpcserver.GrpcServerConfig) []error {
	if process == nil {
		return nil
	}
	return []error{process.KillAndWait(), deleteStartedDaemonPidFile(process.PID(), config.Host, config.Port)}
}

func checkStartedDaemonPidFile(pid int, config grpcserver.GrpcServerConfig) error {
	pidfile, err := pidfiles.ConstructPidFilePath(config.Host, config.Port)
	if err != nil {
		return err
	}
	savedPid, err := pidfiles.GetPid(pidfile)
	if err != nil {
		return fmt.Errorf("started daemon did not create PID file %q: %w", pidfile, err)
	}
	if savedPid != pid {
		return fmt.Errorf("started daemon PID %d does not match PID file %q containing %d", pid, pidfile, savedPid)
	}
	return nil
}

func deleteStartedDaemonPidFile(pid int, host string, port int) error {
	if pid <= 0 {
		return nil
	}
	pidfile, err := pidfiles.ConstructPidFilePath(host, port)
	if err != nil {
		return err
	}
	savedPid, err := pidfiles.GetPid(pidfile)
	if err != nil || savedPid != pid {
		return nil
	}
	if err := os.Remove(pidfile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func waitForDaemonReady(config grpcserver.GrpcServerConfig) error {
	conn, err := grpcconnection.Connect(daemonReadinessHost(config.Host), config.Port, daemonReadinessTimeout, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), daemonReadinessTimeout)
	defer cancel()
	_, err = apapproto.NewApapClient(conn).GetVersion(ctx, &emptypb.Empty{})
	return err
}

func daemonReadinessHost(host string) string {
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsUnspecified() {
		return host
	}
	if ip.To4() != nil {
		return "127.0.0.1"
	}
	return "::1"
}

func serverConfigToArgs(config grpcserver.GrpcServerConfig) []string {
	args := []string{
		"--server-hostname", config.Host,
		"--server-port", fmt.Sprint(config.Port),
		"--auth-port", fmt.Sprint(config.AuthPort),
		"--http-port", fmt.Sprint(config.HttpPort),
		"--http-chunk-bytes", fmt.Sprint(config.HttpChunkBytes),
		"--jobs", fmt.Sprint(config.ParallelJobs),
		"--data-dir", fmt.Sprint(config.DataDirectory),
		"--log-level", fmt.Sprint(config.LogLevel),
		"--log-file", fmt.Sprint(config.LogPath),
		"--deployment-tools-dir", fmt.Sprint(config.DeploymentToolsDir),
		"--adb-path", fmt.Sprint(config.ADBPath),
		fmt.Sprintf("--enable-render-db-sandbox=%t", config.EnableRenderDBSandbox),
		fmt.Sprintf("--enable-jfr-capture=%t", config.EnableJfrCapture),
	}
	return args
}

func NewBackgroundRunner() backgroundRunner {
	return NewBackgroundRunnerWithOutput(os.Stdout)
}

func NewBackgroundRunnerWithOutput(output io.Writer) backgroundRunner {
	return backgroundRunner{
		runCommand: daemonStart,
		output:     output,
	}
}

// NewAttachedBackgroundRunnerWithOutput starts a background server in the
// caller's process group, so signals sent to that group also reach the server.
func NewAttachedBackgroundRunnerWithOutput(output io.Writer) backgroundRunner {
	return backgroundRunner{
		runCommand: daemonStartAttached,
		output:     output,
	}
}

func daemonStart(commandName string, args ...string) (startedProcess, error) {
	return daemon.NewDaemon().StartProcess(commandName, args)
}

func daemonStartAttached(commandName string, args ...string) (startedProcess, error) {
	return daemon.NewDaemon().StartAttachedProcess(commandName, args)
}
