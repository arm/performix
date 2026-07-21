// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	engineSSH "github.com/Arm-Debug/apap-cli/apap-engine/ssh"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// testPrivateKey is a sample private key for testing purposes.
const testPrivateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBokv9RR+7uouPHf93eUUX955NzcFNGqPVhE0m6Ww1WXQAAAJiBICEOgSAh
DgAAAAtzc2gtZWQyNTUxOQAAACBokv9RR+7uouPHf93eUUX955NzcFNGqPVhE0m6Ww1WXQ
AAAECkvbXnrn7dL5LpvOrDoV1B5Wjwy3H8Y+2sT70Lco87g2iS/1FH7u6i48d/3d5RRf3n
k3NwU0ao9WETSbpbDVZdAAAAEGxleGthbjAxQGUxMzQ4MjIBAgMEBQ==
-----END OPENSSH PRIVATE KEY-----`

// passphraseProtectedPrivateKey is a sample passphrase-protected key (passphrase: "passphrase") for testing purposes.
const passphraseProtectedPrivateKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAAABDq1Yv8ZX
IqLmdXsD+RyxX6AAAAGAAAAAEAAAAzAAAAC3NzaC1lZDI1NTE5AAAAIMdxTeo0p+t/feLs
80klzFeK8RR5+lP+evglssXzwMtZAAAAoE5OPK+gJDjTZf46KpUeu7tUq9EHjrkZqwGWf0
g5meQMGo9mwBkHGP0IED5P7XZqepIE3E3Pd8mkOTfEG4isGdC1YG9PBSbxFJ3BBGymz9yK
F8ozorOYrgkK8nVcE8yZ7Fs/3t3tvwNV2RgSngp0bbxOGzdt9TAXGhMMiRm5nChAgZNnXf
F3bYTnse8mfxbEWf/RnkD7uFdlpb9H+POFJrw=
-----END OPENSSH PRIVATE KEY-----`

func makeKnownHostsLine(t *testing.T, host string, port int) string {
	t.Helper()
	signer, err := ssh.ParsePrivateKey([]byte(testPrivateKey))
	require.NoError(t, err)
	return makeKnownHostsLineForSigner(host, port, signer)
}

func makeKnownHostsLineForSigner(host string, port int, signer ssh.Signer) string {
	return knownhosts.Line([]string{net.JoinHostPort(host, strconv.Itoa(port))}, signer.PublicKey())
}

type testSSHConn struct {
	net.Conn
}

func (c *testSSHConn) User() string          { return "" }
func (c *testSSHConn) SessionID() []byte     { return []byte("test-session") }
func (c *testSSHConn) ClientVersion() []byte { return []byte("SSH-2.0-test-client") }
func (c *testSSHConn) ServerVersion() []byte { return []byte("SSH-2.0-test-server") }
func (c *testSSHConn) RemoteAddr() net.Addr {
	if c.Conn != nil {
		return c.Conn.RemoteAddr()
	}
	return nil
}
func (c *testSSHConn) LocalAddr() net.Addr {
	if c.Conn != nil {
		return c.Conn.LocalAddr()
	}
	return nil
}
func (c *testSSHConn) SendRequest(string, bool, []byte) (bool, []byte, error) {
	return false, nil, nil
}
func (c *testSSHConn) OpenChannel(string, []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	return nil, nil, errors.New("not implemented")
}
func (c *testSSHConn) Close() error {
	if c.Conn != nil {
		return c.Conn.Close()
	}
	return nil
}
func (c *testSSHConn) Wait() error { return nil }

func newTestSSHClient(t *testing.T) *ssh.Client {
	t.Helper()

	conn1, conn2 := net.Pipe()
	go conn2.Close()

	chans := make(chan ssh.NewChannel)
	reqs := make(chan *ssh.Request)
	close(chans)
	close(reqs)

	client := ssh.NewClient(&testSSHConn{Conn: conn1}, chans, reqs)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestFindKnownHosts(t *testing.T) {
	t.Run("should return known_hosts located next to the private key if present", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create a directory for the private key.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)

		// Write a dummy private key file.
		err = afero.WriteFile(fs, "/keys/private_key", []byte("dummy key"), perms.PrivateKeyPerm)
		require.NoError(t, err)

		// Write a known_hosts file in the same directory.
		err = afero.WriteFile(fs, "/keys/known_hosts", []byte("dummy known hosts"), perms.PrivateKeyPerm)
		require.NoError(t, err)

		// Inject a fake UserHomeDir function (won't be used in this test).
		var promptSleeps []time.Duration
		connector := NewSecureConnector(
			fs,
			nil,
			nil,
			func() (string, error) { return "/home/test", nil },
			func(d time.Duration) { promptSleeps = append(promptSleeps, d) },
		)

		kh, err := connector.findKnownHosts("/keys/private_key")
		require.NoError(t, err)
		assert.Equal(t, "/keys/known_hosts", filepath.ToSlash(kh))
	})

	t.Run("should return known_hosts located in user's .ssh directory if not present next to key", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create a directory for the private key.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)

		// Write a dummy private key file.
		err = afero.WriteFile(fs, "/keys/private_key", []byte("dummy key"), perms.PrivateKeyPerm)
		require.NoError(t, err)

		// Do NOT create /keys/known_hosts.
		// Create the user's .ssh directory and a known_hosts file.
		err = fs.MkdirAll("/home/test/.ssh", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/home/test/.ssh/known_hosts", []byte("dummy known hosts"), perms.PrivateKeyPerm)
		require.NoError(t, err)

		var promptSleeps []time.Duration
		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, func(d time.Duration) {
			promptSleeps = append(promptSleeps, d)
		})

		kh, err := connector.findKnownHosts("/keys/private_key")
		require.NoError(t, err)
		assert.Equal(t, "/home/test/.ssh/known_hosts", filepath.ToSlash(kh))
	})

	t.Run("should fail when no known_hosts file exists", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create a directory for the private key.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)

		// Write a dummy private key file.
		err = afero.WriteFile(fs, "/keys/private_key", []byte("dummy key"), perms.PrivateKeyPerm)
		require.NoError(t, err)

		// No known_hosts file exists anywhere.
		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)

		_, err = connector.findKnownHosts("/keys/private_key")
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshLocateKnownHostsFile, msgErr.Code())

		expectedLocations := "`" + string(filepath.Separator) + filepath.Join("keys", "known_hosts") + "`, "
		expectedLocations += "`" + string(filepath.Separator) + filepath.Join("home", "test", ".ssh", "known_hosts") + "`"
		assert.Equal(t, expectedLocations, msgErr.Metadata()["locations"])
	})

	t.Run("should only list each searched location once", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create a directory for the private key.
		err := fs.MkdirAll("/home/test/.ssh", perms.SshDirPerm)
		require.NoError(t, err)

		// Write a dummy private key file.
		err = afero.WriteFile(fs, "/home/test/.ssh/private_key", []byte("dummy key"), perms.PrivateKeyPerm)
		require.NoError(t, err)

		// No known_hosts file exists anywhere.
		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)

		_, err = connector.findKnownHosts("/home/test/.ssh/private_key")
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshLocateKnownHostsFile, msgErr.Code())

		expectedLocations := "`" + string(filepath.Separator) + filepath.Join("home", "test", ".ssh", "known_hosts") + "`"
		assert.Equal(t, expectedLocations, msgErr.Metadata()["locations"])
	})
}

func TestKnownHostsForKey(t *testing.T) {
	signer, err := ssh.ParsePrivateKey([]byte(testPrivateKey))
	require.NoError(t, err)
	pubKey := signer.PublicKey()

	t.Run("returns unique aliases and skips current host and invalid lines", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")
		entries := []string{
			knownhosts.Line([]string{"old.example.com:22"}, pubKey),
			knownhosts.Line([]string{"old.example.com:22"}, pubKey),
			knownhosts.Line([]string{"new.example.com:22"}, pubKey),
			"not a valid known hosts entry",
		}
		err := os.WriteFile(knownHostsPath, []byte(strings.Join(entries, "\n")+"\n"), perms.KnownHostsPerm)
		require.NoError(t, err)

		knownAs, err := knownHostsForKey(afero.NewOsFs(), knownHostsPath, "new.example.com:22", pubKey)
		require.NoError(t, err)
		assert.Equal(t, []string{"old.example.com"}, knownAs)
	})

	t.Run("returns wrapped error when known_hosts cannot be opened", func(t *testing.T) {
		_, err := knownHostsForKey(afero.NewOsFs(), "/definitely/missing/known_hosts", "new.example.com:22", pubKey)
		require.Error(t, err)
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, message.EngineConductorSshOpenKnownHosts, msg.Code())
	})
}

