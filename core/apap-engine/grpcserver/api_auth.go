// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/authproto"
)

// AuthServer implements the TLS-protected authentication service.
type AuthServer struct {
	authproto.UnimplementedAuthServer
	shutdownCtx    context.Context
	targetSessions targetsession.TargetSessionProvider
	targetAccess   *target.TargetAccess
}

// NewAuthServer creates a new AuthServer instance.
func NewAuthServer(shutdownCtx context.Context, targetSessions targetsession.TargetSessionProvider, targetAccess *target.TargetAccess) *AuthServer {
	return &AuthServer{
		shutdownCtx:    shutdownCtx,
		targetSessions: targetSessions,
		targetAccess:   targetAccess,
	}
}

// sendTargetLoginError sends a TargetLoginResponse with the provided error details to the client.
// Return the error as a successful response with the error message, to allow the GUI to parse the ErrorChain
// TODO: remove after GUI has implemented error decoding (https://jira.arm.com/browse/APAP-3771)
func sendTargetLoginError(stream authproto.Auth_TargetLoginServer, err error) error {
	resp := &authproto.TargetLoginServerMessage{
		Message: &authproto.TargetLoginServerMessage_Response{
			Response: &authproto.TargetLoginResponse{
				ReturnCode: apapproto.StatusCode_ERROR,
				Error:      message.BuildErrorChain(err),
			},
		},
	}
	if sendErr := stream.Send(resp); sendErr != nil {
		return message.New(message.CliServiceTargetloginStreamFailure).WithCause(sendErr)
	}
	return nil
}

// TargetLogin allows clients to ensure that the engine has authenticated with the target. If we do not have a connection
// to the target, it will attempt to establish one. This might involve prompting the user for credentials.
func (s *AuthServer) TargetLogin(stream authproto.Auth_TargetLoginServer) error {
	if s.targetSessions == nil || s.targetAccess == nil {
		err := message.New(message.CommonUnknownError).
			WithCause(errors.New("AuthServer is missing required dependencies"))
		return sendTargetLoginError(stream, err)
	}

	ctx := stream.Context()
	msg, err := stream.Recv()
	if err != nil {
		return message.New(message.CliServiceTargetloginStreamFailure).WithCause(err)
	}
	request := msg.GetRequest()
	if request == nil {
		err := message.New(message.CommonUnknownError).
			WithCause(errors.New("did not receive a TargetLoginRequest message during TargetLogin"))
		return sendTargetLoginError(stream, err)
	}
	tgt, err := TargetFromProto(request.Target)
	if err != nil {
		return sendTargetLoginError(stream, err)
	}

	log.Debugf("Received TargetLogin request for target: %s", tgt.String())

	lock := s.targetAccess.LockWithCancellation(tgt, "target login", ctx.Done())
	if lock == nil {
		return sendTargetLoginError(stream, message.New(message.EngineCommonUserCancellationError))
	}
	defer lock.Unlock()

	prompter := newTargetLoginPrompter(stream, s.shutdownCtx)
	targetSession, err := s.targetSessions.TargetSession(tgt)
	if err != nil {
		return sendTargetLoginError(stream, err)
	}

	_, err = targetSession.Connect(ctx, targetsession.ConnectOptions{
		PromptProviders: conductor.PromptProviders{
			SecretPromptProvider:      prompter.PromptSecret,
			FingerprintPromptProvider: prompter.PromptAcceptance,
		},
	})
	if err != nil {
		return sendTargetLoginError(stream, err)
	}

	resp := &authproto.TargetLoginServerMessage{
		Message: &authproto.TargetLoginServerMessage_Response{
			Response: &authproto.TargetLoginResponse{ReturnCode: apapproto.StatusCode_SUCCESS},
		},
	}
	if err := stream.Send(resp); err != nil {
		return message.New(message.CliServiceTargetloginStreamFailure).WithCause(err)
	}
	return nil
}

// targetLoginPrompter sends prompt requests to the client and waits for the response.
// The requests and responses are sent over a secure auth service.
type targetLoginPrompter struct {
	stream      authproto.Auth_TargetLoginServer
	shutdownCtx context.Context
}

