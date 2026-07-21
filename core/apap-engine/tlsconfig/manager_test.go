// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tlsconfig

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

func TestGenerateServerCredentialsCreatesArtifacts(t *testing.T) {
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)

	creds, err := manager.GenerateServerCredentials("127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, creds)

	tlsDir := filepath.Join(configDir, tlsDirectoryName)
	assertFilePerms := func(path string, expected os.FileMode) {
		info, err := os.Stat(path)
		require.NoError(t, err)
		if runtime.GOOS == "windows" {
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			require.NoError(t, err)
			_ = f.Close()
			return
		}
		require.Equal(t, expected, info.Mode().Perm())
	}

	rootCertPath := filepath.Join(tlsDir, rootCACertName)
	rootKeyPath := filepath.Join(tlsDir, rootCAKeyName)
	assertFilePerms(rootCertPath, perms.RootCACertPerm)
	assertFilePerms(rootKeyPath, perms.RootCAKeyPerm)

	entries, err := os.ReadDir(tlsDir)
	require.NoError(t, err)
	names := map[string]struct{}{}
	for _, entry := range entries {
		// Ignore the lock file, which may or may not be present depending on timing and isn't really a credential artifact
		if entry.Name() == rootCALockName {
			continue
		}
		names[entry.Name()] = struct{}{}
	}
	_, hasCert := names[rootCACertName]
	_, hasKey := names[rootCAKeyName]
	require.True(t, hasCert)
	require.True(t, hasKey)

	_, err = os.Stat(filepath.Join(tlsDir, "server_cert.pem"))
	require.Error(t, err)
	require.True(t, errors.Is(err, os.ErrNotExist))

	// Ensure credentials remain usable on subsequent calls.
	creds, err = manager.ServerCredentials()
	require.NoError(t, err)
	require.NotNil(t, creds)
}

func TestGenerateServerCredentialsRegeneratesExpiredRootCA(t *testing.T) {
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)

	initialNow := time.Now()
	manager.now = func() time.Time { return initialNow }

	_, err = manager.GenerateServerCredentials("127.0.0.1")
	require.NoError(t, err)

	tlsDir := filepath.Join(configDir, tlsDirectoryName)
	rootPath := filepath.Join(tlsDir, rootCACertName)

	initialPEM, err := os.ReadFile(rootPath)
	require.NoError(t, err)
	initialBlock, _ := pem.Decode(initialPEM)
	require.NotNil(t, initialBlock)
	initialCert, err := x509.ParseCertificate(initialBlock.Bytes)
	require.NoError(t, err)

	// Still within validity window; CA should not rotate.
	midpoint := initialNow.Add(rootCACertValidity / 2)
	manager.now = func() time.Time { return midpoint }
	_, err = manager.GenerateServerCredentials("127.0.0.1")
	require.NoError(t, err)
	currentPEM, err := os.ReadFile(rootPath)
	require.NoError(t, err)
	require.Equal(t, initialPEM, currentPEM)

	// Advance beyond expiry; CA should rotate.
	manager.now = func() time.Time { return initialCert.NotAfter.Add(time.Minute) }
	_, err = manager.GenerateServerCredentials("127.0.0.1")
	require.NoError(t, err)
	rotatedPEM, err := os.ReadFile(rootPath)
	require.NoError(t, err)
	require.NotEqual(t, initialPEM, rotatedPEM)
}

