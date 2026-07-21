// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package targetlogin

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	"github.com/Arm-Debug/apap-cli/clients/go/authproto"
	"github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockCloser struct {
	mock.Mock
}

func (m *mockCloser) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestPromptSecretReadsFromStdinWhenNotTTY(t *testing.T) {
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
		_ = w.Close()
	})

	// Write the password to the write end and close to signal EOF.
	_, err = w.Write([]byte("piped-secret\n"))
	require.NoError(t, err)
	_ = w.Close()

	os.Stdin = r

	pw, err := PromptSecret("Enter password: ")
	require.NoError(t, err)
	assert.Equal(t, []byte("piped-secret"), pw)
}

func TestPromptSecretAcceptsEmptyStdin(t *testing.T) {
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
		_ = w.Close()
	})

	// Write only a newline (or nothing) to simulate empty password.
	_, err = w.Write([]byte("\n"))
	require.NoError(t, err)
	_ = w.Close()

	os.Stdin = r

	pw, err := PromptSecret("Enter password: ")
	require.NoError(t, err)
	assert.Len(t, pw, 0)
}

func TestPromptFingerprintAcceptsFingerprint(t *testing.T) {
	answers := []string{"SHA256:abc123"}
	index := 0

	ok, err := promptFingerprint(
		"Accept this host key? ",
		"SHA256:abc123",
		func(prompt string) (string, error) {
			response := answers[index]
			index++
			return response, nil
		},
	)

	require.NoError(t, err)
	assert.True(t, ok)
}

func TestPromptFingerprintAcceptsYes(t *testing.T) {
	ok, err := promptFingerprint(
		"Accept this host key? ",
		"SHA256:abc123",
		func(prompt string) (string, error) {
			return "yes", nil
		},
	)

	require.NoError(t, err)
	assert.True(t, ok)
}

func TestPromptFingerprintReturnsErrorAfterMaxAttempts(t *testing.T) {
	answers := []string{"nope", "still-no", "nah"}
	index := 0

	ok, err := promptFingerprint(
		"Accept this host key? ",
		"SHA256:abc123",
		func(prompt string) (string, error) {
			response := answers[index]
			index++
			return response, nil
		},
	)

	require.Error(t, err)
	assert.False(t, ok)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	assert.Equal(t, message.CliServiceTargetloginHostKeyMaxAttempts, msg.Code())
}

func TestPromptFingerprintUsesRetryPrompt(t *testing.T) {
	var prompts []string
	answers := []string{"nope", "no"}
	index := 0

	ok, err := promptFingerprint(
		"Accept this host key? ",
		"SHA256:abc123",
		func(prompt string) (string, error) {
			prompts = append(prompts, prompt)
			response := answers[index]
			index++
			return response, nil
		},
	)

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, []string{
		"Accept this host key? ",
		"Please type 'yes', 'no' or the fingerprint: ",
	}, prompts)
}

func TestFormatTargetLoginPrompt(t *testing.T) {
	t.Run("password prompt includes user and host", func(t *testing.T) {
		prompt, err := formatTargetLoginPrompt(&authproto.TargetLoginPrompt{
			Type:     authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD,
			Host:     "1.2.3.4",
			Username: "alice",
		})
		require.NoError(t, err)
		assert.Equal(t, "alice@1.2.3.4's password: ", prompt)
	})

	t.Run("key passphrase prompt includes key path", func(t *testing.T) {
		prompt, err := formatTargetLoginPrompt(&authproto.TargetLoginPrompt{
			Type:    authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_KEYPASSPHRASE,
			Host:    "5.6.7.8",
			KeyPath: "/tmp/test_key",
		})
		require.NoError(t, err)
		assert.Equal(t, "Enter passphrase for key '/tmp/test_key': ", prompt)
	})

	t.Run("retry prompt includes permission denied message", func(t *testing.T) {
		prompt, err := formatTargetLoginPrompt(&authproto.TargetLoginPrompt{
			Type:           authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD,
			Host:           "1.2.3.4",
			Username:       "alice",
			CurrentAttempt: 2,
			MaxAttempts:    3,
		})
		require.NoError(t, err)
		assert.Equal(t, "Permission denied, please try again.\nalice@1.2.3.4's password: ", prompt)
	})

	t.Run("password prompt missing host returns error", func(t *testing.T) {
		_, err := formatTargetLoginPrompt(&authproto.TargetLoginPrompt{
			Type: authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD,
		})
		require.Error(t, err)
	})

	t.Run("passphrase prompt missing key returns error", func(t *testing.T) {
		_, err := formatTargetLoginPrompt(&authproto.TargetLoginPrompt{
			Type: authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_KEYPASSPHRASE,
		})
		require.Error(t, err)
	})
}

