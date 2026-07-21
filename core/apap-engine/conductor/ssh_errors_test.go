// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"errors"
	"fmt"
	maps0 "maps"
	"net"
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type TimeoutError struct {
}

func (t *TimeoutError) Error() string {
	return "Timeout error!"
}

func (t *TimeoutError) Timeout() bool {
	return true
}

func TestGetErrType(t *testing.T) {
	t.Run("correctly sorts various errors", func(t *testing.T) {
		testCases := []struct {
			Err             error
			ExpectedErrType SSHErrorType
		}{
			{
				Err:             message.New(message.EngineConductorSshAcceptNewKeyMismatch),
				ExpectedErrType: KnownHostsErr,
			},
			{
				Err:             &net.OpError{Err: syscall.ENETUNREACH},
				ExpectedErrType: NetErr,
			},
			{
				Err:             errors.New("connect: network is unreachable"),
				ExpectedErrType: NetErr,
			},
			{
				Err:             &net.DNSError{},
				ExpectedErrType: DNSErr,
			},
			{
				Err:             &net.OpError{Err: context.DeadlineExceeded},
				ExpectedErrType: TimeoutErr,
			},
			{
				Err:             &net.OpError{Err: &TimeoutError{}},
				ExpectedErrType: TimeoutErr,
			},
			{
				Err:             &net.OpError{Err: errors.New("this is an error")},
				ExpectedErrType: UnknownErr,
			},
			{
				Err:             &ssh.AlgorithmNegotiationError{},
				ExpectedErrType: AlgsErr,
			},
			{
				Err:             errors.New("ssh: unable to authenticate"),
				ExpectedErrType: AuthErr,
			},
			{
				Err:             errors.New("who knows what this is"),
				ExpectedErrType: UnknownErr,
			},
		}

		for _, test := range testCases {
			result := getErrType(test.Err)
			assert.Equal(t, test.ExpectedErrType, result)
		}
	})
}

func TestAuthFailureMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name string
		Err  error
		Want string
	}{
		{
			Name: "nil error returns unknown",
			Err:  nil,
			Want: authFailureUnknown,
		},
		{
			Name: "message password codes return password",
			Err:  message.New(message.EngineConductorSshPasswordIncorrect),
			Want: authFailurePassword,
		},
		{
			Name: "message key codes return key",
			Err:  message.New(message.EngineConductorSshKeyFileIncorrectPassphraseForTarget),
			Want: authFailureKey,
		},
		{
			Name: "string with password but not publickey returns password",
			Err:  errors.New("ssh: unable to authenticate; password auth failed"),
			Want: authFailurePassword,
		},
		{
			Name: "string with keyboard-interactive but not publickey returns password",
			Err:  errors.New("ssh: unable to authenticate, attempted methods [keyboard-interactive]"),
			Want: authFailurePassword,
		},
		{
			Name: "string with publickey but not password returns key",
			Err:  errors.New("ssh: unable to authenticate using publickey"),
			Want: authFailureKey,
		},
		{
			Name: "string with both methods returns unknown",
			Err:  errors.New("ssh: failed with password and publickey methods"),
			Want: authFailureUnknown,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.Name, func(t *testing.T) {
			got := authFailureMethod(tt.Err)
			assert.Equal(t, tt.Want, got)
		})
	}
}