func TestBuildSSHClientConfig(t *testing.T) {
	t.Run("should succeed with valid private key and known_hosts", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create directory and files for the test.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/private_key", []byte(testPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/known_hosts", []byte("dummy known hosts"), perms.KnownHostsPerm)
		require.NoError(t, err)

		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/private_key",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		dialInfo := SSHDialInfo{HopIndex: 0, TotalHops: 1}
		config, err := connector.buildSSHClientConfig(hostCfg, dialInfo, nil, true)
		require.NoError(t, err)
		assert.Equal(t, "testuser", config.User)
	})

	t.Run("should fail when no private keys can be located", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		hostCfg := target.SSHHostConfig{
			Host:          "example.com",
			Port:          22,
			Username:      "testuser",
			HostKeyPolicy: target.IgnoreHostKey,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)

		_, err := connector.buildSSHClientConfig(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1}, nil, true)
		require.Error(t, err)

		var msgErr message.Message
		require.True(t, errors.As(err, &msgErr))
		assert.Equal(t, message.EngineConductorSshNoValidKeys, msgErr.Code())
		assert.Equal(t, hostCfg.DisplayString(), msgErr.Metadata()["target"])
		assert.Equal(t, "none", msgErr.Metadata()["keyPath"])
		assert.Equal(t, util.DisplayErrorStringSlice(engineSSH.GetPrivateKeySearchDirs()), msgErr.Metadata()["keyDirs"])
	})

	t.Run("should succeed with password auth when prompt provider is available", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		hostCfg := target.SSHHostConfig{
			Host:          "example.com",
			Port:          22,
			Username:      "testuser",
			HostKeyPolicy: target.IgnoreHostKey,
			AuthMethod:    target.SSHAuthMethodPassword,
		}

		secretPromptProvider := func(cfg PromptConfig) ([]byte, error) {
			return []byte("password"), nil
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		dialInfo := SSHDialInfo{
			SecretPromptProvider: secretPromptProvider,
			HopIndex:             1,
			TotalHops:            2,
		}
		jumpIndex := 0
		if dialInfo.TotalHops > 1 {
			jumpIndex = dialInfo.HopIndex + 1
		}
		prompter, err := newPasswordAuthPrompter(hostCfg.Host, hostCfg.Username, jumpIndex, dialInfo, SSHPromptMaxAttempts)
		require.NoError(t, err)

		config, err := connector.buildSSHClientConfig(hostCfg, dialInfo, prompter, true)
		require.NoError(t, err)
		require.NotNil(t, config)
		require.Len(t, config.Auth, 2)
	})

	t.Run("should fail with password auth when prompt provider is missing", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		hostCfg := target.SSHHostConfig{
			Host:          "example.com",
			Port:          22,
			Username:      "testuser",
			HostKeyPolicy: target.IgnoreHostKey,
			AuthMethod:    target.SSHAuthMethodPassword,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		_, err := connector.buildSSHClientConfig(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1}, nil, true)
		require.Error(t, err)

		var msgErr message.Message
		require.True(t, errors.As(err, &msgErr))
		assert.Equal(t, message.CommonUnknownError, msgErr.Code())
	})

	t.Run("should succeed without known_hosts if host key policy is ignore", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create directory and files for the test.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/private_key", []byte(testPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/private_key",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		config, err := connector.buildSSHClientConfig(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1}, nil, true)
		require.NoError(t, err)
		assert.Equal(t, "testuser", config.User)
	})

	t.Run("should prefer host key algorithms from known hosts", func(t *testing.T) {
		fs := afero.NewOsFs()
		tmpDir := t.TempDir()
		keysDir := filepath.Join(tmpDir, "keys")

		err := os.MkdirAll(keysDir, perms.SshDirPerm)
		require.NoError(t, err)
		privateKeyPath := filepath.Join(keysDir, "private_key")
		err = os.WriteFile(privateKeyPath, []byte(testPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(keysDir, "known_hosts"), []byte(makeKnownHostsLine(t, "example.com", 22)+"\n"), perms.KnownHostsPerm)
		require.NoError(t, err)

		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: privateKeyPath,
			HostKeyPolicy:      target.AcceptNewHost,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return tmpDir, nil }, nil)
		config, err := connector.buildSSHClientConfig(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1}, nil, true)
		require.NoError(t, err)

		expectedAlgorithms := []string{ssh.KeyAlgoED25519}
		for _, algorithm := range defaultHostKeyAlgorithms() {
			if algorithm != ssh.KeyAlgoED25519 {
				expectedAlgorithms = append(expectedAlgorithms, algorithm)
			}
		}

		assert.Equal(t, expectedAlgorithms, config.HostKeyAlgorithms)
	})

	t.Run("should keep default host key algorithms when host is absent from known_hosts", func(t *testing.T) {
		fs := afero.NewOsFs()
		tmpDir := t.TempDir()
		keysDir := filepath.Join(tmpDir, "keys")

		err := os.MkdirAll(keysDir, perms.SshDirPerm)
		require.NoError(t, err)
		privateKeyPath := filepath.Join(keysDir, "private_key")
		err = os.WriteFile(privateKeyPath, []byte(testPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(keysDir, "known_hosts"), []byte(makeKnownHostsLine(t, "other.example.com", 22)+"\n"), perms.KnownHostsPerm)
		require.NoError(t, err)

		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: privateKeyPath,
			HostKeyPolicy:      target.AcceptNewHost,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return tmpDir, nil }, nil)
		config, err := connector.buildSSHClientConfig(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1}, nil, true)
		require.NoError(t, err)
		assert.Equal(t, defaultHostKeyAlgorithms(), config.HostKeyAlgorithms)
	})

	t.Run("should expand RSA known_hosts entries to supported RSA host key algorithms", func(t *testing.T) {
		fs := afero.NewOsFs()
		tmpDir := t.TempDir()
		keysDir := filepath.Join(tmpDir, "keys")

		err := os.MkdirAll(keysDir, perms.SshDirPerm)
		require.NoError(t, err)
		privateKeyPath := filepath.Join(keysDir, "private_key")
		err = os.WriteFile(privateKeyPath, []byte(testPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		rsaSigner, err := ssh.NewSignerFromKey(rsaKey)
		require.NoError(t, err)

		err = os.WriteFile(filepath.Join(keysDir, "known_hosts"), []byte(makeKnownHostsLineForSigner("example.com", 22, rsaSigner)+"\n"), perms.KnownHostsPerm)
		require.NoError(t, err)

		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: privateKeyPath,
			HostKeyPolicy:      target.AcceptNewHost,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return tmpDir, nil }, nil)
		config, err := connector.buildSSHClientConfig(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1}, nil, true)
		require.NoError(t, err)
		require.Len(t, config.HostKeyAlgorithms, len(defaultHostKeyAlgorithms()))
		assert.Equal(t, []string{ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSA}, config.HostKeyAlgorithms[:3])
		assert.Contains(t, config.HostKeyAlgorithms, ssh.KeyAlgoED25519)
	})

}

func TestPublicKeyAuthCallback(t *testing.T) {
	t.Run("should fail with invalid private key", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create directory and files for the test.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/bad_key", []byte("not a valid key"), perms.PrivateKeyPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/known_hosts", []byte("dummy known hosts"), perms.KnownHostsPerm)
		require.NoError(t, err)

		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/bad_key",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		callback, err := connector.publicKeyAuthCallback(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1})
		require.NoError(t, err)
		require.NotNil(t, callback)
		_, err = callback()
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshKeyFileNotParsableForTarget, msgErr.Code())
		assert.Equal(t, "/keys/bad_key", msgErr.Metadata()["path"])
		assert.Equal(t, "testuser@example.com", msgErr.Metadata()["target"])
	})

	t.Run("prompts for passphrase-protected key", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create directory and files for the test.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/protected", []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/protected",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		var promptCfg PromptConfig
		called := false
		secretPromptProvider := func(cfg PromptConfig) ([]byte, error) {
			called = true
			promptCfg = cfg
			return []byte("passphrase"), nil
		}

		dialInfo := SSHDialInfo{
			SecretPromptProvider: secretPromptProvider,
			HopIndex:             1,
			TotalHops:            3,
		}
		callback, err := connector.publicKeyAuthCallback(hostCfg, dialInfo)
		require.NoError(t, err)
		require.NotNil(t, callback)
		_, err = callback()
		require.NoError(t, err)
		require.True(t, called)
		assert.Equal(t, SecretTypeKeyPassphrase, promptCfg.SecretType)
		assert.Equal(t, hostCfg.Host, promptCfg.Host)
		assert.Equal(t, 2, promptCfg.JumpIndex)
		assert.Equal(t, hostCfg.PrivateKeyFilename, promptCfg.KeyPath)
		assert.Equal(t, 1, promptCfg.CurrentAttempt)
		assert.Equal(t, SSHPromptMaxAttempts, promptCfg.MaxAttempts)
	})

	t.Run("retries passphrase prompt until success", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/protected", []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/protected",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		var attempts []int
		var maxAttempts []int
		secretPromptProvider := func(cfg PromptConfig) ([]byte, error) {
			attempts = append(attempts, cfg.CurrentAttempt)
			maxAttempts = append(maxAttempts, cfg.MaxAttempts)
			if len(attempts) < SSHPromptMaxAttempts {
				return []byte("wrong"), nil
			}
			return []byte("passphrase"), nil
		}

		dialInfo := SSHDialInfo{
			SecretPromptProvider: secretPromptProvider,
			HopIndex:             0,
			TotalHops:            1,
			SignerCache:          &signerCache{},
		}
		callback, err := connector.publicKeyAuthCallback(hostCfg, dialInfo)
		require.NoError(t, err)
		require.NotNil(t, callback)
		_, err = callback()
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3}, attempts)
		assert.Equal(t, []int{SSHPromptMaxAttempts, SSHPromptMaxAttempts, SSHPromptMaxAttempts}, maxAttempts)
	})

	t.Run("reuses cached passphrase signer", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/protected", []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/protected",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		promptCalls := 0
		secretPromptProvider := func(cfg PromptConfig) ([]byte, error) {
			promptCalls++
			return []byte("passphrase"), nil
		}

		dialInfo := SSHDialInfo{
			SecretPromptProvider: secretPromptProvider,
			HopIndex:             0,
			TotalHops:            1,
			SignerCache:          &signerCache{},
		}

		callback, err := connector.publicKeyAuthCallback(hostCfg, dialInfo)
		require.NoError(t, err)
		require.NotNil(t, callback)
		_, err = callback()
		require.NoError(t, err)
		assert.Equal(t, 1, promptCalls)

		callback, err = connector.publicKeyAuthCallback(hostCfg, dialInfo)
		require.NoError(t, err)
		require.NotNil(t, callback)
		_, err = callback()
		require.NoError(t, err)
		assert.Equal(t, 1, promptCalls)
	})

	t.Run("returns prompt failure error", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create directory and files for the test.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/protected", []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/protected",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		promptErr := errors.New("prompt failed")
		secretPromptProvider := func(cfg PromptConfig) ([]byte, error) {
			return nil, promptErr
		}

		callback, err := connector.publicKeyAuthCallback(hostCfg, SSHDialInfo{
			SecretPromptProvider: secretPromptProvider,
			HopIndex:             0,
			TotalHops:            1,
		})
		require.NoError(t, err)
		require.NotNil(t, callback)
		_, err = callback()
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		require.True(t, ok)
		assert.Equal(t, message.CliServiceTargetloginPromptFailed, msgErr.Code())
		assert.Equal(t, hostCfg.DisplayString(), msgErr.Metadata()["target"])
		assert.Equal(t, hostCfg.PrivateKeyFilename, msgErr.Metadata()["path"])
	})

	t.Run("returns incorrect passphrase error", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		// Create directory and files for the test.
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/protected", []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		var promptSleeps []time.Duration
		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, func(d time.Duration) {
			promptSleeps = append(promptSleeps, d)
		})
		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/protected",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		var attempts []PromptConfig
		secretPromptProvider := func(cfg PromptConfig) ([]byte, error) {
			attempts = append(attempts, cfg)
			return []byte("incorrect_passphrase"), nil
		}

		callback, err := connector.publicKeyAuthCallback(hostCfg, SSHDialInfo{
			SecretPromptProvider: secretPromptProvider,
			HopIndex:             0,
			TotalHops:            1,
		})
		require.NoError(t, err)
		require.NotNil(t, callback)
		_, err = callback()
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		require.True(t, ok)
		assert.Equal(t, message.EngineConductorSshKeyFileIncorrectPassphraseForTarget, msgErr.Code())
		assert.Equal(t, hostCfg.DisplayString(), msgErr.Metadata()["target"])
		assert.Equal(t, hostCfg.PrivateKeyFilename, msgErr.Metadata()["path"])
		assert.Len(t, attempts, SSHPromptMaxAttempts)
		for i, attemptCfg := range attempts {
			assert.Equal(t, SecretTypeKeyPassphrase, attemptCfg.SecretType)
			assert.Equal(t, hostCfg.Host, attemptCfg.Host)
			assert.Equal(t, 0, attemptCfg.JumpIndex)
			assert.Equal(t, hostCfg.PrivateKeyFilename, attemptCfg.KeyPath)
			assert.Equal(t, i+1, attemptCfg.CurrentAttempt)
			assert.Equal(t, SSHPromptMaxAttempts, attemptCfg.MaxAttempts)
		}
		assert.Equal(t, []time.Duration{sshInitialRetryDelay, sshInitialRetryDelay * 2}, promptSleeps)
	})

	t.Run("auto-detect skips passphrase-protected keys when prompting is disabled", func(t *testing.T) {
		homeDir := t.TempDir()
		fs := afero.NewOsFs()
		t.Setenv("HOME", homeDir)

		sshDir := filepath.Join(homeDir, ".ssh")

		err := fs.MkdirAll(sshDir, perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, filepath.Join(sshDir, "01_passphrase"), []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, filepath.Join(sshDir, "02_no_passphrase"), []byte(testPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		hostCfg := target.SSHHostConfig{
			Host:          "example.com",
			Port:          22,
			Username:      "testuser",
			HostKeyPolicy: target.IgnoreHostKey,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return homeDir, nil }, nil)
		callback, err := connector.publicKeyAuthCallback(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1})
		require.NoError(t, err)
		require.NotNil(t, callback)
		signers, err := callback()
		require.NoError(t, err)
		require.NotEmpty(t, signers)
	})
}

