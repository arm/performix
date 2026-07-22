// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestRun(t *testing.T) {
	t.Run("errors when server cannot be run in the background", func(t *testing.T) {
		config := grpcserver.GrpcServerConfig{Host: "0.0.0.0", Port: 1337}
		expectedError := errors.New("🧨")
		runCommand := func(command string, args ...string) error {
			return expectedError
		}

		s := backgroundRunner{runCommand: runCommand}
		err := s.Run(config)

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliServiceServerCannotStartNewServer, msgErr.Code())
		assert.Equal(t, msgErr.Unwrap(), expectedError)
	})

	t.Run("runs daemon in blocking mode", func(t *testing.T) {
		var receivedArgs []string
		runCommand := func(_ string, args ...string) error {
			receivedArgs = args
			return nil
		}
		s := backgroundRunner{runCommand: runCommand}
		_ = s.Run(grpcserver.GrpcServerConfig{})

		assert.Contains(t, strings.Join(receivedArgs, " "), "daemon start --block")
	})

	t.Run("writes daemon started message to configured output", func(t *testing.T) {
		var output bytes.Buffer
		runCommand := func(_ string, args ...string) error {
			return nil
		}
		s := backgroundRunner{runCommand: runCommand, output: &output}

		err := s.Run(grpcserver.GrpcServerConfig{})

		assert.NoError(t, err)
		assert.Contains(t, output.String(), "Daemon process started")
	})

	t.Run("does not write daemon started message in JSON mode", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)
		viper.Set("json", true)
		var output bytes.Buffer
		runCommand := func(_ string, args ...string) error {
			return nil
		}
		s := backgroundRunner{runCommand: runCommand, output: &output}

		err := s.Run(grpcserver.GrpcServerConfig{})

		assert.NoError(t, err)
		assert.Empty(t, output.String())
	})

	t.Run("turns config into cli args", func(t *testing.T) {
		var receivedArgs []string
		runCommand := func(_ string, args ...string) error {
			receivedArgs = args
			return nil
		}

		s := backgroundRunner{runCommand: runCommand}

		assertCalledWithArg := func(t testing.TB, arg string) {
			t.Helper()
			assert.Contains(t, strings.Join(receivedArgs, " "), arg)
		}

		t.Run("handles arguments with values", func(t *testing.T) {
			cases := []struct {
				config          grpcserver.GrpcServerConfig
				argsMustContain string
			}{
				{grpcserver.GrpcServerConfig{Host: "example.com"}, "--server-hostname example.com"},
				{grpcserver.GrpcServerConfig{Port: 123}, "--server-port 123"},
				{grpcserver.GrpcServerConfig{AuthPort: 456}, "--auth-port 456"},
				{grpcserver.GrpcServerConfig{ParallelJobs: 12}, "--jobs 12"},
				{grpcserver.GrpcServerConfig{DataDirectory: "~/some-data-dir"}, "--data-dir ~/some-data-dir"},
				{grpcserver.GrpcServerConfig{LogPath: "/var/log/apxd.log"}, "--log-file /var/log/apxd.log"},
				{grpcserver.GrpcServerConfig{LogLevel: "info"}, "--log-level info"},
				{grpcserver.GrpcServerConfig{DeploymentToolsDir: "/tmp/tools"}, "--deployment-tools-dir /tmp/tools"},
				{grpcserver.GrpcServerConfig{EnableRenderDBSandbox: false}, "--enable-render-db-sandbox=false"},
			}

			for _, test := range cases {
				_ = s.Run(test.config)
				assertCalledWithArg(t, test.argsMustContain)
			}
		})
	})
}
