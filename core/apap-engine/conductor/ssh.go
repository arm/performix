// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	slices0 "slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gofrs/flock"
	"github.com/pkg/sftp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	engineSSH "github.com/Arm-Debug/apap-cli/apap-engine/ssh"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const (
	SSHDialMaxAttempts     = 3
	SSHPromptMaxAttempts   = 3
	sshInitialRetryDelay   = 1 * time.Second
	tcpConnectivityTimeout = 3 * time.Second
)

var tcpDialContext = func(ctx context.Context, address string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: tcpConnectivityTimeout,
	}
	return dialer.DialContext(ctx, "tcp", address)
}

var (
	// errPasswordProbeSuccess is returned by the password auth probe callback after password auth support is observed.
	errPasswordProbeSuccess = errors.New("ssh password auth supported")
	// errKeyboardInteractivePasswordProbeSuccess is returned by the keyboard-interactive probe callback after password auth support is observed.
	errKeyboardInteractivePasswordProbeSuccess = errors.New("ssh keyboard-interactive password auth supported")
	// errUnsupportedKeyboardInteractivePasswordPrompt is raised when keyboard-interactive asks for something other than a password.
	errUnsupportedKeyboardInteractivePasswordPrompt = errors.New("unsupported ssh keyboard-interactive password prompt")
	// errPasswordAuthNotOffered is the error raised when password auth is not offered by the remote host.
	errPasswordAuthNotOffered = errors.New("ssh password authentication not offered by remote host")
	// errPublicKeyProbeSuccess is returned by the public key auth probe callback after public key auth support is observed.
	errPublicKeyProbeSuccess = errors.New("ssh public key auth supported")
	// errPublicKeyAuthNotOffered is the error raised when key auth is not offered by the remote host.
	errPublicKeyAuthNotOffered = errors.New("ssh public key authentication not offered by remote host")
)

// SecureClient is the interface for managing SSH connections.
type SecureClient interface {
	CommandRunner() CommandRunner
	Close() error
	Dial(n, addr string) (net.Conn, error)
	SFTPClient() (*sftp.Client, error)
}

// SecretType differentiates between the kinds of secrets we may prompt for.
type SecretType int

const (
	SecretTypeKeyPassphrase SecretType = iota
	SecretTypePassword
)

// PromptConfig contains the details needed to prompt the user for a secret.
type PromptConfig struct {
	SecretType     SecretType
	Host           string
	Username       string
	JumpIndex      int
	TotalJumps     int
	KeyPath        string
	CurrentAttempt int
	MaxAttempts    int
}

// HostKeyPromptConfig contains details needed for host key acceptance prompts.
type HostKeyPromptConfig struct {
	Host               string
	HostKeyType        string
	HostKeyFingerprint string
	KnownAs            []string
}

// SecretPromptProvider securely prompts the client for SSH secrets.
type SecretPromptProvider func(promptConfig PromptConfig) ([]byte, error)

// FingerprintPromptProvider securely prompts the client for host-key acceptance.
type FingerprintPromptProvider func(promptConfig HostKeyPromptConfig) (bool, error)

// PromptProviders groups secret and fingerprint prompt providers.
type PromptProviders struct {
	SecretPromptProvider      SecretPromptProvider
	FingerprintPromptProvider FingerprintPromptProvider
}

// SSHDialInfo contains info and callbacks required for dialing SSH connections.
type SSHDialInfo struct {
	SecretPromptProvider      SecretPromptProvider
	FingerprintPromptProvider FingerprintPromptProvider
	CurrentHost               string
	HopIndex                  int
	TotalHops                 int
	SignerCache               *signerCache
}

func (d SSHDialInfo) IsTarget() bool {
	return d.HopIndex == d.TotalHops-1
}

// closeSSHClient closes an ssh.Client in a way that doesn't panic if it's nil (e.g. mock clients).
func closeSSHClient(client *ssh.Client) {
	if client != nil {
		_ = client.Close()
	}
}

// SSHClient implements SecureClient and holds a chain of SSH clients (one per jump host).
// An SFTP client is cached for reuse.
// SSHClient also implements CommandRunner for running commands on an SSH target.
type SSHClient struct {
	clients    []*ssh.Client
	sftpClient *sftp.Client
	mu         sync.Mutex
}

type Sleeper func(d time.Duration)

// SecureConnector holds dependencies for creating SSH connections.
type SecureConnector struct {
	// If nil, the OS fs is used
	FS afero.Fs
	// DialSSHDirect is the function used for a direct SSH connection.
	// If nil, a default implementation using ssh.Dial is used.
	DialSSHDirect func(address string, config *ssh.ClientConfig) (*ssh.Client, error)
	// DialSSHViaClient is the function used for dialing through an existing SSH client.
	// If nil, a default implementation is used.
	DialSSHViaClient func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error)
	// UserHomeDir is used to obtain the user's home directory.
	// If nil, os.UserHomeDir is used.
	UserHomeDir func() (string, error)
	// Sleep sleeps for the specified duration.
	// If nil, time.sleep is used.
	Sleep Sleeper
}

// signerCache is a cache for ssh.Signers keyed by the private key file path. This avoids re-prompting the user
// for the same key's passphrase multiple times during a connection attempt.
type signerCache struct {
	mu      sync.Mutex
	entries map[string]ssh.Signer
}

// createTempKnownHostsFile writes entries into a new temp file sitting next to
// knownHostsPath, and returns the file path.
// Caller is responsible for cleaning it up (e.g. defer fs.Remove(tempPath)).
func createTempKnownHostsFile(fs afero.Fs, knownHostsPath string, entries *bytes.Buffer) (string, error) {
	dir := filepath.Dir(knownHostsPath)
	base := filepath.Base(knownHostsPath)

	tempFile, err := afero.TempFile(fs, dir, base+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("could not create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Close it so that aero.WriteFile can work
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("could not close temp file: %w", err)
	}

	if err := afero.WriteFile(fs, tempPath, entries.Bytes(), perms.KnownHostsPerm); err != nil {
		return "", fmt.Errorf("could not write entries to temp file: %w", err)
	}

	return tempPath, nil
}

func knownHostsForKey(fs afero.Fs, knownHostsPath, hostname string, key ssh.PublicKey) ([]string, error) {
	file, err := fs.OpenFile(knownHostsPath, os.O_RDONLY, 0)
	if err != nil {
		return nil, message.New(message.EngineConductorSshOpenKnownHosts).
			WithCause(err).
			WithMetadata(map[string]string{"filePath": knownHostsPath})
	}
	defer file.Close()

	normalizedHost := knownhosts.Normalize(hostname)
	scanner := bufio.NewScanner(file)
	knownAs := []string{}

	for scanner.Scan() {
		entry := strings.TrimSpace(scanner.Text())
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}

		_, hosts, pubKey, _, _, err := ssh.ParseKnownHosts([]byte(entry))
		if err != nil {
			continue
		}
		if !bytes.Equal(pubKey.Marshal(), key.Marshal()) {
			continue
		}

		for _, host := range hosts {
			normalizedKnownHost := knownhosts.Normalize(host)
			if normalizedKnownHost == normalizedHost || slices0.Contains([]string(knownAs), string(normalizedKnownHost)) {
				continue
			}
			knownAs = append(knownAs, normalizedKnownHost)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, message.New(message.EngineConductorSshOpenKnownHosts).
			WithCause(err).
			WithMetadata(map[string]string{"filePath": knownHostsPath})
	}
	return knownAs, nil
}

// createNewKnownHosts acts as a wrapper around knownhosts.New() by
// creating a ssh.HostKeyCallback and ignoring any invalid known_hosts entries.
// If there is an invalid entry (e.g., SSH1) it is logged and skipped over.
// The original knownhosts.New() fails if any of the entries is invalid,
// thus making the whole file invalid. This wrapper is workaround on that behaviour.
func createNewKnownHosts(knownHostsPath string) (ssh.HostKeyCallback, error) {
	fs := afero.NewOsFs()
	metadata := map[string]string{"filePath": knownHostsPath}

	// Open the original file
	file, err := fs.OpenFile(knownHostsPath, os.O_RDONLY, 0)
	if err != nil {
		return nil, message.New(message.EngineConductorSshOpenKnownHosts).WithCause(err).WithMetadata(metadata)
	}
	defer file.Close()

	// Validate each entry/line -- ignore invalid ones
	var validEntries bytes.Buffer
	hasInvalidEntry := false
	lineNumber := 0
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lineNumber++
		entry := scanner.Text()

		if len(entry) == 0 || entry[0] == '#' {
			continue
		}

		// Validation
		_, _, _, _, _, err := ssh.ParseKnownHosts([]byte(entry))
		if err != nil {
			hasInvalidEntry = true
			log.Warnf("Invalid known_hosts entry line %d: %v", lineNumber, err)
			continue
		}

		_, err = validEntries.WriteString(entry + "\n")
		if err != nil {
			return nil, message.New(message.EngineConductorSshSanitizeKnownHosts).WithCause(err).WithMetadata(metadata)
		}
	}

	// Default to original file
	validKnownHostsPath := knownHostsPath

	if hasInvalidEntry {
		tempPath, err := createTempKnownHostsFile(fs, knownHostsPath, &validEntries)
		if err != nil {
			return nil, message.New(message.EngineConductorSshSanitizeKnownHosts).WithCause(err).WithMetadata(metadata)
		}
		defer func() {
			_ = fs.Remove(tempPath)
		}()
		validKnownHostsPath = tempPath
	}

	callback, err := knownhosts.New(validKnownHostsPath)
	if err != nil {
		return nil, message.New(message.CommonUnknownError).WithCause(err)
	}

	return callback, nil
}

