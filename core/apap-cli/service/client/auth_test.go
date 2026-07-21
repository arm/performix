// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/authproto"
)

type noopAuthServer struct {
	authproto.UnimplementedAuthServer
}

func (s *noopAuthServer) TargetLogin(stream authproto.Auth_TargetLoginServer) error {
	msg, err := stream.Recv()
	if err != nil {
		return err
	}
	if msg.GetRequest() == nil {
		return context.Canceled
	}
	return stream.Send(&authproto.TargetLoginServerMessage{
		Message: &authproto.TargetLoginServerMessage_Response{
			Response: &authproto.TargetLoginResponse{ReturnCode: apapproto.StatusCode_SUCCESS},
		},
	})
}

func TestAuthenticateWithAuthService(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), tmpDir)
	server, port := startAuthServer(t, tmpDir, 0)
	t.Cleanup(func() {
		server.GracefulStop()
	})
	t.Cleanup(resetAuthClientCertificateCache)

	client, conn, err := AuthenticateWithAuthService("127.0.0.1", port)
	require.NoError(t, err)
	defer conn.Close()
	stream, err := client.TargetLogin(context.Background())
	require.NoError(t, err)
	require.NoError(t, stream.Send(&authproto.TargetLoginClientMessage{
		Message: &authproto.TargetLoginClientMessage_Request{
			Request: &authproto.TargetLoginRequest{
				Target: &apapproto.Target{},
			},
		},
	}))
	resp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, resp.GetResponse())
}

func TestAuthenticateWithAuthServiceFailsWhenPortClosed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), tmpDir)
	manager, err := tlsconfig.NewManager(tmpDir)
	require.NoError(t, err)
	_, err = manager.GenerateServerCredentials("127.0.0.1")
	require.NoError(t, err)
	t.Cleanup(resetAuthClientCertificateCache)
	client, conn, err := AuthenticateWithAuthService("127.0.0.1", 65000)
	require.Error(t, err)
	require.Nil(t, client)
	require.Nil(t, conn)
}

func TestAuthenticateWithAuthServiceCachesCertificate(t *testing.T) {
	// Ensures EnsureAuthClientCertificates reuses the in-process TLS certificate for repeated
	// calls within the same CLI instance, avoiding unnecessary regenerations.
	tmpDir := t.TempDir()
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), tmpDir)
	server, _ := startAuthServer(t, tmpDir, 0)
	t.Cleanup(func() { server.GracefulStop() })
	t.Cleanup(resetAuthClientCertificateCache)

	require.NoError(t, EnsureAuthClientCertificates())
	cacheKey := filepath.Clean(filepath.Join(tmpDir, tlsDirectoryName))
	clientCertCacheMu.Lock()
	initial := clientCertCache[cacheKey]
	clientCertCacheMu.Unlock()
	require.NotNil(t, initial)

	require.NoError(t, EnsureAuthClientCertificates())
	clientCertCacheMu.Lock()
	cached := clientCertCache[cacheKey]
	clientCertCacheMu.Unlock()
	require.NotNil(t, cached)
	require.Equal(t, initial, cached)
}

func TestAuthenticateWithAuthServiceRegeneratesWhenAuthorityChanges(t *testing.T) {
	// Verifies that the process-local cache is refreshed when the underlying root authority
	// rotates (e.g., engine restart), so clients seamlessly pick up new credentials.
	tmpDir := t.TempDir()
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), tmpDir)
	server, _ := startAuthServer(t, tmpDir, 0)
	t.Cleanup(resetAuthClientCertificateCache)

	require.NoError(t, EnsureAuthClientCertificates())
	cacheKey := filepath.Clean(filepath.Join(tmpDir, tlsDirectoryName))
	clientCertCacheMu.Lock()
	initial := clientCertCache[cacheKey]
	initialFingerprint := clientCertRootFingerprint[cacheKey]
	clientCertCacheMu.Unlock()
	require.NotNil(t, initial)

	server.GracefulStop()
	server, _ = startAuthServer(t, tmpDir, 0)
	t.Cleanup(func() { server.GracefulStop() })

	manager := newTLSManager(t, tmpDir)
	rootPEM, err := manager.RootCertificatePEM()
	require.NoError(t, err)
	block, _ := pem.Decode(rootPEM)
	require.NotNil(t, block)
	_, err = x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	// Remove the existing root authority material to force regeneration on the next call.
	require.NoError(t, os.Remove(filepath.Join(tmpDir, "tls", "root_ca.pem")))
	require.NoError(t, os.Remove(filepath.Join(tmpDir, "tls", "root_ca.key")))
	_, err = manager.GenerateServerCredentials("127.0.0.1")
	require.NoError(t, err)

	require.NoError(t, EnsureAuthClientCertificates())
	clientCertCacheMu.Lock()
	rotated := clientCertCache[cacheKey]
	rotatedFingerprint := clientCertRootFingerprint[cacheKey]
	clientCertCacheMu.Unlock()
	require.NotNil(t, rotated)
	require.NotEqual(t, initial, rotated)
	require.NotEqual(t, initialFingerprint, rotatedFingerprint)
}

