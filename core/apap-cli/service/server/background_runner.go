// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-engine/daemon"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

type commandRunner func(command string, args ...string) error

type backgroundRunner struct {
	runCommand commandRunner
	output     io.Writer
}

func (s backgroundRunner) Run(config grpcserver.GrpcServerConfig) error {
	apapExe := os.Args[0]
	args := append([]string{"daemon", "start", "--block"}, serverConfigToArgs(config)...)
	err := s.runCommand(apapExe, args...)
	if err != nil {
		return message.New(message.CliServiceServerCannotStartNewServer).WithCause(err)
	}
	if s.output != nil && !viper.GetBool("json") {
		fmt.Fprintf(s.output, "Daemon process started; to stop call `%v daemon stop`.\n", terminology.GetProductBinaryName())
	}
	return nil
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
		fmt.Sprintf("--enable-render-db-sandbox=%t", config.EnableRenderDBSandbox),
	}
	return args
}

func NewBackgroundRunner() backgroundRunner {
	return NewBackgroundRunnerWithOutput(os.Stdout)
}

func NewBackgroundRunnerWithOutput(output io.Writer) backgroundRunner {
	return backgroundRunner{
		runCommand: daemonStartDiscardPid,
		output:     output,
	}
}

func daemonStartDiscardPid(commandName string, args ...string) error {
	_, err := daemon.NewDaemon().Start(commandName, args)
	return err
}