func TestGenerateClientCertificateVerifiesAgainstAuthority(t *testing.T) {
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)
	require.NotNil(t, manager)

	_, err = manager.GenerateServerCredentials("localhost")
	require.NoError(t, err)

	clientCert, err := manager.GenerateClientCertificate("test-client")
	require.NoError(t, err)
	require.NotNil(t, clientCert.Certificate)

	cert, err := x509.ParseCertificate(clientCert.Certificate[0])
	require.NoError(t, err)

	rootPEM, err := manager.RootCertificatePEM()
	require.NoError(t, err)
	block, _ := pem.Decode(rootPEM)
	require.NotNil(t, block)
	rootCert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(rootPEM))

	_, err = cert.Verify(x509.VerifyOptions{Roots: pool, Intermediates: x509.NewCertPool(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	require.NoError(t, err)
	require.Equal(t, "test-client", cert.Subject.CommonName)
	require.Equal(t, fmt.Sprintf("%v Root CA", terminology.GetProductFullName()), rootCert.Subject.CommonName)
}

func TestGenerateServerCredentialsConcurrentAccess(t *testing.T) {
	// Simulate multiple engine instances starting simultaneously to ensure the lock prevents
	// divergent root CAs; every worker should observe identical certificate material.
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)

	const workers = 5
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	rootPEMs := make(chan []byte, workers)
	rootPath := filepath.Join(configDir, tlsDirectoryName, rootCACertName)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := manager.GenerateServerCredentials("127.0.0.1"); err != nil {
				errCh <- err
				return
			}
			data, err := os.ReadFile(rootPath)
			if err != nil {
				errCh <- err
				return
			}
			rootPEMs <- data
		}()
	}

	wg.Wait()
	close(errCh)
	close(rootPEMs)

	for err := range errCh {
		require.NoError(t, err)
	}

	var reference []byte
	for pemBytes := range rootPEMs {
		if reference == nil {
			reference = pemBytes
			continue
		}
		require.True(t, bytes.Equal(reference, pemBytes), "root CA PEM should be identical across concurrent generations")
	}
	require.NotNil(t, reference, "expected at least one root CA PEM")
}

func TestEnsureTLSDirectoryChownsConfigAndTlsDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ownership adjustments rely on POSIX semantics")
	}

	originalChown := chownFunc
	var chowned []string
	chownFunc = func(path string, uid, gid int) error {
		chowned = append(chowned, path)
		return errors.New("inject chown failure")
	}
	defer func() {
		chownFunc = originalChown
	}()

	configDir := filepath.Join(t.TempDir(), "config")
	manager, err := NewManager(configDir)
	require.NoError(t, err)
	manager.owner = &userdirs.LaunchUserInfo{UID: -1, GID: -1}

	logger := log.StandardLogger()
	prevOut := logger.Out
	prevLevel := logger.GetLevel()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(log.WarnLevel)
	defer func() {
		logger.SetOutput(prevOut)
		logger.SetLevel(prevLevel)
	}()

	require.NoError(t, manager.ensureTLSDirectory())

	output := buf.String()
	require.Contains(t, chowned, manager.configDir)
	require.Contains(t, chowned, manager.baseDir)
	require.Contains(t, output, manager.configDir)
	require.Contains(t, output, manager.baseDir)
}

func TestNewManagerRebasesConfigDirForLaunchUser(t *testing.T) {
	tempHome := t.TempDir()

	originalLookup := launchUserLookup
	launchUserLookup = func() (*userdirs.LaunchUserInfo, bool) {
		return &userdirs.LaunchUserInfo{
			HomeDir: tempHome,
			UID:     2000,
			GID:     2000,
		}, true
	}
	t.Cleanup(func() {
		launchUserLookup = originalLookup
	})

	manager, err := NewManager("/custom/path")
	require.NoError(t, err)

	require.Equal(t, filepath.Clean("/custom/path"), manager.configDir, "non-default override should be preserved")
	require.Equal(t, filepath.Join("/custom/path", tlsDirectoryName), manager.baseDir)
	require.Equal(t, filepath.Clean("/custom/path"), filepath.Dir(manager.baseDir))

	manager, err = NewManager(userdirs.ConfigDirForHome(tempHome))
	require.NoError(t, err)
	expectedConfigDir := filepath.Clean(userdirs.ConfigDirForHome(tempHome))
	require.Equal(t, expectedConfigDir, manager.configDir)
	require.Equal(t, filepath.Join(expectedConfigDir, tlsDirectoryName), manager.baseDir)
	require.Equal(t, expectedConfigDir, filepath.Dir(manager.baseDir))
	require.NotNil(t, manager.owner)
	require.Equal(t, 2000, manager.owner.UID)
	require.Equal(t, 2000, manager.owner.GID)
}

