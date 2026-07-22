// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetlogin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	promptsvc "github.com/Arm-Debug/apap-cli/apap-cli/service/prompt"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/authproto"
)

type SecretPrompter func(prompt string) ([]byte, error)
type FingerprintPrompter func(prompt string, fingerprint string) (bool, error)
type AuthConnector func(host string, port int) (authproto.AuthClient, io.Closer, error)

// TargetLoginService handles logging in to a target over the Auth service, securely prompting the user for credentials as needed.
type TargetLoginService interface {
	LoginToTarget(ctx context.Context, target engine_target.Target, config grpcserver.GrpcServerConfig) error
}

// targetLoginService implements TargetLoginService.
type targetLoginService struct {
	authConnector     AuthConnector
	promptSecret      SecretPrompter
	promptFingerprint FingerprintPrompter
}

// NewTargetLoginService creates a targetLoginService with the provided dependencies.
func NewTargetLoginService(authConnector AuthConnector, secret SecretPrompter, fingerprint FingerprintPrompter) TargetLoginService {
	return &targetLoginService{
		authConnector:     authConnector,
		promptSecret:      secret,
		promptFingerprint: fingerprint,
	}
}

// NewDefaultTargetLoginService creates a targetLoginService with default dependencies.
func NewDefaultTargetLoginService() TargetLoginService {
	return NewTargetLoginService(defaultAuthConnector, PromptSecret, PromptFingerprint)
}

// LoginToTarget establishes a connection to the target over the Auth service.
// Passwords and passphrases are securely prompted from the user over the Auth service as needed.
func (c *targetLoginService) LoginToTarget(ctx context.Context, target engine_target.Target, config grpcserver.GrpcServerConfig) error {
	if c.promptSecret == nil || c.promptFingerprint == nil || c.authConnector == nil {
		return message.New(message.CommonUnknownError).
			WithCause(errors.New("TargetLoginService is missing required dependencies"))
	}

	// Dial the Auth service
	authClient, authCloser, err := c.authConnector(config.Host, config.AuthPort)
	if err != nil {
		return err
	}
	if authCloser != nil {
		defer authCloser.Close()
	}

	// Establish the TargetLogin stream
	stream, err := authClient.TargetLogin(ctx)
	if err != nil {
		return message.New(message.CliServiceTargetloginStreamFailure).WithCause(err)
	}
	defer func() { _ = stream.CloseSend() }()

	// Send over taget details as the first message
	targetProto := grpcserver.TargetToProto(target)
	err = stream.Send(&authproto.TargetLoginClientMessage{
		Message: &authproto.TargetLoginClientMessage_Request{
			Request: &authproto.TargetLoginRequest{Target: targetProto},
		},
	})
	if err != nil {
		return message.New(message.CliServiceTargetloginStreamFailure).WithCause(err)
	}

	for {
		serverMsg, err := stream.Recv()
		if err != nil {
			if message.IsMessage(err) != nil {
				return err
			}
			return message.New(message.CliServiceTargetloginStreamFailure).WithCause(err)
		}
		switch {
		case serverMsg.GetPrompt() != nil:
			if err := c.handleSecretPrompt(stream, serverMsg.GetPrompt()); err != nil {
				return err
			}
		case serverMsg.GetFingerprintPrompt() != nil:
			if err := c.handleFingerprintPrompt(stream, serverMsg.GetFingerprintPrompt()); err != nil {
				return err
			}
		case serverMsg.GetResponse() != nil:
			return c.handleTargetLoginResponse(serverMsg.GetResponse())
		default:
			return message.New(message.CommonUnknownError).
				WithCause(errors.New("unexpected message type received during target login"))
		}
	}
}

