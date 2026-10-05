// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const localhost = "127.0.0.1"

func TestRun(t *testing.T) {
	t.Run("errors when server cannot be run in the background", func(t *testing.T) {
		config := grpcserver.GrpcServerConfig{Host: "0.0.0.0", Port: 1337}
		expectedError := errors.New("🧨")
		runCommand := func(command string, args ...string) (startedProcess, error) {
			return nil, expectedError
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
		expectedError := errors.New("stop before readiness check")
		runCommand := func(_ string, args ...string) (startedProcess, error) {
			receivedArgs = args
			return nil, expectedError
		}
		s := backgroundRunner{runCommand: runCommand}
		_ = s.Run(grpcserver.GrpcServerConfig{})

		assert.Contains(t, strings.Join(receivedArgs, " "), "daemon start --block")
	})

	t.Run("writes daemon started message to configured output", func(t *testing.T) {
		setTestStateDir(t)
		var output bytes.Buffer
		config, runCommand := newReadyServerCommand(t)
		s := backgroundRunner{runCommand: runCommand, output: &output}

		err := s.Run(config)

		assert.NoError(t, err)
		assert.Contains(t, output.String(), "Daemon process started")
	})

	t.Run("errors when readiness is answered by an existing daemon", func(t *testing.T) {
		setTestStateDir(t)
		var output bytes.Buffer
		config, existingServerPid, runReadyServer := newReadyServerCommandWithPid(t)
		waitForExit := make(chan struct{})
		runCommand := newBlockingProcessCommand(t, waitForExit, func(pid int) {
			require.NotEqual(t, existingServerPid, pid)
			require.NoError(t, pidfiles.SavePid(existingServerPid, config.Host, config.Port))
			runReadyServer()
		})
		s := backgroundRunner{runCommand: runCommand, output: &output}

		err := s.Run(config)

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliServiceServerCannotStartNewServer, msgErr.Code())
		assert.NotContains(t, output.String(), "Daemon process started")
		require.Eventually(t, func() bool {
			select {
			case <-waitForExit:
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond)
		pidfile, err := pidfiles.ConstructPidFilePath(config.Host, config.Port)
		require.NoError(t, err)
		savedPid, err := pidfiles.GetPid(pidfile)
		require.NoError(t, err)
		assert.Equal(t, existingServerPid, savedPid)
	})

	t.Run("errors when daemon does not become ready", func(t *testing.T) {
		setTestStateDir(t)
		listener := newTestListener(t)
		port := listener.Addr().(*net.TCPAddr).Port
		config := grpcserver.GrpcServerConfig{Host: localhost, Port: port}
		pidfile, err := pidfiles.ConstructPidFilePath(config.Host, config.Port)
		require.NoError(t, err)
		// Serve gRPC without registering APAP so Connect succeeds quickly, but
		// GetVersion fails without waiting for the full readiness timeout.
		server := grpc.NewServer()
		done := make(chan struct{})
		t.Cleanup(func() {
			server.Stop()
			<-done
		})
		waitForExit := make(chan struct{})
		runCommand := newBlockingProcessCommand(t, waitForExit, func(pid int) {
			require.NoError(t, pidfiles.SavePid(pid, config.Host, config.Port))
			go func() {
				defer close(done)
				_ = server.Serve(listener)
			}()
		})
		s := backgroundRunner{runCommand: runCommand}

		err = s.Run(config)

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliServiceServerCannotStartNewServer, msgErr.Code())
		require.Eventually(t, func() bool {
			select {
			case <-waitForExit:
				return true
			default:
				return false
			}
		}, 5*time.Second, 100*time.Millisecond)
		assert.NoFileExists(t, pidfile)
	})

	t.Run("does not write daemon started message in JSON mode", func(t *testing.T) {
		setTestStateDir(t)
		viper.Reset()
		t.Cleanup(viper.Reset)
		viper.Set("json", true)
		var output bytes.Buffer
		config, runCommand := newReadyServerCommand(t)
		s := backgroundRunner{runCommand: runCommand, output: &output}

		err := s.Run(config)

		assert.NoError(t, err)
		assert.Empty(t, output.String())
	})

	t.Run("turns config into cli args", func(t *testing.T) {
		var receivedArgs []string
		expectedError := errors.New("stop before readiness check")
		runCommand := func(_ string, args ...string) (startedProcess, error) {
			receivedArgs = args
			return nil, expectedError
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

func TestDaemonReadinessHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "keeps concrete IPv4", host: "127.0.0.1", want: "127.0.0.1"},
		{name: "keeps hostnames", host: "localhost", want: "localhost"},
		{name: "maps IPv4 wildcard to loopback", host: "0.0.0.0", want: "127.0.0.1"},
		{name: "maps IPv6 wildcard to loopback", host: "::", want: "::1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, daemonReadinessHost(test.host))
		})
	}
}

func newTestListener(t testing.TB) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", localhost+":0")
	require.NoError(t, err)
	return listener
}

