// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	conductormocks "github.com/Arm-Debug/apap-cli/apap-engine/conductor/conductormocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/authproto"
	"github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestTargetLogin(t *testing.T) {
	connectOptsMatcher := mock.MatchedBy(func(opts targetsession.ConnectOptions) bool {
		return opts.PlatformGate == conductor.TargetSupported &&
			opts.PromptProviders.SecretPromptProvider != nil &&
			opts.PromptProviders.FingerprintPromptProvider != nil
	})

	t.Run("errors on stream send failure in sendTargetLoginError", func(t *testing.T) {
		sendErr := errors.New("send failed")
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(sendErr).Once()

		err := sendTargetLoginError(stream, errors.New("original error"))
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(sendErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
	})
	t.Run("errors on missing dependencies", func(t *testing.T) {
		server := &AuthServer{}
		stream := mocks.NewAuthTargetLoginServer(t)
		expectedErr := message.New(message.CommonUnknownError).
			WithCause(errors.New("AuthServer is missing required dependencies"))
		var got *authproto.TargetLoginServerMessage
		stream.On("Send", mock.Anything).Run(func(args mock.Arguments) {
			got = args.Get(0).(*authproto.TargetLoginServerMessage)
		}).Return(nil).Once()

		err := server.TargetLogin(stream)
		require.NoError(t, err)
		require.NotNil(t, got)
		resp := got.GetResponse()
		require.NotNil(t, resp)
		require.Equal(t, apapproto.StatusCode_ERROR, resp.GetReturnCode())
		require.True(t, proto.Equal(message.BuildErrorChain(expectedErr), resp.GetError()))
		stream.AssertExpectations(t)
	})
	t.Run("successful login", func(t *testing.T) {
		session := &targetsessionmocks.MockTargetSession{}
		session.On("Connect", mock.Anything, connectOptsMatcher).Return(nil, nil).Once()

		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(session, nil)

		server := NewAuthServer(context.Background(), provider, &target.TargetAccess{})
		tgt := &apapproto.Target{Connection: &apapproto.Target_LocalConfig{LocalConfig: &apapproto.LocalConnectionConfig{}}}
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{Target: tgt},
			},
		}

		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(req, nil).Once()
		stream.On("Send", mock.Anything).Return(nil).Once()

		require.NoError(t, server.TargetLogin(stream))
		stream.AssertExpectations(t)
		session.AssertExpectations(t)
		provider.AssertExpectations(t)
	})
	t.Run("errors on connect failure", func(t *testing.T) {
		connectErr := errors.New("connect failed")
		session := &targetsessionmocks.MockTargetSession{}
		session.On("Connect", mock.Anything, connectOptsMatcher).Return(nil, connectErr).Once()

		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(session, nil)

		server := NewAuthServer(context.Background(), provider, &target.TargetAccess{})
		tgt := &apapproto.Target{Connection: &apapproto.Target_LocalConfig{LocalConfig: &apapproto.LocalConnectionConfig{}}}
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{Target: tgt},
			},
		}

		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(req, nil).Once()
		var got *authproto.TargetLoginServerMessage
		stream.On("Send", mock.Anything).Run(func(args mock.Arguments) {
			got = args.Get(0).(*authproto.TargetLoginServerMessage)
		}).Return(nil).Once()

		err := server.TargetLogin(stream)
		require.NoError(t, err)
		require.NotNil(t, got)
		resp := got.GetResponse()
		require.NotNil(t, resp)
		require.Equal(t, apapproto.StatusCode_ERROR, resp.GetReturnCode())
		require.True(t, proto.Equal(message.BuildErrorChain(connectErr), resp.GetError()))
		stream.AssertExpectations(t)
		session.AssertExpectations(t)
		provider.AssertExpectations(t)
	})
	t.Run("errors on send response failure", func(t *testing.T) {
		session := &targetsessionmocks.MockTargetSession{}
		session.On("Connect", mock.Anything, connectOptsMatcher).Return(nil, nil).Once()

		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(session, nil)

		server := NewAuthServer(context.Background(), provider, &target.TargetAccess{})
		tgt := &apapproto.Target{Connection: &apapproto.Target_LocalConfig{LocalConfig: &apapproto.LocalConnectionConfig{}}}
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{Target: tgt},
			},
		}

		sendErr := errors.New("send failed")
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(req, nil).Once()
		stream.On("Send", mock.Anything).Return(sendErr).Once()

		err := server.TargetLogin(stream)
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(sendErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
		session.AssertExpectations(t)
		provider.AssertExpectations(t)
	})
	t.Run("errrors on stream EOF", func(t *testing.T) {
		server := NewAuthServer(context.Background(), &targetsessionmocks.MockTargetSessionProvider{}, &target.TargetAccess{})
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(nil, io.EOF).Once()

		err := server.TargetLogin(stream)
		assert.Equal(t, message.New(message.CliServiceTargetloginStreamFailure).WithCause(io.EOF), err)

		stream.AssertExpectations(t)
	})
	t.Run("errors on missing target", func(t *testing.T) {
		server := NewAuthServer(context.Background(), &targetsessionmocks.MockTargetSessionProvider{}, &target.TargetAccess{})
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(&authproto.TargetLoginClientMessage{}, nil).Once()
		expectedErr := message.New(message.CommonUnknownError).
			WithCause(errors.New("did not receive a TargetLoginRequest message during TargetLogin"))
		var got *authproto.TargetLoginServerMessage
		stream.On("Send", mock.Anything).Run(func(args mock.Arguments) {
			got = args.Get(0).(*authproto.TargetLoginServerMessage)
		}).Return(nil).Once()

		err := server.TargetLogin(stream)
		require.NoError(t, err)
		require.NotNil(t, got)
		resp := got.GetResponse()
		require.NotNil(t, resp)
		require.Equal(t, apapproto.StatusCode_ERROR, resp.GetReturnCode())
		require.True(t, proto.Equal(message.BuildErrorChain(expectedErr), resp.GetError()))
		stream.AssertExpectations(t)
	})
	t.Run("errors on invalid target", func(t *testing.T) {
		server := NewAuthServer(context.Background(), &targetsessionmocks.MockTargetSessionProvider{}, &target.TargetAccess{})
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{Target: nil},
			},
		}
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(req, nil).Once()
		expectedErr := message.New(message.CommonUnknownError).WithCause(errors.New("missing target protobuf"))
		var got *authproto.TargetLoginServerMessage
		stream.On("Send", mock.Anything).Run(func(args mock.Arguments) {
			got = args.Get(0).(*authproto.TargetLoginServerMessage)
		}).Return(nil).Once()

		err := server.TargetLogin(stream)
		require.NoError(t, err)
		require.NotNil(t, got)
		resp := got.GetResponse()
		require.NotNil(t, resp)
		require.Equal(t, apapproto.StatusCode_ERROR, resp.GetReturnCode())
		require.True(t, proto.Equal(message.BuildErrorChain(expectedErr), resp.GetError()))
		stream.AssertExpectations(t)
	})
	t.Run("errors on cancel while waiting for lock", func(t *testing.T) {
		targetAccess := &target.TargetAccess{}
		lock := targetAccess.LockTarget(&target.LocalTarget{}, "hold-lock")
		defer lock.Unlock()

		server := NewAuthServer(context.Background(), &targetsessionmocks.MockTargetSessionProvider{}, targetAccess)
		tgt := &apapproto.Target{Connection: &apapproto.Target_LocalConfig{LocalConfig: &apapproto.LocalConnectionConfig{}}}
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{Target: tgt},
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(ctx)
		stream.On("Recv").Return(req, nil).Once()
		var got *authproto.TargetLoginServerMessage
		stream.On("Send", mock.Anything).Run(func(args mock.Arguments) {
			got = args.Get(0).(*authproto.TargetLoginServerMessage)
		}).Return(nil).Once()

		err := server.TargetLogin(stream)
		require.NoError(t, err)
		require.NotNil(t, got)
		resp := got.GetResponse()
		require.NotNil(t, resp)
		require.Equal(t, apapproto.StatusCode_ERROR, resp.GetReturnCode())
		require.True(t, proto.Equal(message.BuildErrorChain(message.New(message.EngineCommonUserCanceled)), resp.GetError()))
		stream.AssertExpectations(t)
	})
	t.Run("errors on target session provider failure", func(t *testing.T) {
		targetSessionErr := errors.New("target session provider error")
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return((*targetsessionmocks.MockTargetSession)(nil), targetSessionErr).Once()

		server := NewAuthServer(context.Background(), provider, &target.TargetAccess{})
		tgt := &apapproto.Target{Connection: &apapproto.Target_LocalConfig{LocalConfig: &apapproto.LocalConnectionConfig{}}}
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{Target: tgt},
			},
		}

		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(req, nil).Once()
		var got *authproto.TargetLoginServerMessage
		stream.On("Send", mock.Anything).Run(func(args mock.Arguments) {
			got = args.Get(0).(*authproto.TargetLoginServerMessage)
		}).Return(nil).Once()

		err := server.TargetLogin(stream)
		require.NoError(t, err)
		require.NotNil(t, got)
		resp := got.GetResponse()
		require.NotNil(t, resp)
		require.Equal(t, apapproto.StatusCode_ERROR, resp.GetReturnCode())
		require.True(t, proto.Equal(message.BuildErrorChain(targetSessionErr), resp.GetError()))
		stream.AssertExpectations(t)
		provider.AssertExpectations(t)
	})
	t.Run("transient SSH connection bypasses target session cache and closes the connection", func(t *testing.T) {
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		server := NewAuthServer(context.Background(), provider, &target.TargetAccess{})
		secureClient := &conductormocks.SecureClientMock{}
		secureClient.On("Close").Return(nil).Once()
		server.createSSHConnection = func(ctx context.Context, tgt *target.SSHTarget, pp conductor.PromptProviders) (conductor.SecureClient, error) {
			require.Equal(t, "user@target-host", tgt.DisplayHost())
			require.NotNil(t, pp.SecretPromptProvider)
			require.NotNil(t, pp.FingerprintPromptProvider)
			return secureClient, nil
		}

		hostKeyPolicy := apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ASK_IF_MISSING
		authMethod := apapproto.SSHAuthMethod_SSH_AUTH_METHOD_KEY
		tgt := &apapproto.Target{
			Connection: &apapproto.Target_SshConfig{
				SshConfig: &apapproto.SSHConnectionConfig{
					HostKeyPolicy: &hostKeyPolicy,
					Hosts: []*apapproto.SSHHostConfig{{
						Host:       "target-host",
						Port:       22,
						Username:   "user",
						AuthMethod: &authMethod,
					}},
				},
			},
		}
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{
					Target:                       tgt,
					CreateTransientSshConnection: true,
				},
			},
		}

		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(req, nil).Once()
		stream.On("Send", mock.Anything).Return(nil).Once()

		require.NoError(t, server.TargetLogin(stream))
		provider.AssertNotCalled(t, "TargetSession", mock.Anything)
		secureClient.AssertExpectations(t)
		provider.AssertExpectations(t)
		stream.AssertExpectations(t)
	})
	t.Run("transient SSH connection returns connect errors", func(t *testing.T) {
		connectErr := errors.New("connect failed")
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		server := NewAuthServer(context.Background(), provider, &target.TargetAccess{})
		server.createSSHConnection = func(context.Context, *target.SSHTarget, conductor.PromptProviders) (conductor.SecureClient, error) {
			return nil, connectErr
		}

		hostKeyPolicy := apapproto.SSHHostKeyPolicy_SSH_HOST_KEY_POLICY_ASK_IF_MISSING
		authMethod := apapproto.SSHAuthMethod_SSH_AUTH_METHOD_KEY
		tgt := &apapproto.Target{
			Connection: &apapproto.Target_SshConfig{
				SshConfig: &apapproto.SSHConnectionConfig{
					HostKeyPolicy: &hostKeyPolicy,
					Hosts: []*apapproto.SSHHostConfig{{
						Host:       "target-host",
						Port:       22,
						Username:   "user",
						AuthMethod: &authMethod,
					}},
				},
			},
		}
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{
					Target:                       tgt,
					CreateTransientSshConnection: true,
				},
			},
		}

		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(req, nil).Once()
		var got *authproto.TargetLoginServerMessage
		stream.On("Send", mock.Anything).Run(func(args mock.Arguments) {
			got = args.Get(0).(*authproto.TargetLoginServerMessage)
		}).Return(nil).Once()

		err := server.TargetLogin(stream)
		require.NoError(t, err)
		require.NotNil(t, got)
		resp := got.GetResponse()
		require.NotNil(t, resp)
		require.Equal(t, apapproto.StatusCode_ERROR, resp.GetReturnCode())
		require.True(t, proto.Equal(message.BuildErrorChain(connectErr), resp.GetError()))
		provider.AssertNotCalled(t, "TargetSession", mock.Anything)
		provider.AssertExpectations(t)
		stream.AssertExpectations(t)
	})
	t.Run("transient SSH connection request for non-ssh target falls back to target session", func(t *testing.T) {
		session := &targetsessionmocks.MockTargetSession{}
		session.On("Connect", mock.Anything, connectOptsMatcher).Return(nil, nil).Once()

		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(session, nil).Once()
		server := NewAuthServer(context.Background(), provider, &target.TargetAccess{})

		tgt := &apapproto.Target{Connection: &apapproto.Target_LocalConfig{LocalConfig: &apapproto.LocalConnectionConfig{}}}
		req := &authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Request{
				Request: &authproto.TargetLoginRequest{
					Target:                       tgt,
					CreateTransientSshConnection: true,
				},
			},
		}

		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Context").Return(context.Background())
		stream.On("Recv").Return(req, nil).Once()
		stream.On("Send", mock.Anything).Return(nil).Once()

		require.NoError(t, server.TargetLogin(stream))
		session.AssertExpectations(t)
		provider.AssertExpectations(t)
		stream.AssertExpectations(t)
	})
}