func TestGenerateServerCredentialsWithStaleLock(t *testing.T) {
	// Pre-create a lock file to mimic a crashed process; generating credentials must still succeed
	// and leave the root CA in place.
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)

	// Manually create a stale lock file to ensure the manager can recover.
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, tlsDirectoryName), perms.LocalDirPerm))
	staleLock := manager.rootLockPath()
	require.NoError(t, os.WriteFile(staleLock, []byte("stale"), perms.RootCACertPerm))

	_, err = manager.GenerateServerCredentials("127.0.0.1")
	require.NoError(t, err)

	// Lock file should still be present (flock uses it), but root CA must exist.
	_, err = os.Stat(manager.rootCACertPath())
	require.NoError(t, err)
}

func TestWaitForAuthority(t *testing.T) {
	configDir := t.TempDir()
	done := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		done <- writeAuthorityMaterial(configDir)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, WaitForAuthority(ctx, configDir, 10*time.Millisecond))
	require.NoError(t, <-done)

	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer timeoutCancel()
	configDir2 := t.TempDir()
	err := WaitForAuthority(timeoutCtx, configDir2, 10*time.Millisecond)
	require.Error(t, err)
}

func writeAuthorityMaterial(configDir string) error {
	tlsDir := filepath.Join(configDir, tlsDirectoryName)
	if err := os.MkdirAll(tlsDir, perms.LocalDirPerm); err != nil {
		return err
	}
	certPath := filepath.Join(tlsDir, rootCACertName)
	if err := os.WriteFile(certPath, []byte("cert"), perms.RootCACertPerm); err != nil {
		return err
	}
	keyPath := filepath.Join(tlsDir, rootCAKeyName)
	if err := os.WriteFile(keyPath, []byte("key"), perms.RootCAKeyPerm); err != nil {
		return err
	}
	return nil
}

func TestGenerateClientCertificateFailsWithoutAuthority(t *testing.T) {
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)
	_, err = manager.GenerateClientCertificate("missing-root")
	require.Error(t, err)
}

func TestLoadAuthorityErrorsOnCorruptMaterial(t *testing.T) {
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)
	tlsDir := filepath.Join(configDir, tlsDirectoryName)
	require.NoError(t, os.MkdirAll(tlsDir, perms.LocalDirPerm))
	certPath := filepath.Join(tlsDir, rootCACertName)
	keyPath := filepath.Join(tlsDir, rootCAKeyName)
	require.NoError(t, os.WriteFile(certPath, []byte("bad"), perms.RootCACertPerm))
	require.NoError(t, os.WriteFile(keyPath, []byte("bad"), perms.RootCAKeyPerm))
	_, _, err = manager.loadAuthority()
	require.Error(t, err)
}

func TestGenerateServerCredentialsFailsWhenTLSDirectoryIsFile(t *testing.T) {
	configDir := t.TempDir()
	tlsDir := filepath.Join(configDir, tlsDirectoryName)
	require.NoError(t, os.WriteFile(tlsDir, []byte("not-a-dir"), perms.LocalDirPerm))
	manager, err := NewManager(configDir)
	require.NoError(t, err)
	_, err = manager.GenerateServerCredentials("127.0.0.1")
	require.Error(t, err)
}

func TestServerCredentialsFailsWhenArtifactsMissing(t *testing.T) {
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)
	_, err = manager.ServerCredentials()
	require.Error(t, err)
}

func TestRootCertificatePEMFailsWhenMissing(t *testing.T) {
	configDir := t.TempDir()
	manager, err := NewManager(configDir)
	require.NoError(t, err)
	_, err = manager.RootCertificatePEM()
	require.Error(t, err)
}