func setTestStateDir(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", t.TempDir())
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

type fakeStartedProcess struct {
	pid         int
	killAndWait func() error
}

func (p fakeStartedProcess) PID() int {
	return p.pid
}

func (p fakeStartedProcess) KillAndWait() error {
	if p.killAndWait == nil {
		return nil
	}
	return p.killAndWait()
}

func newBlockingProcessCommand(t testing.TB, waitForExit chan struct{}, afterStart func(pid int)) commandRunner {
	t.Helper()
	return func(_ string, args ...string) (startedProcess, error) {
		cmd := exec.Command(os.Args[0], "-test.run=TestBackgroundRunnerHelperProcess") //nolint:gosec // Test helper re-execs the current test binary with fixed args.
		cmd.Env = append(os.Environ(), "BACKGROUND_RUNNER_HELPER_PROCESS=1")
		require.NoError(t, cmd.Start())
		if afterStart != nil {
			afterStart(cmd.Process.Pid)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			select {
			case <-waitForExit:
			case <-time.After(5 * time.Second):
			}
		})
		go func() {
			_ = cmd.Wait()
			close(waitForExit)
		}()
		return fakeStartedProcess{
			pid: cmd.Process.Pid,
			killAndWait: func() error {
				_ = cmd.Process.Kill()
				select {
				case <-waitForExit:
				case <-time.After(5 * time.Second):
				}
				return nil
			},
		}, nil
	}
}

func TestBackgroundRunnerHelperProcess(t *testing.T) {
	if os.Getenv("BACKGROUND_RUNNER_HELPER_PROCESS") != "1" {
		return
	}
	select {}
}

func newReadyServerCommand(t testing.TB) (grpcserver.GrpcServerConfig, commandRunner) {
	t.Helper()
	config, pid, runServer := newReadyServerCommandWithPid(t)
	runCommand := func(_ string, args ...string) (startedProcess, error) {
		require.NoError(t, pidfiles.SavePid(pid, config.Host, config.Port))
		runServer()
		return fakeStartedProcess{pid: pid}, nil
	}
	return config, runCommand
}

func newReadyServerCommandWithPid(t testing.TB) (grpcserver.GrpcServerConfig, int, func()) {
	t.Helper()
	listener := newTestListener(t)
	server := grpc.NewServer()
	apapproto.RegisterApapServer(server, testApapServer{})
	done := make(chan struct{})
	t.Cleanup(func() {
		server.Stop()
		<-done
	})

	runServer := func() {
		go func() {
			defer close(done)
			_ = server.Serve(listener)
		}()
	}

	port := listener.Addr().(*net.TCPAddr).Port
	return grpcserver.GrpcServerConfig{Host: localhost, Port: port}, os.Getpid(), runServer
}

type testApapServer struct {
	apapproto.UnimplementedApapServer
}

func (testApapServer) GetVersion(context.Context, *emptypb.Empty) (*apapproto.ServiceVersion, error) {
	return &apapproto.ServiceVersion{Version: "test-version"}, nil
}

func TestBackgroundRunnerForwardsJfrCapture(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			var receivedArgs []string
			stop := errors.New("stop before starting a daemon")
			runner := backgroundRunner{runCommand: func(_ string, args ...string) (startedProcess, error) {
				receivedArgs = args
				return nil, stop
			}}
			require.ErrorIs(t, runner.Run(grpcserver.GrpcServerConfig{EnableJfrCapture: enabled}), stop)
			assert.Contains(t, receivedArgs, fmt.Sprintf("--enable-jfr-capture=%t", enabled))
		})
	}
}
