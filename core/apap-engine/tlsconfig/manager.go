// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tlsconfig

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gofrs/flock"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/credentials"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

const (
	tlsDirectoryName      = "tls"
	rootCACertName        = "root_ca.pem"
	rootCAKeyName         = "root_ca.key"
	rootCALockName        = "root_ca.lock"
	rootCertificateHeader = "CERTIFICATE"
	privateKeyHeader      = "EC PRIVATE KEY"
)

// DirectoryName exposes the folder name containing TLS artifacts (root CA, keys, etc.)
const DirectoryName = tlsDirectoryName

const (
	rootCACertValidity = 10 * 365 * 24 * time.Hour
	clientCertValidity = 365 * 24 * time.Hour
	serverCertValidity = 365 * 24 * time.Hour
)

const DefaultPollInterval = 100 * time.Millisecond

var errInvalidAuthority = errors.New("invalid TLS authority material")

var chownFunc = os.Chown

// Manager owns the lifecycle of TLS credentials used by the engine and its clients. It ensures the root certificate
// authority is present, issues server certificates on startup, and generates client certificates on startup which the
// callers store in memory.
type Manager struct {
	baseDir           string
	configDir         string
	now               func() time.Time
	rand              io.Reader
	owner             *userdirs.LaunchUserInfo
	serverCertMu      sync.RWMutex
	serverCertificate *tls.Certificate
}

// launchUserLookup provides an overrideable hook for discovering the invoking user.
var launchUserLookup = userdirs.LaunchUser

// NewManager constructs a Manager rooted at the provided configuration directory. When the process was launched via
// sudo, it detects the launch user and uses that as the base directory for TLS storage instead of the root-owned config
// directory. This ensures generated certificates remain owned by the invoking user and avoids permission issues.
func NewManager(configDir string) (*Manager, error) {
	if configDir == "" {
		return nil, message.New(message.CommonUnknownError).WithCause(errors.New("configuration directory cannot be empty"))
	}
	configPath := filepath.Clean(configDir)
	var launchUser *userdirs.LaunchUserInfo
	if user, ok := launchUserLookup(); ok && user != nil {
		launchUser = user
		// If the config directory has been overridden (e.g. using the env var), just use it without changing it.
		if user.HomeDir != "" && userdirs.IsDefaultConfigDir(configPath) {
			if ownerConfig := userdirs.ConfigDirForHome(user.HomeDir); ownerConfig != "" {
				configPath = filepath.Clean(ownerConfig)
			}
		}
	}
	return &Manager{
		baseDir:   filepath.Join(configPath, tlsDirectoryName),
		configDir: configPath,
		now:       time.Now,
		rand:      rand.Reader,
		owner:     launchUser,
	}, nil
}

// GenerateServerCredentials ensures the root authority exists and is valid, creates an
// in-memory server certificate, and returns the transport credentials for the AuthService.
func (m *Manager) GenerateServerCredentials(host string) (credentials.TransportCredentials, error) {
	if err := m.ensureTLSDirectory(); err != nil {
		return nil, err
	}
	if err := m.ensureRootAuthority(); err != nil {
		return nil, err
	}
	serverCert, err := m.generateServerCertificate(host)
	if err != nil {
		return nil, err
	}
	m.serverCertMu.Lock()
	m.serverCertificate = &serverCert
	m.serverCertMu.Unlock()
	return m.ServerCredentials()
}

// ServerCredentials builds transport credentials requiring mutual TLS with TLS 1.2 and
// hardened cipher suites using the previously generated in-memory certificate.
func (m *Manager) ServerCredentials() (credentials.TransportCredentials, error) {
	m.serverCertMu.RLock()
	cert := m.serverCertificate
	m.serverCertMu.RUnlock()
	if cert == nil {
		return nil, message.New(message.EngineTlsLoadServerCredentialsFailed)
	}
	rootPEM, err := os.ReadFile(m.rootCACertPath())
	if err != nil {
		return nil, message.New(message.EngineTlsLoadRootCaFailed).WithMetadata(map[string]string{"path": m.rootCACertPath()}).WithCause(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(rootPEM) {
		return nil, message.New(message.EngineTlsLoadRootCaFailed).WithMetadata(map[string]string{"path": m.rootCACertPath()}).WithCause(errInvalidAuthority)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
		// MaxVersion is omitted so we negotiate the highest supported TLS version (e.g. TLS 1.3).
		// CipherSuites is omitted so we negotiate the highest supported cipher suite, i.e. maximum compatibility.
	}
	return credentials.NewTLS(tlsConfig), nil
}

// GenerateClientCertificate creates a new client certificate, signs it using the stored root certificate authority,
// and returns a tls.Certificate that the caller retains in memory for as long as needed.
func (m *Manager) GenerateClientCertificate(commonName string) (tls.Certificate, error) {
	caCert, caKey, err := m.loadAuthority()
	if err != nil {
		return tls.Certificate{}, err
	}
	serialNumber, err := randomSerial(m.rand)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateClientCertFailed).WithCause(err)
	}
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), m.rand)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateClientCertFailed).WithCause(err)
	}
	now := m.now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(clientCertValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(m.rand, &template, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateClientCertFailed).WithCause(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: rootCertificateHeader, Bytes: certDER})
	keyBytes, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateClientCertFailed).WithCause(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: privateKeyHeader, Bytes: keyBytes})
	clientCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateClientCertFailed).WithCause(err)
	}
	return clientCert, nil
}