// failNewandFailMismatchKnownHosts is used to determine whether the host key is in
// the specified known_hosts file. If it's missing or mismatched, it fails with a
// detailed error message
func failNewandFailMismatchKnownHosts(knownHostsPath string) (ssh.HostKeyCallback, error) {
	verifyCallback, err := createNewKnownHosts(knownHostsPath)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := verifyCallback(hostname, remote, key)
		if err == nil {
			return nil // key matches
		}

		metadata := map[string]string{
			"hostName":       knownhosts.Normalize(hostname),
			"knownHostsPath": knownHostsPath,
		}

		// Check if the key was revoked
		var revoked *knownhosts.RevokedError
		if errors.As(err, &revoked) {
			return message.New(message.EngineConductorSshKeyRevoked).WithMetadata(metadata).WithCause(err)
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			// No accepted host keys i.e. host is unknown
			if len(keyErr.Want) == 0 {
				return message.New(message.EngineConductorSshStrictKeyUnknown).WithMetadata(metadata).WithCause(err)
			}
			// Accepted host keys exist, but we didn't get one i.e. mismatch
			return message.New(message.EngineConductorSshStrictKeyMismatch).WithMetadata(metadata).WithCause(err)
		}

		// Unknown error
		return message.New(message.CommonUnknownError).WithCause(err)
	}, nil
}

// TrustNewOrFailMismatchKnownHosts is used to determine whether the host key is in
// the specified known_hosts file. If it's missing, it's added. If it exists but
// there's a mismatch, it fails with a detailed error message
func TrustNewOrFailMismatchKnownHostsWithFs(fs afero.Fs, knownHostsPath string) (ssh.HostKeyCallback, error) {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// Rebuild on each call so retries see newly appended keys
		verifyCallback, err := createNewKnownHosts(knownHostsPath)
		if err != nil {
			return err
		}

		err = verifyCallback(hostname, remote, key)
		if err == nil {
			return nil // key matches
		}

		metadata := map[string]string{
			"hostName":       knownhosts.Normalize(hostname),
			"knownHostsPath": knownHostsPath,
		}

		// Check if the key was revoked
		var revoked *knownhosts.RevokedError
		if errors.As(err, &revoked) {
			return message.New(message.EngineConductorSshKeyRevoked).WithMetadata(metadata).WithCause(err)
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			// No accepted host keys i.e. host is unknown
			if len(keyErr.Want) == 0 {
				return appendHostKeyToKnownHosts(fs, knownHostsPath, hostname, key)
			}
			// Accepted host keys exist, but we didn't get one i.e. mismatch
			return message.New(message.EngineConductorSshAcceptNewKeyMismatch).WithMetadata(metadata).WithCause(err)
		}

		// Unknown error
		return message.New(message.CommonUnknownError).WithCause(err)
	}, nil
}

// promptNewOrFailMismatchKnownHosts prompts the user to accept and store an unknown host key.
func promptNewOrFailMismatchKnownHosts(fs afero.Fs, knownHostsPath string, dialInfo SSHDialInfo, hostCfg target.SSHHostConfig) (ssh.HostKeyCallback, error) {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// Rebuild on each call so retries see newly appended keys
		verifyCallback, err := createNewKnownHosts(knownHostsPath)
		if err != nil {
			return err
		}

		err = verifyCallback(hostname, remote, key)
		if err == nil {
			return nil // key matches
		}

		metadata := map[string]string{
			"hostName":       knownhosts.Normalize(hostname),
			"knownHostsPath": knownHostsPath,
		}

		// Check if the key was revoked
		var revoked *knownhosts.RevokedError
		if errors.As(err, &revoked) {
			return message.New(message.EngineConductorSshKeyRevoked).WithMetadata(metadata).WithCause(err)
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			// No accepted host keys i.e. host is unknown
			if len(keyErr.Want) == 0 {
				if dialInfo.FingerprintPromptProvider == nil {
					return message.New(message.EngineConductorSshStrictKeyUnknown).WithMetadata(metadata).WithCause(err)
				}

				knownAs, knownAsErr := knownHostsForKey(fs, knownHostsPath, hostname, key)
				if knownAsErr != nil {
					return knownAsErr
				}

				accepted, perr := dialInfo.FingerprintPromptProvider(HostKeyPromptConfig{
					Host:               hostCfg.Host,
					HostKeyType:        key.Type(),
					HostKeyFingerprint: ssh.FingerprintSHA256(key),
					KnownAs:            knownAs,
				})
				if perr != nil {
					return message.New(message.CliServiceTargetloginPromptFailed).WithCause(perr).WithMetadata(metadata)
				}
				if !accepted {
					return message.New(message.EngineConductorSshPromptKeyRejected).WithMetadata(metadata)
				}
				return appendHostKeyToKnownHosts(fs, knownHostsPath, hostname, key)
			}
			// Accepted host keys exist, but we didn't get one i.e. mismatch
			return message.New(message.EngineConductorSshAcceptNewKeyMismatch).WithMetadata(metadata).WithCause(err)
		}

		// Unknown error
		return message.New(message.CommonUnknownError).WithCause(err)
	}, nil
}

