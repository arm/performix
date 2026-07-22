// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package grpcserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	apapenginemocks "github.com/Arm-Debug/apap-cli/apap-engine/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/authproto"
)

func newTestServer(t *testing.T) (GrpcServer, *bufconn.Listener, *bufconn.Listener, context.CancelFunc) {
	t.Helper()
	apapListener, _ := apapenginemocks.GetBufferDialerFunc()
	authListener, _ := apapenginemocks.GetBufferDialerFunc()
	ctx, cancel := context.WithCancel(context.Background())
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:                    "127.0.0.1",
			Port:                    8080,
			AuthPort:                8081,
			DataDirectory:           t.TempDir(),
			ConfigDirectory:         t.TempDir(),
			ParallelJobs:            1,
			LogLevel:                "debug",
			LogPath:                 "stdout",
			RunContext:              func() context.Context { return ctx },
			EnableSecondaryRunPaths: false,
		},
		Listen:    apapenginemocks.GetBufferListenerFunc(apapListener),
		ListenTLS: apapenginemocks.GetBufferListenerFunc(authListener),
	}
	return server, apapListener, authListener, cancel
}

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func TestRunBlocking(t *testing.T) {
	runGrpcServer := func(grpcServer GrpcServer, waitGroup *sync.WaitGroup) {
		defer waitGroup.Done()
		_ = grpcServer.RunBlocking()
	}

	t.Run("Killing with sigterm logs shutdown message and closes all go routines", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		hook := newLogHook(t)

		grpcServer, _, _, cancel := newTestServer(t)
		defer cancel()

		var waitGroup sync.WaitGroup
		waitGroup.Add(1)

		go runGrpcServer(grpcServer, &waitGroup)
		time.Sleep(time.Second)

		cancel()
		waitGroup.Wait()

		assertLogContains(t, hook, "Attempting graceful shutdown")
	})

	t.Run("Killing with sigterm cleans up pidfile and closes all go routines", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		grpcServer, _, _, cancel := newTestServer(t)
		defer cancel()

		var waitGroup sync.WaitGroup
		waitGroup.Add(1)

		go runGrpcServer(grpcServer, &waitGroup)
		time.Sleep(time.Second)

		pidfile, err := pidfiles.ConstructPidFilePath(grpcServer.Config.Host, grpcServer.Config.Port)
		assert.NoError(t, err)
		assert.FileExists(t, pidfile)

		cancel()
		waitGroup.Wait()

		assert.NoFileExists(t, pidfile)
	})

	t.Run("Killing with shutdown endpoint shuts down gracefully and closes all go routines", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		hook := newLogHook(t)

		grpcServer, listener, _, cancel := newTestServer(t)
		defer cancel()

		conn, err := grpc.NewClient(
			"passthrough://", // passthrough will use the buffer listener to connect to the server
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return listener.Dial()
			}))
		assert.NoError(t, err)
		defer conn.Close()

		apapClient := apapproto.NewApapClient(conn)
		clientCtx, clientCancel := context.WithCancel(context.Background())
		defer clientCancel()

		var waitGroup sync.WaitGroup
		waitGroup.Add(1)

		go runGrpcServer(grpcServer, &waitGroup)
		time.Sleep(time.Second)

		_, err = apapClient.Shutdown(clientCtx, &emptypb.Empty{})
		require.NoError(t, err)
		cancel()
		waitGroup.Wait()

		assertLogContains(t, hook, "Attempting graceful shutdown")
	})

	t.Run("Auth service accepts mutual TLS clients", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skipf("TLS auth client test only supported on linux, skipping on %s", runtime.GOOS)
		}
		defer goleak.VerifyNone(t)
		grpcServer, _, authListener, serverCancel := newTestServer(t)
		defer serverCancel()

		var waitGroup sync.WaitGroup
		waitGroup.Add(1)
		go runGrpcServer(grpcServer, &waitGroup)
		time.Sleep(500 * time.Millisecond)

		manager, err := tlsconfig.NewManager(grpcServer.Config.ConfigDirectory)
		require.NoError(t, err)
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer waitCancel()
		require.NoError(t, tlsconfig.WaitForAuthority(waitCtx, grpcServer.Config.ConfigDirectory, tlsconfig.DefaultPollInterval))
		clientCert, err := manager.GenerateClientCertificate("test-client")
		require.NoError(t, err)
		rootPEM, err := manager.RootCertificatePEM()
		require.NoError(t, err)
		pool := x509.NewCertPool()
		require.True(t, pool.AppendCertsFromPEM(rootPEM))
		tlsCreds := credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      pool,
			ServerName:   grpcServer.Config.Host,
			MinVersion:   tls.VersionTLS12,
		})

		conn, err := grpc.NewClient(
			"passthrough://",
			grpc.WithTransportCredentials(tlsCreds),
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return authListener.Dial()
			}),
		)
		require.NoError(t, err)
		defer conn.Close()

		authClient := authproto.NewAuthClient(conn)
		stream, err := authClient.TargetLogin(context.Background())
		require.NoError(t, err)
		require.NoError(t, stream.Send(&authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{
					Target: &apapproto.Target{Connection: &apapproto.Target_LocalConfig{LocalConfig: &apapproto.LocalConnectionConfig{}}},
				},
			},
		}))
		serverMsg, err := stream.Recv()
		require.NoError(t, err)
		resp := serverMsg.GetResponse()
		require.NotNil(t, resp)
		require.Equal(t, apapproto.StatusCode_SUCCESS, resp.GetReturnCode())
		serverCancel()
		waitGroup.Wait()
	})

	t.Run("Auth service starts within timeout", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		grpcServer, _, authListener, serverCancel := newTestServer(t)
		defer serverCancel()

		var waitGroup sync.WaitGroup
		waitGroup.Add(1)
		go runGrpcServer(grpcServer, &waitGroup)

		manager, err := tlsconfig.NewManager(grpcServer.Config.ConfigDirectory)
		require.NoError(t, err)

		// 3 seconds should be enough to start the auth service
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, tlsconfig.WaitForAuthority(ctx, grpcServer.Config.ConfigDirectory, tlsconfig.DefaultPollInterval))

		clientCert, err := manager.GenerateClientCertificate("test-client")
		require.NoError(t, err)
		rootPEM, err := manager.RootCertificatePEM()
		require.NoError(t, err)
		pool := x509.NewCertPool()
		require.True(t, pool.AppendCertsFromPEM(rootPEM))

		tlsCreds := credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      pool,
			ServerName:   grpcServer.Config.Host,
			MinVersion:   tls.VersionTLS12,
		})

		start := time.Now()
		conn, err := grpc.NewClient(
			"passthrough://",
			grpc.WithTransportCredentials(tlsCreds),
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return authListener.Dial()
			}),
		)
		require.NoError(t, err)
		conn.Close()
		require.LessOrEqual(t, time.Since(start), 3*time.Second)
		serverCancel()
		waitGroup.Wait()
	})
}