func TestPromptConfig(t *testing.T) {
	t.Run("password prompt is converted to and from proto", func(t *testing.T) {
		cfg := conductor.PromptConfig{
			SecretType:     conductor.SecretTypePassword,
			Host:           "10.0.0.1",
			JumpIndex:      1,
			CurrentAttempt: 2,
			MaxAttempts:    3,
		}

		promptMsg, err := PromptConfigToProto(cfg)
		require.NoError(t, err)

		roundTrip, err := ProtoToPromptConfig(promptMsg.Prompt)
		require.NoError(t, err)
		assert.Equal(t, cfg, roundTrip)
	})

	t.Run("passphrase prompt is converted to and from proto", func(t *testing.T) {
		cfg := conductor.PromptConfig{
			SecretType:     conductor.SecretTypeKeyPassphrase,
			Host:           "10.0.0.2",
			JumpIndex:      2,
			KeyPath:        "/tmp/test_key",
			CurrentAttempt: 1,
			MaxAttempts:    3,
		}

		promptMsg, err := PromptConfigToProto(cfg)
		require.NoError(t, err)

		roundTrip, err := ProtoToPromptConfig(promptMsg.Prompt)
		require.NoError(t, err)
		assert.Equal(t, cfg, roundTrip)
	})

	t.Run("password prompt missing host", func(t *testing.T) {
		_, err := PromptConfigToProto(conductor.PromptConfig{
			SecretType: conductor.SecretTypePassword,
		})
		require.Error(t, err)
	})

	t.Run("passphrase prompt missing host", func(t *testing.T) {
		_, err := PromptConfigToProto(conductor.PromptConfig{
			SecretType: conductor.SecretTypeKeyPassphrase,
			KeyPath:    "/tmp/test_key",
		})
		require.Error(t, err)
	})

	t.Run("passphrase prompt missing key path", func(t *testing.T) {
		_, err := PromptConfigToProto(conductor.PromptConfig{
			SecretType: conductor.SecretTypeKeyPassphrase,
			Host:       "10.0.0.3",
		})
		require.Error(t, err)
	})

	t.Run("proto missing host for password", func(t *testing.T) {
		_, err := ProtoToPromptConfig(&authproto.TargetLoginPrompt{
			Type: authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD,
		})
		require.Error(t, err)
	})

	t.Run("proto missing host for passphrase", func(t *testing.T) {
		_, err := ProtoToPromptConfig(&authproto.TargetLoginPrompt{
			Type:    authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_KEYPASSPHRASE,
			KeyPath: "/tmp/test_key",
		})
		require.Error(t, err)
	})

	t.Run("proto missing key path for passphrase", func(t *testing.T) {
		_, err := ProtoToPromptConfig(&authproto.TargetLoginPrompt{
			Type: authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_KEYPASSPHRASE,
			Host: "10.0.0.4",
		})
		require.Error(t, err)
	})

	t.Run("proto missing prompt", func(t *testing.T) {
		_, err := ProtoToPromptConfig(nil)
		require.Error(t, err)
		assert.EqualError(t, err, "missing prompt")
	})
}