func TestFormatFingerprintPrompt(t *testing.T) {
	t.Run("host key type is normalized", func(t *testing.T) {
		prompt, err := formatFingerprintPrompt(&authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.9",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
		})
		require.NoError(t, err)
		assert.Equal(t, "The authenticity of host '10.0.0.9' can't be established. The ED25519 host key fingerprint is SHA256:abc123.\nAre you sure you want to continue connecting and add this host to the list of known hosts (yes/no/[fingerprint])? ", prompt)
	})
	t.Run("rsa is normalized", func(t *testing.T) {
		prompt, err := formatFingerprintPrompt(&authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.9",
			HostKeyType:        "rsa-sha2-512",
			HostKeyFingerprint: "SHA256:abc123",
		})
		require.NoError(t, err)
		assert.Equal(t, "The authenticity of host '10.0.0.9' can't be established. The RSA host key fingerprint is SHA256:abc123.\nAre you sure you want to continue connecting and add this host to the list of known hosts (yes/no/[fingerprint])? ", prompt)
	})
	t.Run("ecdsa is normalized", func(t *testing.T) {
		prompt, err := formatFingerprintPrompt(&authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.9",
			HostKeyType:        "ecdsa-sha2-nistp256",
			HostKeyFingerprint: "SHA256:abc123",
		})
		require.NoError(t, err)
		assert.Equal(t, "The authenticity of host '10.0.0.9' can't be established. The ECDSA host key fingerprint is SHA256:abc123.\nAre you sure you want to continue connecting and add this host to the list of known hosts (yes/no/[fingerprint])? ", prompt)
	})
	t.Run("known aliases are included in prompt", func(t *testing.T) {
		prompt, err := formatFingerprintPrompt(&authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.9",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"10.0.0.8", "jump.internal"},
		})
		require.NoError(t, err)
		assert.Equal(t, "The authenticity of host '10.0.0.9' can't be established. The ED25519 host key fingerprint is SHA256:abc123.\nThis host key is already known by the following other names/addresses:\n    10.0.0.8\n    jump.internal\nAre you sure you want to continue connecting and add this host to the list of known hosts (yes/no/[fingerprint])? ", prompt)
	})
	t.Run("known aliases are included when host key type is missing", func(t *testing.T) {
		prompt, err := formatFingerprintPrompt(&authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.9",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"10.0.0.8"},
		})
		require.NoError(t, err)
		assert.Equal(t, "The authenticity of host '10.0.0.9' can't be established. The host key fingerprint is SHA256:abc123.\nThis host key is already known by the following other names/addresses:\n    10.0.0.8\nAre you sure you want to continue connecting and add this host to the list of known hosts (yes/no/[fingerprint])? ", prompt)
	})
}

func TestNormalizeHostKeyType(t *testing.T) {
	t.Run("empty value returns empty", func(t *testing.T) {
		assert.Equal(t, "", normalizeHostKeyType("   "))
	})

	t.Run("unknown value returns original", func(t *testing.T) {
		assert.Equal(t, "unknown-ssh-key-type", normalizeHostKeyType("unknown-ssh-key-type"))
	})
}