func TestPublicKeyAuthFunc(t *testing.T) {
	t.Run("returns error when no keys are available", func(t *testing.T) {
		fs := afero.NewMemMapFs()

		hostCfg := target.SSHHostConfig{
			Host:          "example.com",
			Port:          22,
			Username:      "testuser",
			HostKeyPolicy: target.IgnoreHostKey,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		authMethod, err := connector.publicKeyAuthFunc(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1})
		require.Error(t, err)
		assert.Nil(t, authMethod)

		var msgErr message.Message
		require.True(t, errors.As(err, &msgErr))
		assert.Equal(t, message.EngineConductorSshNoValidKeys, msgErr.Code())
	})

	t.Run("returns auth method when key path is provided", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/private_key", []byte("unused"), perms.PrivateKeyPerm)
		require.NoError(t, err)

		hostCfg := target.SSHHostConfig{
			Host:               "example.com",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/private_key",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		connector := NewSecureConnector(fs, nil, nil, func() (string, error) { return "/home/test", nil }, nil)
		authMethod, err := connector.publicKeyAuthFunc(hostCfg, SSHDialInfo{HopIndex: 0, TotalHops: 1})
		require.NoError(t, err)
		require.NotNil(t, authMethod)
	})
}

func TestSSHConnect(t *testing.T) {
	t.Run("should fail when no jump hosts provided", func(t *testing.T) {
		connector := NewSecureConnector(afero.NewMemMapFs(), nil, nil, func() (string, error) { return "/home/test", nil }, nil)

		tgt := &target.SSHTarget{
			Jumps: []target.SSHHostConfig{},
		}

		_, err := connector.SSHConnect(context.Background(), tgt, PromptProviders{}, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no SSH host configuration provided")
	})

	t.Run("connects direct target using provided dialer", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/private_key", []byte(testPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		var dialCalled bool
		fakeDial := func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			dialCalled = true
			assert.Equal(t, "single-host:22", address)
			require.NotNil(t, config)
			assert.Equal(t, "directuser", config.User)
			return newTestSSHClient(t), nil
		}

		connector := NewSecureConnector(fs, fakeDial, nil, func() (string, error) { return "/home/test", nil }, nil)

		tgt := &target.SSHTarget{
			Jumps: []target.SSHHostConfig{
				{
					Host:               "single-host",
					Port:               22,
					Username:           "directuser",
					PrivateKeyFilename: "/keys/private_key",
					HostKeyPolicy:      target.IgnoreHostKey,
				},
			},
		}

		client, err := connector.SSHConnect(context.Background(), tgt, PromptProviders{}, 0)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.True(t, dialCalled)
	})
}

func noSleep(time.Duration) {}

func TestPasswordPrompter(t *testing.T) {
	t.Run("enforces max attempts for password prompt", func(t *testing.T) {
		var attempts []int
		provider := func(cfg PromptConfig) ([]byte, error) {
			attempts = append(attempts, cfg.CurrentAttempt)
			return []byte("pw"), nil
		}

		prompter, err := newPasswordAuthPrompter("example.com", "testuser", 1, SSHDialInfo{SecretPromptProvider: provider}, SSHPromptMaxAttempts)
		require.NoError(t, err)

		for i := 0; i < SSHPromptMaxAttempts; i++ {
			password, err := prompter.Password()
			require.NoError(t, err)
			assert.Equal(t, "pw", password)

			canRetry := prompter.MarkAuthFailureAndCheckRetry()
			if i < SSHPromptMaxAttempts-1 {
				assert.True(t, canRetry)
			} else {
				assert.False(t, canRetry)
			}
		}

		_, err = prompter.Password()
		require.Error(t, err)
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		assert.Equal(t, message.EngineConductorSshPasswordIncorrect, msg.Code())
		assert.Equal(t, "example.com", msg.Metadata()["host"])
		assert.Equal(t, "1", msg.Metadata()["jumpIndex"])
		assert.Equal(t, strconv.Itoa(SSHPromptMaxAttempts), msg.Metadata()["maxAttempts"])
		assert.Equal(t, []int{1, 2, 3}, attempts)
	})

	t.Run("resets attempts after success", func(t *testing.T) {
		var attempts []int
		provider := func(cfg PromptConfig) ([]byte, error) {
			attempts = append(attempts, cfg.CurrentAttempt)
			return []byte("pw"), nil
		}

		prompter, err := newPasswordAuthPrompter("example.com", "testuser", 0, SSHDialInfo{SecretPromptProvider: provider}, SSHPromptMaxAttempts)
		require.NoError(t, err)

		password, err := prompter.Password()
		require.NoError(t, err)
		assert.Equal(t, "pw", password)
		assert.Equal(t, []int{1}, attempts)

		prompter.ResetOnSuccess()
		attempts = attempts[:0]

		password, err = prompter.Password()
		require.NoError(t, err)
		assert.Equal(t, "pw", password)
		assert.Equal(t, []int{1}, attempts)
	})
}