// appendHostKeyToKnownHosts does the actual FS update and writes the key to the user's known_host file
func appendHostKeyToKnownHosts(fs afero.Fs, knownHostsPath, hostname string, key ssh.PublicKey) error {
	// Lock the known_hosts file to prevent concurrent updates
	lockPath := knownHostsPath + ".lock"
	lock := flock.New(lockPath)

	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("failed to acquire file lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("could not acquire lock on %s", knownHostsPath)
	}
	defer func() {
		if unlockErr := lock.Unlock(); unlockErr != nil {
			log.Warnf("failed to unlock known_hosts file: %v", unlockErr)
		}
		// Remove stale lock file if it exists
		_ = fs.Remove(lockPath)
	}()

	dir := filepath.Dir(knownHostsPath)
	base := filepath.Base(knownHostsPath)
	tempFile, err := afero.TempFile(fs, dir, base+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp known_hosts file: %w", err)
	}
	cleaner := util.ScopeCleaner{}
	defer cleaner.MaybeCleanup(func() {
		_ = tempFile.Close()
	})

	// Read existing content, if any
	var existing []byte
	if exists, _ := afero.Exists(fs, knownHostsPath); exists {
		existing, err = afero.ReadFile(fs, knownHostsPath)
		if err != nil {
			return fmt.Errorf("failed to read existing known_hosts: %w", err)
		}
	}

	// Generate new line to append
	newLine := knownhosts.Line([]string{hostname}, key)

	// Write everything to temp file
	if len(existing) > 0 {
		if _, err := tempFile.Write(existing); err != nil {
			return fmt.Errorf("failed to write existing data to temp file: %w", err)
		}
	}
	if _, err := tempFile.WriteString(newLine + "\n"); err != nil {
		return fmt.Errorf("failed to write new host key: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp known_hosts file: %w", err)
	}
	cleaner.CancelCleanup()

	// Rename temp file to known_hosts after closing the descriptor to avoid sharing violations.
	if err := fs.Rename(tempFile.Name(), knownHostsPath); err != nil {
		return fmt.Errorf("failed to replace known_hosts file: %w", err)
	}

	log.Infof("Added new host key for %s to %s", hostname, knownHostsPath)
	return nil
}

// NewDefaultSecureConnector creates a new SecureConnector with defaults for all fields.
func NewDefaultSecureConnector() *SecureConnector {
	return NewSecureConnector(nil, nil, nil, nil, nil)
}

// NewSecureConnector constructs an SecureConnector with the given afero.Fs and uses default functions if nil.
func NewSecureConnector(fs afero.Fs,
	dialDirect func(address string, config *ssh.ClientConfig) (*ssh.Client, error),
	dialViaClient func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error),
	userHomeDir func() (string, error),
	sleeper Sleeper,
) *SecureConnector {
	if fs == nil {
		fs = afero.NewOsFs()
	}

	if dialDirect == nil {
		dialDirect = func(address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			return ssh.Dial("tcp", address, config)
		}
	}

	if dialViaClient == nil {
		dialViaClient = func(prevClient *ssh.Client, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
			// Enforce a timeout with DialContext to avoid hanging connections.
			timeoutCtx, cancel := context.WithTimeout(context.Background(), config.Timeout)
			defer cancel()
			netConn, err := prevClient.DialContext(timeoutCtx, "tcp", address)
			if err != nil {
				return nil, err
			}
			conn, chans, reqs, err := ssh.NewClientConn(netConn, address, config)
			if err != nil {
				netConn.Close()
				return nil, err
			}
			return ssh.NewClient(conn, chans, reqs), nil
		}
	}

	if userHomeDir == nil {
		userHomeDir = os.UserHomeDir
	}

	if sleeper == nil {
		sleeper = time.Sleep
	}

	return &SecureConnector{
		FS:               fs,
		DialSSHDirect:    dialDirect,
		DialSSHViaClient: dialViaClient,
		UserHomeDir:      userHomeDir,
		Sleep:            sleeper,
	}
}

// SecureConnectNoRetry establishes a SecureClient connection to the target, with no retries if the connection fails
func (c *SecureConnector) SecureConnectNoRetry(ctx context.Context, tgt target.Target, prompts PromptProviders) (SecureClient, error) {
	return c.secureConnectWithRetry(ctx, tgt, prompts, 0)
}

// SecureConnect establishes a SecureClient connection to the target.
func (c *SecureConnector) SecureConnect(ctx context.Context, tgt target.Target, prompts PromptProviders) (SecureClient, error) {
	switch tgt := tgt.(type) {
	case *target.SSHTarget:
		return c.secureConnectWithRetry(ctx, tgt, prompts, SSHDialMaxAttempts)
	default:
		return nil, fmt.Errorf("unsupported target type: %T", tgt)
	}
}

// SecureConnect establishes a SecureClient connection to the target.
func (c *SecureConnector) secureConnectWithRetry(ctx context.Context, tgt target.Target, prompts PromptProviders, retryAttempts int) (SecureClient, error) {
	switch tgt := tgt.(type) {
	case *target.SSHTarget:
		return c.SSHConnect(ctx, tgt, prompts, retryAttempts)
	case *target.LocalTarget:
		return nil, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "local"})
	default:
		targetType := reflect.TypeOf(tgt).String()
		return nil, message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": targetType})
	}
}

// SSHConnect creates a chain of SSH connections through the jump hosts.
// The final element in the SSHTarget.Jumps is the actual target.
func (c *SecureConnector) SSHConnect(ctx context.Context, tgt *target.SSHTarget, prompts PromptProviders, retryAttempts int) (*SSHClient, error) {
	if len(tgt.Jumps) == 0 {
		return nil, message.New(message.CommonUnknownError).WithCause(errors.New("no SSH host configuration provided"))
	}

	var clients []*ssh.Client
	var signerCache signerCache
	totalHops := len(tgt.Jumps)
	for i, hostCfg := range tgt.Jumps {
		var currentClient *ssh.Client
		var currentHost string
		if i > 0 {
			currentClient = clients[i-1]
			currentHost = tgt.Jumps[i-1].Host
		}
		dialInfo := SSHDialInfo{
			SecretPromptProvider:      prompts.SecretPromptProvider,
			FingerprintPromptProvider: prompts.FingerprintPromptProvider,
			CurrentHost:               currentHost,
			HopIndex:                  i,
			TotalHops:                 totalHops,
			SignerCache:               &signerCache,
		}
		client, err := c.DialSSHWithRetry(ctx, hostCfg, currentClient, retryAttempts, dialInfo)
		if err != nil {
			_ = closeClients(clients)
			return nil, err
		}
		clients = append(clients, client)
	}

	return &SSHClient{clients: clients}, nil
}

// DialSSHWithRetry dials an SSH connection via currentClient (if currentClient is nil, this will be a direct dial).
func (c *SecureConnector) DialSSHWithRetry(ctx context.Context, nextClientTgtConfig target.SSHHostConfig, currentClient *ssh.Client, retryAttempts int, dialInfo SSHDialInfo) (*ssh.Client, error) {
	var prompter *passwordAuthPrompter
	if nextClientTgtConfig.AuthMethod == target.SSHAuthMethodPassword {
		jumpIndex := 0
		if dialInfo.TotalHops > 1 {
			jumpIndex = dialInfo.HopIndex + 1
		}
		var err error
		prompter, err = newPasswordAuthPrompter(
			nextClientTgtConfig.Host,
			nextClientTgtConfig.Username,
			jumpIndex,
			dialInfo,
			SSHPromptMaxAttempts,
		)
		if err != nil {
			return nil, err
		}
	}

	// The probe to determine auth methods is already covered for password auth on a different path.
	// So this probe is only needed for key auth.
	address := fmt.Sprintf("%s:%d", nextClientTgtConfig.Host, nextClientTgtConfig.Port)
	if nextClientTgtConfig.AuthMethod == target.SSHAuthMethodKey {
		if err := c.probePublicKeyAuthSupport(address, currentClient, dialInfo, nextClientTgtConfig); err != nil {
			return nil, err
		}
	}

	config, err := c.buildSSHClientConfig(nextClientTgtConfig, dialInfo, prompter, true)
	if err != nil {
		return nil, err
	}

	var nextClient *ssh.Client
	if currentClient == nil {
		// Connect directly from the local machine
		nextClient, err = c.dialSSHDirectWithRetry(ctx, nextClientTgtConfig.Host, nextClientTgtConfig.Port, config, retryAttempts, prompter)
		if err != nil {
			return nil, err
		}
	} else {
		// Connect via the previous jump host
		nextClient, err = c.dialSSHViaClientWithRetry(ctx, currentClient, dialInfo.CurrentHost,
			nextClientTgtConfig.Host, nextClientTgtConfig.Port, config, retryAttempts, prompter)
		if err != nil {
			return nil, err
		}
	}
	return nextClient, nil
}

