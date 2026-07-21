// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestGenerateAndWriteKeys(t *testing.T) {
	t.Run("create SSH key pair succeeds", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		provisioner := &DefaultSSHKeyProvisioner{FS: fs}
		dataDir := "/foo"

		privateKeyPath, err := provisioner.CreateSSHKeyPair(dataDir)
		require.NoError(t, err)
		require.NotEmpty(t, privateKeyPath)

		// Read private key file
		privKeyBytes, err := afero.ReadFile(fs, privateKeyPath)
		require.NoError(t, err)
		require.NotEmpty(t, privKeyBytes)

		// Read corresponding public key file
		publicKeyPath := privateKeyPath + ".pub"
		pubKeyBytes, err := afero.ReadFile(fs, publicKeyPath)
		require.NoError(t, err)
		require.NotEmpty(t, pubKeyBytes)

		// Parse private key
		block, _ := pem.Decode(privKeyBytes)
		require.NotNil(t, block, "failed to parse private key PEM block")
		parsedPrivKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		require.NoError(t, err)

		// Derive public key from private key
		derivedPubKey, err := ssh.NewPublicKey(&parsedPrivKey.PublicKey)
		require.NoError(t, err)

		// Parse stored public key
		parsedPubKey, _, _, _, err := ssh.ParseAuthorizedKey(pubKeyBytes)
		require.NoError(t, err)

		// Check they match
		require.Equal(t, string(ssh.MarshalAuthorizedKey(derivedPubKey)), string(ssh.MarshalAuthorizedKey(parsedPubKey)))
	})

	t.Run("create SSH key pair succeeds", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		privateKeyPath := "/tmp/test-id"

		err := generateAndWriteSSHKeys(fs, privateKeyPath)
		require.NoError(t, err)

		privateBytes, err := afero.ReadFile(fs, privateKeyPath)
		require.NoError(t, err)
		block, _ := pem.Decode(privateBytes)
		require.NotNil(t, block)
		parsedPrivate, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		require.NoError(t, err)
		assert.Equal(t, 2048, parsedPrivate.N.BitLen(), "expected RSA 2048 private key")

		publicBytes, err := afero.ReadFile(fs, privateKeyPath+".pub")
		require.NoError(t, err)
		pubKey, _, _, _, err := ssh.ParseAuthorizedKey(publicBytes)
		require.NoError(t, err)

		expectedPub, err := ssh.NewPublicKey(&parsedPrivate.PublicKey)
		require.NoError(t, err)
		assert.Equal(t, string(ssh.MarshalAuthorizedKey(expectedPub)), string(ssh.MarshalAuthorizedKey(pubKey)))
	})

	t.Run("create SSH key pair succeeds", func(t *testing.T) {
		t.Parallel()

		mem := afero.NewMemMapFs()
		fs := &failWriteFs{
			Fs: mem,
			failures: map[string]error{
				"/tmp/fail-id": fmt.Errorf("write private boom"),
			},
		}

		err := generateAndWriteSSHKeys(fs, "/tmp/fail-id")
		var msgErr message.Message
		require.True(t, errors.As(err, &msgErr))
		assert.Equal(t, message.CliServiceSshKeysWriteKeyFile, msgErr.Code())
		assert.Equal(t, "private", msgErr.Metadata()["keyType"])
		assert.Equal(t, "/tmp/fail-id", msgErr.Metadata()["path"])
		assert.ErrorContains(t, msgErr.Unwrap(), "write private boom")
	})

	t.Run("public key write failure", func(t *testing.T) {
		t.Parallel()

		mem := afero.NewMemMapFs()
		privatePath := "/tmp/id-public-fail"
		publicPath := privatePath + ".pub"
		fs := &failWriteFs{
			Fs: mem,
			failures: map[string]error{
				publicPath: fmt.Errorf("write public boom"),
			},
		}

		err := generateAndWriteSSHKeys(fs, privatePath)
		var msgErr message.Message
		require.True(t, errors.As(err, &msgErr))
		assert.Equal(t, message.CliServiceSshKeysWriteKeyFile, msgErr.Code())
		assert.Equal(t, "public", msgErr.Metadata()["keyType"])
		assert.Equal(t, publicPath, msgErr.Metadata()["path"])
		assert.ErrorContains(t, msgErr.Unwrap(), "write public boom")
	})

	t.Run("wrong key mismatch", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		provisioner := &DefaultSSHKeyProvisioner{FS: fs}
		dataDir := "/foo"

		// Generate two independent key pairs
		privateKeyPath1, err := provisioner.CreateSSHKeyPair(dataDir)
		require.NoError(t, err)
		privateKeyPath2, err := provisioner.CreateSSHKeyPair(dataDir)
		require.NoError(t, err)

		// Read private key 1
		privKeyBytes1, err := afero.ReadFile(fs, privateKeyPath1)
		require.NoError(t, err)
		block1, _ := pem.Decode(privKeyBytes1)
		require.NotNil(t, block1)
		privateKey1, err := x509.ParsePKCS1PrivateKey(block1.Bytes)
		require.NoError(t, err)
		derivedPubKey1, err := ssh.NewPublicKey(&privateKey1.PublicKey)
		require.NoError(t, err)

		// Read public key 2
		pubKeyPath2 := privateKeyPath2 + ".pub"
		pubKeyBytes2, err := afero.ReadFile(fs, pubKeyPath2)
		require.NoError(t, err)
		parsedPubKey2, _, _, _, err := ssh.ParseAuthorizedKey(pubKeyBytes2)
		require.NoError(t, err)

		// Check keys do NOT match
		require.NotEqual(t,
			string(ssh.MarshalAuthorizedKey(derivedPubKey1)),
			string(ssh.MarshalAuthorizedKey(parsedPubKey2)),
			"different keys incorrectly matched",
		)
	})
}

