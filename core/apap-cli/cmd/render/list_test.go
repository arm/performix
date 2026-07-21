// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestListCommand(t *testing.T) {
	t.Run("returns an error if arguments are provided", func(t *testing.T) {
		cc := &mocks.MockAutostartClientConnector{}

		cmd := newListCmd(cc)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"fake_arg1"})

		_, err := cmd.ExecuteC()
		assert.Error(t, err)

		expectedErr := "accepts 0 arg(s), received 1"
		assert.Equal(t, expectedErr, err.Error())
	})
	t.Run("reports 0 sessions correctly", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		sessions := []*apapproto.SessionInfo{}
		response := &apapproto.RenderListing{Sessions: sessions}

		client.On("ListRenders", context.Background(), &emptypb.Empty{}).Return(response, nil)

		cmd := newListCmd(cc)
		utils.SetPersistentFlags(cmd)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		_, err := cmd.ExecuteC()
		assert.Nil(t, err)
		assert.Equal(t, "0 render sessions found\n", cmdBuf.String())
	})
	t.Run("reports sessions correctly", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		sessions := []*apapproto.SessionInfo{
			{
				SessionId: "id1",
				DbKey:     "key-b",
			},
			{
				SessionId: "id2",
				DbKey:     "key-a",
			},
			{
				SessionId: "id4",
				DbKey:     "key-b",
			},
			{
				SessionId: "id3",
				DbKey:     "key-a",
			},
		}
		response := &apapproto.RenderListing{
			Sessions: sessions,
			DbInstances: []*apapproto.DbInstanceInfo{
				{DbKey: "key-b", MemoryUsageGib: 2.1},
				{DbKey: "key-a", MemoryUsageGib: 1.1},
			},
		}

		client.On("ListRenders", context.Background(), &emptypb.Empty{}).Return(response, nil)

		cmd := newListCmd(cc)
		utils.SetPersistentFlags(cmd)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		_, err := cmd.ExecuteC()
		assert.Nil(t, err)
		output := cmdBuf.String()

		// Verify header presence
		lines := strings.Split(output, "\n")
		var headerPresent bool
		for _, line := range lines {
			if strings.Contains(line, "SESSION ID") && strings.Contains(line, "DB KEY") {
				headerPresent = true
				break
			}
		}
		assert.True(t, headerPresent)
		// Ensuring sessions are in correct order: key-a (id2, id3), key-b (id1, id4)
		assert.Less(t, strings.Index(output, "id2"), strings.Index(output, "id3"))
		assert.Less(t, strings.Index(output, "id3"), strings.Index(output, "id1"))
		assert.Less(t, strings.Index(output, "id1"), strings.Index(output, "id4"))

		var dbHeaderPresent bool
		for _, line := range lines {
			if strings.Contains(line, "DB KEY") && strings.Contains(line, "MEMORY USAGE (GiB)") {
				dbHeaderPresent = true
				break
			}
		}
		assert.True(t, dbHeaderPresent)
	})
	t.Run("returns an error if the client connector fails", func(t *testing.T) {
		testError := "This is a test error!"
		cc := &mocks.MockAutostartClientConnector{}
		cc.On("ApapClient", mock.Anything).Return(apapprotomocks.NewApapClient(t), errors.New(testError))

		cmd := newListCmd(cc)
		utils.SetPersistentFlags(cmd)
		_, err := cmd.ExecuteC()
		assert.Error(t, err)

		assert.ErrorContains(t, err, testError)
	})
}
