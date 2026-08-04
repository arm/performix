// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
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
}