// probePublicKeyAuthSupport checks whether the remote host advertises public key authentication before proceeding.
func (c *SecureConnector) probePublicKeyAuthSupport(address string, currentClient *ssh.Client, dialInfo SSHDialInfo, nextClientTgtConfig target.SSHHostConfig) error {
	probeConfig, err := c.buildSSHClientConfig(nextClientTgtConfig, dialInfo, nil, false)
	if err != nil {
		return err
	}
	var dialProbe func(*ssh.ClientConfig) (*ssh.Client, error)
	if currentClient == nil {
		dialProbe = func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
			return c.DialSSHDirect(address, cfg)
		}
	} else {
		dialProbe = func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
			return c.DialSSHViaClient(currentClient, address, cfg)
		}
	}

	supported, probeErr := c.publicKeyAuthSupported(probeConfig, dialProbe)
	metadata := map[string]string{
		"hostName": nextClientTgtConfig.Host,
	}
	if currentClient != nil {
		metadata = map[string]string{
			"nextHost": nextClientTgtConfig.Host,
			"prevHost": dialInfo.CurrentHost,
			"hostName": nextClientTgtConfig.Host,
		}
	}
	if probeErr != nil {
		if currentClient == nil {
			return message.New(message.EngineConductorSshDirectConnFailedKeyAuth).WithCause(probeErr).WithMetadata(metadata)
		}
		return message.New(message.EngineConductorSshJumpConnFailedKeyAuth).WithCause(probeErr).WithMetadata(metadata)
	}
	if !supported {
		if currentClient == nil {
			return message.New(message.EngineConductorSshDirectConnKeyAuthDisabled).WithCause(errPublicKeyAuthNotOffered).WithMetadata(metadata)
		}
		return message.New(message.EngineConductorSshJumpConnKeyAuthDisabled).WithCause(errPublicKeyAuthNotOffered).WithMetadata(metadata)
	}
	return nil
}

// probePasswordAuthSupport checks whether the remote host advertises password authentication before prompting.
func (c *SecureConnector) probePasswordAuthSupport(config *ssh.ClientConfig, dial func(*ssh.ClientConfig) (*ssh.Client, error), metadata map[string]string, failureCode message.MessageCode, disabledCode message.MessageCode) error {
	supported, err := c.passwordAuthSupported(config, dial)
	if err != nil {
		if message.IsMessage(err) != nil {
			return err
		}
		return message.New(failureCode).WithCause(err).WithMetadata(metadata)
	}
	if !supported {
		return message.New(disabledCode).WithCause(errPasswordAuthNotOffered).WithMetadata(metadata)
	}
	return nil
}

// publicKeyAuthFunc returns the ssh.AuthMethod for the key file of the provided host config. This is a callback
// function that gets invoked by the SSH library during authentication.
func (c *SecureConnector) publicKeyAuthFunc(hostCfg target.SSHHostConfig, dialInfo SSHDialInfo) (ssh.AuthMethod, error) {
	keyCallback, err := c.publicKeyAuthCallback(hostCfg, dialInfo)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeysCallback(keyCallback), nil
}

// publicKeyAuthCallback returns a callback that resolves ssh.Signers when public key auth is requested.
func (c *SecureConnector) publicKeyAuthCallback(hostCfg target.SSHHostConfig, dialInfo SSHDialInfo) (func() ([]ssh.Signer, error), error) {
	isTarget := dialInfo.IsTarget()
	metadata := map[string]string{}
	if isTarget {
		metadata["target"] = hostCfg.DisplayString()
	} else {
		metadata["jumpNode"] = hostCfg.DisplayString()
	}

	keyPaths := []string{}
	if hostCfg.PrivateKeyFilename != "" {
		keyPaths = append(keyPaths, hostCfg.PrivateKeyFilename)
	} else {
		// Only use keys without passphrases when auto-detecting
		keys := engineSSH.ListHostPrivateKeys(c.FS)
		keyPaths = append(keyPaths, engineSSH.GetPassphraselessKeyPaths(keys)...)
	}

	if len(keyPaths) == 0 {
		metadata["keyPath"] = util.DisplayErrorStringSlice(keyPaths)
		metadata["keyDirs"] = util.DisplayErrorStringSlice(engineSSH.GetPrivateKeySearchDirs())
		return nil, message.New(message.EngineConductorSshNoValidKeys).WithMetadata(metadata)
	}

	return func() ([]ssh.Signer, error) {
		var signers []ssh.Signer
		for _, path := range keyPaths {
			metadata["path"] = path
			if dialInfo.SignerCache != nil {
				if signer, ok := dialInfo.SignerCache.lookupSigner(path); ok {
					signers = append(signers, signer)
					continue
				}
			}

			key, err := afero.ReadFile(c.FS, path)
			if err != nil {
				if isTarget {
					return nil, message.New(message.EngineConductorSshKeyFileNotReadableForTarget).WithCause(err).WithMetadata(metadata)
				}
				return nil, message.New(message.EngineConductorSshKeyFileNotReadable).WithCause(err).WithMetadata(metadata)
			}

			signer, err := ssh.ParsePrivateKey(key)
			if err != nil {
				_, passphraseMissing := err.(*ssh.PassphraseMissingError)
				if !passphraseMissing || dialInfo.SecretPromptProvider == nil {
					if isTarget {
						return nil, message.New(message.EngineConductorSshKeyFileNotParsableForTarget).WithCause(err).WithMetadata(metadata)
					}
					return nil, message.New(message.EngineConductorSshKeyFileNotParsable).WithCause(err).WithMetadata(metadata)
				}
				// Set jumpIndex to 0 for connections with no jumps, otherwise, index from 1.
				jumpIndex := 0
				if dialInfo.TotalHops > 1 {
					jumpIndex = dialInfo.HopIndex + 1
				}
				var passphraseErr error
				delay := sshInitialRetryDelay
				for attempt := 1; attempt <= SSHPromptMaxAttempts; attempt++ {
					passphrase, perr := dialInfo.SecretPromptProvider(PromptConfig{
						SecretType:     SecretTypeKeyPassphrase,
						Host:           hostCfg.Host,
						Username:       hostCfg.Username,
						JumpIndex:      jumpIndex,
						TotalJumps:     dialInfo.TotalHops,
						KeyPath:        path,
						CurrentAttempt: attempt,
						MaxAttempts:    SSHPromptMaxAttempts,
					})
					if perr != nil {
						return nil, message.New(message.CliServiceTargetloginPromptFailed).WithCause(perr).WithMetadata(metadata)
					}
					signer, passphraseErr = ssh.ParsePrivateKeyWithPassphrase(key, passphrase)

					// Clear the secret from memory after use
					for i := range passphrase {
						passphrase[i] = 0
					}

					if passphraseErr == nil {
						err = nil
						break
					}
					if attempt < SSHPromptMaxAttempts {
						c.Sleep(delay)
						delay *= 2
					}
					if attempt == SSHPromptMaxAttempts {
						err = passphraseErr
					}
				}
				if err != nil {
					if isTarget {
						return nil, message.New(message.EngineConductorSshKeyFileIncorrectPassphraseForTarget).WithCause(err).WithMetadata(metadata)
					}
					return nil, message.New(message.EngineConductorSshKeyFileIncorrectPassphrase).WithCause(err).WithMetadata(metadata)
				}
			}
			if dialInfo.SignerCache != nil {
				dialInfo.SignerCache.storeSigner(path, signer)
			}
			signers = append(signers, signer)
		}

		return signers, nil
	}, nil
}

