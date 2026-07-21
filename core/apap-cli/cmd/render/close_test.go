// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestCloseCommand_Fails(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	rc := &mocks.MockRenderCloser{}
	cc.SetApapClient(client, nil)

	cmd := newCloseCmd(cc, rc)

	rc.On("CloseRender", mock.Anything, mock.Anything).Return(fmt.Errorf("some reason"))

	t.Run("non-existing session ID", func(t *testing.T) {
		cmd.SetArgs([]string{"i-dont-exist"})
		_, err := cmd.ExecuteC()
		require.Error(t, err)
		assert.Contains(t, err.Error(), clierror.Render.Close.CloseRenderFailed)
	})
}

func TestCloseCommand_Succeeds(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	rc := &mocks.MockRenderCloser{}
	cc.SetApapClient(client, nil)

	cmd := newCloseCmd(cc, rc)

	rc.On("CloseRender", mock.Anything, mock.Anything).Return(nil)

	t.Run("existing session ID is given (CLI)", func(t *testing.T) {
		cmd.SetArgs([]string{"i-exist"})
		_, err := cmd.ExecuteC()
		assert.NoError(t, err)
	})

	t.Run("existing session ID is given (JSON)", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		utils.SetPersistentFlags(cmd)

		cmd.SetArgs([]string{"i-exist", "--json"})
		_, err := cmd.ExecuteC()
		require.NoError(t, err)
		assert.True(t, utils.IsValidJSON(cmdBuf.String()))
	})
}
