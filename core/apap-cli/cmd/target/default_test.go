// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
)

type mockDefaultSetter struct {
	mock.Mock
}

func (md *mockDefaultSetter) SetDefaultTarget(targetName string) error {
	return md.Called(targetName).Error(0)
}

func TestDefaultTargetCmd(t *testing.T) {
	t.Run("no arg produces an error", func(t *testing.T) {
		cmd := newTargetDefaultTestCmd(&mockDefaultSetter{})
		cmd.SetArgs(nil)
		err := cmd.Execute()
		require.Error(t, err)
	})

	t.Run("valid arg produces no error", func(t *testing.T) {
		mockDefaultSetter := mockDefaultSetter{}
		mockDefaultSetter.On("SetDefaultTarget", "knownTarget").Return(nil)
		cmd := newTargetDefaultTestCmd(&mockDefaultSetter)
		cmd.SetArgs([]string{"knownTarget"})
		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("unknown target creates error", func(t *testing.T) {
		mockDefaultSetter := mockDefaultSetter{}
		mockDefaultSetter.On("SetDefaultTarget", "unknownTarget").Return(errors.New("rekt"))
		cmd := newTargetDefaultTestCmd(&mockDefaultSetter)
		cmd.SetArgs([]string{"unknownTarget"})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("empty struct output on success with --json", func(t *testing.T) {
		mockDefaultSetter := mockDefaultSetter{}
		mockDefaultSetter.On("SetDefaultTarget", "knownTarget").Return(nil)
		cmd := newTargetDefaultTestCmd(&mockDefaultSetter)
		cmd.SetArgs([]string{"knownTarget", "--json"})
		utils.SetPersistentFlags(cmd)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		require.NoError(t, err)

		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
		assert.Contains(t, cmdBuf.String(), `"code":"0"`)
		assert.Contains(t, cmdBuf.String(), `"data":{}`)
		assert.Contains(t, cmdBuf.String(), `"error":{"message_code":""`)
	})
}