// newTargetLoginPrompter creates a new targetLoginPrompter instance.
func newTargetLoginPrompter(stream authproto.Auth_TargetLoginServer, shutdownCtx context.Context) *targetLoginPrompter {
	return &targetLoginPrompter{
		stream:      stream,
		shutdownCtx: shutdownCtx,
	}
}

// PromptSecret sends a prompt to the client and waits for the secure response containing the secret.
func (p *targetLoginPrompter) PromptSecret(cfg conductor.PromptConfig) ([]byte, error) {
	promptMsg, err := PromptConfigToProto(cfg)
	if err != nil {
		return nil, message.New(message.CliServiceTargetloginPromptFailed).WithCause(err)
	}
	serverMsg := &authproto.TargetLoginServerMessage{
		Message: promptMsg,
	}
	// Send the secret prompt to the client
	if err := p.stream.Send(serverMsg); err != nil {
		return nil, message.New(message.CliServiceTargetloginPromptFailed).WithCause(err)
	}

	msg, err := p.recvClientMessage()
	if err != nil {
		return nil, err
	}

	creds := msg.GetCredentials()
	if creds == nil {
		return nil, message.New(message.CommonUnknownError).
			WithCause(errors.New("unexpected message type received during target login"))
	}
	return creds.GetSecret(), nil
}

// PromptAcceptance sends a host key fingerprint prompt to the client and waits for the acceptance response.
func (p *targetLoginPrompter) PromptAcceptance(cfg conductor.HostKeyPromptConfig) (bool, error) {
	prompt, err := HostKeyPromptConfigToProto(cfg)
	if err != nil {
		return false, message.New(message.CliServiceTargetloginPromptFailed).WithCause(err)
	}
	serverMsg := &authproto.TargetLoginServerMessage{
		Message: &authproto.TargetLoginServerMessage_FingerprintPrompt{FingerprintPrompt: prompt},
	}
	if err := p.stream.Send(serverMsg); err != nil {
		return false, message.New(message.CliServiceTargetloginPromptFailed).WithCause(err)
	}

	msg, err := p.recvClientMessage()
	if err != nil {
		return false, err
	}

	acceptance := msg.GetFingerprintAcceptance()
	if acceptance == nil {
		return false, message.New(message.CommonUnknownError).
			WithCause(errors.New("unexpected message type received during target login"))
	}

	return acceptance.GetAccepted(), nil
}

func (p *targetLoginPrompter) recvClientMessage() (*authproto.TargetLoginClientMessage, error) {
	type recvResult struct {
		msg *authproto.TargetLoginClientMessage
		err error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		msg, err := p.stream.Recv()
		recvCh <- recvResult{msg: msg, err: err}
	}()

	var msg *authproto.TargetLoginClientMessage
	var err error
	select {
	case <-p.shutdownCtx.Done():
		return nil, message.New(message.EngineCommonUserCancellationError)
	case result := <-recvCh:
		msg, err = result.msg, result.err
	}
	if p.shutdownCtx.Err() != nil {
		return nil, message.New(message.EngineCommonUserCancellationError)
	}
	if err != nil {
		return nil, message.New(message.CliServiceTargetloginStreamFailure).WithCause(err)
	}
	if msg == nil {
		return nil, message.New(message.CommonUnknownError).
			WithCause(errors.New("unexpected empty message received during target login"))
	}
	return msg, nil
}

// HostKeyPromptConfigToProto validates and converts a host key fingerprint prompt config to proto.
func HostKeyPromptConfigToProto(cfg conductor.HostKeyPromptConfig) (*authproto.TargetLoginFingerprintPrompt, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, errors.New("missing host for host key fingerprint prompt")
	}
	fingerprint := strings.TrimSpace(cfg.HostKeyFingerprint)
	if fingerprint == "" {
		return nil, errors.New("missing fingerprint for host key fingerprint prompt")
	}
	return &authproto.TargetLoginFingerprintPrompt{
		Host:               host,
		HostKeyType:        strings.TrimSpace(cfg.HostKeyType),
		HostKeyFingerprint: fingerprint,
		KnownAs:            trimStrings(cfg.KnownAs),
	}, nil
}

