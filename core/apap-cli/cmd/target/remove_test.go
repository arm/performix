// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	servicemocks "github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestRemoveCommand(t *testing.T) {
	t.Run("no output on success", func(t *testing.T) {
		mtm := servicemocks.MockTargetManager{}
		mtm.On("RemoveTarget", testTargetName).Return(nil)

		buf := &bytes.Buffer{}
		cmd := newRemoveCommand(&mtm)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{testTargetName})

		_, err := cmd.ExecuteC()
		assert.NoError(t, err)
		assert.Empty(t, buf.String())
	})
	t.Run("empty struct output on success with --json", func(t *testing.T) {
		mtm := servicemocks.MockTargetManager{}
		mtm.On("RemoveTarget", testTargetName).Return(nil)

		cmdBuf := &bytes.Buffer{}
		cmd := newRemoveCommand(&mtm)
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{testTargetName, "--json"})

		_, err := cmd.ExecuteC()
		assert.NoError(t, err)
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), `"code":"0"`)
		assert.Contains(t, cmdBuf.String(), `"data":{}`)
		assert.Contains(t, cmdBuf.String(), `"error":{"message_code":""`)
		mtm.AssertExpectations(t)
	})
}

func TestRemoveCommandInvalidArguments(t *testing.T) {
	mtm := servicemocks.MockTargetManager{}
	cmd := newRemoveCommand(&mtm)
	cmd.SetArgs([]string{"target_name", "too_many_args"})

	_, err := cmd.ExecuteC()

	require.Error(t, err)

	expectedErr := "accepts at most 1 arg(s), received 2"
	assert.Equal(t, expectedErr, err.Error())
}

func TestRemoveCommandMissingTargetName(t *testing.T) {
	mtm := servicemocks.MockTargetManager{}
	cmd := newRemoveCommand(&mtm)
	cmd.SetArgs([]string{})

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	_, err := cmd.ExecuteC()
	expectedErr := message.New(message.CliCmdTargetRemoveNoTargetSpecified)
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestRemoveCommandValidArguments(t *testing.T) {
	mtm := servicemocks.MockTargetManager{}
	mtm.On("RemoveTarget", testTargetName).Return(nil)
	cmd := newRemoveCommand(&mtm)

	cmd.SetArgs([]string{testTargetName})
	_, err := cmd.ExecuteC()
	assert.NoError(t, err)
}

func TestRemoveCommandRemoveAllTargets(t *testing.T) {
	mtm := servicemocks.MockTargetManager{}
	mtm.On("RemoveAllTargets").Return(nil)
	cmd := newRemoveCommand(&mtm)

	cmd.SetArgs([]string{"--all"})
	_, err := cmd.ExecuteC()
	assert.NoError(t, err)
}
