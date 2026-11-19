// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/featureflags"
	apapenginemocks "github.com/Arm-Debug/apap-cli/apap-engine/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/settings"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

// TestSettingsRPCsUseResolvedConfigDirectory starts the server without an explicit
// config directory and checks that settings are saved in the user's config directory.
func TestSettingsRPCsUseResolvedConfigDirectory(t *testing.T) {
	// Given
	setTestDirs(t)
	workingDirectory := t.TempDir()
	t.Chdir(workingDirectory)
	configDirectory := t.TempDir()
	dataDirectory := t.TempDir()
	envPrefix := strings.ToUpper(terminology.GetEnvVarPrefix())
	t.Setenv(envPrefix+"_CONFIG_DIR", configDirectory)
	t.Setenv(envPrefix+"_DATA_DIR", dataDirectory)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, dialer := apapenginemocks.GetBufferDialerFunc()
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	server := GrpcServer{
		Config: GrpcServerConfig{
			Host:            "127.0.0.1",
			Port:            9000,
			ConfigDirectory: "",
			DataDirectory:   dataDirectory,
			ParallelJobs:    1,
			FeatureFlags:    featureflags.Flags{SecondaryRunPaths: false},
			RunContext:      func() context.Context { return ctx },
		},
		Listen: apapenginemocks.GetBufferListenerFunc(listener),
	}

	manager, err := tlsconfig.NewManager(configDirectory)
	require.NoError(t, err)
	_, err = manager.GenerateServerCredentials(server.Config.Host)
	require.NoError(t, err)
	certificate, err := manager.GenerateClientCertificate("settings-test-client")
	require.NoError(t, err)
	rootPEM, err := manager.RootCertificatePEM()
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(rootPEM))
	connection, err := grpc.NewClient("passthrough://",
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{certificate},
			RootCAs:      roots,
			ServerName:   server.Config.Host,
			MinVersion:   tls.VersionTLS12,
		})),
		grpc.WithContextDialer(dialer),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	client := apapproto.NewApapClient(connection)
	requestCtx, cancelRequest := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelRequest()

	// When
	finished := make(chan error, 1)
	go func() { finished <- server.runServer() }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-finished:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("server did not stop after cancellation")
		}
	})
	_, err = client.SetSetting(requestCtx, &apapproto.Setting{
		Name: settings.ProfilingFocus, Value: structpb.NewStringValue("server"),
	}, grpc.WaitForReady(true))
	actual, readErr := client.GetSetting(requestCtx, &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})

	// Then
	require.NoError(t, err)
	require.NoError(t, readErr)
	assert.Equal(t, "server", actual.Value.GetStringValue())
	data, err := os.ReadFile(filepath.Join(configDirectory, settings.Filename))
	require.NoError(t, err)
	assert.JSONEq(t, `{"profilingFocus":"server"}`, string(data))
	assert.NoFileExists(t, filepath.Join(workingDirectory, settings.Filename))
	assert.NoFileExists(t, filepath.Join(workingDirectory, settings.Filename+".lock"))
}
