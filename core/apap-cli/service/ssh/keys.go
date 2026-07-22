// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"github.com/spf13/afero"
	"golang.org/x/crypto/ssh"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

var sshDial = ssh.Dial

// SSHKeyProvisioner defines the contract for managing SSH keys and provisioning.
type SSHKeyProvisioner interface {
	// CreateSSHKeyPair generates and saves an SSH key pair.
	CreateSSHKeyPair(dataDir string) (privateKeyPath string, err error)

	// ReadPublicKey reads a public key from disk.
	ReadPublicKey(pubKeyPath string) ([]byte, error)

	// ProvisionPublicKeyWithPassword provisions the public key to the target over SSH using a password.
	ProvisionPublicKeyWithPassword(
		dataDir string,
		host string,
		port int,
		username string,
		password string,
		pubKey string,
	) error
}

// DefaultSSHKeyProvisioner is the production implementation using an Afero filesystem.
type DefaultSSHKeyProvisioner struct {
	FS                         afero.Fs
	HostKeyCallbackFactoryFunc func(fs afero.Fs, path string) (ssh.HostKeyCallback, error)
}

// NewDefaultSSHKeyProvisioner returns a DefaultSSHKeyProvisioner using the real OS filesystem.
func NewDefaultSSHKeyProvisioner() *DefaultSSHKeyProvisioner {
	return &DefaultSSHKeyProvisioner{
		FS: afero.NewOsFs(),
		HostKeyCallbackFactoryFunc: func(fs afero.Fs, path string) (ssh.HostKeyCallback, error) {
			return conductor.TrustNewOrFailMismatchKnownHostsWithFs(fs, path)
		},
	}
}

// CreateSSHKeyPair generates an RSA SSH key pair and stores the private key and
// public key in the given dataDir. It returns the full path to the private key file.
//
// The generated keys are stored as:
//
//	<dataDir>/id_rsa      - private key (perms.PrivateKeyPerm)
//	<dataDir>/id_rsa.pub  - public key (perms.PublicKeyPerm)
//
// The method ensures the target directory exists with proper permissions.
func (p *DefaultSSHKeyProvisioner) CreateSSHKeyPair(dataDir string) (string, error) {
	sshDir := filepath.Join(dataDir, "ssh")

	// Ensure sshDir exists
	if err := p.FS.MkdirAll(sshDir, perms.SshDirPerm); err != nil {
		return "", message.New(message.CliServiceSshKeysCreateSshDir).WithCause(err).WithMetadata(map[string]string{"path": sshDir})
	}

	// Generate a unique random filename
	keyBase := generateRandomSuffix()
	privateKeyPath := filepath.Join(sshDir, keyBase)
	publicKeyPath := privateKeyPath + ".pub"

	// Check if files already exist (highly unlikely but safe)
	exists, err := util.PathExists(privateKeyPath)
	if exists || err != nil {
		return "", message.New(message.CliServiceSshKeysKeyAlreadyExists).WithMetadata(map[string]string{"keyType": "private", "name": keyBase, "dir": sshDir}).WithCause(err)
	}
	exists, err = util.PathExists(publicKeyPath)
	if exists || err != nil {
		return "", message.New(message.CliServiceSshKeysKeyAlreadyExists).WithMetadata(map[string]string{"keyType": "public", "name": keyBase + ".pub", "dir": sshDir}).WithCause(err)
	}

	// Generate and write keys
	if err := generateAndWriteSSHKeys(p.FS, privateKeyPath); err != nil {
		return "", err
	}

	return privateKeyPath, nil
}