// RootCertificatePEM returns the PEM encoded root certificate used for TLS bootstrapping.
func (m *Manager) RootCertificatePEM() ([]byte, error) {
	data, err := os.ReadFile(m.rootCACertPath())
	if err != nil {
		return nil, message.New(message.EngineTlsLoadRootCaFailed).WithCause(err)
	}
	return data, nil
}

// WaitForAuthority blocks until the root certificate authority is available on disk or the context is cancelled.
// If the engine is being launched for the first time, the client will wait for up to the specified interval for the
// authority to be generated.
func WaitForAuthority(ctx context.Context, configDir string, interval time.Duration) error {
	manager, err := NewManager(configDir)
	if err != nil {
		return err
	}
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if manager.authorityExists() {
			return nil
		}
		select {
		case <-ctx.Done():
			return message.New(message.EngineTlsRootCaUnavailable).WithCause(ctx.Err())
		case <-ticker.C:
		}
	}
}

// authorityExists reports whether the root CA certificate and key are already present on disk.
func (m *Manager) authorityExists() bool {
	if _, err := os.Stat(m.rootCACertPath()); err != nil {
		return false
	}
	if _, err := os.Stat(m.rootCAKeyPath()); err != nil {
		return false
	}
	return true
}

// ensureTLSDirectory creates the TLS output directory with the correct permissions if it does not exist.
func (m *Manager) ensureTLSDirectory() error {
	if err := os.MkdirAll(m.baseDir, perms.LocalDirPerm); err != nil {
		return message.New(message.EngineTlsCreateServerCertFailed).WithCause(err)
	}
	m.ensureOwnership(m.configDir)
	m.ensureOwnership(m.baseDir)
	return nil
}

// ensureRootAuthority guarantees a valid root certificate authority exists on disk, regenerating it when it expires.
func (m *Manager) ensureRootAuthority() error {
	lock := flock.New(m.rootLockPath())
	if err := lock.Lock(); err != nil {
		return message.New(message.EngineTlsCreateServerCertFailed).WithCause(err)
	}
	m.ensureOwnership(m.rootLockPath())
	defer func() {
		if err := lock.Unlock(); err != nil {
			log.WithError(err).Warn("Failed to release TLS root authority lock")
		}
	}()

	if !m.authorityExists() {
		return m.generateRootAuthority()
	}
	cert, _, err := m.loadAuthority()
	if err != nil {
		log.WithError(err).Warn("Failed to load existing TLS root authority; regenerating")
		return m.generateRootAuthority()
	}
	if !m.now().Before(cert.NotAfter) {
		log.WithField("expiredAt", cert.NotAfter).Warn("TLS root authority expired; regenerating")
		return m.generateRootAuthority()
	}
	fields := log.Fields{
		"path": m.rootCACertPath(),
	}
	log.WithFields(fields).Debug("Using existing TLS root certificate authority")
	return nil
}

// generateRootAuthority creates a new root CA key pair and certificate and writes them to disk.
func (m *Manager) generateRootAuthority() error {
	fields := log.Fields{
		"path": m.rootCACertPath(),
	}
	log.WithFields(fields).Debugf("Creating %v TLS root certificate authority", terminology.GetProductFullName())

	serial, err := randomSerial(m.rand)
	if err != nil {
		return message.New(message.EngineTlsCreateRootCaFailed).WithCause(err)
	}
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), m.rand)
	if err != nil {
		return message.New(message.EngineTlsCreateRootCaFailed).WithCause(err)
	}
	now := m.now()
	pubDER, err := x509.MarshalPKIXPublicKey(&rootKey.PublicKey)
	if err != nil {
		return message.New(message.EngineTlsCreateRootCaFailed).WithCause(err)
	}
	subjectKeyID := sha256.Sum256(pubDER)
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: fmt.Sprintf("%v Root CA", terminology.GetProductFullName())},
		NotBefore:             now.Add(-1 * time.Minute),
		NotAfter:              now.Add(rootCACertValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		SubjectKeyId:          subjectKeyID[:],
	}
	certDER, err := x509.CreateCertificate(m.rand, template, template, &rootKey.PublicKey, rootKey)
	if err != nil {
		return message.New(message.EngineTlsCreateRootCaFailed).WithCause(err)
	}
	if err := writePEMFile(m.rootCACertPath(), rootCertificateHeader, certDER, perms.RootCACertPerm); err != nil {
		return message.New(message.EngineTlsCreateRootCaFailed).WithCause(err)
	}
	m.ensureOwnership(m.rootCACertPath())
	keyBytes, err := x509.MarshalECPrivateKey(rootKey)
	if err != nil {
		return message.New(message.EngineTlsCreateRootCaFailed).WithCause(err)
	}
	if err := writePEMFile(m.rootCAKeyPath(), privateKeyHeader, keyBytes, perms.RootCAKeyPerm); err != nil {
		return message.New(message.EngineTlsCreateRootCaFailed).WithCause(err)
	}
	m.ensureOwnership(m.rootCAKeyPath())

	m.serverCertMu.Lock()
	m.serverCertificate = nil
	m.serverCertMu.Unlock()
	return nil
}

