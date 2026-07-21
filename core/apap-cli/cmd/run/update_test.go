// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func successfulRunUpdateResponse() *apapproto.UpdateRunsResponse {
	return &apapproto.UpdateRunsResponse{
		Statuses: []*apapproto.RunUpdateStatus{{Id: "run_id"}},
	}
}

func TestRunUpdateCommand(t *testing.T) {
	t.Run("Test no parameters supplied", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		cmd := NewUpdateCmd(cc, &mocks.MockUpdateService{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 1 arg(s), received 0")
	})

	t.Run("Test too many parameters supplied", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		cmd := NewUpdateCmd(cc, &mocks.MockUpdateService{})
		cmd.SetArgs([]string{"beep1", "beep2", "beep3"})
		err := cmd.Execute()
		assert.ErrorContains(t, err, "accepts 1 arg(s), received 3")
	})

	t.Run("Test run update success", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)
		mockService := &mocks.MockUpdateService{}
		mockService.On("UpdateRuns", mock.Anything, mock.Anything).Return(successfulRunUpdateResponse(), nil)
		cmd := NewUpdateCmd(cc, mockService)

		cmd.SetArgs([]string{"run_id", "--source", "path/to/source:/foo/bar:/baz"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})
}

func TestRunUpdateCommand_SourceFlagChanged(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	mockService := &mocks.MockUpdateService{}
	mockService.On("UpdateRuns", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			req := args.Get(1).(*apapproto.UpdateRunsRequest)
			require.Len(t, req.RunIds, 1)
			assert.Equal(t, "run_id", req.RunIds[0].Value)
			require.NotNil(t, req.Patch)
			require.Len(t, req.Patch.Operations, 1)

			operation, ok := req.Patch.Operations[0].Operation.(*apapproto.RunUpdateOperation_SetHostSourceCodePaths)
			require.True(t, ok)
			assert.Equal(t, []string{"foo", "bar"}, operation.SetHostSourceCodePaths.GetValue().GetPaths())
		}).
		Return(successfulRunUpdateResponse(), nil)

	cmd := NewUpdateCmd(cc, mockService)
	cmd.SetArgs([]string{"run_id", "--source", "foo" + string(os.PathListSeparator) + "bar"})
	err := cmd.Execute()
	assert.NoError(t, err)
}

func TestRunUpdateCommand_EmptyUpdateSkipsEngine(t *testing.T) {
	cc := &mocks.MockAutostartClientConnector{}
	mockService := &mocks.MockUpdateService{}
	cmd := NewUpdateCmd(cc, mockService)

	cmd.SetArgs([]string{"run_id"})
	err := cmd.Execute()

	assert.NoError(t, err)
	cc.AssertNotCalled(t, "ApapClient", mock.Anything)
	mockService.AssertNotCalled(t, "UpdateRuns", mock.Anything, mock.Anything)
}

func TestRunUpdateCommand_UpdateRunError(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	mockService := &mocks.MockUpdateService{}
	mockService.On("UpdateRuns", mock.Anything, mock.Anything).
		Return(&apapproto.UpdateRunsResponse{}, errors.New("update failed"))

	cmd := NewUpdateCmd(cc, mockService)
	cmd.SetArgs([]string{"run_id", "--source", "foo:bar"})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Equal(t, errors.New("update failed"), err)
}

func TestRunUpdateCommand_UpdateRunsStatusError(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	mockService := &mocks.MockUpdateService{}
	firstErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "first_run"})
	secondErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": "second_run"})
	mockService.On("UpdateRuns", mock.Anything, mock.Anything).
		Return(&apapproto.UpdateRunsResponse{
			Statuses: []*apapproto.RunUpdateStatus{
				{
					Id:    "first_run",
					Error: message.BuildErrorChain(firstErr),
				},
				{
					Id:    "second_run",
					Error: message.BuildErrorChain(secondErr),
				},
			},
		}, nil)

	cmd := NewUpdateCmd(cc, mockService)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"run_id", "--source", "foo:bar"})
	err := cmd.Execute()
	assert.Error(t, err)

	var msgErr message.Message
	require.ErrorAs(t, err, &msgErr)
	assert.Equal(t, message.CliCmdRunUpdateUpdateFailedMultipleRuns, msgErr.Code())
	assert.Equal(t, map[string]string{
		"numFailures": "2",
		"numToUpdate": "2",
		"failureIDs":  "`first_run`, `second_run`",
	}, msgErr.Metadata())
	assert.Contains(t, out.String(), "first_run:")
	assert.Contains(t, out.String(), "The run with the ID `first_run` does not exist.")
	assert.Contains(t, out.String(), "second_run:")
	assert.Contains(t, out.String(), "The run with ID `second_run` is currently busy and cannot be modified.")
}

func TestRunUpdateCommand_UpdateRunsJSONStatusError(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	mockService := &mocks.MockUpdateService{}
	updateErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "this_run_doesnt_exist"})
	mockService.On("UpdateRuns", mock.Anything, mock.Anything).
		Return(&apapproto.UpdateRunsResponse{
			Statuses: []*apapproto.RunUpdateStatus{
				{
					Id:    "this_run_doesnt_exist",
					Error: message.BuildErrorChain(updateErr),
				},
			},
		}, nil)

	cmd := NewUpdateCmd(cc, mockService)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	t.Cleanup(func() { viper.Set("json", false) })
	viper.Set("json", true)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"this_run_doesnt_exist", "--source", "foo:bar"})
	err := cmd.Execute()
	require.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
	assert.ErrorIs(t, err, updateErr)
	assert.True(t, utils.IsValidJSON(out.String()), out.String())
	assert.Contains(t, out.String(), `"code":"-1"`)
	assert.Contains(t, out.String(), `"message_code":"engine.run.DOES_NOT_EXIST"`)
}
