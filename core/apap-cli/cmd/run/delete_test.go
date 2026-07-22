// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func buildDeleter(ids []string, errs []error, client apapproto.ApapClient, t *testing.T) run.Deleter {
	assert.Equal(t, len(ids), len(errs))
	request := &apapproto.DeleteRunsRequest{Ids: ids}
	statuses := make([]*apapproto.RunDeletionStatus, len(ids))
	for i, id := range ids {
		statuses[i] = &apapproto.RunDeletionStatus{
			Id:    id,
			Error: message.BuildErrorChain(errs[i]),
		}
	}
	response := &apapproto.DeleteRunsResponse{Statuses: statuses}

	deleter := mocks.MockDeleter{}
	deleter.On("DeleteRuns", client, request).Return(response, nil)

	return &deleter
}

func TestDeleteCommand(t *testing.T) {
	t.Run("error is raised when invalid arguments specified", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		deleter := mocks.MockDeleter{}

		cmd := NewDeleteCmd(cc, &deleter)
		cmd.SetArgs([]string{})

		_, err := cmd.ExecuteC()

		require.Error(t, err)
		expectedErr := "requires at least 1 arg(s), only received 0"
		assert.Equal(t, expectedErr, err.Error())
	})
	t.Run("successfully deletes run when valid arguments specified", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		id := "abcdef123456"
		request := &apapproto.DeleteRunsRequest{Ids: []string{id}}
		response := &apapproto.DeleteRunsResponse{Statuses: []*apapproto.RunDeletionStatus{{Id: id, Error: nil}}}

		deleter := mocks.MockDeleter{}
		deleter.On("DeleteRuns", client, request).Return(response, nil)

		cmd := NewDeleteCmd(cc, &deleter)
		cmd.SetArgs([]string{"abcdef123456"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Empty(t, cmdBuf.String(), "Expected no output for successful deletion")
	})
	t.Run("returns error when client connector fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, expectedError)

		cmd := NewDeleteCmd(cc, &mocks.MockDeleter{})
		cmd.SetArgs([]string{"fake_entry_to_delete"})
		err := cmd.Execute()

		assert.Equal(t, expectedError, err)
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		id := "fake_entry_to_delete"
		request := &apapproto.DeleteRunsRequest{Ids: []string{id}}
		response := &apapproto.DeleteRunsResponse{}

		deleter := &mocks.MockDeleter{}
		deleter.On("DeleteRuns", client, request).Return(response, expectedError)

		cmd := NewDeleteCmd(cc, deleter)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"fake_entry_to_delete"})
		err := cmd.Execute()

		assert.Equal(t, expectedError, err)
	})
	t.Run("returns error when run fails to be deleted", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		ids := []string{"a"}
		errs := []error{message.New(message.EngineRunDoesNotExist)}
		deleter := buildDeleter(ids, errs, client, t)

		cmd := NewDeleteCmd(cc, deleter)
		cmd.SetArgs([]string{"a"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		err := cmd.Execute()

		assert.True(t, errors.Is(err, message.New(message.EngineRunDoesNotExist)))
	})
	t.Run("fails when both ids and all flag set", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		deleter := mocks.MockDeleter{}

		cmd := NewDeleteCmd(cc, &deleter)
		cmd.SetArgs([]string{"abc", "--all"})

		err := cmd.Execute()
		expectedErr := message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(map[string]string{
			"flag1": "--all",
			"flag2": "run IDs",
		})
		assert.Equal(t, expectedErr, err)
	})
	t.Run("aborts when all flag not confirmed", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		deleter := mocks.MockDeleter{}
		cmd := NewDeleteCmd(cc, &deleter)

		prevPrompter := deleteAllPrompter
		deleteAllPrompter = func(prompt string) (string, error) { return "no", nil }
		defer func() { deleteAllPrompter = prevPrompter }()

		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"--all"})
		utils.SetPersistentFlags(cmd)

		err := cmd.Execute()
		assert.NoError(t, err)
		assert.Empty(t, cmdBuf.String())
	})
	t.Run("deletes all when confirmed", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		request := &apapproto.DeleteRunsRequest{Ids: []string{}, DeleteAll: true}
		response := &apapproto.DeleteRunsResponse{}

		deleter := mocks.MockDeleter{}
		deleter.On("DeleteRuns", client, request).Return(response, nil)

		cmd := NewDeleteCmd(cc, &deleter)

		prevPrompter := deleteAllPrompter
		deleteAllPrompter = func(prompt string) (string, error) { return "yes", nil }
		defer func() { deleteAllPrompter = prevPrompter }()

		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"--all"})
		utils.SetPersistentFlags(cmd)

		err := cmd.Execute()
		assert.NoError(t, err)
		assert.Empty(t, cmdBuf.String())
		deleter.AssertExpectations(t)
	})
}