func (c *signerCache) lookupSigner(path string) (ssh.Signer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[path]
	return entry, ok
}

func (c *signerCache) storeSigner(path string, signer ssh.Signer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]ssh.Signer)
	}
	c.entries[path] = signer
}

type passwordAuthPrompter struct {
	secretPromptProvider SecretPromptProvider
	host                 string
	username             string
	jumpIndex            int
	totalJumps           int
	maxAttempts          int
	currentAttempt       int
	shouldPrompt         bool
	currentSecret        string
}

// newPasswordAuthPrompter configures a prompter that caches passwords between dial attempts.
func newPasswordAuthPrompter(host string, username string, jumpIndex int, dialInfo SSHDialInfo, maxAttempts int) (*passwordAuthPrompter, error) {
	if dialInfo.SecretPromptProvider == nil {
		return nil, message.New(message.CommonUnknownError).
			WithCause(errors.New("no prompt provider available for password authentication"))
	}

	return &passwordAuthPrompter{
		secretPromptProvider: dialInfo.SecretPromptProvider,
		host:                 host,
		username:             username,
		jumpIndex:            jumpIndex,
		totalJumps:           dialInfo.TotalHops,
		maxAttempts:          maxAttempts,
		shouldPrompt:         true,
	}, nil
}

// Password returns the cached password or prompts for a new one when necessary.
// This gets called in two places:
//   - Ahead of the first SSH dial attempt to get the initial password and cache it
//   - At SSH dial time when the callback is invoked to get the password (which should already be cached)
func (p *passwordAuthPrompter) Password() (string, error) {
	if p == nil {
		return "", nil
	}

	if !p.shouldPrompt {
		return p.currentSecret, nil
	}

	if p.currentAttempt >= p.maxAttempts {
		metadata := map[string]string{
			"host":        p.host,
			"jumpIndex":   strconv.Itoa(p.jumpIndex),
			"maxAttempts": strconv.Itoa(p.maxAttempts),
		}
		return "", message.New(message.EngineConductorSshPasswordIncorrect).WithMetadata(metadata)
	}

	nextAttempt := p.currentAttempt + 1
	secret, err := p.secretPromptProvider(PromptConfig{
		SecretType:     SecretTypePassword,
		Host:           p.host,
		Username:       p.username,
		JumpIndex:      p.jumpIndex,
		CurrentAttempt: nextAttempt,
		MaxAttempts:    p.maxAttempts,
	})
	if err != nil {
		return "", message.New(message.CliServiceTargetloginPromptFailed).WithCause(err)
	}

	password := string(secret)
	// Clear the secret from memory after use
	for i := range secret {
		secret[i] = 0
	}

	p.currentSecret = password
	p.currentAttempt = nextAttempt
	p.shouldPrompt = false

	return password, nil
}

// MarkAuthFailureAndCheckRetry clears the cached password and reports whether another attempt is allowed.
func (p *passwordAuthPrompter) MarkAuthFailureAndCheckRetry() bool {
	if p == nil {
		return false
	}
	p.currentSecret = ""
	p.shouldPrompt = true
	return p.currentAttempt < p.maxAttempts
}

// ResetOnSuccess clears stored state so subsequent connections start clean.
func (p *passwordAuthPrompter) ResetOnSuccess() {
	if p == nil {
		return
	}
	p.currentSecret = ""
	p.currentAttempt = 0
	p.shouldPrompt = true
}

// passwordAuthFunc builds the ssh.AuthMethod using the provided password prompter. This is a callback function that
// gets invoked by the SSH library during authentication.
func (c *SecureConnector) passwordAuthFunc(prompter *passwordAuthPrompter) (ssh.AuthMethod, error) {
	if prompter == nil {
		return nil, message.New(message.CommonUnknownError).
			WithCause(errors.New("password prompter not provided for password authentication"))
	}

	authMethod := ssh.PasswordCallback(func() (string, error) {
		return prompter.Password()
	})
	return authMethod, nil
}

// keyboardInteractivePasswordAuthFunc builds an SSH auth method for password over keyboard-interactive.
// ssh.KeyboardInteractive takes the client-side challenge callback directly and returns an ssh.AuthMethod.
func (c *SecureConnector) keyboardInteractivePasswordAuthFunc(prompter *passwordAuthPrompter) (ssh.AuthMethod, error) {
	challenge, err := keyboardInteractivePasswordChallenge(prompter)
	if err != nil {
		return nil, err
	}

	return ssh.KeyboardInteractive(challenge), nil
}

// keyboardInteractivePasswordChallenge returns the client-side callback passed to ssh.KeyboardInteractive.
// It supports simple password entry plus zero-question informational challenges, and rejects MFA, password
// expiry/change, or other PAM prompts.
func keyboardInteractivePasswordChallenge(prompter *passwordAuthPrompter) (ssh.KeyboardInteractiveChallenge, error) {
	if prompter == nil {
		return nil, message.New(message.CommonUnknownError).
			WithCause(errors.New("password prompter not provided for keyboard-interactive password authentication"))
	}

	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		if isKeyboardInteractiveInfoPrompt(questions, echos) {
			return []string{}, nil
		}
		if !isKeyboardInteractivePasswordPrompt(questions, echos) {
			logRejectedKeyboardInteractivePrompt(name, instruction, questions, echos)
			return nil, errUnsupportedKeyboardInteractivePasswordPrompt
		}
		password, err := prompter.Password()
		if err != nil {
			return nil, err
		}
		return []string{password}, nil
	}, nil
}

// isKeyboardInteractiveInfoPrompt checks whether the prompt looks like an info message that doesn't require an answer.
// We need to support these because real keyboard-interactive/PAM configs could send an informational challenge during
// the flow with no questions.
func isKeyboardInteractiveInfoPrompt(questions []string, echos []bool) bool {
	return len(questions) == 0 && len(echos) == 0
}