func TestKeyboardInteractivePasswordChallenge(t *testing.T) {
	acceptCases := []struct {
		name     string
		question string
	}{
		{
			name:     "answers one hidden password prompt",
			question: "Password:",
		},
		{
			name:     "answers lowercase password prompt",
			question: "password:",
		},
		{
			name:     "answers enter password prompt",
			question: "Enter password:",
		},
		{
			name:     "answers enter your password prompt",
			question: "Enter your password:",
		},
		{
			name:     "answers username password prompt",
			question: "testuser's password:",
		},
		{
			name:     "answers username host password prompt",
			question: "testuser@example.com's password:",
		},
		{
			name:     "answers host password prompt",
			question: "example.com password:",
		},
		{
			name:     "answers ssh password prompt",
			question: "SSH password:",
		},
		{
			name:     "answers login password prompt",
			question: "Login password:",
		},
		{
			name:     "answers unix password prompt",
			question: "Unix password:",
		},
	}
	for _, word := range []string{
		"again",
		"change",
		"code",
		"confirm",
		"current",
		"expired",
		"new",
		"old",
		"otp",
		"repeat",
		"retype",
		"token",
		"verification",
	} {
		acceptCases = append(acceptCases, struct {
			name     string
			question string
		}{
			name:     fmt.Sprintf("answers possessive username %q matching unsupported prompt word", word),
			question: fmt.Sprintf("%s's password:", word),
		})
	}

	for _, tc := range acceptCases {
		t.Run(tc.name, func(t *testing.T) {
			promptCalls := 0
			provider := func(cfg PromptConfig) ([]byte, error) {
				promptCalls++
				return []byte("pw"), nil
			}
			prompter, err := newPasswordAuthPrompter("example.com", "testuser", 0, SSHDialInfo{SecretPromptProvider: provider}, SSHPromptMaxAttempts)
			require.NoError(t, err)
			challenge, err := keyboardInteractivePasswordChallenge(prompter)
			require.NoError(t, err)

			answers, err := challenge("", "", []string{tc.question}, []bool{false})
			require.NoError(t, err)
			assert.Equal(t, []string{"pw"}, answers)
			assert.Equal(t, 1, promptCalls)
		})
	}

	t.Run("answers zero-question informational challenge without prompting", func(t *testing.T) {
		promptCalls := 0
		provider := func(cfg PromptConfig) ([]byte, error) {
			promptCalls++
			return []byte("pw"), nil
		}
		prompter, err := newPasswordAuthPrompter("example.com", "testuser", 0, SSHDialInfo{SecretPromptProvider: provider}, SSHPromptMaxAttempts)
		require.NoError(t, err)
		challenge, err := keyboardInteractivePasswordChallenge(prompter)
		require.NoError(t, err)

		answers, err := challenge("", "authenticated", nil, nil)
		require.NoError(t, err)
		assert.Empty(t, answers)
		assert.Zero(t, promptCalls)
	})

	rejectCases := []struct {
		name      string
		questions []string
		echos     []bool
	}{
		{
			name:      "rejects multi-question prompt",
			questions: []string{"Password:", "Verification code:"},
			echos:     []bool{false, false},
		},
		{
			name:      "rejects echo-enabled prompt",
			questions: []string{"Password:"},
			echos:     []bool{true},
		},
		{
			name:      "rejects non-password prompt",
			questions: []string{"Verification code:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects new password prompt",
			questions: []string{"New password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects old password prompt",
			questions: []string{"Old password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects password again prompt",
			questions: []string{"Password again:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects change password prompt",
			questions: []string{"Change password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects password code prompt",
			questions: []string{"Password code:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects confirm password prompt",
			questions: []string{"Confirm password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects expired password prompt",
			questions: []string{"Expired password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects repeat password prompt",
			questions: []string{"Repeat password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects retype password prompt",
			questions: []string{"Retype password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects password token prompt",
			questions: []string{"Password token:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects verification password prompt",
			questions: []string{"Verification password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects current password prompt",
			questions: []string{"Current password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects one-time password prompt",
			questions: []string{"One-time password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects one time password prompt",
			questions: []string{"One time password:"},
			echos:     []bool{false},
		},
		{
			name:      "rejects otp password prompt",
			questions: []string{"OTP password:"},
			echos:     []bool{false},
		},
	}

	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			hook := test.NewGlobal()
			promptCalls := 0
			provider := func(cfg PromptConfig) ([]byte, error) {
				promptCalls++
				return []byte("pw"), nil
			}
			prompter, err := newPasswordAuthPrompter("example.com", "testuser", 0, SSHDialInfo{SecretPromptProvider: provider}, SSHPromptMaxAttempts)
			require.NoError(t, err)
			challenge, err := keyboardInteractivePasswordChallenge(prompter)
			require.NoError(t, err)

			answers, err := challenge("", "", tc.questions, tc.echos)
			require.ErrorIs(t, err, errUnsupportedKeyboardInteractivePasswordPrompt)
			assert.Nil(t, answers)
			assert.Zero(t, promptCalls)
			entry := hook.LastEntry()
			require.NotNil(t, entry)
			assert.Equal(t, log.InfoLevel, entry.Level)
			assert.Equal(t, "Rejected unsupported SSH keyboard-interactive password prompt", entry.Message)
			assert.Equal(t, tc.questions, entry.Data["questions"])
			assert.Equal(t, tc.echos, entry.Data["echos"])
		})
	}
}

func TestDialSSHDirect(t *testing.T) {
	t.Run("should retry and succeed after an initial failure", func(t *testing.T) {
		attempts := 0
		fakeDial := func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			attempts++
			if attempts < 2 {
				return nil, errors.New("simulated dial failure")
			}
			// Return a dummy client (we do not use its methods in this test).
			return newTestSSHClient(t), nil
		}

		connector := NewSecureConnector(afero.NewMemMapFs(), fakeDial, nil, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}

		client, err := connector.dialSSHDirectWithRetry(context.Background(), "dummy", 22, config, 1, nil)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, 2, attempts)
	})

	t.Run("should fail after maximum attempts", func(t *testing.T) {
		dialErr := errors.New("simulated dial failure")
		fakeDial := func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			return nil, dialErr
		}

		connector := NewSecureConnector(afero.NewMemMapFs(), fakeDial, nil, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}

		client, err := connector.dialSSHDirectWithRetry(context.Background(), "dummy", 22, config, 2, nil)
		assert.Nil(t, client)

		expectedMetadata := map[string]string{
			"hostName": "dummy",
		}
		expectedErr := message.New(message.EngineConductorSshDirectConnFailedUnknown).WithCause(dialErr).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("re-prompts password on authentication failure even without network retries", func(t *testing.T) {
		authErr := errors.New("ssh: unable to authenticate, attempted methods [password]")
		attempts := 0
		var prompter *passwordAuthPrompter
		fakeDial := func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			attempts++
			if prompter != nil {
				_, err := prompter.Password()
				require.NoError(t, err)
			}
			if attempts < 3 {
				return nil, authErr
			}
			return newTestSSHClient(t), nil
		}

		var promptAttempts []int
		provider := func(cfg PromptConfig) ([]byte, error) {
			promptAttempts = append(promptAttempts, cfg.CurrentAttempt)
			return []byte(fmt.Sprintf("pw-%d", cfg.CurrentAttempt)), nil
		}

		var err error
		prompter, err = newPasswordAuthPrompter("dummy", "testuser", 0, SSHDialInfo{SecretPromptProvider: provider}, SSHPromptMaxAttempts)
		require.NoError(t, err)

		connector := NewSecureConnector(afero.NewMemMapFs(), fakeDial, nil, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}

		client, err := connector.dialSSHDirectWithRetry(context.Background(), "dummy", 22, config, 0, prompter)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, 3, attempts)
		assert.Equal(t, []int{1, 2}, promptAttempts)
	})

	t.Run("does not prompt password on non-auth failure", func(t *testing.T) {
		attempts := 0
		netErr := errors.New("simulated network failure")
		fakeDial := func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			attempts++
			if attempts == 1 {
				return nil, netErr
			}
			return newTestSSHClient(t), nil
		}

		promptCalls := 0
		provider := func(cfg PromptConfig) ([]byte, error) {
			promptCalls++
			return []byte("pw"), nil
		}

		prompter, err := newPasswordAuthPrompter("dummy", "testuser", 0, SSHDialInfo{SecretPromptProvider: provider}, SSHPromptMaxAttempts)
		require.NoError(t, err)

		connector := NewSecureConnector(afero.NewMemMapFs(), fakeDial, nil, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}

		client, err := connector.dialSSHDirectWithRetry(context.Background(), "dummy", 22, config, 1, prompter)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, 2, attempts)
		assert.Equal(t, 0, promptCalls)
	})

	t.Run("skips prompting when server does not offer password auth", func(t *testing.T) {
		dialCalls := 0
		fakeDial := func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			dialCalls++
			return nil, fmt.Errorf("ssh: unable to authenticate, attempted methods [none], no supported methods remain")
		}

		promptCalls := 0
		provider := func(cfg PromptConfig) ([]byte, error) {
			promptCalls++
			return []byte("pw"), nil
		}

		prompter, err := newPasswordAuthPrompter("dummy", "testuser", 0, SSHDialInfo{SecretPromptProvider: provider}, SSHPromptMaxAttempts)
		require.NoError(t, err)

		connector := NewSecureConnector(afero.NewMemMapFs(), fakeDial, nil, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}

		client, err := connector.dialSSHDirectWithRetry(context.Background(), "dummy", 22, config, 1, prompter)
		assert.Nil(t, client)

		expected := message.New(message.EngineConductorSshDirectConnPasswordAuthDisabled).
			WithCause(errPasswordAuthNotOffered).
			WithMetadata(map[string]string{"hostName": "dummy"})
		assert.Equal(t, expected, err)
		assert.Zero(t, promptCalls)
		assert.Equal(t, 1, dialCalls)
	})
}

func TestDialSSHWithRetry(t *testing.T) {
	t.Run("skips key passphrase when publickey unsupported", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/protected", []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		dialCalls := 0
		dialDirect := func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			dialCalls++
			require.Equal(t, "unsupported:22", address)
			return nil, fmt.Errorf("ssh: unable to authenticate, attempted methods [none], no supported methods remain")
		}

		connector := NewSecureConnector(fs, dialDirect, nil, func() (string, error) { return "/home/test", nil }, noSleep)

		hostCfg := target.SSHHostConfig{
			Host:               "unsupported",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/protected",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		dialInfo := SSHDialInfo{
			HopIndex:  0,
			TotalHops: 1,
		}

		client, err := connector.DialSSHWithRetry(context.Background(), hostCfg, nil, 0, dialInfo)
		assert.Nil(t, client)

		expected := message.New(message.EngineConductorSshDirectConnKeyAuthDisabled).
			WithCause(errPublicKeyAuthNotOffered).
			WithMetadata(map[string]string{"hostName": "unsupported"})
		assert.Equal(t, expected, err)
		assert.Equal(t, 1, dialCalls, "probe only expected")
	})

	t.Run("connects when publickey supported", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		err := fs.MkdirAll("/keys", perms.SshDirPerm)
		require.NoError(t, err)
		err = afero.WriteFile(fs, "/keys/protected", []byte(passphraseProtectedPrivateKey), perms.PrivateKeyPerm)
		require.NoError(t, err)

		dialCalls := 0
		dialDirect := func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			dialCalls++
			require.Equal(t, "supported:22", address)
			if dialCalls == 1 {
				return nil, errPublicKeyProbeSuccess
			}
			return newTestSSHClient(t), nil
		}

		connector := NewSecureConnector(fs, dialDirect, nil, func() (string, error) { return "/home/test", nil }, noSleep)

		hostCfg := target.SSHHostConfig{
			Host:               "supported",
			Port:               22,
			Username:           "testuser",
			PrivateKeyFilename: "/keys/protected",
			HostKeyPolicy:      target.IgnoreHostKey,
		}

		dialInfo := SSHDialInfo{
			HopIndex:  0,
			TotalHops: 1,
		}

		client, err := connector.DialSSHWithRetry(context.Background(), hostCfg, nil, 0, dialInfo)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, 2, dialCalls, "probe plus real dial expected")
	})
}