func TestConsolidateDirectErrs(t *testing.T) {
	t.Run("only uses the last error", func(t *testing.T) {
		cause := &net.DNSError{Err: "this is a DNS error!"}
		errs := []error{
			errors.New("unknown error"),
			&net.OpError{},
			message.New(message.EngineConductorSshAcceptNewKeyMismatch),
			&net.OpError{},
			&net.OpError{},
			cause,
		}
		result := consolidateDirectErrs(errs, "fakeHost", 22)

		expectedMetadata := map[string]string{
			"hostName": "fakeHost",
		}
		expectedErr := message.New(message.EngineConductorSshDirectConnFailedDns).WithCause(cause).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("returns nil if provided errors slice is empty", func(t *testing.T) {
		errs := []error{}
		result := consolidateDirectErrs(errs, "", 0)
		assert.Nil(t, result)
	})
}

func TestConsolidateViaJumpHostErrs(t *testing.T) {
	t.Run("only uses the last error", func(t *testing.T) {
		cause := errors.New("ssh: unable to authenticate, attempted methods [publickey]")
		errs := []error{
			message.New(message.EngineConductorSshAcceptNewKeyMismatch),
			&net.DNSError{},
			&net.DNSError{},
			&net.DNSError{},
			cause,
		}
		result := consolidateJumpErrs(errs, "prevAddr", "fakeHost", 22)

		expectedMetadata := map[string]string{
			"prevHost": "prevAddr",
			"nextHost": "fakeHost",
			"hostName": "fakeHost",
		}
		expectedErr := message.New(message.EngineConductorSshJumpConnFailedKeyAuth).WithCause(cause).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, result)
		assert.NoError(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("returns nil if provided errors slice is empty", func(t *testing.T) {
		errs := []error{}
		result := consolidateJumpErrs(errs, "", "", 0)
		assert.Nil(t, result)
	})
}

func TestConsolidateErrorsCorrectness(t *testing.T) {
	testCases := []struct {
		Name             string
		Err              error
		Host             string
		Port             int32
		PrevHost         string
		ExpectedMetadata map[string]string
		ExpectedMsgCode  message.MessageCode
	}{
		{
			Name:             "network error",
			Err:              errors.New("connect: network is unreachable"),
			Host:             "myHost",
			Port:             123,
			ExpectedMetadata: map[string]string{"hostName": "myHost"},
			ExpectedMsgCode:  message.EngineConductorSshDirectConnFailedNet,
		},
		{
			Name:     "dns error",
			Err:      &net.DNSError{Err: "this is an error!"},
			Host:     "nextHost",
			Port:     3,
			PrevHost: "prevHost",
			ExpectedMetadata: map[string]string{
				"nextHost": "nextHost",
				"prevHost": "prevHost",
			},
			ExpectedMsgCode: message.EngineConductorSshJumpConnFailedDns,
		},
		{
			Name:     "timeout error",
			Err:      &net.OpError{Err: context.DeadlineExceeded},
			Host:     "fakeHost",
			Port:     2022,
			PrevHost: "prevAddr",
			ExpectedMetadata: map[string]string{
				"nextHost":    "fakeHost",
				"prevHost":    "prevAddr",
				"nextAddress": "fakeHost:2022",
				"nextPort":    "2022",
			},
			ExpectedMsgCode: message.EngineConductorSshJumpConnFailedTimeout,
		},
		{
			Name: "timeout error 2",
			Err:  &net.OpError{Err: context.DeadlineExceeded},
			Host: "fakeHost",
			Port: 2022,
			ExpectedMetadata: map[string]string{
				"hostName": "fakeHost",
				"address":  "fakeHost:2022",
				"portNum":  "2022",
			},
			ExpectedMsgCode: message.EngineConductorSshDirectConnFailedTimeout,
		},
		{
			Name: "algs error",
			Err: &ssh.AlgorithmNegotiationError{
				RequestedAlgorithms: []string{"targetAlg1", "anotherAlg"},
				SupportedAlgorithms: []string{"weirdLocalAlg"},
			},
			Host:     "someHost",
			Port:     2022,
			PrevHost: "myHost",
			ExpectedMetadata: map[string]string{
				"prevHost":     "myHost",
				"nextHost":     "someHost",
				"prevHostAlgs": "`weirdLocalAlg`",
				"nextHostAlgs": "`targetAlg1`, `anotherAlg`",
			},
			ExpectedMsgCode: message.EngineConductorSshJumpConnFailedAlgs,
		},
		{
			Name:             "auth error (key)",
			Err:              errors.New("ssh: unable to authenticate, attempted methods [publickey]"),
			Host:             "someHost",
			Port:             2022,
			ExpectedMetadata: map[string]string{"hostName": "someHost"},
			ExpectedMsgCode:  message.EngineConductorSshDirectConnFailedKeyAuth,
		},
		{
			Name:             "auth error (password)",
			Err:              errors.New("ssh: unable to authenticate, attempted methods [password]"),
			Host:             "someHost",
			Port:             2022,
			ExpectedMetadata: map[string]string{"hostName": "someHost"},
			ExpectedMsgCode:  message.EngineConductorSshDirectConnFailedPasswordAuth,
		},
		{
			Name:             "auth error jump (key)",
			Err:              errors.New("ssh: unable to authenticate, attempted methods [publickey]"),
			Host:             "someHost",
			Port:             2022,
			PrevHost:         "prevHost",
			ExpectedMetadata: map[string]string{"nextHost": "someHost", "prevHost": "prevHost", "hostName": "someHost"},
			ExpectedMsgCode:  message.EngineConductorSshJumpConnFailedKeyAuth,
		},
		{
			Name:             "auth error jump (password)",
			Err:              errors.New("ssh: unable to authenticate, attempted methods [password]"),
			Host:             "someHost",
			Port:             2022,
			PrevHost:         "prevHost",
			ExpectedMetadata: map[string]string{"nextHost": "someHost", "prevHost": "prevHost", "hostName": "someHost"},
			ExpectedMsgCode:  message.EngineConductorSshJumpConnFailedPasswordAuth,
		},
		{
			Name:             "unknown error",
			Err:              &exec.Error{Err: errors.New("an error!")},
			Host:             "10.10.100.1000",
			Port:             56,
			ExpectedMetadata: map[string]string{"hostName": "10.10.100.1000"},
			ExpectedMsgCode:  message.EngineConductorSshDirectConnFailedUnknown,
		},
	}
	for _, testCase := range testCases {
		t.Run(fmt.Sprintf("test consolidate errors correctness for %v", testCase.Name), func(t *testing.T) {
			var result message.Message
			if testCase.PrevHost == "" {
				result = consolidateDirectErrs([]error{testCase.Err}, testCase.Host, testCase.Port)
			} else {
				result = consolidateJumpErrs([]error{testCase.Err}, testCase.PrevHost, testCase.Host, testCase.Port)
			}

			metadata := maps0.Clone(map[string]string(testCase.ExpectedMetadata))
			if metadata == nil {
				metadata = map[string]string{}
			}
			if testCase.PrevHost != "" {
				metadata["hostName"] = testCase.Host
			}
			expectedErr := message.New(testCase.ExpectedMsgCode).WithCause(testCase.Err).WithMetadata(metadata)
			assert.Equal(t, expectedErr, result)
			assert.Nil(t, message.ValidateMetadataPlaceholders(result))
		})
	}
}

func TestHandleFindKeysDirectErr(t *testing.T) {
	t.Run("returns a wrapper error containing the underlying message if the error cause indicates key detection should stop", func(t *testing.T) {
		algsError := ssh.AlgorithmNegotiationError{
			RequestedAlgorithms: []string{"nextAlg1", "nextAlg2"},
			SupportedAlgorithms: []string{"prevAlg1", "prevAlg2"},
		}
		metadata := map[string]string{
			"hostName":  "myHost123",
			"hostAlgs":  "`nextAlg1`, `nextAlg2`",
			"localAlgs": "`prevAlg1`, `prevAlg2`",
		}
		msg := message.New(message.EngineConductorSshDirectConnFailedAlgs).WithCause(&algsError).WithMetadata(metadata)

		result := handleFindKeysDirectErr(msg, "myHost123", 456)

		expectedMetadata2 := map[string]string{
			"hostName": "myHost123",
			"portNum":  "456",
			"address":  "myHost123:456",
		}
		expectedErr := message.New(message.EngineConductorSshFindKeysFailedDirect).WithCause(msg).WithMetadata(expectedMetadata2)

		assert.Equal(t, expectedErr, result)
		assert.Nil(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("returns nil if the error does not indicate key detection should stop", func(t *testing.T) {
		authError := message.New(message.EngineConductorSshDirectConnFailedKeyAuth).WithCause(errors.New("ssh: unable to authenticate"))
		result := handleFindKeysDirectErr(authError, "myHost123", 456)
		assert.Nil(t, result)
	})
}

func TestHandleFindKeysJumpErr(t *testing.T) {
	t.Run("returns a wrapper error containing the underlying message if the error cause indicates key detection should stop", func(t *testing.T) {
		dnsError := net.DNSError{Err: "this is an error!"}
		metadata := map[string]string{
			"nextHost": "nextHost123",
			"prevHost": "prevHost123",
		}
		msg := message.New(message.EngineConductorSshJumpConnFailedDns).WithCause(&dnsError).WithMetadata(metadata)

		result := handleFindKeysJumpErr(msg, "prevHost123", "nextHost123", 456)

		expectedMetadata2 := map[string]string{
			"nextHost":    "nextHost123",
			"prevHost":    "prevHost123",
			"nextAddress": "nextHost123:456",
			"nextPort":    "456",
		}
		expectedErr := message.New(message.EngineConductorSshFindKeysFailedJump).WithCause(msg).WithMetadata(expectedMetadata2)

		assert.Equal(t, expectedErr, result)
		assert.Nil(t, message.ValidateMetadataPlaceholders(result))
	})
	t.Run("returns nil if the error does not indicate key detection should stop", func(t *testing.T) {
		authError := errors.New("ssh: unable to authenticate")
		result := handleFindKeysJumpErr(authError, "prevHost123", "nextHost123", 456)
		assert.Nil(t, result)
	})
}
