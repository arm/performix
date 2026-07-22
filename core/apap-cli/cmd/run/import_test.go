// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestRunImportCommand(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	t.Run("Test no parameters supplied", func(t *testing.T) {
		cmd := NewImportCmd(cc, &mocks.MockImporter{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 1 arg(s), received 0")
	})

	t.Run("Import succeeds when correct number of parameters supplied", func(t *testing.T) {
		mockImp := &mocks.MockImporter{}
		mockImp.On("ImportRun", mock.Anything, mock.Anything).Return(&apapproto.RunImportResponse{ReturnCode: apapproto.StatusCode_SUCCESS.Enum(), NewId: &apapproto.RunId{Value: "ABCD"}}, nil)
		cmd := NewImportCmd(cc, mockImp)
		cmd.SetArgs([]string{"valid/path/ABCD.zip"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("Import fails when the service returns an error", func(t *testing.T) {
		mockImp := &mocks.MockImporter{}
		mockImp.On("ImportRun", mock.Anything, mock.Anything).Return(&apapproto.RunImportResponse{ReturnCode: apapproto.StatusCode_ERROR.Enum(), NewId: &apapproto.RunId{}}, errors.New("rekt"))
		cmd := NewImportCmd(cc, mockImp)
		cmd.SetArgs([]string{"error/path"})
		err := cmd.Execute()
		assert.Equal(t, errors.New("rekt"), err)
	})
}