// isKeyboardInteractivePasswordPrompt checks whether the prompt looks like the simple login password prompt we support.
func isKeyboardInteractivePasswordPrompt(questions []string, echos []bool) bool {
	if len(questions) != 1 || len(echos) != 1 || echos[0] {
		return false
	}

	prompt := strings.ToLower(strings.TrimSpace(questions[0]))
	if !strings.Contains(prompt, "password") {
		return false
	}
	if strings.Contains(prompt, "one-time password") || strings.Contains(prompt, "one time password") {
		return false
	}

	words := strings.FieldsFunc(prompt, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, word := range words {
		if !isUnsupportedKeyboardInteractivePasswordPromptWord(word) {
			continue
		}
		if i > 0 && words[i-1] == "password" {
			return false
		}
		if i < len(words)-1 && words[i+1] == "password" {
			return false
		}
	}

	return true
}

func isUnsupportedKeyboardInteractivePasswordPromptWord(word string) bool {
	switch word {
	case "again",
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
		"verification":
		return true
	default:
		return false
	}
}

func logRejectedKeyboardInteractivePrompt(name, instruction string, questions []string, echos []bool) {
	log.WithFields(log.Fields{
		"name":        name,
		"instruction": instruction,
		"questions":   questions,
		"echos":       echos,
	}).Info("Rejected unsupported SSH keyboard-interactive password prompt")
}

// cloneClientConfig returns a shallow copy so probe tweaks to Auth don't mutate the real dial config.
func cloneClientConfig(cfg *ssh.ClientConfig) *ssh.ClientConfig {
	if cfg == nil {
		return nil
	}
	cfgCopy := *cfg
	return &cfgCopy
}

// isUnsupportedAuthErr spots when the server rejects all auth methods in our list, so we know whether to continue prompting or fail fast.
func isUnsupportedAuthErr(err error) bool {
	if err == nil {
		return false
	}
	// The Go SSH client returns this string when an auth method isn't accepted by the server.
	return strings.Contains(err.Error(), "no supported methods remain")
}

// passwordAuthSupported checks whether the remote end offers password auth without triggering a user prompt.
func (c *SecureConnector) passwordAuthSupported(baseCfg *ssh.ClientConfig, dial func(*ssh.ClientConfig) (*ssh.Client, error)) (bool, error) {
	probeCfg := cloneClientConfig(baseCfg)
	if probeCfg == nil {
		return false, errors.New("missing SSH client config for password auth probe")
	}

	// Avoid host key fingerprint prompts during auth checks - the real dial enforces host key policy.
	probeCfg.HostKeyCallback = ssh.InsecureIgnoreHostKey() // #nosec G106 -- probe-only

	passwordAuthSupported := false
	keyboardInteractivePasswordAuthSupported := false
	probeCfg.Auth = []ssh.AuthMethod{
		ssh.PasswordCallback(func() (string, error) {
			passwordAuthSupported = true
			return "", errPasswordProbeSuccess
		}),
		ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			if isKeyboardInteractiveInfoPrompt(questions, echos) {
				return []string{}, nil
			}
			if !isKeyboardInteractivePasswordPrompt(questions, echos) {
				logRejectedKeyboardInteractivePrompt(name, instruction, questions, echos)
				return nil, errUnsupportedKeyboardInteractivePasswordPrompt
			}
			keyboardInteractivePasswordAuthSupported = true
			return nil, errKeyboardInteractivePasswordProbeSuccess
		}),
	}
	client, err := dial(probeCfg)
	closeSSHClient(client)

	switch {
	case err == nil:
		// This should not typically happen, but treat a successful probe as confirmation.
		return true, nil
	case passwordAuthSupported:
		// Probe callback fired, meaning the server offered password auth.
		return true, nil
	case keyboardInteractivePasswordAuthSupported:
		// Probe callback fired, meaning the server offered password auth via keyboard-interactive.
		return true, nil
	case message.IsMessage(err) != nil:
		// Preserve raised catalog messages (e.g host key or prompting issues)
		return false, err
	case errors.Is(err, errKeyboardInteractivePasswordProbeSuccess):
		// Probe callback fired, meaning the server offered a password via keyboard-interactive auth flow.
		return true, nil
	case errors.Is(err, errUnsupportedKeyboardInteractivePasswordPrompt):
		// The server offered keyboard-interactive, but not the password flow we support.
		return false, nil
	case isUnsupportedAuthErr(err):
		// Server explicitly said password auth is not supported.
		return false, nil
	case strings.Contains(err.Error(), "unable to authenticate"):
		// Server accepted the password method and then rejected the credentials.
		return true, nil
	default:
		// Any other failure (network, handshake, etc.) might still allow password auth; let the main dial handle it.
		return true, nil
	}
}

// publicKeyAuthSupported checks whether the server advertises publickey auth before we prompt for passphrases.
func (c *SecureConnector) publicKeyAuthSupported(baseCfg *ssh.ClientConfig, dial func(*ssh.ClientConfig) (*ssh.Client, error)) (bool, error) {
	probeCfg := cloneClientConfig(baseCfg)
	if probeCfg == nil {
		return false, errors.New("missing SSH client config for public key auth probe")
	}

	// Avoid host key fingerprint prompts during auth checks - the real dial enforces host key policy.
	probeCfg.HostKeyCallback = ssh.InsecureIgnoreHostKey() // #nosec G106 -- probe-only
	probeCfg.Auth = []ssh.AuthMethod{
		ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			return nil, errPublicKeyProbeSuccess
		}),
	}

	client, err := dial(probeCfg)
	closeSSHClient(client)

	switch {
	case err == nil:
		// This should not typically happen, but treat a successful probe as confirmation.
		return true, nil
	case message.IsMessage(err) != nil:
		// Preserve raised catalog messages (e.g host key or prompting issues)
		return false, err
	case errors.Is(err, errPublicKeyProbeSuccess):
		// Probe callback fired, meaning the server offered public key auth.
		return true, nil
	case isUnsupportedAuthErr(err):
		// Server explicitly said public key auth is not supported.
		return false, nil
	case err != nil && strings.Contains(err.Error(), "unable to authenticate"):
		// Server accepted the public key method and then rejected the credentials.
		return true, nil
	default:
		// Treat other failures as inconclusive and continue so the main dial path can surface the real error.
		return true, nil
	}
}

// buildAuthMethods builds a list of ssh.AuthMethods. These are callback functions that get invoked by the SSH library
// during authentication. For password authentication, the password prompter already contains the host and dial info.
// For key authentication, the hostCfg and dialInfo are required.
func (c *SecureConnector) buildAuthMethods(hostCfg target.SSHHostConfig, dialInfo SSHDialInfo, prompter *passwordAuthPrompter) ([]ssh.AuthMethod, error) {
	authMethods := []ssh.AuthMethod{}

	switch hostCfg.AuthMethod {
	case target.SSHAuthMethodPassword:
		passwordAuth, err := c.passwordAuthFunc(prompter)
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, passwordAuth)
		keyboardInteractiveAuth, err := c.keyboardInteractivePasswordAuthFunc(prompter)
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, keyboardInteractiveAuth)
	case target.SSHAuthMethodKey:
		fallthrough
	default:
		keyAuth, err := c.publicKeyAuthFunc(hostCfg, dialInfo)
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, keyAuth)
	}
	return authMethods, nil
}