func TestTargetLoginPrompter(t *testing.T) {
	t.Run("returns secret on success", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_Credentials{
				Credentials: &authproto.TargetLoginCredentials{Secret: []byte("secret")},
			},
		}, nil).Once()

		prompter := newTargetLoginPrompter(stream, context.Background())
		secret, err := prompter.PromptSecret(conductor.PromptConfig{
			SecretType: conductor.SecretTypePassword,
			Host:       "10.0.0.10",
		})
		require.NoError(t, err)
		assert.Equal(t, []byte("secret"), secret)

		stream.AssertExpectations(t)
	})
	t.Run("returns cancellation when context done while waiting for secret", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(nil).Once()

		unblockRecv := make(chan struct{})
		recvStarted := make(chan struct{})
		stream.On("Recv").Run(func(args mock.Arguments) {
			close(recvStarted)
			<-unblockRecv
		}).Return(nil, io.EOF).Once()

		ctx, cancel := context.WithCancel(context.Background())
		prompter := newTargetLoginPrompter(stream, ctx)

		go func() {
			<-recvStarted
			cancel()
			close(unblockRecv)
		}()

		_, err := prompter.PromptSecret(conductor.PromptConfig{
			SecretType: conductor.SecretTypePassword,
			Host:       "10.0.0.1",
		})
		assert.Equal(t, message.New(message.EngineCommonUserCanceled), err)

		stream.AssertExpectations(t)
	})
	t.Run("returns acceptance for host key fingerprint prompt", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Run(func(args mock.Arguments) {
			msg := args.Get(0).(*authproto.TargetLoginServerMessage)
			require.NotNil(t, msg.GetFingerprintPrompt())
			assert.Equal(t, []string{"10.0.0.11"}, msg.GetFingerprintPrompt().GetKnownAs())
		}).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginClientMessage{
			Message: &authproto.TargetLoginClientMessage_FingerprintAcceptance{
				FingerprintAcceptance: &authproto.TargetLoginFingerprintAcceptance{Accepted: true},
			},
		}, nil).Once()

		prompter := newTargetLoginPrompter(stream, context.Background())
		accepted, err := prompter.PromptAcceptance(conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"10.0.0.11"},
		})
		require.NoError(t, err)
		assert.True(t, accepted)

		stream.AssertExpectations(t)
	})
	t.Run("errors when host key fingerprint prompt config is invalid", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginServer(t)

		prompter := newTargetLoginPrompter(stream, context.Background())
		_, err := prompter.PromptAcceptance(conductor.HostKeyPromptConfig{
			HostKeyFingerprint: "SHA256:abc123",
		})
		msg := message.IsMessage(err)
		require.NotNil(t, msg)
		assert.Equal(t, message.CliServiceTargetloginPromptFailed, msg.Code())
		assert.Contains(t, err.Error(), "missing host for host key fingerprint prompt")
	})
	t.Run("errors on send failure for host key fingerprint prompt", func(t *testing.T) {
		sendErr := errors.New("send failed")
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(sendErr).Once()

		prompter := newTargetLoginPrompter(stream, context.Background())
		_, err := prompter.PromptAcceptance(conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
		})
		expectedErr := message.New(message.CliServiceTargetloginPromptFailed).WithCause(sendErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
	})
	t.Run("errors on recv failure for host key fingerprint prompt", func(t *testing.T) {
		recvErr := io.EOF
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(nil, recvErr).Once()

		prompter := newTargetLoginPrompter(stream, context.Background())
		_, err := prompter.PromptAcceptance(conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
		})
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(recvErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
	})
	t.Run("errors on unexpected response type for host key fingerprint prompt", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginClientMessage{}, nil).Once()

		prompter := newTargetLoginPrompter(stream, context.Background())
		_, err := prompter.PromptAcceptance(conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
		})
		expectedErr := message.New(message.CommonUnknownError).
			WithCause(errors.New("unexpected message type received during target login"))
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
	})
	t.Run("errors on send failure", func(t *testing.T) {
		sendErr := errors.New("send failed")
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(sendErr).Once()

		prompter := newTargetLoginPrompter(stream, context.Background())
		_, err := prompter.PromptSecret(conductor.PromptConfig{
			SecretType: conductor.SecretTypePassword,
			Host:       "10.0.0.1",
		})
		expectedErr := message.New(message.CliServiceTargetloginPromptFailed).WithCause(sendErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
	})
	t.Run("errors on recv failure", func(t *testing.T) {
		recvErr := io.EOF
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(nil, recvErr).Once()

		prompter := newTargetLoginPrompter(stream, context.Background())
		_, err := prompter.PromptSecret(conductor.PromptConfig{
			SecretType: conductor.SecretTypePassword,
			Host:       "10.0.0.2",
		})
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(recvErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
	})
	t.Run("errors on unexpected response type", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginServer(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginClientMessage{}, nil).Once()

		prompter := newTargetLoginPrompter(stream, context.Background())
		_, err := prompter.PromptSecret(conductor.PromptConfig{
			SecretType: conductor.SecretTypePassword,
			Host:       "10.0.0.11",
		})
		expectedErr := message.New(message.CommonUnknownError).
			WithCause(errors.New("unexpected message type received during target login"))
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
	})
}