func TestRunServerDelaysListeningUntilTLSReady(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	configDir := t.TempDir()
	host := "127.0.0.1"
	port := 20500

	rootCertPath := filepath.Join(configDir, tlsconfig.DirectoryName, "root_ca.pem")
	rootKeyPath := filepath.Join(configDir, tlsconfig.DirectoryName, "root_ca.key")

	mainListener := bufconn.Listen(apapenginemocks.BufferSize)
	authListener := bufconn.Listen(apapenginemocks.BufferSize)
	t.Cleanup(func() {
		_ = mainListener.Close()
		_ = authListener.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu         sync.Mutex
		mainCalled bool
		authCalled bool
	)

	listen := func(network, address string) (net.Listener, error) {
		if _, err := os.Stat(rootCertPath); err != nil {
			return nil, fmt.Errorf("root certificate not present before main listener started: %w", err)
		}
		if _, err := os.Stat(rootKeyPath); err != nil {
			return nil, fmt.Errorf("root key not present before main listener started: %w", err)
		}
		mu.Lock()
		mainCalled = true
		mu.Unlock()
		return mainListener, nil
	}

	listenTLS := func(network, address string) (net.Listener, error) {
		if _, err := os.Stat(rootCertPath); err != nil {
			return nil, fmt.Errorf("root certificate not present before auth listener started: %w", err)
		}
		if _, err := os.Stat(rootKeyPath); err != nil {
			return nil, fmt.Errorf("root key not present before auth listener started: %w", err)
		}
		mu.Lock()
		authCalled = true
		mu.Unlock()
		return authListener, nil
	}

	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            host,
			Port:            port,
			AuthPort:        port + 1,
			ConfigDirectory: configDir,
			LogPath:         "stdout",
			RunContext:      func() context.Context { return ctx },
		},
		Listen:    listen,
		ListenTLS: listenTLS,
	}

	done := make(chan error, 1)
	go func() {
		done <- server.runServer()
	}()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return mainCalled && authCalled
	}, 5*time.Second, 10*time.Millisecond)

	cancel()

	require.NoError(t, <-done)
}

func TestNewgrpcServer(t *testing.T) {
	config := GrpcServerConfig{
		Host:            "some-host",
		Port:            8000,
		AuthPort:        8001,
		DataDirectory:   "/some-data-dir",
		ConfigDirectory: t.TempDir(),
		ParallelJobs:    1,
	}

	grpcServer := NewGrpcServer(config)

	assert.Equal(t, config.Host, grpcServer.Config.Host)
	assert.Equal(t, config.Port, grpcServer.Config.Port)
	assert.Equal(t, config.AuthPort, grpcServer.Config.AuthPort)
	assert.Equal(t, config.DataDirectory, grpcServer.Config.DataDirectory)
	assert.Equal(t, config.ParallelJobs, grpcServer.Config.ParallelJobs)
}

