// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/targetagentproto"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
)

func TestAndroidSuPrivilegeProofConversion(t *testing.T) {
	proof := &targetagentproto.PrivilegeProof{
		Mech: &targetagentproto.PrivilegeProof_AndroidSu{AndroidSu: true},
	}

	mech, err := PrivilegeProofProtoToMech(proof)
	assert.NoError(t, err)
	assert.Equal(t, privilege.AndroidSu, mech)
	assert.True(t, MechToPrivilegeProofProto(privilege.AndroidSu).GetAndroidSu())
}

func TestBuildLaunchCommand(t *testing.T) {
	t.Run("launch parameters are required", func(t *testing.T) {
		_, err := buildLaunchCommand(nil)

		expectedErr := message.New(message.AgentApiMissingLaunchRequest)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("command is required", func(t *testing.T) {
		_, err := buildLaunchCommand(&targetagentproto.StartProcessRequest{})

		expectedErr := message.New(message.AgentApiMissingCommand)
		assert.Equal(t, expectedErr, err)
	})
	t.Run("executable is required", func(t *testing.T) {
		_, err := buildLaunchCommand(&targetagentproto.StartProcessRequest{Command: []string{""}})

		expectedErr := message.New(message.AgentApiMissingCommand)
		assert.Equal(t, expectedErr, err)
	})

	invalidRequests := []struct {
		name    string
		request *targetagentproto.StartProcessRequest
	}{
		{name: "NUL executable", request: &targetagentproto.StartProcessRequest{Command: []string{"bad\x00program"}}},
		{name: "NUL argument", request: &targetagentproto.StartProcessRequest{Command: []string{"program", "bad\x00argument"}}},
		{name: "empty environment name", request: &targetagentproto.StartProcessRequest{Command: []string{"program"}, Environment: map[string]string{"": "value"}}},
		{name: "equals in environment name", request: &targetagentproto.StartProcessRequest{Command: []string{"program"}, Environment: map[string]string{"A=B": "value"}}},
		{name: "NUL environment name", request: &targetagentproto.StartProcessRequest{Command: []string{"program"}, Environment: map[string]string{"BAD\x00NAME": "value"}}},
		{name: "NUL environment value", request: &targetagentproto.StartProcessRequest{Command: []string{"program"}, Environment: map[string]string{"NAME": "bad\x00value"}}},
		{name: "NUL working directory", request: &targetagentproto.StartProcessRequest{Command: []string{"program"}, WorkingDirectory: "bad\x00directory"}},
	}
	for _, tt := range invalidRequests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildLaunchCommand(tt.request)
			var messageError *message.MessageImpl
			require.True(t, errors.As(err, &messageError))
			assert.Equal(t, message.AgentApiInvalidLaunchRequest, messageError.Code())
		})
	}
}