func TestPromptConfigToProto(t *testing.T) {
	t.Run("password prompt creates prompt proto", func(t *testing.T) {
		promptMsg, err := PromptConfigToProto(conductor.PromptConfig{
			SecretType: conductor.SecretTypePassword,
			Host:       "10.0.0.10",
		})
		require.NoError(t, err)
		msg := &authproto.TargetLoginServerMessage{Message: promptMsg}
		prompt := msg.GetPrompt()
		require.NotNil(t, prompt)
		assert.Equal(t, authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD, prompt.GetType())
	})
}

func TestHostKeyPromptConfigToProto(t *testing.T) {
	t.Run("host key fingerprint prompt creates host key proto", func(t *testing.T) {
		prompt, err := HostKeyPromptConfigToProto(conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"10.0.0.11", " example-host "},
		})
		require.NoError(t, err)
		msg := &authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_FingerprintPrompt{FingerprintPrompt: prompt},
		}
		hostKeyPrompt := msg.GetFingerprintPrompt()
		require.NotNil(t, hostKeyPrompt)
		assert.Equal(t, "10.0.0.12", hostKeyPrompt.GetHost())
		assert.Equal(t, "ssh-ed25519", hostKeyPrompt.GetHostKeyType())
		assert.Equal(t, "SHA256:abc123", hostKeyPrompt.GetHostKeyFingerprint())
		assert.Equal(t, []string{"10.0.0.11", "example-host"}, hostKeyPrompt.GetKnownAs())
	})
	t.Run("host key fingerprint prompt missing host errors", func(t *testing.T) {
		_, err := HostKeyPromptConfigToProto(conductor.HostKeyPromptConfig{
			HostKeyFingerprint: "SHA256:abc123",
		})
		require.Error(t, err)
	})
	t.Run("host key fingerprint prompt missing fingerprint errors", func(t *testing.T) {
		_, err := HostKeyPromptConfigToProto(conductor.HostKeyPromptConfig{
			Host: "10.0.0.12",
		})
		require.Error(t, err)
	})
	t.Run("host key fingerprint prompt missing key type allowed", func(t *testing.T) {
		prompt, err := HostKeyPromptConfigToProto(conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyFingerprint: "SHA256:abc123",
		})
		require.NoError(t, err)
		assert.Equal(t, "", prompt.GetHostKeyType())
	})
	t.Run("whitespace-only aliases are dropped", func(t *testing.T) {
		prompt, err := HostKeyPromptConfigToProto(conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"   ", "\t"},
		})
		require.NoError(t, err)
		assert.Nil(t, prompt.GetKnownAs())
	})
}