// handleSecretPrompt prompts the user for a secret (password or passphrase) and sends it back over the stream.
func (c *targetLoginService) handleSecretPrompt(stream authproto.Auth_TargetLoginClient, prompt *authproto.TargetLoginPrompt) error {
	promptText, err := formatTargetLoginPrompt(prompt)
	if err != nil {
		return message.New(message.CommonUnknownError).WithCause(err)
	}

	secret, err := c.promptSecret(promptText)
	if err != nil {
		return message.New(message.CliServiceTargetloginPromptFailed).WithCause(err)
	}

	err = stream.Send(&authproto.TargetLoginClientMessage{
		Message: &authproto.TargetLoginClientMessage_Credentials{
			Credentials: &authproto.TargetLoginCredentials{
				Secret: secret,
			},
		},
	})

	// Clear the secret from memory after sending
	for i := range secret {
		secret[i] = 0
	}

	if err != nil {
		return message.New(message.CliServiceTargetloginStreamFailure).WithCause(err)
	}
	return nil
}

// handleFingerprintPrompt prompts the user to confirm the host key fingerprint and sends their response back over the stream.
func (c *targetLoginService) handleFingerprintPrompt(stream authproto.Auth_TargetLoginClient, prompt *authproto.TargetLoginFingerprintPrompt) error {
	promptText, err := formatFingerprintPrompt(prompt)
	if err != nil {
		return message.New(message.CommonUnknownError).WithCause(err)
	}

	accepted, err := c.promptFingerprint(promptText, prompt.GetHostKeyFingerprint())
	if err != nil {
		return err
	}

	err = stream.Send(&authproto.TargetLoginClientMessage{
		Message: &authproto.TargetLoginClientMessage_FingerprintAcceptance{
			FingerprintAcceptance: &authproto.TargetLoginFingerprintAcceptance{Accepted: accepted},
		},
	})

	if err != nil {
		return message.New(message.CliServiceTargetloginStreamFailure).WithCause(err)
	}
	return nil
}

// handleTargetLoginResponse handles the final target login status response.
func (c *targetLoginService) handleTargetLoginResponse(response *authproto.TargetLoginResponse) error {
	switch response.GetReturnCode() {
	case apapproto.StatusCode_SUCCESS:
		return nil
	case apapproto.StatusCode_ERROR:
		if err := message.ReconstructFromChain(response.GetError()); err != nil {
			return err
		}
		return message.New(message.CommonUnknownError).
			WithCause(fmt.Errorf("target login failed without error details"))
	default:
		return message.New(message.CommonUnknownError).
			WithCause(fmt.Errorf("unexpected status when logging in to target: %s", response.GetReturnCode().String()))
	}
}

// formatTargetLoginPrompt creates a user-friendly prompt string to display to the user for secret prompting.
func formatTargetLoginPrompt(prompt *authproto.TargetLoginPrompt) (string, error) {
	promptConfig, err := grpcserver.ProtoToPromptConfig(prompt)
	if err != nil {
		return "", err
	}

	promptString := ""
	if promptConfig.CurrentAttempt > 1 {
		promptString = "Permission denied, please try again.\n"
	}
	switch promptConfig.SecretType {
	case conductor.SecretTypePassword:
		promptString += fmt.Sprintf("%s@%s's password: ", promptConfig.Username, promptConfig.Host)
	case conductor.SecretTypeKeyPassphrase:
		promptString += fmt.Sprintf("Enter passphrase for key '%s': ", promptConfig.KeyPath)
	default:
		return "", fmt.Errorf("unsupported secret type: %v", promptConfig.SecretType)
	}

	return promptString, nil
}

// formatFingerprintPrompt creates a user-friendly prompt string to display to the user for host key fingerprint confirmation.
func formatFingerprintPrompt(prompt *authproto.TargetLoginFingerprintPrompt) (string, error) {
	cfg, err := grpcserver.ProtoToHostKeyPromptConfig(prompt)
	if err != nil {
		return "", err
	}

	knownAs := ""
	if len(cfg.KnownAs) > 0 {
		knownAs = fmt.Sprintf("This host key is already known by the following other names/addresses:\n    %s\n",
			strings.Join(cfg.KnownAs, "\n    "))
	}
	hostKeyType := normalizeHostKeyType(cfg.HostKeyType)
	if hostKeyType != "" {
		return fmt.Sprintf("The authenticity of host '%s' can't be established. The %s host key fingerprint is %s.\n%sAre you sure you want to continue connecting and "+
			"add this host to the list of known hosts (yes/no/[fingerprint])? ", cfg.Host, hostKeyType, cfg.HostKeyFingerprint, knownAs), nil
	}
	return fmt.Sprintf("The authenticity of host '%s' can't be established. The host key fingerprint is %s.\n%sAre you sure you want to continue connecting and "+
		"add this host to the list of known hosts (yes/no/[fingerprint])? ", cfg.Host, cfg.HostKeyFingerprint, knownAs), nil
}

