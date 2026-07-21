// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"

	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/clients/go/authproto"
)

const (
	credentialWaitInterval = 100 * time.Millisecond
	credentialWaitTimeout  = 5 * time.Second
	tlsDirectoryName       = tlsconfig.DirectoryName
)

var cliClientCommonName = fmt.Sprintf("%v CLI Client", terminology.GetProductFullName())

var (
	clientCertCacheMu         sync.Mutex
	clientCertCache           = make(map[string]*tls.Certificate)
	clientCertRootFingerprint = make(map[string]string)
)

// EnsureAuthClientCertificates prepares the TLS client credentials used to talk to the AuthService.
// It can be called multiple times; subsequent invocations reuse the in-memory certificate for the
// current root authority.
func EnsureAuthClientCertificates() error {
	certDir, err := resolveConfigDir()
	if err != nil {
		return message.New(message.EngineTlsLoadRootCaFailed).WithCause(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), credentialWaitTimeout)
	defer cancel()
	if err := tlsconfig.WaitForAuthority(ctx, certDir, credentialWaitInterval); err != nil {
		return err
	}
	manager, err := tlsconfig.NewManager(certDir)
	if err != nil {
		return err
	}
	rootPEM, err := manager.RootCertificatePEM()
	if err != nil {
		return err
	}
	rootCert, err := decodeCertificate(rootPEM)
	if err != nil {
		return err
	}
	_, err = ensureCLIClientCertificate(manager, rootCert, certDir)
	return err
}

// AuthenticateWithAuthService establishes a mutual TLS connection to the AuthService and returns a ready-to-use client.
// The caller is responsible for closing the returned connection when finished.
func AuthenticateWithAuthService(host string, authPort int) (authproto.AuthClient, *grpc.ClientConn, error) {
	if err := EnsureAuthClientCertificates(); err != nil {
		return nil, nil, err
	}
	configDir, err := resolveConfigDir()
	if err != nil {
		return nil, nil, message.New(message.EngineTlsLoadRootCaFailed).WithCause(err)
	}
	manager, err := tlsconfig.NewManager(configDir)
	if err != nil {
		return nil, nil, err
	}
	rootPEM, err := manager.RootCertificatePEM()
	if err != nil {
		return nil, nil, err
	}
	rootCert, err := decodeCertificate(rootPEM)
	if err != nil {
		return nil, nil, err
	}
	clientCert, err := ensureCLIClientCertificate(manager, rootCert, configDir)
	if err != nil {
		return nil, nil, err
	}
	rootPool := x509.NewCertPool()
	if !rootPool.AppendCertsFromPEM(rootPEM) {
		return nil, nil, message.New(message.EngineTlsLoadRootCaFailed)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      rootPool,
		ServerName:   host,
		MinVersion:   tls.VersionTLS12,
	}
	address := net.JoinHostPort(host, strconv.Itoa(authPort))
	conn, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithChainUnaryInterceptor(message.ErrorHandlingClientInterceptor()),
		grpc.WithChainStreamInterceptor(message.ErrorHandlingClientStreamInterceptor()),
	)
	if err != nil {
		return nil, nil, message.New(message.EngineGrpcconnectionCreateClient).WithMetadata(map[string]string{"serverAddress": address}).WithCause(err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), retryConnectionTimeout)
	if err := waitForAuthReady(waitCtx, conn); err != nil {
		waitCancel()
		conn.Close()
		username, userErr := utils.CurrentUsername()
		if userErr != nil || username == "" {
			username = "unknown"
		}
		metadata := map[string]string{
			"timeout": strconv.FormatFloat(retryConnectionTimeout.Seconds(), 'f', -1, 64),
			"user":    username,
		}
		return nil, nil, message.New(message.CliServiceClientAuthServiceUnavailable).WithMetadata(metadata).WithCause(err)
	}
	waitCancel()
	return authproto.NewAuthClient(conn), conn, nil
}

// resolveConfigDir returns the daemon configuration directory, falling back to the default userdirs logic.
func resolveConfigDir() (string, error) {
	resolved, err := userdirs.ConfigDir()
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// waitForAuthReady waits until the AuthService is ready to accept connections.
func waitForAuthReady(ctx context.Context, conn *grpc.ClientConn) error {
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if !conn.WaitForStateChange(ctx, state) {
			return ctx.Err()
		}
	}
}

// ensureCLIClientCertificate loads the cached CLI client certificate signed by the current root authority or creates a
// new one if the root authority has changed (e.g. engine has restarted).
func ensureCLIClientCertificate(manager *tlsconfig.Manager, rootCert *x509.Certificate, configDir string) (tls.Certificate, error) {
	rootFingerprint := fingerprintCertificate(rootCert)
	cacheKey := filepath.Clean(filepath.Join(configDir, tlsDirectoryName))
	clientCertCacheMu.Lock()
	defer clientCertCacheMu.Unlock()

	if cached, ok := clientCertCache[cacheKey]; ok && clientCertRootFingerprint[cacheKey] == rootFingerprint {
		return *cached, nil
	}

	cert, err := manager.GenerateClientCertificate(cliClientCommonName)
	if err != nil {
		return tls.Certificate{}, err
	}
	clientCertCache[cacheKey] = &cert
	clientCertRootFingerprint[cacheKey] = rootFingerprint
	return cert, nil
}

// decodeCertificate parses the given PEM-encoded certificate.
func decodeCertificate(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, message.New(message.EngineTlsLoadRootCaFailed)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, message.New(message.EngineTlsLoadRootCaFailed).WithCause(err)
	}
	return cert, nil
}

// fingerprintCertificate returns the SHA-256 fingerprint of the given certificate.
func fingerprintCertificate(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// resetAuthClientCertificateCache clears the cached client certificate. Used in tests.
func resetAuthClientCertificateCache() {
	clientCertCacheMu.Lock()
	defer clientCertCacheMu.Unlock()
	clientCertCache = make(map[string]*tls.Certificate)
	clientCertRootFingerprint = make(map[string]string)
}
