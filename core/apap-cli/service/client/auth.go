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
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tlsconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/api/authproto"
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

func newTransportCredentials(host string, waitForAuthority bool) (credentials.TransportCredentials, error) {
	certDir, err := userdirs.ConfigDir()
	if err != nil {
		return nil, message.New(message.EngineTlsLoadRootCaFailed).WithCause(err)
	}
	if waitForAuthority {
		ctx, cancel := context.WithTimeout(context.Background(), credentialWaitTimeout)
		defer cancel()
		if err := tlsconfig.WaitForAuthority(ctx, certDir, credentialWaitInterval); err != nil {
			return nil, err
		}
	}
	manager, err := tlsconfig.NewManager(certDir)
	if err != nil {
		return nil, err
	}
	rootPEM, err := manager.RootCertificatePEM()
	if err != nil {
		return nil, err
	}
	rootCert, err := decodeCertificate(rootPEM)
	if err != nil {
		return nil, err
	}
	clientCert, err := ensureCLIClientCertificate(manager, rootCert, certDir)
	if err != nil {
		return nil, err
	}
	return tlsconfig.NewClientCredentials(host, rootPEM, clientCert)
}

// AuthenticateWithAuthService establishes a mutual TLS connection to the AuthService and returns a ready-to-use client.
// The caller is responsible for closing the returned connection when finished.
func AuthenticateWithAuthService(host string, port int) (authproto.AuthClient, *grpc.ClientConn, error) {
	connector, err := newEngineConnector(host, true)
	if err != nil {
		return nil, nil, err
	}
	conn, err := connector.Connect(host, port, retryConnectionTimeout)
	if err != nil {
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
	return authproto.NewAuthClient(conn), conn, nil
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