func TestEnsureKnownHostsFile(t *testing.T) {
	t.Run("creates known_hosts when missing", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		prov := &DefaultSSHKeyProvisioner{
			FS: fs,
			HostKeyCallbackFactoryFunc: func(fs afero.Fs, path string) (ssh.HostKeyCallback, error) {
				return ssh.InsecureIgnoreHostKey(), nil // #nosec G106 -- test-only override
			},
		}

		path := "/tmp/test-known_hosts"
		_, err := prov.createKnownHostsFile(path)
		require.NoError(t, err)

		exists, err := afero.Exists(fs, path)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("fails if MkdirAll errors", func(t *testing.T) {
		brokenFs := afero.NewReadOnlyFs(afero.NewMemMapFs())
		prov := &DefaultSSHKeyProvisioner{
			FS: brokenFs,
			HostKeyCallbackFactoryFunc: func(fs afero.Fs, path string) (ssh.HostKeyCallback, error) {
				return ssh.InsecureIgnoreHostKey(), nil // #nosec G106 -- test-only override
			},
		}
		path := "/readonly/ssh/known_hosts"
		_, err := prov.createKnownHostsFile(path)
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CliServiceSshKeysCreateKnownHostsFile, msgErr.Code())
		assert.Equal(t, path, msgErr.Metadata()["path"])
		assert.NotNil(t, msgErr.Unwrap())
	})

	t.Run("fails if Exists check errors", func(t *testing.T) {
		fs := &failingStatFs{afero.NewMemMapFs()}
		prov := &DefaultSSHKeyProvisioner{
			FS: fs,
			HostKeyCallbackFactoryFunc: func(fs afero.Fs, path string) (ssh.HostKeyCallback, error) {
				return ssh.InsecureIgnoreHostKey(), nil // #nosec G106 -- test-only override
			},
		}

		_, err := prov.createKnownHostsFile("/some/path/known_hosts")
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CommonUnknownError, msgErr.Code())
		assert.NotNil(t, msgErr.Unwrap())
	})

	t.Run("fails if host key callback returns error", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		prov := &DefaultSSHKeyProvisioner{
			FS: fs,
			HostKeyCallbackFactoryFunc: func(fs afero.Fs, path string) (ssh.HostKeyCallback, error) {
				return nil, fmt.Errorf("simulated hostkey callback error")
			},
		}

		_, err := prov.createKnownHostsFile("/tmp/some/known_hosts")
		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.CommonUnknownError, msgErr.Code())
		assert.NotNil(t, msgErr.Unwrap())
	})
}

func TestConnectWithPasswordSetsHostKeyAlgorithms(t *testing.T) {
	tmpDir := t.TempDir()
	knownHostsPath := filepath.Join(tmpDir, "known_hosts")

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)

	entry := knownhosts.Line([]string{"[example.com]:22"}, signer.PublicKey())
	err = os.WriteFile(knownHostsPath, []byte(entry+"\n"), 0o600)
	require.NoError(t, err)

	prov := &DefaultSSHKeyProvisioner{}

	var capturedConfig *ssh.ClientConfig
	oldDial := sshDial
	defer func() {
		sshDial = oldDial
	}()

	sshDial = func(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
		capturedConfig = config
		return nil, errors.New("stub dial error")
	}

	_, err = prov.connectWithPassword("example.com", 22, "testuser", "password", knownHostsPath, ssh.InsecureIgnoreHostKey()) // #nosec G106 -- test-only callback
	require.Error(t, err)
	require.NotNil(t, capturedConfig)
	require.NotEmpty(t, capturedConfig.HostKeyAlgorithms)
	assert.Equal(t, ssh.KeyAlgoED25519, capturedConfig.HostKeyAlgorithms[0])
	assert.Contains(t, capturedConfig.HostKeyAlgorithms, ssh.KeyAlgoECDSA256)
	assert.Contains(t, capturedConfig.HostKeyAlgorithms, ssh.KeyAlgoRSASHA256)
	assert.NotContains(t, capturedConfig.HostKeyAlgorithms[1:], ssh.KeyAlgoED25519)
}

// failingStatFs simulates failure on Stat calls (used by afero.Exists)
type failingStatFs struct {
	afero.Fs
}

func (f *failingStatFs) Stat(name string) (os.FileInfo, error) {
	return nil, fmt.Errorf("simulated Stat failure")
}

type failWriteFs struct {
	afero.Fs
	failures map[string]error
}

func (f *failWriteFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if err, ok := f.failures[name]; ok && flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, err
	}
	return f.Fs.OpenFile(name, flag, perm)
}