func TestAuthClientCertificatesReusedAcrossEngines(t *testing.T) {
	// Validates that a single CLI process reuses its cached client certificate even when
	// connecting to multiple engine instances sharing the same root CA.
	tmpDir := t.TempDir()
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), tmpDir)
	resetAuthClientCertificateCache()

	server, portOne := startAuthServer(t, tmpDir, 0)
	t.Cleanup(func() { server.GracefulStop() })

	_, connOne, err := AuthenticateWithAuthService("127.0.0.1", portOne)
	require.NoError(t, err)
	connOne.Close()

	cacheKey := filepath.Clean(filepath.Join(tmpDir, tlsDirectoryName))
	clientCertCacheMu.Lock()
	firstCert := clientCertCache[cacheKey]
	clientCertCacheMu.Unlock()
	require.NotNil(t, firstCert)

	manager := newTLSManager(t, tmpDir)
	rootBefore, err := manager.RootCertificatePEM()
	require.NoError(t, err)

	serverTwo, portTwo := startAuthServer(t, tmpDir, 0)
	t.Cleanup(func() { serverTwo.GracefulStop() })

	_, connTwo, err := AuthenticateWithAuthService("127.0.0.1", portTwo)
	require.NoError(t, err)
	connTwo.Close()

	rootAfter, err := manager.RootCertificatePEM()
	require.NoError(t, err)
	require.Equal(t, rootBefore, rootAfter)

	clientCertCacheMu.Lock()
	secondCert := clientCertCache[cacheKey]
	clientCertCacheMu.Unlock()
	require.Equal(t, firstCert, secondCert)
}

func TestMultipleClientsAndEnginesCommunicate(t *testing.T) {
	// Simulates two distinct CLI processes talking to separate engines so we can assert that
	// each process generates its own certificate while both engines operate concurrently.
	tmpDir := t.TempDir()
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), tmpDir)
	resetAuthClientCertificateCache()

	serverOne, portOne := startAuthServer(t, tmpDir, 0)
	t.Cleanup(func() { serverOne.GracefulStop() })
	serverTwo, portTwo := startAuthServer(t, tmpDir, 0)
	t.Cleanup(func() { serverTwo.GracefulStop() })

	// Client 1 -> Engine 1
	_, connOne, err := AuthenticateWithAuthService("127.0.0.1", portOne)
	require.NoError(t, err)
	connOne.Close()
	cacheKey := filepath.Clean(filepath.Join(tmpDir, tlsDirectoryName))
	clientCertCacheMu.Lock()
	clientOneCert := clientCertCache[cacheKey]
	clientCertCacheMu.Unlock()
	require.NotNil(t, clientOneCert)

	// Simulate separate CLI process.
	resetAuthClientCertificateCache()

	// Client 2 -> Engine 2
	_, connTwo, err := AuthenticateWithAuthService("127.0.0.1", portTwo)
	require.NoError(t, err)
	connTwo.Close()
	clientCertCacheMu.Lock()
	clientTwoCert := clientCertCache[cacheKey]
	clientCertCacheMu.Unlock()
	require.NotNil(t, clientTwoCert)
	require.NotEqual(t, clientOneCert, clientTwoCert)
}

// TestDialWithClientCertificateFailsAgainstNonTLSServer verifies that a mutual-TLS-only
// client certificate cannot connect to a plaintext gRPC endpoint, which guards against
// regressions where we might accidentally re-enable insecure fallbacks.
func TestDialWithClientCertificateFailsAgainstNonTLSServer(t *testing.T) {
	// Confirms that mutual TLS credentials cannot be used to connect to a plaintext listener,
	// guarding against regressions that might silently downgrade security.
	configDir := t.TempDir()
	resetAuthClientCertificateCache()
	t.Cleanup(resetAuthClientCertificateCache)

	port := getFreePort(localhost)
	stop := runServer(localhost, port)
	defer stop()

	err := dialWithClientCertificate(t, configDir, localhost, port)
	require.Error(t, err)
}

// startAuthServer is a helper function that starts a gRPC server that implements the Auth service.
func startAuthServer(t *testing.T, configDir string, preferredPort int) (*grpc.Server, int) {
	t.Helper()
	addr := "127.0.0.1:0"
	if preferredPort > 0 {
		addr = fmt.Sprintf("127.0.0.1:%d", preferredPort)
	}
	lis, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	port := lis.Addr().(*net.TCPAddr).Port
	manager := newTLSManager(t, configDir)
	creds := generateServerCreds(t, manager)
	server := grpc.NewServer(grpc.Creds(creds))
	authproto.RegisterAuthServer(server, &noopAuthServer{})
	go func() { _ = server.Serve(lis) }()
	return server, port
}

// generateServerCreds is a helper function that generates server credentials for the gRPC server.
func generateServerCreds(t *testing.T, manager *tlsconfig.Manager) credentials.TransportCredentials {
	t.Helper()
	creds, err := manager.GenerateServerCredentials("127.0.0.1")
	require.NoError(t, err)
	return creds
}

// newTLSManager is a helper function that creates a new TLS manager.
func newTLSManager(t *testing.T, dir string) *tlsconfig.Manager {
	t.Helper()
	manager, err := tlsconfig.NewManager(dir)
	require.NoError(t, err)
	return manager
}

// dialWithClientCertificate creates a fresh CLI client certificate for the provided
// configuration directory and attempts to dial the specified target. It expects the dial
// to fail, allowing callers to assert that plaintext listeners cannot be reached with TLS.
func dialWithClientCertificate(t *testing.T, configDir, host string, targetPort int) error {
	t.Helper()

	manager := newTLSManager(t, configDir)
	_, err := manager.GenerateServerCredentials(host)
	require.NoError(t, err)

	rootPEM, err := manager.RootCertificatePEM()
	require.NoError(t, err)

	rootCert, err := decodeCertificate(rootPEM)
	require.NoError(t, err)

	cert, err := ensureCLIClientCertificate(manager, rootCert, configDir)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(rootPEM))

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   host,
		MinVersion:   tls.VersionTLS12,
	})

	conn, err := grpc.NewClient(
		fmt.Sprintf("passthrough://%s:%d", host, targetPort),
		grpc.WithTransportCredentials(creds),
	)
	if err == nil {
		conn.Close()
		return fmt.Errorf("unexpectedly established connection")
	}
	return err
}