// generateAndWriteSSHKeys generates an RSA 2048 SSH keypair and writes them to disk.
func generateAndWriteSSHKeys(fs afero.Fs, privateKeyPath string) error {
	publicKeyPath := privateKeyPath + ".pub"

	// Generate RSA 2048-bit key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return message.New(message.CliServiceSshKeysGenerateRsaKeyPair).WithCause(err)
	}
	publicKey := &privateKey.PublicKey

	// Save private key
	if err := writePrivateKey(fs, privateKeyPath, privateKey); err != nil {
		return message.New(message.CliServiceSshKeysWriteKeyFile).WithCause(err).WithMetadata(map[string]string{"keyType": "private", "path": privateKeyPath})
	}

	// Save public key
	if err := writePublicKey(fs, publicKeyPath, publicKey); err != nil {
		return message.New(message.CliServiceSshKeysWriteKeyFile).WithCause(err).WithMetadata(map[string]string{"keyType": "public", "path": publicKeyPath})
	}

	return nil
}

// writePrivateKey saves the RSA private key in PEM format.
func writePrivateKey(fs afero.Fs, path string, privateKey *rsa.PrivateKey) error {
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)

	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	if err := afero.WriteFile(fs, path, pem.EncodeToMemory(block), perms.PrivateKeyPerm); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}
	return nil
}

// writePublicKey saves the SSH public key to disk in authorized_keys format.
func writePublicKey(fs afero.Fs, path string, publicKey *rsa.PublicKey) error {
	sshPubKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}

	authorizedKeyBytes := ssh.MarshalAuthorizedKey(sshPubKey)

	if err := afero.WriteFile(fs, path, authorizedKeyBytes, perms.PublicKeyPerm); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}
	return nil
}

func generateRandomSuffix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("failed to generate random suffix: %w", err))
	}
	return hex.EncodeToString(b)
}

// ProvisionPublicKeyWithPassword connects to a remote target using password-based SSH
// and appends the given public key to the user's ~/.ssh/authorized_keys file.
//
// It uses known_hosts for host verification, stored under:
//
//	<Config-Dir>/ssh/known_hosts
//
// The update is done atomically by writing to a temporary file and renaming it into place.
//
// This method requires:
//
//   - SSH server must be reachable at host:port
//   - user/password must authenticate successfully
//   - public key must be in OpenSSH format
//
// Returns an error on connection failure, SFTP error, or key write failure.
func (p *DefaultSSHKeyProvisioner) ProvisionPublicKeyWithPassword(configDir, host string, port int, username, password, pubKey string) error {
	knownHostsPath := filepath.Join(configDir, "ssh", "known_hosts")

	hostKeyCallback, err := p.createKnownHostsFile(knownHostsPath)
	if err != nil {
		return err
	}

	client, err := p.connectWithPassword(host, port, username, password, knownHostsPath, hostKeyCallback)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := p.appendAuthorizedKeyViaSFTP(client, pubKey); err != nil {
		return message.New(message.CliServiceSshKeysCannotAppendToAuthorizedKeys).WithCause(err)
	}

	return nil
}

// createKnownHostsFile makes sure the ssh/known_hosts file exists under the given path
// and returns a host key callback for SSH verification. Creates parent directories if needed.
//
// Returns an error if the file cannot be created or the callback cannot be initialized.
func (p *DefaultSSHKeyProvisioner) createKnownHostsFile(knownHostsPath string) (ssh.HostKeyCallback, error) {
	metadata := map[string]string{
		"path": knownHostsPath,
	}
	if err := p.FS.MkdirAll(filepath.Dir(knownHostsPath), perms.SshDirPerm); err != nil {
		return nil, message.New(message.CliServiceSshKeysCreateKnownHostsFile).WithCause(err).WithMetadata(metadata)
	}

	exists, err := afero.Exists(p.FS, knownHostsPath)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}
	if !exists {
		if err := afero.WriteFile(p.FS, knownHostsPath, []byte{}, perms.PrivateKeyPerm); err != nil {
			return nil, message.New(message.CliServiceSshKeysCreateKnownHostsFile).WithCause(err).WithMetadata(metadata)
		}
	}

	if p.HostKeyCallbackFactoryFunc == nil {
		return nil, message.New(message.CommonUnknownError).WithCause(errors.New("HostKeyCallbackFactoryFunc is not set"))
	}
	hostKeyCallback, err := p.HostKeyCallbackFactoryFunc(p.FS, knownHostsPath)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	return hostKeyCallback, nil
}

