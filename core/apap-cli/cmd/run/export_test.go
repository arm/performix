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

func TestRunExportCommand(t *testing.T) {
	t.Run("Test no parameters supplied", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		cmd := NewExportCmd(cc, &mocks.MockExporter{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 2 arg(s), received 0")
	})

	t.Run("Export success when correct number of parameters provided", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		mockExporter := &mocks.MockExporter{}
		mockExporter.On("ExportRun", mock.Anything, mock.Anything).Return(&apapproto.RunExportResponse{ReturnCode: apapproto.StatusCode_SUCCESS.Enum()}, nil)
		cmd := NewExportCmd(cc, mockExporter)
		cmd.SetArgs([]string{"0101010101", "bah/hum/bug"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("Export fails when the service returns an error", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		mockExporter := &mocks.MockExporter{}
		mockExporter.On("ExportRun", mock.Anything, mock.Anything).Return(&apapproto.RunExportResponse{ReturnCode: apapproto.StatusCode_ERROR.Enum()}, errors.New("rekt"))
		cmd := NewExportCmd(cc, mockExporter)
		cmd.SetArgs([]string{"0101010101", "bah/hum/bug"})
		err := cmd.Execute()
		assert.Equal(t, errors.New("rekt"), err)
	})
}