func TestSecureConnectSupportsKeyboardInteractivePasswordAuth(t *testing.T) {
	server := StartTestSSHKeyboardInteractiveServer(t, "user", "pass")
	tgt := &target.SSHTarget{
		Jumps: []target.SSHHostConfig{
			{
				Host:          server.Host,
				Port:          server.Port,
				Username:      server.User,
				HostKeyPolicy: target.IgnoreHostKey,
				AuthMethod:    target.SSHAuthMethodPassword,
			},
		},
	}

	promptCalls := 0
	provider := func(cfg PromptConfig) ([]byte, error) {
		promptCalls++
		return []byte(server.Password), nil
	}

	client, err := NewDefaultSecureConnector().SecureConnectNoRetry(context.Background(), tgt, PromptProviders{
		SecretPromptProvider: provider,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	assert.Equal(t, 1, promptCalls)
}

func TestDialSSHViaClient(t *testing.T) {
	t.Run("should retry and succeed after an initial failure", func(t *testing.T) {
		attempts := 0
		fakeDialVia := func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			attempts++
			if attempts < 2 {
				return nil, errors.New("simulated dial via failure")
			}
			return newTestSSHClient(t), nil
		}

		connector := NewSecureConnector(afero.NewMemMapFs(), nil, fakeDialVia, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}
		dummyPrev := newTestSSHClient(t)

		client, err := connector.dialSSHViaClientWithRetry(context.Background(), dummyPrev, "prev:22", "dummy", 22, config, 1, nil)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, 2, attempts)
	})

	t.Run("should fail after maximum attempts", func(t *testing.T) {
		dialErr := errors.New("simulated dial via failure")
		fakeDialVia := func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			return nil, dialErr
		}

		connector := NewSecureConnector(afero.NewMemMapFs(), nil, fakeDialVia, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}
		dummyPrev := newTestSSHClient(t)

		client, err := connector.dialSSHViaClientWithRetry(context.Background(), dummyPrev, "prev", "dummy", 22, config, 2, nil)
		assert.Nil(t, client)

		expectedMetadata := map[string]string{
			"nextHost": "dummy",
			"prevHost": "prev",
			"hostName": "dummy",
		}
		expectedErr := message.New(message.EngineConductorSshJumpConnFailedUnknown).WithCause(dialErr).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("returns timeout-specific message when dial times out", func(t *testing.T) {
		timeoutErr := &net.OpError{Err: context.DeadlineExceeded}
		fakeDialVia := func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			return nil, timeoutErr
		}

		connector := NewSecureConnector(afero.NewMemMapFs(), nil, fakeDialVia, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}
		dummyPrev := newTestSSHClient(t)

		client, err := connector.dialSSHViaClientWithRetry(context.Background(), dummyPrev, "prev", "dummy", 2200, config, 0, nil)
		assert.Nil(t, client)

		expectedMetadata := map[string]string{
			"prevHost":    "prev",
			"nextAddress": "dummy:2200",
			"nextHost":    "dummy",
			"nextPort":    "2200",
			"hostName":    "dummy",
		}
		expectedErr := message.New(message.EngineConductorSshJumpConnFailedTimeout).WithCause(timeoutErr).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("re-prompts password on authentication failure", func(t *testing.T) {
		authErr := errors.New("ssh: unable to authenticate, attempted methods [password]")
		attempts := 0
		var prompter *passwordAuthPrompter
		fakeDialVia := func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			attempts++
			if prompter != nil {
				_, err := prompter.Password()
				require.NoError(t, err)
			}
			if attempts < 3 {
				return nil, authErr
			}
			return newTestSSHClient(t), nil
		}

		var promptAttempts []int
		provider := func(cfg PromptConfig) ([]byte, error) {
			promptAttempts = append(promptAttempts, cfg.CurrentAttempt)
			return []byte(fmt.Sprintf("pw-%d", cfg.CurrentAttempt)), nil
		}

		var err error
		prompter, err = newPasswordAuthPrompter("dummy", "testuser", 1, SSHDialInfo{SecretPromptProvider: provider, TotalHops: 2, HopIndex: 1}, SSHPromptMaxAttempts)
		require.NoError(t, err)

		connector := NewSecureConnector(afero.NewMemMapFs(), nil, fakeDialVia, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}
		dummyPrev := newTestSSHClient(t)

		client, err := connector.dialSSHViaClientWithRetry(context.Background(), dummyPrev, "prev", "dummy", 22, config, 0, prompter)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, 3, attempts)
		assert.Equal(t, []int{1, 2}, promptAttempts)
	})

	t.Run("does not prompt password on non-auth failure", func(t *testing.T) {
		attempts := 0
		netErr := errors.New("simulated network failure")
		fakeDialVia := func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			attempts++
			if attempts == 1 {
				return nil, netErr
			}
			return newTestSSHClient(t), nil
		}

		promptCalls := 0
		provider := func(cfg PromptConfig) ([]byte, error) {
			promptCalls++
			return []byte("pw"), nil
		}

		prompter, err := newPasswordAuthPrompter("dummy", "testuser", 1, SSHDialInfo{SecretPromptProvider: provider, TotalHops: 2, HopIndex: 1}, SSHPromptMaxAttempts)
		require.NoError(t, err)

		connector := NewSecureConnector(afero.NewMemMapFs(), nil, fakeDialVia, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}
		dummyPrev := newTestSSHClient(t)

		client, err := connector.dialSSHViaClientWithRetry(context.Background(), dummyPrev, "prev", "dummy", 22, config, 1, prompter)
		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Equal(t, 2, attempts)
		assert.Equal(t, 0, promptCalls)
	})

	t.Run("skips prompting when jump host does not offer password auth", func(t *testing.T) {
		dialCalls := 0
		fakeDialVia := func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			dialCalls++
			return nil, fmt.Errorf("ssh: unable to authenticate, attempted methods [none], no supported methods remain")
		}

		promptCalls := 0
		provider := func(cfg PromptConfig) ([]byte, error) {
			promptCalls++
			return []byte("pw"), nil
		}

		prompter, err := newPasswordAuthPrompter("jump", "testuser", 1, SSHDialInfo{SecretPromptProvider: provider, TotalHops: 2, HopIndex: 1}, SSHPromptMaxAttempts)
		require.NoError(t, err)

		connector := NewSecureConnector(afero.NewMemMapFs(), nil, fakeDialVia, nil, noSleep)
		config := &ssh.ClientConfig{Timeout: 1 * time.Second}
		dummyPrev := newTestSSHClient(t)

		client, err := connector.dialSSHViaClientWithRetry(context.Background(), dummyPrev, "prev", "jump", 22, config, 1, prompter)
		assert.Nil(t, client)

		expected := message.New(message.EngineConductorSshJumpConnPasswordAuthDisabled).
			WithCause(errPasswordAuthNotOffered).
			WithMetadata(map[string]string{
				"nextHost": "jump",
				"prevHost": "prev",
				"hostName": "jump",
			})
		assert.Equal(t, expected, err)
		assert.Zero(t, promptCalls)
		assert.Equal(t, 1, dialCalls)
	})
}