func TestMultiRunDeletion(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)
	t.Run("no output on successful deletion of all requested runs", func(t *testing.T) {
		ids := []string{"a", "d", "b", "c"}
		errs := []error{nil, nil, nil, nil}
		deleter := buildDeleter(ids, errs, client, t)

		cmd := NewDeleteCmd(cc, deleter)
		cmd.SetArgs([]string{"a", "d", "b", "c"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Empty(t, cmdBuf.String(), "Expected no output for successful deletion")
	})
	t.Run("JSON output on successful deletion of all requested runs is correct", func(t *testing.T) {
		ids := []string{"a", "d", "b", "c"}
		errs := []error{nil, nil, nil, nil}
		deleter := buildDeleter(ids, errs, client, t)

		cmd := NewDeleteCmd(cc, deleter)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"a", "d", "b", "c", "--json"})

		err := cmd.Execute()
		assert.NoError(t, err)

		out := cmdBuf.String()
		assert.True(t, utils.IsValidJSON(out))
		assert.Contains(t, out, `{"code":"0","error":{"message_code":""`)
		assert.Contains(t, out, `"data":{"statuses":[{"id":"a","error":null`)
	})
	t.Run("correct output when some runs failed to delete", func(t *testing.T) {
		ids := []string{"a", "d", "b", "c"}
		errs := []error{nil, message.New(message.EngineRunDoesNotExist), message.New(message.EngineConductorSshDirectConnFailedAlgs), nil}
		deleter := buildDeleter(ids, errs, client, t)

		cmd := NewDeleteCmd(cc, deleter)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"a", "d", "b", "c"})

		err := cmd.Execute()
		expectedMetadata := map[string]string{
			"numFailures":  "2",
			"numToDelete":  "4",
			"numSuccesses": "2",
			"failureIDs":   "`d`, `b`",
		}
		expectedErr := message.New(message.CliCmdRunDeleteDeletionFailedPluralSuccesses).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)

		out := cmdBuf.String()
		assert.Contains(t, out, "d:\n  [Error]: The run with the ID")
		assert.Contains(t, out, "  [Code]: engine.run.DOES_NOT_EXIST")
		assert.Contains(t, out, fmt.Sprintf("b:\n  [Error]: %v cannot connect to", terminology.GetProductFullName()))
		assert.Contains(t, out, "  [Code]: engine.conductor.ssh.DIRECT_CONN_FAILED_ALGS")
	})
	t.Run("correct output when all requested runs failed to delete", func(t *testing.T) {
		ids := []string{"a", "d", "b"}
		errs := []error{message.New(message.EngineAgentTargetInfoProbeErrorCode),
			message.New(message.EngineRunDoesNotExist),
			message.New(message.EngineConductorSshDirectConnFailedAlgs)}
		deleter := buildDeleter(ids, errs, client, t)

		cmd := NewDeleteCmd(cc, deleter)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"a", "d", "b", "--json"})

		err := cmd.Execute()
		assert.NoError(t, err)

		out := cmdBuf.String()
		assert.True(t, utils.IsValidJSON(out))
		assert.Contains(t, out, `{"code":"-1","error":{"message_code":"cli.cmd.run.delete.DELETION_FAILED_NO_SUCCESSES"`)
		assert.Contains(t, out, `"data":{"statuses":[{"id":"a","error":{"message_code":"engine.agent.target_info.PROBE_ERROR_CODE"`)
	})
	t.Run("correct output when all but one run failed to delete", func(t *testing.T) {
		ids := []string{"a", "d", "b"}
		errs := []error{nil, message.New(message.EngineRunDoesNotExist), message.New(message.EngineConductorSshDirectConnFailedAlgs)}
		deleter := buildDeleter(ids, errs, client, t)

		cmd := NewDeleteCmd(cc, deleter)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"a", "d", "b"})

		err := cmd.Execute()
		expectedMetadata := map[string]string{
			"numFailures": "2",
			"numToDelete": "3",
			"failureIDs":  "`d`, `b`",
		}
		expectedErr := message.New(message.CliCmdRunDeleteDeletionFailedSingleSuccess).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)

		out := cmdBuf.String()
		assert.Contains(t, out, "d:\n  [Error]: The run with the ID")
		assert.Contains(t, out, "  [Code]: engine.run.DOES_NOT_EXIST")
		assert.Contains(t, out, fmt.Sprintf("b:\n  [Error]: %v cannot connect to", terminology.GetProductFullName()))
		assert.Contains(t, out, "  [Code]: engine.conductor.ssh.DIRECT_CONN_FAILED_ALGS")
	})
}
