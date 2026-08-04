// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cmdmocks "github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestCollect_TextOutput(t *testing.T) {
	viper.Set("json", false)
	t.Cleanup(func() { viper.Set("json", false) })

	client := apapprotomocks.NewApapClient(t)
	connectorMock := &cmdmocks.MockAutostartClientConnector{}
	connectorMock.SetApapClient(client, nil)

	client.On("CreateSupportPackage", mock.Anything, mock.MatchedBy(func(req *apapproto.CreateSupportPackageRequest) bool {
		runIDs := req.GetRunIds()
		require.Len(t, runIDs, 2)
		require.Equal(t, "run-123", runIDs[0].GetValue())
		require.Equal(t, "run-456", runIDs[1].GetValue())
		require.Equal(t, "/tmp/out", req.GetOutputDirectory())
		require.NotEmpty(t, req.GetCliVersion())
		require.Equal(t, uint32(1), req.GetLogCount())
		return true
	})).Return(&apapproto.CreateSupportPackageResponse{
		PackagePath:      "/tmp/out/support_pkg_20240220_12-00-00.zip",
		PackageSizeBytes: 2048,
	}, nil).Once()

	buf := &bytes.Buffer{}
	params := &collectCLIParams{
		runIDs:    []string{"run-123", "run-456"},
		OutputDir: "/tmp/out",
		LogCount:  1,
	}
	err := collect(context.Background(), buf, connectorMock, params)
	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineSupportCollectSuccess, msg.Code())
	expectedMsg := message.New(message.EngineSupportCollectSuccess).WithMetadata(map[string]string{
		"path":        "/tmp/out/support_pkg_20240220_12-00-00.zip",
		"sizeBytes":   "2048",
		"sizeDisplay": util.FormatBytesIEC(2048),
	})
	require.Equal(t, expectedMsg.Metadata(), msg.Metadata())
	require.Empty(t, buf.String())
	client.AssertExpectations(t)
}

func TestCollect_JSONOutput(t *testing.T) {
	viper.Set("json", true)
	t.Cleanup(func() { viper.Set("json", false) })

	client := apapprotomocks.NewApapClient(t)
	connectorMock := &cmdmocks.MockAutostartClientConnector{}
	connectorMock.SetApapClient(client, nil)

	client.On("CreateSupportPackage", mock.Anything, mock.AnythingOfType("*apapproto.CreateSupportPackageRequest")).
		Return(&apapproto.CreateSupportPackageResponse{
			PackagePath:      "/tmp/out/support_pkg_20240220_12-00-00.zip",
			PackageSizeBytes: 512,
		}, nil).Once()

	buf := &bytes.Buffer{}
	params := &collectCLIParams{
		OutputDir: "",
		LogCount:  1,
	}
	err := collect(context.Background(), buf, connectorMock, params)
	require.NoError(t, err)

	var resp clijson.CliJSONResponse[collectJSON]
	require.NoError(t, json.Unmarshal(buf.Bytes(), &resp))
	require.Equal(t, "/tmp/out/support_pkg_20240220_12-00-00.zip", resp.Data.PackagePath)
	require.Equal(t, uint64(512), resp.Data.PackageSizeBytes)
	require.Equal(t, "512 B", resp.Data.PackageSizeDisplay)
	client.AssertExpectations(t)
}

func TestCollect_CanceledContextBeforeRPCReturnsUserCancellation(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	connectorMock := &cmdmocks.MockAutostartClientConnector{}
	connectorMock.SetApapClient(client, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := collect(ctx, &bytes.Buffer{}, connectorMock, &collectCLIParams{LogCount: 1})

	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineCommonUserCanceled, msg.Code())
	client.AssertNotCalled(t, "CreateSupportPackage", mock.Anything, mock.Anything)
	connectorMock.AssertExpectations(t)
}

func TestCollect_CanceledContextAfterConnectReturnsUserCancellation(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	connectorMock := &cmdmocks.MockAutostartClientConnector{}

	ctx, cancel := context.WithCancel(context.Background())
	connectorMock.On("ApapClient", mock.Anything).Run(func(args mock.Arguments) {
		cancel()
	}).Return(client, nil).Once()

	err := collect(ctx, &bytes.Buffer{}, connectorMock, &collectCLIParams{LogCount: 1})

	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineCommonUserCanceled, msg.Code())
	client.AssertNotCalled(t, "CreateSupportPackage", mock.Anything, mock.Anything)
	connectorMock.AssertExpectations(t)
}

func TestCollect_CreateSupportPackageCanceledReturnsUserCancellation(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	connectorMock := &cmdmocks.MockAutostartClientConnector{}
	connectorMock.SetClient(client, nil)

	client.
		On("CreateSupportPackage", mock.Anything, mock.AnythingOfType("*apapproto.CreateSupportPackageRequest")).
		Return((*apapproto.CreateSupportPackageResponse)(nil), context.Canceled).
		Once()

	err := collect(context.Background(), &bytes.Buffer{}, connectorMock, &collectCLIParams{LogCount: 1})

	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineCommonUserCanceled, msg.Code())
	client.AssertExpectations(t)
}

func TestNewCollectCmd_DefaultFlagValues(t *testing.T) {
	cmd := NewCollectCmd(client.NewAutostartClient())

	outputFlag := cmd.Flags().Lookup("output-dir")
	require.NotNil(t, outputFlag)
	require.Equal(t, defaultOutputDir, outputFlag.Value.String())
	require.Equal(t, defaultOutputDir, outputFlag.DefValue)

	logCountFlag := cmd.Flags().Lookup("log-count")
	require.NotNil(t, logCountFlag)
	require.Equal(t, fmt.Sprintf("%d", defaultLogCount), logCountFlag.Value.String())
	require.Equal(t, fmt.Sprintf("%d", defaultLogCount), logCountFlag.DefValue)
}

func TestNewCollectCmd_FlagPropagation(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	connectorMock := &cmdmocks.MockAutostartClientConnector{}
	connectorMock.SetClient(client, nil)

	client.
		On("CreateSupportPackage", mock.Anything, mock.MatchedBy(func(req *apapproto.CreateSupportPackageRequest) bool {
			runIDs := req.GetRunIds()
			require.Len(t, runIDs, 1)
			require.Equal(t, "run-001", runIDs[0].GetValue())
			require.Equal(t, "/tmp/outdir", req.GetOutputDirectory())
			require.Equal(t, uint32(3), req.GetLogCount())
			return true
		})).
		Return(&apapproto.CreateSupportPackageResponse{
			PackagePath:      "/tmp/outdir/support_pkg_20260101_00-00-00.zip",
			PackageSizeBytes: 1024,
		}, nil).
		Once()

	cmd := NewCollectCmd(connectorMock)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	cmd.SetArgs([]string{"--output-dir", "/tmp/outdir", "--log-count", "3", "run-001"})
	err := cmd.Execute()
	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineSupportCollectSuccess, msg.Code())
	client.AssertExpectations(t)
}

func TestCollectCmd_InvalidRunID(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	connectorMock := &cmdmocks.MockAutostartClientConnector{}
	connectorMock.SetClient(client, nil)

	runID := "missing-run"
	client.
		On("CreateSupportPackage", mock.Anything, mock.AnythingOfType("*apapproto.CreateSupportPackageRequest")).
		Return((*apapproto.CreateSupportPackageResponse)(nil), message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID})).
		Once()

	cmd := NewCollectCmd(connectorMock)
	cmd.SetArgs([]string{runID})

	err := cmd.Execute()
	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineRunDoesNotExist, msg.Code())
	client.AssertExpectations(t)
}