func TestDialSSHViaClientTimeoutsReturnOpError(t *testing.T) {
	// Start SSH server that delays accepting direct-tcpip to trigger the timeout.
	server := StartTestSSHServer(t, "user", "pass")
	server.DirectTCPIPAcceptDelay = 200 * time.Millisecond

	jumpCfg := &ssh.ClientConfig{
		User:            server.User,
		Auth:            []ssh.AuthMethod{ssh.Password(server.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // test helper server with known host key
		Timeout:         time.Second,
	}
	jumpClient, err := ssh.Dial("tcp", server.Address(), jumpCfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = jumpClient.Close() })

	connector := NewDefaultSecureConnector()
	timeoutCfg := &ssh.ClientConfig{
		User:            server.User,
		Auth:            []ssh.AuthMethod{ssh.Password(server.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // test helper server with known host key
		Timeout:         50 * time.Millisecond,
	}

	client, err := connector.DialSSHViaClient(jumpClient, server.Address(), timeoutCfg)
	assert.Nil(t, client)
	assert.ErrorContains(t, err, context.DeadlineExceeded.Error())
}

func TestSSHClient(t *testing.T) {
	t.Run("RunCommand should return error when no SSH connection is available", func(t *testing.T) {
		sshClient := &SSHClient{
			clients: []*ssh.Client{}, // empty list simulates no established connection
		}
		stdout, stderr, err := sshClient.RunCommand("ls")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no SSH connection available")
		assert.Empty(t, stdout)
		assert.Empty(t, stderr)
	})

	t.Run("SFTPClient should return error when no SSH connection is available", func(t *testing.T) {
		sshClient := &SSHClient{
			clients: []*ssh.Client{}, // empty list simulates no established connection
		}
		sftpClient, err := sshClient.SFTPClient()
		require.Nil(t, sftpClient)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no SSH connection available")
	})

	t.Run("SFTPClient should return existing SFTP client when available", func(t *testing.T) {
		sftpClient := &sftp.Client{}
		sshClient := &SSHClient{
			clients:    []*ssh.Client{},
			sftpClient: sftpClient,
		}
		sftpc, err := sshClient.SFTPClient()
		require.Equal(t, sftpClient, sftpc)
		require.NoError(t, err)
	})
}

func Test_trustNewOrFailMismatchKnownHostsWithFs(t *testing.T) {

	// Generate a dummy key
	signer, err := ssh.ParsePrivateKey([]byte(testPrivateKey))
	require.NoError(t, err)
	pubKey := signer.PublicKey()

	// Test data with invalid known_host entries
	const InvalidKnownHostsPath = "./test-data/known_hosts/invalid_entries"

	t.Run("adds new host key to known_hosts", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping test: chmod-based permission simulation doesn't work on Windows")
		}

		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		// Create empty file so knownhosts.New doesn't fail
		err := os.WriteFile(knownHostsPath, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		fs := afero.NewOsFs()

		callback, err := TrustNewOrFailMismatchKnownHostsWithFs(fs, knownHostsPath)
		require.NoError(t, err)

		signer, err := ssh.ParsePrivateKey([]byte(testPrivateKey))
		require.NoError(t, err)
		pubKey := signer.PublicKey()

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}

		err = callback("test.example.com:22", dummyAddr, pubKey)
		require.NoError(t, err)

		data, err := os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "test.example.com")
		assert.Contains(t, string(data), pubKey.Type())
	})

	t.Run("does not add host key to known_hosts again on subsequent callback", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping test: chmod-based permission simulation doesn't work on Windows")
		}

		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		err := os.WriteFile(knownHostsPath, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		fs := afero.NewOsFs()
		callback, err := TrustNewOrFailMismatchKnownHostsWithFs(fs, knownHostsPath)
		require.NoError(t, err)

		hostname := "test.example.com:22"
		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback(hostname, dummyAddr, pubKey)
		require.NoError(t, err)
		err = callback(hostname, dummyAddr, pubKey)
		require.NoError(t, err)

		data, err := os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		expectedLine := knownhosts.Line([]string{hostname}, pubKey)
		assert.Equal(t, 1, strings.Count(string(data), expectedLine))
	})

	t.Run("returns mismatch when host key differs", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		signer1, err := ssh.ParsePrivateKey([]byte(testPrivateKey))
		require.NoError(t, err)
		pubKey1 := signer1.PublicKey()

		signer2, err := ssh.ParsePrivateKeyWithPassphrase([]byte(passphraseProtectedPrivateKey), []byte("passphrase"))
		require.NoError(t, err)
		pubKey2 := signer2.PublicKey()

		entry := knownhosts.Line([]string{"[test.example.com]:22"}, pubKey1)
		err = os.WriteFile(knownHostsPath, []byte(entry+"\n"), perms.KnownHostsPerm)
		require.NoError(t, err)

		fs := afero.NewOsFs()
		callback, err := TrustNewOrFailMismatchKnownHostsWithFs(fs, knownHostsPath)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("test.example.com:22", dummyAddr, pubKey2)
		require.Error(t, err)
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, message.EngineConductorSshAcceptNewKeyMismatch, msg.Code())
	})

	t.Run("fails if known_hosts path is invalid", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping test: chmod-based permission simulation doesn't work on Windows")
		}

		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		// Create empty known_hosts file so knownhosts.New succeeds
		err := os.WriteFile(knownHostsPath, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		callback, err := TrustNewOrFailMismatchKnownHostsWithFs(afero.NewOsFs(), knownHostsPath)
		require.NoError(t, err)

		// Remove the parent directory to simulate invalid write path
		err = os.RemoveAll(tmpDir)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("test.example.com:22", dummyAddr, pubKey)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no such file or directory")
	})

	t.Run("fails if known_hosts directory is not writable", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("skipping test: chmod-based permission simulation doesn't work on Windows")
		}

		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "known_hosts")
		invalidPath := filepath.Join(tmpDir, "known_hosts_invalid")

		// Create the file
		err := os.WriteFile(path, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		// Create the invalid file
		err = os.WriteFile(invalidPath, []byte("foobar"), perms.KnownHostsPerm)
		require.NoError(t, err)

		// Make the directory read-only so we can't write lock or temp files
		err = os.Chmod(tmpDir, 0500)
		require.NoError(t, err)
		defer func() {
			err := os.Chmod(tmpDir, perms.SshDirPerm)
			require.NoError(t, err, "failed to restore permissions on tmpDir")
		}()

		fs := afero.NewOsFs()

		callback, err := TrustNewOrFailMismatchKnownHostsWithFs(fs, path)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("test.example.com:22", dummyAddr, pubKey)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to acquire file lock")
		assert.Contains(t, err.Error(), "permission denied")

		// Invalid entries also trigger temporary file writes
		callback, err = TrustNewOrFailMismatchKnownHostsWithFs(fs, invalidPath)
		require.NoError(t, err)
		err = callback("test.example.com:22", dummyAddr, pubKey)
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshSanitizeKnownHosts, msgErr.Code())
		assert.Equal(t, invalidPath, msgErr.Metadata()["filePath"])
	})

	t.Run("warns if known_hosts has invalid entries", func(t *testing.T) {
		fs := afero.NewOsFs()

		// Hook to the global logger to see the warnings
		hook := test.NewGlobal()

		// Warning when failure
		callback, err := TrustNewOrFailMismatchKnownHostsWithFs(fs, InvalidKnownHostsPath)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("test.example.com:22", dummyAddr, pubKey)
		require.NoError(t, err)
		require.NotEqual(t, 0, hook.Entries)

		last := hook.LastEntry()
		assert.EqualValues(t, log.WarnLevel, last.Level)
		assert.Contains(t, last.Message, "Invalid known_hosts entry")
	})
}

func Test_promptNewOrFailMismatchKnownHosts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping test: chmod-based permission simulation doesn't work on Windows")
	}

	signer, err := ssh.ParsePrivateKey([]byte(testPrivateKey))
	require.NoError(t, err)
	pubKey := signer.PublicKey()

	t.Run("prompts and appends when accepted", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		err := os.WriteFile(knownHostsPath, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		fs := afero.NewOsFs()
		dialInfo := SSHDialInfo{
			FingerprintPromptProvider: func(cfg HostKeyPromptConfig) (bool, error) {
				assert.Equal(t, "test.example.com", cfg.Host)
				assert.NotEmpty(t, cfg.HostKeyFingerprint)
				return true, nil
			},
			HopIndex:  0,
			TotalHops: 1,
		}
		hostCfg := target.SSHHostConfig{Host: "test.example.com"}

		callback, err := promptNewOrFailMismatchKnownHosts(fs, knownHostsPath, dialInfo, hostCfg)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("test.example.com:22", dummyAddr, pubKey)
		require.NoError(t, err)

		data, err := os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "test.example.com")
	})

	t.Run("rejects when user declines", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		err := os.WriteFile(knownHostsPath, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		fs := afero.NewOsFs()
		dialInfo := SSHDialInfo{
			FingerprintPromptProvider: func(cfg HostKeyPromptConfig) (bool, error) {
				return false, nil
			},
			HopIndex:  0,
			TotalHops: 1,
		}
		hostCfg := target.SSHHostConfig{Host: "test.example.com"}

		callback, err := promptNewOrFailMismatchKnownHosts(fs, knownHostsPath, dialInfo, hostCfg)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("test.example.com:22", dummyAddr, pubKey)
		require.Error(t, err)
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, message.EngineConductorSshPromptKeyRejected, msg.Code())
	})

	t.Run("errors with key unknown acceptance provider is nil", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		err := os.WriteFile(knownHostsPath, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		fs := afero.NewOsFs()
		dialInfo := SSHDialInfo{
			FingerprintPromptProvider: nil,
			HopIndex:                  0,
			TotalHops:                 1,
		}
		hostCfg := target.SSHHostConfig{Host: "test.example.com"}

		callback, err := promptNewOrFailMismatchKnownHosts(fs, knownHostsPath, dialInfo, hostCfg)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("test.example.com:22", dummyAddr, pubKey)
		require.Error(t, err)
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, message.EngineConductorSshStrictKeyUnknown, msg.Code())
	})

	t.Run("includes known aliases when the same key exists under another host", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		err := os.WriteFile(
			knownHostsPath,
			[]byte(knownhosts.Line([]string{"old.example.com:22"}, pubKey)+"\n"),
			perms.KnownHostsPerm,
		)
		require.NoError(t, err)

		fs := afero.NewOsFs()
		dialInfo := SSHDialInfo{
			FingerprintPromptProvider: func(cfg HostKeyPromptConfig) (bool, error) {
				assert.Equal(t, []string{"old.example.com"}, cfg.KnownAs)
				return true, nil
			},
			HopIndex:  0,
			TotalHops: 1,
		}
		hostCfg := target.SSHHostConfig{Host: "new.example.com"}

		callback, err := promptNewOrFailMismatchKnownHosts(fs, knownHostsPath, dialInfo, hostCfg)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("new.example.com:22", dummyAddr, pubKey)
		require.NoError(t, err)
	})

	t.Run("errors with prompt failed when acceptance provider errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		err := os.WriteFile(knownHostsPath, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		fs := afero.NewOsFs()
		promptErr := errors.New("prompt failed")
		dialInfo := SSHDialInfo{
			FingerprintPromptProvider: func(cfg HostKeyPromptConfig) (bool, error) {
				return false, promptErr
			},
			HopIndex:  0,
			TotalHops: 1,
		}
		hostCfg := target.SSHHostConfig{Host: "test.example.com"}

		callback, err := promptNewOrFailMismatchKnownHosts(fs, knownHostsPath, dialInfo, hostCfg)
		require.NoError(t, err)

		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback("test.example.com:22", dummyAddr, pubKey)
		require.Error(t, err)
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, message.CliServiceTargetloginPromptFailed, msg.Code())
	})

	t.Run("does not prompt host key again on subsequent callback", func(t *testing.T) {
		tmpDir := t.TempDir()
		knownHostsPath := filepath.Join(tmpDir, "known_hosts")

		err := os.WriteFile(knownHostsPath, []byte(""), perms.KnownHostsPerm)
		require.NoError(t, err)

		fs := afero.NewOsFs()
		promptCalls := 0
		dialInfo := SSHDialInfo{
			FingerprintPromptProvider: func(cfg HostKeyPromptConfig) (bool, error) {
				promptCalls++
				return true, nil
			},
			HopIndex:  0,
			TotalHops: 1,
		}
		hostCfg := target.SSHHostConfig{Host: "test.example.com"}

		callback, err := promptNewOrFailMismatchKnownHosts(fs, knownHostsPath, dialInfo, hostCfg)
		require.NoError(t, err)

		hostname := "test.example.com:22"
		dummyAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}
		err = callback(hostname, dummyAddr, pubKey)
		require.NoError(t, err)
		err = callback(hostname, dummyAddr, pubKey)
		require.NoError(t, err)

		assert.Equal(t, 1, promptCalls)

		data, err := os.ReadFile(knownHostsPath)
		require.NoError(t, err)
		expectedLine := knownhosts.Line([]string{hostname}, pubKey)
		assert.Equal(t, 1, strings.Count(string(data), expectedLine))
	})
}