func TestLoginToTarget(t *testing.T) {
	t.Run("Successful login", func(t *testing.T) {
		authHost := "auth-host"
		authPort := 1234

		target := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
			{Host: "1234", Port: 11, Username: "user1"},
			{Host: "5678", Port: 22, Username: "user2"},
		}}

		prompts := []*authproto.TargetLoginPrompt{
			{
				Type: authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD,
				Host: "1.2.3.4",
			},
			{
				Type:    authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_KEYPASSPHRASE,
				Host:    "5.6.7.8",
				KeyPath: "/tmp/test_key",
			},
		}

		prompt0, err := formatTargetLoginPrompt(prompts[0])
		require.NoError(t, err)
		prompt1, err := formatTargetLoginPrompt(prompts[1])
		require.NoError(t, err)

		secrets := map[string][]byte{
			prompt0: []byte("secret-one"),
			prompt1: []byte("secret-two"),
		}

		stream := mocks.NewAuthTargetLoginClient(t)

		// Send the target first
		stream.On("Send", mock.Anything).
			Run(func(args mock.Arguments) {
				msg := args.Get(0).(*authproto.TargetLoginClientMessage)
				req := msg.GetRequest()
				require.NotNil(t, req)
				tgtProto := req.GetTarget()
				require.NotNil(t, tgtProto)
				tgt, err := grpcserver.TargetFromProto(tgtProto)
				assert.NoError(t, err)
				assert.Equal(t, target, tgt)
			}).Return(nil).Once()

		for _, prompt := range prompts {
			prompt := prompt
			stream.On("Recv").
				Return(&authproto.TargetLoginServerMessage{
					Message: &authproto.TargetLoginServerMessage_Prompt{Prompt: prompt},
				}, nil).Once()

			stream.On("Send", mock.Anything).
				Run(func(args mock.Arguments) {
					msg := args.Get(0).(*authproto.TargetLoginClientMessage)
					creds := msg.GetCredentials()
					require.NotNil(t, creds)
					promptText, err := formatTargetLoginPrompt(prompt)
					require.NoError(t, err)
					assert.Equal(t, secrets[promptText], creds.Secret)
				}).Return(nil).Once()
		}

		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_Response{
				Response: &authproto.TargetLoginResponse{
					ReturnCode: apapproto.StatusCode_SUCCESS,
				},
			}}, nil).Once()

		stream.On("CloseSend").Return(nil).Once()

		closer := &mockCloser{}
		closer.On("Close").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				assert.Equal(t, authHost, host)
				assert.Equal(t, authPort, port)
				return authClient, closer, nil
			},
			func(prompt string) ([]byte, error) { return secrets[prompt], nil },
			PromptFingerprint,
		)

		config := grpcserver.GrpcServerConfig{Host: authHost, AuthPort: authPort}

		err = client.LoginToTarget(context.Background(), target, config)
		require.NoError(t, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
		closer.AssertExpectations(t)
	})
	t.Run("Host key prompt uses confirm prompter", func(t *testing.T) {
		authHost := "auth-host"
		authPort := 2345

		target := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
			{Host: "1234", Port: 11, Username: "user1"},
		}}

		prompt := &authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.9",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
		}

		promptText, err := formatFingerprintPrompt(prompt)
		require.NoError(t, err)

		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()

		stream.On("Recv").
			Return(&authproto.TargetLoginServerMessage{
				Message: &authproto.TargetLoginServerMessage_FingerprintPrompt{FingerprintPrompt: prompt},
			}, nil).Once()

		stream.On("Send", mock.Anything).
			Run(func(args mock.Arguments) {
				msg := args.Get(0).(*authproto.TargetLoginClientMessage)
				acceptance := msg.GetFingerprintAcceptance()
				require.NotNil(t, acceptance)
				assert.True(t, acceptance.Accepted)
			}).Return(nil).Once()

		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_Response{
				Response: &authproto.TargetLoginResponse{
					ReturnCode: apapproto.StatusCode_SUCCESS,
				},
			}}, nil).Once()

		stream.On("CloseSend").Return(nil).Once()

		closer := &mockCloser{}
		closer.On("Close").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				assert.Equal(t, authHost, host)
				assert.Equal(t, authPort, port)
				return authClient, closer, nil
			},
			func(prompt string) ([]byte, error) { return []byte("unused"), nil },
			func(prompt string, fingerprint string) (bool, error) {
				assert.Equal(t, promptText, prompt)
				assert.Equal(t, "SHA256:abc123", fingerprint)
				return true, nil
			},
		)

		config := grpcserver.GrpcServerConfig{Host: authHost, AuthPort: authPort}

		err = client.LoginToTarget(context.Background(), target, config)
		require.NoError(t, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
		closer.AssertExpectations(t)
	})
	t.Run("Host key prompt with known aliases still uses fingerprint prompter", func(t *testing.T) {
		authHost := "auth-host"
		authPort := 2345

		target := &engine_target.SSHTarget{Jumps: []engine_target.SSHHostConfig{
			{Host: "1234", Port: 11, Username: "user1"},
		}}

		prompt := &authproto.TargetLoginFingerprintPrompt{
			Host:               "10.0.0.9",
			HostKeyType:        "ssh-ed25519",
			HostKeyFingerprint: "SHA256:abc123",
			KnownAs:            []string{"10.0.0.8"},
		}

		promptText, err := formatFingerprintPrompt(prompt)
		require.NoError(t, err)

		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()

		stream.On("Recv").
			Return(&authproto.TargetLoginServerMessage{
				Message: &authproto.TargetLoginServerMessage_FingerprintPrompt{FingerprintPrompt: prompt},
			}, nil).Once()

		stream.On("Send", mock.Anything).
			Run(func(args mock.Arguments) {
				msg := args.Get(0).(*authproto.TargetLoginClientMessage)
				acceptance := msg.GetFingerprintAcceptance()
				require.NotNil(t, acceptance)
				assert.True(t, acceptance.Accepted)
			}).Return(nil).Once()

		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_Response{
				Response: &authproto.TargetLoginResponse{
					ReturnCode: apapproto.StatusCode_SUCCESS,
				},
			}}, nil).Once()

		stream.On("CloseSend").Return(nil).Once()

		closer := &mockCloser{}
		closer.On("Close").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				assert.Equal(t, authHost, host)
				assert.Equal(t, authPort, port)
				return authClient, closer, nil
			},
			func(prompt string) ([]byte, error) { return []byte("unused"), nil },
			func(prompt string, fingerprint string) (bool, error) {
				assert.Equal(t, promptText, prompt)
				assert.Equal(t, "SHA256:abc123", fingerprint)
				return true, nil
			},
		)

		config := grpcserver.GrpcServerConfig{Host: authHost, AuthPort: authPort}

		err = client.LoginToTarget(context.Background(), target, config)
		require.NoError(t, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
		closer.AssertExpectations(t)
	})
	t.Run("Host key prompt format error returns error", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_FingerprintPrompt{
				FingerprintPrompt: &authproto.TargetLoginFingerprintPrompt{
					Host:               "",
					HostKeyFingerprint: "SHA256:abc123",
					HostKeyType:        "ssh-ed25519",
				},
			},
		}, nil).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			func(prompt string, fingerprint string) (bool, error) { return true, nil },
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		expectedErr := message.New(message.CommonUnknownError).
			WithCause(errors.New("missing host for host key fingerprint prompt"))
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
	t.Run("Host key prompt confirm error returns error", func(t *testing.T) {
		confirmErr := errors.New("confirm failed")

		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_FingerprintPrompt{
				FingerprintPrompt: &authproto.TargetLoginFingerprintPrompt{
					Host:               "10.0.0.9",
					HostKeyFingerprint: "SHA256:abc123",
					HostKeyType:        "ssh-ed25519",
				},
			},
		}, nil).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			func(prompt string, fingerprint string) (bool, error) { return false, confirmErr },
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		assert.Equal(t, confirmErr, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
	t.Run("Host key acceptance send failure returns error", func(t *testing.T) {
		sendErr := errors.New("send acceptance failed")

		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_FingerprintPrompt{
				FingerprintPrompt: &authproto.TargetLoginFingerprintPrompt{
					Host:               "10.0.0.9",
					HostKeyFingerprint: "SHA256:abc123",
					HostKeyType:        "ssh-ed25519",
				},
			},
		}, nil).Once()
		stream.On("Send", mock.Anything).Return(sendErr).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			func(prompt string, fingerprint string) (bool, error) { return true, nil },
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(sendErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
	t.Run("Missing deps", func(t *testing.T) {
		client := &targetLoginService{}
		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		require.Error(t, err)
		expectedErr := message.New(message.CommonUnknownError).
			WithCause(errors.New("TargetLoginService is missing required dependencies"))
		assert.Equal(t, expectedErr, err)
	})
	t.Run("Auth connection failure", func(t *testing.T) {
		expectedErr := errors.New("connection failed")
		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return nil, nil, expectedErr
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			PromptFingerprint,
		)
		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		assert.Equal(t, err, expectedErr)
	})
	t.Run("TargetLogin call failure", func(t *testing.T) {
		streamErr := errors.New("stream error")
		stream := mocks.NewAuthTargetLoginClient(t)
		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, streamErr).Once()

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			PromptFingerprint,
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(streamErr)
		assert.Equal(t, expectedErr, err)

		authClient.AssertExpectations(t)
	})
	t.Run("Sending target details fails", func(t *testing.T) {
		sendErr := errors.New("send failed")
		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(sendErr).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			PromptFingerprint,
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(sendErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
	t.Run("Unexpected EOF from stream", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(nil, io.EOF).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			PromptFingerprint,
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(io.EOF)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
	t.Run("Unexpected server message type", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{}, nil).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			PromptFingerprint,
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		expectedErr := message.New(message.CommonUnknownError).
			WithCause(errors.New("unexpected message type received during target login"))
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
	t.Run("Prompt failure returns error", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_Prompt{
				Prompt: &authproto.TargetLoginPrompt{
					Type: authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD,
					Host: "1.2.3.4",
				},
			},
		}, nil).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		promptErr := errors.New("prompt failed")
		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, promptErr },
			PromptFingerprint,
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		expectedErr := message.New(message.CliServiceTargetloginPromptFailed).WithCause(promptErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
	t.Run("Credential send failure returns error", func(t *testing.T) {
		sendErr := errors.New("send creds failed")
		stream := mocks.NewAuthTargetLoginClient(t)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_Prompt{
				Prompt: &authproto.TargetLoginPrompt{
					Type: authproto.TargetLoginPromptType_TARGET_LOGIN_PROMPT_TYPE_PASSWORD,
					Host: "1.2.3.4",
				},
			},
		}, nil).Once()
		stream.On("Send", mock.Anything).Return(sendErr).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return []byte("secret"), nil },
			PromptFingerprint,
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		expectedErr := message.New(message.CliServiceTargetloginStreamFailure).WithCause(sendErr)
		assert.Equal(t, expectedErr, err)

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
	t.Run("Error status response returns error", func(t *testing.T) {
		stream := mocks.NewAuthTargetLoginClient(t)
		connectErr := errors.New("connect failed")
		errorChain := message.BuildErrorChain(connectErr)
		stream.On("Send", mock.Anything).Return(nil).Once()
		stream.On("Recv").Return(&authproto.TargetLoginServerMessage{
			Message: &authproto.TargetLoginServerMessage_Response{
				Response: &authproto.TargetLoginResponse{
					ReturnCode: apapproto.StatusCode_ERROR,
					Error:      errorChain,
				},
			},
		}, nil).Once()
		stream.On("CloseSend").Return(nil).Once()

		authClient := mocks.NewAuthClient(t)
		authClient.On("TargetLogin", mock.Anything).Return(stream, nil)

		client := NewTargetLoginService(
			func(host string, port int) (authproto.AuthClient, io.Closer, error) {
				return authClient, nil, nil
			},
			func(prompt string) ([]byte, error) { return nil, nil },
			PromptFingerprint,
		)

		err := client.LoginToTarget(context.Background(), &engine_target.SSHTarget{}, grpcserver.GrpcServerConfig{})
		assert.Equal(t, err.Error(), connectErr.Error())

		stream.AssertExpectations(t)
		authClient.AssertExpectations(t)
	})
}