// generateServerCertificate creates a new server certificate, signed by the root authority.
func (m *Manager) generateServerCertificate(host string) (tls.Certificate, error) {
	caCert, caKey, err := m.loadAuthority()
	if err != nil {
		return tls.Certificate{}, err
	}

	log.Debugf("Creating %v daemon TLS server certificate", terminology.GetProductFullName())

	serial, err := randomSerial(m.rand)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateServerCertFailed).WithCause(err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), m.rand)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateServerCertFailed).WithCause(err)
	}
	now := m.now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: fmt.Sprintf("%v Auth Server", terminology.GetProductFullName())},
		NotBefore:    now.Add(-1 * time.Minute),
		NotAfter:     now.Add(serverCertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, host)
	}
	template.DNSNames = append(template.DNSNames, "localhost")
	certDER, err := x509.CreateCertificate(m.rand, template, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateServerCertFailed).WithCause(err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateServerCertFailed).WithCause(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: rootCertificateHeader, Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: privateKeyHeader, Bytes: keyBytes})
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, message.New(message.EngineTlsCreateServerCertFailed).WithCause(err)
	}
	return serverCert, nil
}

// loadAuthority reads the root CA certificate and key from disk and parses them into Go structures.
func (m *Manager) loadAuthority() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(m.rootCACertPath())
	if err != nil {
		return nil, nil, message.New(message.EngineTlsLoadRootCaFailed).WithMetadata(map[string]string{"path": m.rootCACertPath()}).WithCause(err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != rootCertificateHeader {
		return nil, nil, message.New(message.EngineTlsLoadRootCaFailed).WithMetadata(map[string]string{"path": m.rootCACertPath()}).WithCause(errInvalidAuthority)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, message.New(message.EngineTlsLoadRootCaFailed).WithMetadata(map[string]string{"path": m.rootCACertPath()}).WithCause(err)
	}
	keyPEM, err := os.ReadFile(m.rootCAKeyPath())
	if err != nil {
		return nil, nil, message.New(message.EngineTlsLoadRootCaFailed).WithMetadata(map[string]string{"path": m.rootCAKeyPath()}).WithCause(err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != privateKeyHeader {
		return nil, nil, message.New(message.EngineTlsLoadRootCaFailed).WithMetadata(map[string]string{"path": m.rootCAKeyPath()}).WithCause(errInvalidAuthority)
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, message.New(message.EngineTlsLoadRootCaFailed).WithMetadata(map[string]string{"path": m.rootCAKeyPath()}).WithCause(err)
	}
	return cert, key, nil
}

// randomSerial returns a cryptographically random serial number suitable for X.509 certificates.
func randomSerial(reader io.Reader) (*big.Int, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(reader, serialLimit)
}

// writePEMFile stores the provided DER data as a PEM file using the supplied block label and permissions.
func writePEMFile(path string, blockType string, data []byte, perm os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer file.Close()
	return pem.Encode(file, &pem.Block{Type: blockType, Bytes: data})
}

// ensureOwnership attempts to set the owner of the specified path to the configured owner.
func (m *Manager) ensureOwnership(path string) {
	if m.owner == nil {
		return
	}
	if runtime.GOOS == "windows" {
		// Windows uses ACLs instead, so os.Chown not applicable.
		return
	}
	if path == "" {
		return
	}
	if err := chownFunc(path, m.owner.UID, m.owner.GID); err != nil {
		log.WithError(err).WithField("path", path).Warn("Failed to update TLS file ownership")
	}
}

// rootCACertPath returns the filesystem location of the root CA certificate.
func (m *Manager) rootCACertPath() string {
	return filepath.Join(m.baseDir, rootCACertName)
}

// rootCAKeyPath returns the filesystem location of the root CA private key.
func (m *Manager) rootCAKeyPath() string {
	return filepath.Join(m.baseDir, rootCAKeyName)
}

// rootLockPath returns the filesystem location of the lock used to coordinate root CA creation.
func (m *Manager) rootLockPath() string {
	return filepath.Join(m.baseDir, rootCALockName)
}