func Test_probePasswordAuthSupport(t *testing.T) {
	connector := NewDefaultSecureConnector()
	cfg := &ssh.ClientConfig{}
	metadata := map[string]string{"hostName": "test.example.com"}

	t.Run("returns message errors directly", func(t *testing.T) {
		expected := message.New(message.EngineConductorSshPromptKeyRejected)
		err := connector.probePasswordAuthSupport(
			cfg,
			func(*ssh.ClientConfig) (*ssh.Client, error) { return nil, expected },
			metadata,
			message.EngineConductorSshDirectConnFailedPasswordAuth,
			message.EngineConductorSshDirectConnPasswordAuthDisabled,
		)
		require.Error(t, err)
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, expected.Code(), msg.Code())
	})

	t.Run("wraps non-message errors with failure code", func(t *testing.T) {
		var cfg *ssh.ClientConfig
		err := connector.probePasswordAuthSupport(
			cfg,
			func(*ssh.ClientConfig) (*ssh.Client, error) { return nil, nil },
			metadata,
			message.EngineConductorSshDirectConnFailedPasswordAuth,
			message.EngineConductorSshDirectConnPasswordAuthDisabled,
		)
		require.Error(t, err)
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, message.EngineConductorSshDirectConnFailedPasswordAuth, msg.Code())
	})

	t.Run("accepts keyboard-interactive password prompt support", func(t *testing.T) {
		server := StartTestSSHKeyboardInteractiveServer(t, "user", "pass")
		cfg := &ssh.ClientConfig{
			User:    server.User,
			Timeout: time.Second,
		}

		err := connector.probePasswordAuthSupport(
			cfg,
			func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
				return connector.DialSSHDirect(server.Address(), cfg)
			},
			metadata,
			message.EngineConductorSshDirectConnFailedPasswordAuth,
			message.EngineConductorSshDirectConnPasswordAuthDisabled,
		)
		require.NoError(t, err)
	})
}

func Test_passwordAuthSupported_returnsSupportedWhenPasswordAndKeyboardInteractiveSupported(t *testing.T) {
	connector := NewDefaultSecureConnector()
	var keyboardInteractiveChallenges atomic.Int32
	serverCfg := newServerConfig(t, "user", "pass")
	serverCfg.KeyboardInteractiveCallback = func(c ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		keyboardInteractiveChallenges.Add(1)
		_, err := challenge("", "", []string{"Password:"}, []bool{false})
		return nil, err
	}
	server := startTestSSHServer(t, "user", "pass", serverCfg)
	cfg := &ssh.ClientConfig{
		User:    server.User,
		Timeout: time.Second,
	}

	var probeErr error
	supported, err := connector.passwordAuthSupported(cfg, func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
		client, err := connector.DialSSHDirect(server.Address(), cfg)
		probeErr = err
		return client, err
	})

	require.NoError(t, err)
	assert.True(t, supported)
	require.Error(t, probeErr)
	assert.ErrorIs(t, probeErr, errKeyboardInteractivePasswordProbeSuccess)
	assert.NotErrorIs(t, probeErr, errPasswordProbeSuccess)
	assert.Equal(t, int32(1), keyboardInteractiveChallenges.Load())
}

func Test_passwordAuthSupported_configuresPasswordBeforeKeyboardInteractive(t *testing.T) {
	connector := NewDefaultSecureConnector()
	dialCalled := false

	supported, err := connector.passwordAuthSupported(&ssh.ClientConfig{}, func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
		dialCalled = true
		require.Len(t, cfg.Auth, 2)
		assert.Contains(t, fmt.Sprintf("%T", cfg.Auth[0]), "passwordCallback")
		assert.Contains(t, fmt.Sprintf("%T", cfg.Auth[1]), "KeyboardInteractive")
		return nil, fmt.Errorf("ssh: unable to authenticate, attempted methods [none], no supported methods remain")
	})

	require.NoError(t, err)
	assert.False(t, supported)
	assert.True(t, dialCalled)
}

func Test_passwordAuthSupported_returnsSupportedWhenPasswordSupportedAndKeyboardInteractivePromptUnsupported(t *testing.T) {
	connector := NewDefaultSecureConnector()
	var keyboardInteractiveChallenges atomic.Int32
	serverCfg := newServerConfig(t, "user", "pass")
	serverCfg.KeyboardInteractiveCallback = func(c ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		keyboardInteractiveChallenges.Add(1)
		_, err := challenge("", "", []string{"New password:"}, []bool{false})
		return nil, err
	}
	server := startTestSSHServer(t, "user", "pass", serverCfg)
	cfg := &ssh.ClientConfig{
		User:    server.User,
		Timeout: time.Second,
	}

	var probeErr error
	supported, err := connector.passwordAuthSupported(cfg, func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
		client, err := connector.DialSSHDirect(server.Address(), cfg)
		probeErr = err
		return client, err
	})

	require.NoError(t, err)
	assert.True(t, supported)
	require.Error(t, probeErr)
	assert.ErrorIs(t, probeErr, errUnsupportedKeyboardInteractivePasswordPrompt)
	assert.NotErrorIs(t, probeErr, errPasswordProbeSuccess)
	assert.Equal(t, int32(1), keyboardInteractiveChallenges.Load())
}

func Test_passwordAuthSupported_usesPasswordWhenKeyboardInteractiveUnsupported(t *testing.T) {
	connector := NewDefaultSecureConnector()
	server := StartTestSSHServer(t, "user", "pass")
	cfg := &ssh.ClientConfig{
		User:    server.User,
		Timeout: time.Second,
	}

	var probeErr error
	supported, err := connector.passwordAuthSupported(cfg, func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
		client, err := connector.DialSSHDirect(server.Address(), cfg)
		probeErr = err
		return client, err
	})

	require.NoError(t, err)
	assert.True(t, supported)
	require.Error(t, probeErr)
	assert.ErrorIs(t, probeErr, errPasswordProbeSuccess)
	assert.NotErrorIs(t, probeErr, errKeyboardInteractivePasswordProbeSuccess)
}

func Test_passwordAuthSupported_usesKeyboardInteractiveWhenPasswordUnsupported(t *testing.T) {
	connector := NewDefaultSecureConnector()
	var keyboardInteractiveChallenges atomic.Int32
	serverCfg := newKeyboardInteractiveServerConfig(t, "user", "pass")
	serverCfg.KeyboardInteractiveCallback = func(c ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		keyboardInteractiveChallenges.Add(1)
		answers, err := challenge("", "", []string{"Password:"}, []bool{false})
		if err != nil {
			return nil, err
		}
		if c.User() == "user" && len(answers) == 1 && answers[0] == "pass" {
			return nil, nil
		}
		return nil, fmt.Errorf("keyboard-interactive password rejected for %q", c.User())
	}
	server := startTestSSHServer(t, "user", "pass", serverCfg)
	cfg := &ssh.ClientConfig{
		User:    server.User,
		Timeout: time.Second,
	}

	var probeErr error
	supported, err := connector.passwordAuthSupported(cfg, func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
		client, err := connector.DialSSHDirect(server.Address(), cfg)
		probeErr = err
		return client, err
	})

	require.NoError(t, err)
	assert.True(t, supported)
	require.Error(t, probeErr)
	assert.ErrorIs(t, probeErr, errKeyboardInteractivePasswordProbeSuccess)
	assert.NotErrorIs(t, probeErr, errPasswordProbeSuccess)
	assert.Equal(t, int32(1), keyboardInteractiveChallenges.Load())
}

