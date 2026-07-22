// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestRunRenameCommand(t *testing.T) {
	t.Run("Test no parameters supplied", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		cmd := NewRenameCmd(cc, &mocks.MockRenameService{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 2 arg(s), received 0")
	})

	t.Run("Test too many parameters supplied", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		cmd := NewRenameCmd(cc, &mocks.MockRenameService{})
		cmd.SetArgs([]string{"beep1", "beep2", "beep3"})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 2 arg(s), received 3")
	})

	t.Run("Test run rename success", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		mockService := &mocks.MockRenameService{}
		mockService.On("RenameRun", mock.Anything, mock.Anything, "beep").Return(&apapproto.RunRenameResponse{ReturnCode: apapproto.StatusCode_SUCCESS}, nil)
		cmd := NewRenameCmd(cc, mockService)

		cmd.SetArgs([]string{"run_id", "beep"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("Test run rename failure", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		mockService := &mocks.MockRenameService{}
		mockService.On("RenameRun", mock.Anything, mock.Anything, "beep").Return(&apapproto.RunRenameResponse{ReturnCode: apapproto.StatusCode_ERROR}, errors.New("rekt"))
		cmd := NewRenameCmd(cc, mockService)

		cmd.SetArgs([]string{"run_id", "beep"})
		err := cmd.Execute()
		assert.Equal(t, errors.New("rekt"), err)
	})

	t.Run("Test run rename empty new name", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		mockService := &mocks.MockRenameService{}
		mockService.On("RenameRun", mock.Anything, mock.Anything, "beep").Return(&apapproto.RunRenameResponse{ReturnCode: apapproto.StatusCode_ERROR}, errors.New("rekt"))
		cmd := NewRenameCmd(cc, mockService)

		cmd.SetArgs([]string{"run_id", ""})
		err := cmd.Execute()
		assert.Error(t, message.New(message.CliCmdRunRenameEmptyName), err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