func TestCacheInitialisation(t *testing.T) {
	t.Run("Cache is initialised when configured to do so", func(t *testing.T) {
		server, _, _, cancel := newTestServer(t)
		server.Config.DataDirectory = t.TempDir()
		var waitGroup sync.WaitGroup
		waitGroup.Add(1)
		go func() { defer waitGroup.Done(); _ = server.RunBlocking() }()
		time.Sleep(2 * time.Second)
		cancel()
		waitGroup.Wait()
	})

	t.Run("Cache is not initialised when not configured to do so", func(t *testing.T) {
		server, _, _, cancel := newTestServer(t)
		server.Config.DataDirectory = t.TempDir()
		var waitGroup sync.WaitGroup
		waitGroup.Add(1)
		go func() { defer waitGroup.Done(); _ = server.RunBlocking() }()
		time.Sleep(250 * time.Millisecond)
		cancel()
		waitGroup.Wait()
	})
}

func TestNonBlocking(t *testing.T) {
	var serverCancels []context.CancelFunc
	t.Cleanup(func() {
		for _, cancel := range serverCancels {
			cancel()
		}
	})
	buildGrpcServer := func(host string, port int) GrpcServer {
		server, _, _, cancel := newTestServer(t)
		serverCancels = append(serverCancels, cancel)
		server.Config.Host = host
		server.Config.Port = port
		server.Config.AuthPort = port + 1
		return server
	}

	t.Run("Attempting to start a server when already started should fail", func(t *testing.T) {
		host := "some.host"
		port := 80
		err := pidfiles.SavePid(1111, host, port)
		defer pidfiles.DeletePid(host, port)

		assert.NoError(t, err, "Cannot write PID file")

		grpcServer := buildGrpcServer(host, port)
		go func() {
			err = grpcServer.RunBlocking()
			assert.NoError(t, err, "daemon not started when expected to")
		}()

		time.Sleep(250 * time.Millisecond)

		last := serverCancels[len(serverCancels)-1]
		last()
	})
}

func TestRunBlockingWritesLogsToConfiguredFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	host := "127.0.0.1"
	port := 19500
	logPath := filepath.Join(t.TempDir(), "daemon.log")
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            host,
			Port:            port,
			AuthPort:        port + 1,
			LogLevel:        "debug",
			LogPath:         logPath,
			ConfigDirectory: t.TempDir(),
			RunContext:      func() context.Context { return context.Background() },
		},
		Listen: func(string, string) (net.Listener, error) {
			return nil, fmt.Errorf("listen failure")
		},
	}
	t.Cleanup(func() {
		pidfiles.DeletePid(host, port)
		log.SetOutput(io.Discard)
	})

	err := server.RunBlocking()
	require.Error(t, err)

	require.FileExists(t, logPath)
	contents, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(contents), fmt.Sprintf("Launching %v daemon", terminology.GetProductFullName()))
}

func TestRunBlockingWarnsOnInvalidLogLevel(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	host := "127.0.0.1"
	port := 19600
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            host,
			Port:            port,
			AuthPort:        port + 1,
			LogLevel:        "Trace",
			LogPath:         "stdout",
			ConfigDirectory: t.TempDir(),
			RunContext:      func() context.Context { return context.Background() },
		},
		Listen: func(string, string) (net.Listener, error) {
			return nil, fmt.Errorf("listen failure")
		},
	}
	t.Cleanup(func() {
		pidfiles.DeletePid(host, port)
	})

	stderr := captureStderr(t, func() {
		_ = server.RunBlocking()
	})

	require.Contains(t, stderr, "Could not set log level")
}

func TestRunBlockingWarnsWhenLogFileCannotBeCreated(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	host := "127.0.0.1"
	port := 19700
	blocked := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("file"), 0o600))
	logPath := filepath.Join(blocked, "apap.log")
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            host,
			Port:            port,
			AuthPort:        port + 1,
			LogLevel:        "info",
			LogPath:         logPath,
			ConfigDirectory: t.TempDir(),
			RunContext:      func() context.Context { return context.Background() },
		},
		Listen: func(string, string) (net.Listener, error) {
			return nil, fmt.Errorf("listen failure")
		},
	}
	t.Cleanup(func() {
		pidfiles.DeletePid(host, port)
	})

	stderr := captureStderr(t, func() {
		_ = server.RunBlocking()
	})

	require.Contains(t, stderr, "Could not set log file")
}

func newLogHook(t *testing.T) *logtest.Hook {
	hook := logtest.NewGlobal()
	t.Cleanup(func() {
		hook.Reset()
	})
	return hook
}

func assertLogContains(t *testing.T, hook *logtest.Hook, substr string) {
	t.Helper()
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, substr) {
			return
		}
	}
	t.Fatalf("expected log containing %q", substr)
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	original := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() {
		os.Stderr = original
	}()

	run()

	require.NoError(t, w.Close())
	output, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return string(output)
}