// findKnownHosts looks for a known_hosts file near the private key or in the user's .ssh directory.
func (c *SecureConnector) findKnownHosts(privateKeyFilename string) (string, error) {
	locationSuppliers := []func() *string{
		func() *string {
			joined := filepath.Join(filepath.Dir(privateKeyFilename), "known_hosts")
			return &joined
		},
		func() *string {
			homeDir, err := c.UserHomeDir()
			if err != nil {
				return nil
			}
			joined := filepath.Join(homeDir, ".ssh", "known_hosts")
			return &joined
		},
	}

	var triedPaths []string
	for _, supplier := range locationSuppliers {
		candidatePtr := supplier()
		if candidatePtr == nil {
			continue
		}
		candidate := *candidatePtr
		// Only add attempted location if it's not already present
		if !slices0.Contains([]string(triedPaths), string(candidate)) {
			triedPaths = append(triedPaths, candidate)
		}
		if info, err := c.FS.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	locations := util.DisplayErrorStringSlice(triedPaths)
	return "", message.New(message.EngineConductorSshLocateKnownHostsFile).WithMetadata(map[string]string{"locations": locations})
}

// defaultHostKeyAlgorithms returns the list of host-key algorithms in default order of preference.
// The preference order can be altered for known host-keys.
func defaultHostKeyAlgorithms() []string {
	// This list was copied from ssh.defaultHostKeyAlgos because that symbol is not exported.
	// Go doesn't provide an API for querying the list, so we are forced to be explicit about
	// what algorithms we accept.
	// Go does have ssh.SupportedAlgorithms().HostKeys and ssh.InsecureAlgorithms().HostKeys
	// but the ordering is different.
	return []string{
		ssh.CertAlgoRSASHA256v01,
		ssh.CertAlgoRSASHA512v01,
		ssh.CertAlgoRSAv01,
		// deprecated: ssh.InsecureCertAlgoDSAv01,
		ssh.CertAlgoECDSA256v01,
		ssh.CertAlgoECDSA384v01,
		ssh.CertAlgoECDSA521v01,
		ssh.CertAlgoED25519v01,
		ssh.KeyAlgoECDSA256,
		ssh.KeyAlgoECDSA384,
		ssh.KeyAlgoECDSA521,
		ssh.KeyAlgoRSASHA256,
		ssh.KeyAlgoRSASHA512,
		ssh.KeyAlgoRSA,
		// deprecated: ssh.InsecureKeyAlgoDSA,
		ssh.KeyAlgoED25519,
	}
}

type knownHostAddress string

func (a knownHostAddress) Network() string { return "tcp" }
func (a knownHostAddress) String() string  { return string(a) }

// HostKeyAlgorithmsForKnownHost returns a list of host key algorithms that are acceptable by the client.
// If the given host is present in the known_hosts file it prefers the known algorithms for that host.
// Otherwise, it returns a list in default order of preference.
func HostKeyAlgorithmsForKnownHost(knownHostsPath string, host string, port int) []string {
	defaultAlgorithms := defaultHostKeyAlgorithms()

	callback, err := createNewKnownHosts(knownHostsPath)
	if err != nil {
		return defaultAlgorithms
	}

	// Call HostKeyCallback with the host address and a dummy key to get a
	// list of matching host entries in knownhosts.KeyError.Want.
	hostAddress := net.JoinHostPort(host, strconv.Itoa(port))
	dummyKey, err := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
	if err != nil {
		return defaultAlgorithms
	}
	callbackErr := callback(hostAddress, knownHostAddress(hostAddress), dummyKey)
	if callbackErr == nil {
		return defaultAlgorithms
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(callbackErr, &keyErr) {
		return defaultAlgorithms
	}
	if len(keyErr.Want) == 0 {
		return defaultAlgorithms
	}

	algorithms := []string{}
	added := map[string]bool{}
	appendAlgorithm := func(keyType string) {
		add := func(algorithm string) {
			if !added[algorithm] {
				added[algorithm] = true
				algorithms = append(algorithms, algorithm)
			}
		}
		switch keyType {
		case ssh.KeyAlgoRSA:
			// Special case for RSA. See documentation of the constants for an explanation.
			add(ssh.KeyAlgoRSASHA256)
			add(ssh.KeyAlgoRSASHA512)
			add(ssh.KeyAlgoRSA)
		default:
			add(keyType)
		}
	}

	for _, wanted := range keyErr.Want {
		appendAlgorithm(wanted.Key.Type())
	}

	for _, algorithm := range defaultAlgorithms {
		appendAlgorithm(algorithm)
	}

	return algorithms
}

func (c *SecureConnector) buildSSHClientConfig(hostCfg target.SSHHostConfig, dialInfo SSHDialInfo, prompter *passwordAuthPrompter, includeAuth bool) (*ssh.ClientConfig, error) {
	var hostKeyCallback ssh.HostKeyCallback
	var knownHostsPath string
	var err error

	if hostCfg.HostKeyPolicy != target.IgnoreHostKey {
		knownHostsPath, err = c.findKnownHosts(hostCfg.PrivateKeyFilename)
		if err != nil {
			return nil, err
		}
	}

	switch hostCfg.HostKeyPolicy {
	case target.AcceptNewHost:
		hostKeyCallback, err = TrustNewOrFailMismatchKnownHostsWithFs(c.FS, knownHostsPath)
		if err != nil {
			return nil, err
		}
	case target.AskNewHost:
		hostKeyCallback, err = promptNewOrFailMismatchKnownHosts(c.FS, knownHostsPath, dialInfo, hostCfg)
		if err != nil {
			return nil, err
		}
	case target.IgnoreHostKey:
		hostKeyCallback = ssh.InsecureIgnoreHostKey() // #nosec G106
	case target.RejectHostKeyIfMissing:
		fallthrough
	default:
		hostKeyCallback, err = failNewandFailMismatchKnownHosts(knownHostsPath)
		if err != nil {
			return nil, err
		}
	}

	config := &ssh.ClientConfig{
		User:              hostCfg.Username,
		Timeout:           5 * time.Second,
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: HostKeyAlgorithmsForKnownHost(knownHostsPath, hostCfg.Host, int(hostCfg.Port)),
	}

	if includeAuth {
		authMethods, err := c.buildAuthMethods(hostCfg, dialInfo, prompter)
		if err != nil {
			return nil, err
		}
		config.Auth = authMethods
	}

	return config, nil
}

// dialSSHDirectWithRetry dials an SSH connection directly from the local machine.
func (c *SecureConnector) dialSSHDirectWithRetry(ctx context.Context, host string, port int32, config *ssh.ClientConfig, retryAttempts int, prompter *passwordAuthPrompter) (*ssh.Client, error) {
	address := fmt.Sprintf("%s:%d", host, port)
	delay := sshInitialRetryDelay
	errs := []error{}

	if prompter != nil && prompter.shouldPrompt {
		metadata := map[string]string{"hostName": host}
		if err := c.probePasswordAuthSupport(
			config,
			func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
				return c.DialSSHDirect(address, cfg)
			},
			metadata,
			message.EngineConductorSshDirectConnFailedPasswordAuth,
			message.EngineConductorSshDirectConnPasswordAuthDisabled,
		); err != nil {
			return nil, err
		}
	}

	for attempt := 0; attempt <= retryAttempts; attempt++ {
		logx.FromContext(ctx).WithFields(log.Fields{
			"address": address,
			"attempt": attempt,
		}).Info("Attempting direct SSH connection")

		client, err := c.DialSSHDirect(address, config)
		if err == nil {
			if prompter != nil {
				prompter.ResetOnSuccess()
			}
			return client, nil
		}
		errs = append(errs, err)

		logx.FromContext(ctx).WithFields(log.Fields{
			"address": address,
			"attempt": attempt,
			"error":   err,
		}).Info("Direct SSH connection - dial failed")

		if prompter != nil && getErrType(err) == AuthErr {
			if prompter.MarkAuthFailureAndCheckRetry() {
				attempt--
				continue
			}
		}

		if attempt < retryAttempts {
			c.Sleep(delay)
			delay *= 2
		}
	}
	return nil, consolidateDirectErrs(errs, host, port)
}

// dialSSHViaClientWithRetry dials an SSH connection via an existing SSH client (jump host).
func (c *SecureConnector) dialSSHViaClientWithRetry(ctx context.Context, prevClient *ssh.Client, prevHost string, host string, port int32, config *ssh.ClientConfig, retryAttempts int, prompter *passwordAuthPrompter) (*ssh.Client, error) {
	address := fmt.Sprintf("%s:%d", host, port)
	delay := sshInitialRetryDelay
	errs := []error{}

	if prompter != nil && prompter.shouldPrompt {
		metadata := map[string]string{
			"nextHost": host,
			"prevHost": prevHost,
			"hostName": host,
		}
		if err := c.probePasswordAuthSupport(
			config,
			func(cfg *ssh.ClientConfig) (*ssh.Client, error) {
				return c.DialSSHViaClient(prevClient, address, cfg)
			},
			metadata,
			message.EngineConductorSshJumpConnFailedPasswordAuth,
			message.EngineConductorSshJumpConnPasswordAuthDisabled,
		); err != nil {
			return nil, err
		}
	}

	for attempt := 0; attempt <= retryAttempts; attempt++ {
		logx.FromContext(ctx).WithFields(log.Fields{
			"address": address,
			"attempt": attempt,
		}).Info("Attempting SSH connection via jump host")

		client, err := c.DialSSHViaClient(prevClient, address, config)
		if err == nil {
			if prompter != nil {
				prompter.ResetOnSuccess()
			}
			return client, nil
		}
		errs = append(errs, err)

		logx.FromContext(ctx).WithFields(log.Fields{
			"nextAddress": address,
			"prevHost":    prevHost,
			"attempt":     attempt,
			"error":       err,
		}).Info("SSH connection via jump host - dial failed")

		if prompter != nil && getErrType(err) == AuthErr {
			if prompter.MarkAuthFailureAndCheckRetry() {
				attempt--
				continue
			}
		}

		if attempt < retryAttempts {
			c.Sleep(delay)
			delay *= 2
		}
	}
	return nil, consolidateJumpErrs(errs, prevHost, host, port)
}

func (c *SSHClient) CommandRunner() CommandRunner {
	return c
}

func (c *SSHClient) RunCommand(cmd string) (string, string, error) {
	if len(c.clients) == 0 {
		return "", "", fmt.Errorf("no SSH connection available")
	}
	targetClient := c.clients[len(c.clients)-1]

	session, err := targetClient.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if err := session.Run(cmd); err != nil {
		err := fmt.Errorf("failed to run command (%v): %w", cmd, err)
		return stdoutBuf.String(), stderrBuf.String(), err
	}

	return stdoutBuf.String(), stderrBuf.String(), nil
}

// Dial establishes a connection from the final target host.
func (c *SSHClient) Dial(n, addr string) (net.Conn, error) {
	if len(c.clients) == 0 {
		return nil, fmt.Errorf("no SSH connection available")
	}
	return c.clients[len(c.clients)-1].Dial(n, addr)
}

func (c *SSHClient) Close() error {
	return closeClients(c.clients)
}

// SFTPClient creates a new SFTP client, or returns an existing one if already created.
func (c *SSHClient) SFTPClient() (*sftp.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sftpClient != nil {
		return c.sftpClient, nil
	}
	if len(c.clients) == 0 {
		return nil, fmt.Errorf("no SSH connection available")
	}
	sftpClient, err := sftp.NewClient(c.clients[len(c.clients)-1],
		sftp.UseConcurrentWrites(true),
	)
	if err != nil {
		return nil, err
	}
	c.sftpClient = sftpClient
	return sftpClient, nil
}

// closeClients closes a slice of SSH clients.
func closeClients(clients []*ssh.Client) error {
	var firstErr error
	for i := len(clients) - 1; i >= 0; i-- {
		if clients[i].Conn != nil {
			if err := clients[i].Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// FindSSHKeysForTarget finds a slice of private-key file paths that produce a successful SSH
// connection for the given target (one key for each jump).
func FindSSHKeysForTarget(ctx context.Context, tgt *target.SSHTarget) ([]string, error) {
	kf := NewDefaultSSHKeyFinder()
	return kf.FindSSHKeysForTarget(ctx, tgt)
}

func NewDefaultSSHKeyFinder() *SSHKeyFinder {
	dial := NewDefaultSecureConnector().DialSSHWithRetry
	dirs := engineSSH.GetPrivateKeySearchDirs()
	// Only use keys without passphrases when auto-detecting
	keys := engineSSH.ListHostPrivateKeys(afero.NewOsFs())
	keyPaths := engineSSH.GetPassphraselessKeyPaths(keys)
	return &SSHKeyFinder{
		Dial:                  dial,
		HostPrivateKeyPaths:   keyPaths,
		HostPrivateKeyDirs:    dirs,
		CheckPortConnectivity: checkTCPConnectivity,
	}
}

type SSHKeyFinder struct {
	Dial                  func(ctx context.Context, nextClientTgtConfig target.SSHHostConfig, currentClient *ssh.Client, retryAttempts int, dialInfo SSHDialInfo) (*ssh.Client, error)
	HostPrivateKeyPaths   []string
	HostPrivateKeyDirs    []string
	CheckPortConnectivity func(ctx context.Context, host string, port int32) error
}

func checkTCPConnectivity(ctx context.Context, host string, port int32) error {
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	conn, err := tcpDialContext(ctx, address)
	if err != nil {
		return err
	}

	if conn != nil {
		_ = conn.Close()
	}

	return nil
}

func (kf *SSHKeyFinder) FindSSHKeysForTarget(ctx context.Context, tgt *target.SSHTarget) ([]string, error) {
	keys := make([]string, 0, len(tgt.Jumps))
	clients := make([]*ssh.Client, 0, len(tgt.Jumps))
	defer func() { _ = closeClients(clients) }()

	totalHops := len(tgt.Jumps)
	for i, hostCfg := range tgt.Jumps {
		isTarget := i == totalHops-1

		// Only probe the first hop directly; deeper hops require SSH tunnelling and their failures surface through Dial.
		if i == 0 && kf.CheckPortConnectivity != nil {
			if err := kf.CheckPortConnectivity(ctx, hostCfg.Host, hostCfg.Port); err != nil {
				logx.FromContext(ctx).WithField("target", tgt.String()).
					WithError(err).Infof("tcp connectivity check failed for %s", hostCfg.DisplayString())

				if isTarget {
					return nil, message.New(message.EngineConductorSshTcpConnectFailedForTarget).WithMetadata(
						map[string]string{
							"target": hostCfg.DisplayString(),
							"port":   fmt.Sprintf("%d", hostCfg.Port),
						},
					).WithCause(err)
				}
				return nil, message.New(message.EngineConductorSshTcpConnectFailed).WithMetadata(
					map[string]string{
						"jumpNode": hostCfg.DisplayString(),
						"port":     fmt.Sprintf("%d", hostCfg.Port),
					},
				).WithCause(err)
			}
		}

		var keyPathsForJump = kf.HostPrivateKeyPaths
		if len(hostCfg.PrivateKeyFilename) > 0 && !slices0.Contains([]string(keyPathsForJump), string(hostCfg.PrivateKeyFilename)) {
			keyPathsForJump = append([]string{hostCfg.PrivateKeyFilename}, kf.HostPrivateKeyPaths...)
		}

		var currentClient *ssh.Client
		var currentHost string
		if i > 0 {
			currentClient = clients[len(clients)-1]
			currentHost = tgt.Jumps[i-1].Host
		}

		for _, key := range keyPathsForJump {
			hostCfg.PrivateKeyFilename = key
			dialInfo := SSHDialInfo{
				CurrentHost: currentHost,
				HopIndex:    i,
				TotalHops:   totalHops,
			}
			nextClient, err := kf.Dial(ctx, hostCfg, currentClient, 0, dialInfo)
			if err == nil {
				clients = append(clients, nextClient)
				keys = append(keys, key)
				break
			} else {
				var handledErr message.Message
				if i == 0 {
					handledErr = handleFindKeysDirectErr(err, hostCfg.Host, hostCfg.Port)
				} else {
					handledErr = handleFindKeysJumpErr(err, currentHost, hostCfg.Host, hostCfg.Port)
				}
				if handledErr != nil {
					return nil, handledErr
				}
			}
		}

		if len(keys) != i+1 {
			logx.FromContext(ctx).WithField("target", tgt.String()).Infof("searching for compatible ssh keys: failed")
			metadata := map[string]string{
				"keyPaths": util.DisplayErrorStringSlice(keyPathsForJump),
				"keyDirs":  util.DisplayErrorStringSlice(kf.HostPrivateKeyDirs),
			}
			if isTarget {
				metadata["target"] = hostCfg.DisplayString()
				return nil, message.New(message.EngineConductorSshMissingKeyForTarget).WithMetadata(metadata)
			}
			metadata["jumpNode"] = hostCfg.DisplayString()
			return nil, message.New(message.EngineConductorSshMissingKeyForJump).WithMetadata(metadata)
		}
	}

	logx.FromContext(ctx).WithField("target", tgt.String()).Infof("searching for compatible ssh keys: success")
	return keys, nil
}