// normalizeHostKeyType converts various representations of host key types to a more user-friendly format for display in prompts.
func normalizeHostKeyType(hostKeyType string) string {
	hostKeyType = strings.TrimSpace(hostKeyType)
	if hostKeyType == "" {
		return ""
	}

	switch strings.ToLower(hostKeyType) {
	case "ssh-ed25519":
		return "ED25519"
	case "ssh-rsa", "rsa-sha2-256", "rsa-sha2-512":
		return "RSA"
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		return "ECDSA"
	default:
		return hostKeyType
	}
}

// PromptSecret securely reads a secret (i.e password or passphrase) without echoing it.
func PromptSecret(prompt string) ([]byte, error) {
	stdin := os.Stdin
	stdinFD := int(stdin.Fd())

	// If stdin is not a terminal, read directly from stdin (supports piping).
	// Read one byte at a time to avoid buffering.
	if !term.IsTerminal(stdinFD) {
		buf, err := readUntilNewline(stdin)
		if err != nil {
			return nil, err
		}
		pw := bytes.TrimRight(buf, "\r")
		return pw, nil
	}

	// stdin is a terminal; prefer /dev/tty on Unix for a clean prompt.
	fd := stdinFD
	if runtime.GOOS != "windows" {
		if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			fd = int(tty.Fd())
			defer tty.Close()
		}
	}

	// Always write the prompt to stderr
	fmt.Fprint(os.Stderr, prompt)

	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr) // newline after input
	if err != nil {
		return nil, err
	}

	return pw, nil
}

// promptLineFn defines a function for reading a line of input from the user.
type promptLineFn func(promptText string) (string, error)

// PromptFingerprint reads a yes/no confirmation or a fingerprint from the user and returns true
// if 'yes' or the expected fingerprint is provided. The user has 3 attempts to provide a valid response.
// User input is read using promptLine.
func PromptFingerprint(prompt string, fingerprint string) (bool, error) {
	return promptFingerprint(prompt, fingerprint, promptsvc.PromptLine)
}

// promptFingerprint reads a yes/no confirmation or a fingerprint from the user and returns true
// if 'yes' or the expected fingerprint is provided. The user has 3 attempts to provide a valid response.
// The promptFn is used to read user input.
func promptFingerprint(promptText string, fingerprint string, promptFn promptLineFn) (bool, error) {
	const maxAttempts = 3
	fingerprint = strings.TrimSpace(fingerprint)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		response, err := promptFn(promptText)
		if err != nil {
			return false, message.New(message.CliServiceTargetloginPromptFailed).WithCause(err)
		}
		response = strings.TrimSpace(response)
		switch strings.ToLower(response) {
		case "yes":
			return true, nil
		case "no":
			return false, nil
		default:
			if fingerprint != "" && response == fingerprint {
				return true, nil
			}
		}
		promptText = "Please type 'yes', 'no' or the fingerprint: "
	}
	return false, message.New(message.CliServiceTargetloginHostKeyMaxAttempts)
}

// readUntilNewline reads from reader until a newline or EOF.
func readUntilNewline(reader io.Reader) ([]byte, error) {
	return promptsvc.ReadUntilNewline(reader)
}

// defaultAuthConnector dials the Auth service and returns the Auth client.
// The caller is responsible for closing the returned io.Closer when finished.
func defaultAuthConnector(host string, port int) (authproto.AuthClient, io.Closer, error) {
	client, conn, err := client.AuthenticateWithAuthService(host, port)
	if err != nil {
		return nil, nil, err
	}
	return client, conn, nil
}