// connectWithPassword establishes an SSH connection to the given host:port using
// the provided username and password. It returns an *ssh.Client instance.
//
// The SSH client config includes both password and keyboard-interactive authentication
// to maximize compatibility with common SSH server configurations.
//
// Host key verification is enforced using the provided hostKeyCallback.
//
// Returns an error if the connection fails or times out.
func (p *DefaultSSHKeyProvisioner) connectWithPassword(host string, port int, username, password string, knownHostsPath string, hostKeyCallback ssh.HostKeyCallback) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: conductor.HostKeyAlgorithmsForKnownHost(knownHostsPath, host, port),
		Timeout:           5 * time.Second,
	}

	address := fmt.Sprintf("%s:%d", host, port)
	client, err := sshDial("tcp", address, config)
	if err != nil {
		return nil, message.New(message.CliServiceSshKeysConnectWithPassword).WithCause(err).WithMetadata(map[string]string{"address": host + ":" + strconv.Itoa(port)})
	}

	return client, nil
}

// appendAuthorizedKeyViaSFTP opens an SFTP session using the given ssh.Client,
// ensures the ~/.ssh directory exists, and appends the given public key
// to the user's authorized_keys file on the remote system.
//
// The update is performed atomically by writing to a temporary file and then
// renaming it over the existing authorized_keys file.
//
// If the authorized_keys file already exists, its current contents are preserved
// and the new key is appended.
//
// Returns an error if any SFTP operation fails (mkdir, open, write, rename).
func (p *DefaultSSHKeyProvisioner) appendAuthorizedKeyViaSFTP(client *ssh.Client, pubKey string) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("failed to open SFTP session: %w", err)
	}
	defer sftpClient.Close()

	sshDir := ".ssh"
	authKeysPath := path.Join(sshDir, "authorized_keys")

	if err := sftpClient.MkdirAll(sshDir); err != nil {
		return fmt.Errorf("failed to create ~/.ssh: %w", err)
	}

	// Read existing authorized_keys content
	existingContent := []byte{}
	remoteFile, err := sftpClient.OpenFile(authKeysPath, os.O_RDONLY)
	if err == nil {
		existingContent, _ = io.ReadAll(remoteFile)
		remoteFile.Close()
	}

	// Ensure a newline if needed before appending
	if len(existingContent) > 0 && existingContent[len(existingContent)-1] != '\n' {
		existingContent = append(existingContent, '\n')
	}

	// Strip newlines from pubKey before appending one explicitly
	pubKey = strings.TrimSpace(pubKey)
	updatedContent := append(existingContent, []byte(pubKey+"\n")...)

	// Write to a temporary file
	tmpPath := authKeysPath + ".tmp"
	tmpFile, err := sftpClient.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("failed to create temporary authorized_keys file: %w", err)
	}
	if _, err := tmpFile.Write(updatedContent); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write to temporary authorized_keys file: %w", err)
	}
	tmpFile.Close()

	// "Atomically" rename
	// A straightforward sftpClient.rename() fails with SSH_FX_FAILURE, to work around
	// try to remove the old file first (ignore error if it doesn't exist)
	_ = sftpClient.Remove(authKeysPath)

	// And then attempt to rename
	if err := sftpClient.Rename(tmpPath, authKeysPath); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", tmpPath, authKeysPath, err)
	}

	// Ensure correct permissions (0600) on authorized_keys
	if err := sftpClient.Chmod(authKeysPath, perms.AuthorizedKeysPerm); err != nil {
		return fmt.Errorf("failed to set permissions on authorized_keys: %w", err)
	}
	return nil
}

// ReadPublicKey reads and returns the contents of an SSH public key file from the given path.
func (p *DefaultSSHKeyProvisioner) ReadPublicKey(pubKeyPath string) ([]byte, error) {
	data, err := afero.ReadFile(p.FS, pubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key: %w", err)
	}
	return data, nil
}
