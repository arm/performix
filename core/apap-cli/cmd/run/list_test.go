// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestListCommand(t *testing.T) {
	t.Run("error is raised when invalid arguments specified", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		lister := mocks.MockLister{}

		cmd := NewListCmd(cc, &lister)
		cmd.SetArgs([]string{"abcdef123456"})

		_, err := cmd.ExecuteC()

		require.Error(t, err)

		expectedErr := "accepts 0 arg(s), received 1"
		assert.Equal(t, expectedErr, err.Error())
	})

	t.Run("entire run directory is listed correctly when called with no arguments", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		lister := mocks.MockLister{}
		expectedData := clijson.CLIRunListing{Runs: []clijson.CLIRunDescription{
			{ID: "abcdef123456", Name: "entry1", StartTime: "2024-10-13T16:40:01.789Z", EndTime: "2024-10-13T16:41:01.789Z", RecipeName: "cpu_hotspot", RunResult: "in_progress", Target: clijson.CLITarget{}},
			{ID: "fedcba654321", Name: "entry2", StartTime: "2024-10-14T10:06:12.345Z", EndTime: "2024-10-14T10:05:12.345Z", RecipeName: "cpu_microarchitecture", RunResult: "success", Target: clijson.CLITarget{}},
			{ID: "a0b1c2d3e4f5", Name: "entry3", StartTime: "2024-09-22T01:54:13.444Z", EndTime: "2024-09-22T01:55:13.444Z", RecipeName: "instruction_mix", RunResult: "failure", Target: clijson.CLITarget{}},
		}}
		lister.On("ListRuns", client).Return(expectedData, nil)

		cmd := NewListCmd(cc, &lister)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "3 runs found")
	})

	t.Run("no runs are displayed when called with no arguments", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		expectedData := clijson.CLIRunListing{}
		lister := mocks.MockLister{}
		lister.On("ListRuns", client).Return(expectedData, nil)

		cmd := NewListCmd(cc, &lister)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), "0 runs found")
	})

	t.Run("returns error when client connector fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, expectedError)

		cmd := NewListCmd(cc, &mocks.MockLister{})
		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		lister := &mocks.MockLister{}
		lister.On("ListRuns", client).Return(clijson.CLIRunListing{}, expectedError)

		cmd := NewListCmd(cc, lister)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})
}