func TestProtoToHostKeyPromptConfig(t *testing.T) {
	t.Run("host key fingerprint prompt proto converts to config", func(t *testing.T) {
		cfg, err := ProtoToHostKeyPromptConfig(&authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.12",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"10.0.0.11", " example-host "},
		})
		require.NoError(t, err)
		assert.Equal(t, conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"10.0.0.11", "example-host"},
		}, cfg)
	})
	t.Run("missing prompt errors", func(t *testing.T) {
		_, err := ProtoToHostKeyPromptConfig(nil)
		require.Error(t, err)
	})
	t.Run("missing host errors", func(t *testing.T) {
		_, err := ProtoToHostKeyPromptConfig(&authproto.TargetLoginFingerprintPrompt{
			HostKeyFingerprint: "SHA256:abc123",
		})
		require.Error(t, err)
	})
	t.Run("missing fingerprint errors", func(t *testing.T) {
		_, err := ProtoToHostKeyPromptConfig(&authproto.TargetLoginFingerprintPrompt{
			Host: "10.0.0.12",
		})
		require.Error(t, err)
	})
	t.Run("missing key type allowed", func(t *testing.T) {
		cfg, err := ProtoToHostKeyPromptConfig(&authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.12",
			HostKeyFingerprint: "SHA256:abc123",
		})
		require.NoError(t, err)
		assert.Equal(t, conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyType:        "",
			HostKeyFingerprint: "SHA256:abc123",
		}, cfg)
	})
	t.Run("whitespace-only aliases are dropped", func(t *testing.T) {
		cfg, err := ProtoToHostKeyPromptConfig(&authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.12",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"   ", "\t"},
		})
		require.NoError(t, err)
		assert.Equal(t, conductor.HostKeyPromptConfig{
			Host:               "10.0.0.12",
			HostKeyType:        "",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            nil,
		}, cfg)
	})
}