// ProtoToHostKeyPromptConfig validates and converts a proto host key fingerprint prompt to config.
func ProtoToHostKeyPromptConfig(prompt *authproto.TargetLoginFingerprintPrompt) (conductor.HostKeyPromptConfig, error) {
	if prompt == nil {
		return conductor.HostKeyPromptConfig{}, errors.New("missing host key fingerprint prompt")
	}

	host := strings.TrimSpace(prompt.GetHost())
	if host == "" {
		return conductor.HostKeyPromptConfig{}, errors.New("missing host for host key fingerprint prompt")
	}
	fingerprint := strings.TrimSpace(prompt.GetHostKeyFingerprint())
	if fingerprint == "" {
		return conductor.HostKeyPromptConfig{}, errors.New("missing fingerprint for host key fingerprint prompt")
	}

	return conductor.HostKeyPromptConfig{
		Host:               host,
		HostKeyType:        strings.TrimSpace(prompt.GetHostKeyType()),
		HostKeyFingerprint: fingerprint,
		KnownAs:            trimStrings(prompt.GetKnownAs()),
	}, nil
}

func trimStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			trimmed = append(trimmed, value)
		}
	}
	if len(trimmed) == 0 {
		return nil
	}
	return trimmed
}

// PromptConfigToProto converts a PromptConfig to proto and validates required fields.
func PromptConfigToProto(cfg conductor.PromptConfig) (*authproto.TargetLoginServerMessage_Prompt, error) {
	prompt := &authproto.TargetLoginPrompt{
		JumpIndex:      uint32(cfg.JumpIndex),  // #nosec G115
		TotalJumps:     uint32(cfg.TotalJumps), // #nosec G115
		Host:           strings.TrimSpace(cfg.Host),
		Username:       strings.TrimSpace(cfg.Username),
		KeyPath:        strings.TrimSpace(cfg.KeyPath),
		CurrentAttempt: uint32(cfg.CurrentAttempt), // #nosec G115
		MaxAttempts:    uint32(cfg.MaxAttempts),    // #nosec G115
	}
	switch cfg.SecretType {
	case conductor.SecretTypeKeyPassphrase:
		prompt.Type = authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_KEYPASSPHRASE
		if prompt.KeyPath == "" {
			return nil, errors.New("missing key path for passphrase prompt")
		}
		if prompt.Host == "" {
			return nil, errors.New("missing host for passphrase prompt")
		}
	case conductor.SecretTypePassword:
		prompt.Type = authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD
		if prompt.Host == "" {
			return nil, errors.New("missing host for password prompt")
		}
	default:
		return nil, errors.New("unsupported secret type for target login prompt")
	}
	return &authproto.TargetLoginServerMessage_Prompt{Prompt: prompt}, nil
}

// ProtoToPromptConfig converts a prompt proto into a PromptConfig and validates required fields.
func ProtoToPromptConfig(prompt *authproto.TargetLoginPrompt) (conductor.PromptConfig, error) {
	if prompt == nil {
		return conductor.PromptConfig{}, errors.New("missing prompt")
	}

	cfg := conductor.PromptConfig{
		JumpIndex:      int(prompt.GetJumpIndex()),
		TotalJumps:     int(prompt.GetTotalJumps()),
		Host:           strings.TrimSpace(prompt.GetHost()),
		Username:       strings.TrimSpace(prompt.GetUsername()),
		KeyPath:        strings.TrimSpace(prompt.GetKeyPath()),
		CurrentAttempt: int(prompt.GetCurrentAttempt()),
		MaxAttempts:    int(prompt.GetMaxAttempts()),
	}

	switch prompt.GetType() {
	case authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD:
		if cfg.Host == "" {
			return conductor.PromptConfig{}, errors.New("missing host for password prompt")
		}
		cfg.SecretType = conductor.SecretTypePassword
	case authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_KEYPASSPHRASE:
		if cfg.KeyPath == "" {
			return conductor.PromptConfig{}, errors.New("missing key path for passphrase prompt")
		}
		if cfg.Host == "" {
			return conductor.PromptConfig{}, errors.New("missing host for passphrase prompt")
		}
		cfg.SecretType = conductor.SecretTypeKeyPassphrase
	default:
		return conductor.PromptConfig{}, errors.New("unsupported prompt type for target login")
	}

	return cfg, nil
}