func TestFindSSHKeysForTarget(t *testing.T) {
	newTestTarget := func(explicitKeys ...string) *target.SSHTarget {
		tgt := &target.SSHTarget{
			Jumps: []target.SSHHostConfig{
				{Host: "1.1.1.1", Port: 1, Username: "Hop1"},
				{Host: "2.2.2.2", Port: 2, Username: "Hop2"},
				{Host: "3.3.3.3", Port: 3, Username: "Hop3"},
				{Host: "4.4.4.4", Port: 4, Username: "Hop4"},
				{Host: "5.5.5.5", Port: 5, Username: "finalHop"},
			},
		}
		for i := 0; i < len(explicitKeys) && i < len(tgt.Jumps); i++ {
			if k := explicitKeys[i]; k != "" {
				tgt.Jumps[i].PrivateKeyFilename = k
			}
		}
		return tgt
	}

	// Map of host to valid keys for that host (only used with the fakeDialKeyCheck dial)
	validKeysPerHost := map[string][]string{
		"1.1.1.1": {"Key_1_3", "Key_1_5_External"},
		"2.2.2.2": {"Key_2"},
		"3.3.3.3": {"Key_1_3", "Key_3_External"},
		"4.4.4.4": {"Key_4_5"},
		"5.5.5.5": {"Key_4_5", "Key_1_5_External"},
	}

	keysFoundInCommonSSHDirs := []string{"Key_1_3", "Key_2", "Key_4_5"}

	firstValidKey := func(host string) string {
		for _, keyPath := range keysFoundInCommonSSHDirs {
			if slices.Contains(validKeysPerHost[host], keyPath) {
				return keyPath
			}
		}
		return ""
	}

	fakeDialKeyCheck := func(_ context.Context, tgt target.SSHHostConfig, _ *ssh.Client, _ int, _ SSHDialInfo) (*ssh.Client, error) {
		if validKeys, ok := validKeysPerHost[tgt.Host]; ok {
			if slices.Contains(validKeys, tgt.PrivateKeyFilename) {
				return newTestSSHClient(t), nil
			}
		}
		return nil, errors.New("simulated dial via failure")
	}
	fakeDialAlwaysFail := func(_ context.Context, _ target.SSHHostConfig, _ *ssh.Client, _ int, _ SSHDialInfo) (*ssh.Client, error) {
		return nil, errors.New("simulated dial via failure")
	}
	fakeDialAlwaysSucceed := func(_ context.Context, _ target.SSHHostConfig, _ *ssh.Client, _ int, _ SSHDialInfo) (*ssh.Client, error) {
		return newTestSSHClient(t), nil
	}

	t.Run("fails when dial fails", func(t *testing.T) {
		tgt := newTestTarget()
		kf := &SSHKeyFinder{
			Dial:                fakeDialAlwaysFail,
			HostPrivateKeyPaths: keysFoundInCommonSSHDirs,
			HostPrivateKeyDirs:  []string{"my/ssh/dir", "another/ssh/dir"},
		}

		_, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshMissingKeyForJump, msgErr.Code())
		assert.Equal(t, tgt.Jumps[0].DisplayString(), msgErr.Metadata()["jumpNode"])
		assert.Equal(t, "`Key_1_3`, `Key_2`, `Key_4_5`", msgErr.Metadata()["keyPaths"])
		assert.Equal(t, "`my/ssh/dir`, `another/ssh/dir`", msgErr.Metadata()["keyDirs"])
	})

	t.Run("direct target failures use direct missing key message", func(t *testing.T) {
		tgt := &target.SSHTarget{
			Jumps: []target.SSHHostConfig{
				{Host: "10.0.0.1", Port: 22, Username: "direct"},
			},
		}
		kf := &SSHKeyFinder{
			Dial:                fakeDialAlwaysFail,
			HostPrivateKeyPaths: keysFoundInCommonSSHDirs,
			HostPrivateKeyDirs:  []string{"my/ssh/dir"},
		}

		_, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshMissingKeyForTarget, msgErr.Code())
		assert.Equal(t, "direct@10.0.0.1", msgErr.Metadata()["target"])
		assert.Equal(t, "`Key_1_3`, `Key_2`, `Key_4_5`", msgErr.Metadata()["keyPaths"])
		assert.Equal(t, "`my/ssh/dir`", msgErr.Metadata()["keyDirs"])
	})

	t.Run("fails when tcp connectivity check fails", func(t *testing.T) {
		tgt := newTestTarget()
		kf := &SSHKeyFinder{
			Dial:                fakeDialAlwaysSucceed,
			HostPrivateKeyPaths: keysFoundInCommonSSHDirs,
			HostPrivateKeyDirs:  []string{"my/ssh/dir"},
			CheckPortConnectivity: func(context.Context, string, int32) error {
				return errors.New("simulated dial failure")
			},
		}

		_, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshTcpConnectFailed, msgErr.Code())
		assert.Equal(t, tgt.Jumps[0].DisplayString(), msgErr.Metadata()["jumpNode"])
		assert.Equal(t, "1", msgErr.Metadata()["port"])
	})

	t.Run("direct target fails when tcp connectivity check fails", func(t *testing.T) {
		tgt := &target.SSHTarget{
			Jumps: []target.SSHHostConfig{
				{Host: "10.0.0.1", Port: 22, Username: "direct"},
			},
		}

		kf := &SSHKeyFinder{
			Dial:                fakeDialAlwaysSucceed,
			HostPrivateKeyPaths: keysFoundInCommonSSHDirs,
			HostPrivateKeyDirs:  []string{"my/ssh/dir"},
			CheckPortConnectivity: func(context.Context, string, int32) error {
				return errors.New("simulated dial failure")
			},
		}

		_, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshTcpConnectFailedForTarget, msgErr.Code())
		assert.Equal(t, tgt.Jumps[0].DisplayString(), msgErr.Metadata()["target"])
		assert.Equal(t, "22", msgErr.Metadata()["port"])
	})

	t.Run("succeeds when tcp connectivity check passes", func(t *testing.T) {
		tgt := newTestTarget()
		var calls int
		kf := &SSHKeyFinder{
			Dial:                fakeDialAlwaysSucceed,
			HostPrivateKeyPaths: keysFoundInCommonSSHDirs,
			CheckPortConnectivity: func(_ context.Context, host string, port int32) error {
				calls++
				assert.Equal(t, tgt.Jumps[0].Host, host)
				assert.Equal(t, tgt.Jumps[0].Port, port)
				return nil
			},
		}

		keys, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		require.NoError(t, err)
		assert.Equal(t, 1, calls)
		assert.Equal(t, len(tgt.Jumps), len(keys))
	})

	t.Run("succeeds with first found key when dial always succeeds", func(t *testing.T) {
		tgt := newTestTarget()
		kf := &SSHKeyFinder{Dial: fakeDialAlwaysSucceed, HostPrivateKeyPaths: keysFoundInCommonSSHDirs}
		keys, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		require.NoError(t, err)

		assert.Equal(t, len(tgt.Jumps), len(keys))
		for _, k := range keys {
			assert.Equal(t, k, keysFoundInCommonSSHDirs[0])
		}
	})

	t.Run("succeeds with correctly found keys", func(t *testing.T) {
		tgt := newTestTarget()
		kf := &SSHKeyFinder{Dial: fakeDialKeyCheck, HostPrivateKeyPaths: keysFoundInCommonSSHDirs}
		keys, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		require.NoError(t, err)
		assert.Equal(t, len(keys), len(tgt.Jumps))
		for i := range tgt.Jumps {
			host := tgt.Jumps[i].Host
			assert.Equal(t, keys[i], firstValidKey(host))
		}
	})

	t.Run("fails when dial for an explicitly set key path fails", func(t *testing.T) {
		tgt := newTestTarget("Key_Explicit")

		kf := &SSHKeyFinder{
			Dial:                fakeDialAlwaysFail,
			HostPrivateKeyPaths: keysFoundInCommonSSHDirs,
			HostPrivateKeyDirs:  []string{"my/ssh/dir", "another/ssh/dir"},
		}
		_, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorSshMissingKeyForJump, msgErr.Code())
		assert.Equal(t, tgt.Jumps[0].DisplayString(), msgErr.Metadata()["jumpNode"])
		assert.Equal(t, "`Key_Explicit`, `Key_1_3`, `Key_2`, `Key_4_5`", msgErr.Metadata()["keyPaths"])
		assert.Equal(t, "`my/ssh/dir`, `another/ssh/dir`", msgErr.Metadata()["keyDirs"])
	})

	t.Run("succeeds with explicitly set non-common key paths", func(t *testing.T) {
		tgt := newTestTarget("Key_1_5_External", "", "Key_1_3", "", "Key_1_5_External")

		var expectedKeys []string
		for i := range tgt.Jumps {
			expectedKey := tgt.Jumps[i].PrivateKeyFilename
			if len(expectedKey) == 0 {
				host := tgt.Jumps[i].Host
				expectedKey = firstValidKey(host)
			}
			expectedKeys = append(expectedKeys, expectedKey)
		}

		kf := &SSHKeyFinder{Dial: fakeDialKeyCheck, HostPrivateKeyPaths: keysFoundInCommonSSHDirs}
		keys, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		require.NoError(t, err)
		assert.Equal(t, len(keys), len(tgt.Jumps))
		assert.Equal(t, len(keys), len(expectedKeys))
		for i := range keys {
			assert.Equal(t, keys[i], expectedKeys[i])
		}
	})

	t.Run("succeeds with common key path when explicitly set key path fails", func(t *testing.T) {
		tgt := newTestTarget("Key_Bad_Explicit", "", "", "Key_Bad_Explicit", "Key_1_5_External")

		var expectedKeys []string
		for i := range tgt.Jumps {
			expectedKey := tgt.Jumps[i].PrivateKeyFilename
			if len(expectedKey) == 0 || expectedKey == "Key_Bad_Explicit" {
				host := tgt.Jumps[i].Host
				expectedKey = firstValidKey(host)
			}
			expectedKeys = append(expectedKeys, expectedKey)
		}

		kf := &SSHKeyFinder{Dial: fakeDialKeyCheck, HostPrivateKeyPaths: keysFoundInCommonSSHDirs}
		keys, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		require.NoError(t, err)
		assert.Equal(t, len(keys), len(tgt.Jumps))
		assert.Equal(t, len(keys), len(expectedKeys))
		for i := range keys {
			assert.Equal(t, keys[i], expectedKeys[i])
		}
	})

	t.Run("succeeds with explicit key paths when no common key paths", func(t *testing.T) {
		tgt := newTestTarget("Key_1_5_External", "Key_2", "Key_1_3", "Key_4_5", "Key_4_5")

		var expectedKeys []string
		for i := range tgt.Jumps {
			expectedKey := tgt.Jumps[i].PrivateKeyFilename
			expectedKeys = append(expectedKeys, expectedKey)
		}

		kf := &SSHKeyFinder{Dial: fakeDialKeyCheck, HostPrivateKeyPaths: []string{}}
		keys, err := kf.FindSSHKeysForTarget(context.Background(), tgt)

		require.NoError(t, err)
		assert.Equal(t, len(keys), len(tgt.Jumps))
		assert.Equal(t, len(keys), len(expectedKeys))
		for i := range keys {
			assert.Equal(t, keys[i], expectedKeys[i])
		}
	})
}

type fakeConn struct{}

func (fakeConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (fakeConn) Write([]byte) (int, error)        { return 0, nil }
func (fakeConn) Close() error                     { return nil }
func (fakeConn) LocalAddr() net.Addr              { return nil }
func (fakeConn) RemoteAddr() net.Addr             { return nil }
func (fakeConn) SetDeadline(time.Time) error      { return nil }
func (fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (fakeConn) SetWriteDeadline(time.Time) error { return nil }

func TestCheckTCPConnectivity(t *testing.T) {
	t.Run("succeeds when port is reachable", func(t *testing.T) {
		oldFunc := tcpDialContext
		defer func() { tcpDialContext = oldFunc }()

		tcpDialContext = func(context.Context, string) (net.Conn, error) {
			return fakeConn{}, nil
		}

		err := checkTCPConnectivity(context.Background(), "example.com", 22)
		require.NoError(t, err)
	})

	t.Run("fails when port is not reachable", func(t *testing.T) {
		oldFunc := tcpDialContext
		defer func() { tcpDialContext = oldFunc }()

		tcpDialContext = func(context.Context, string) (net.Conn, error) {
			return nil, errors.New("simulated dial failure")
		}

		err := checkTCPConnectivity(context.Background(), "example.com", 22)
		require.Error(t, err)
	})
}

func TestNewDefaultSSHKeyFinder(t *testing.T) {
	t.Run("constructs default finder correctly", func(t *testing.T) {
		kf := NewDefaultSSHKeyFinder()

		require.NotNil(t, kf)
		assert.NotNil(t, kf.Dial)
		assert.NotNil(t, kf.CheckPortConnectivity)
		assert.NotNil(t, kf.HostPrivateKeyPaths)
		assert.NotNil(t, kf.HostPrivateKeyDirs)
	})
}
