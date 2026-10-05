// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	targetservice "github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestSearchTargetProcessesTool(t *testing.T) {
	t.Run("advertises non-destructive hint", func(t *testing.T) {
		ctx := context.Background()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{}, SearchTargetProcessesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		assert.Equal(t, "search_target_processes", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].Annotations)
		require.NotNil(t, tools.Tools[0].Annotations.DestructiveHint)
		assert.False(t, *tools.Tools[0].Annotations.DestructiveHint)
	})

	t.Run("returns processes", func(t *testing.T) {
		ctx := context.Background()
		tgt := newTestTarget("10.0.0.1")
		targets := &targetservice.MockTargetManager{}
		targets.On("GetTarget", "remote").Return(tgt, nil).Once()

		engine := apapprotomocks.NewApapClient(t)
		engine.On("TargetPrepare", mock.Anything, mock.MatchedBy(func(request *apapproto.TargetPrepareRequest) bool {
			return proto.Equal(request.GetTarget(), grpcserver.TargetToProto(tgt)) &&
				request.GetDeploymentType() == apapproto.ToolDeploy_AUTO
		})).Return(&apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, nil).Once()
		engine.On("TargetInfoCollector", mock.Anything, mock.MatchedBy(func(request *apapproto.TargetInfoRequest) bool {
			return proto.Equal(request.GetTarget(), grpcserver.TargetToProto(tgt)) &&
				len(request.GetCollectors()) == 1 && request.GetCollectors()[0] == targetProcessesCollectorName
		})).Return(targetProcessesResponse(
			listedTargetProcess{PID: 123, Name: "worker", Username: "alice", CommandLine: "/opt/worker --serve"},
			listedTargetProcess{PID: 456, Name: "helper", Username: "bob", CommandLine: "/opt/helper"},
		), nil).Once()

		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchTargetProcessesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_target_processes",
			Arguments: map[string]any{"target": "remote"},
		})

		require.NoError(t, err)
		require.False(t, result.IsError)
		content := decodeSearchTargetProcessesResult(t, result)
		assert.Equal(t, []listedTargetProcess{
			{PID: 123, Name: "worker", Username: "alice", CommandLine: "/opt/worker --serve"},
			{PID: 456, Name: "helper", Username: "bob", CommandLine: "/opt/helper"},
		}, content.Processes)
		assert.Equal(t, 2, content.TotalMatches)
		assert.False(t, content.ResultsTruncated)
		assert.Nil(t, content.Error)
	})

	t.Run("returns preparation and collection errors", func(t *testing.T) {
		engineErr := message.New(message.EngineTargetSessionConnectionNotEstablished)
		for _, test := range []struct {
			name            string
			prepareResponse *apapproto.TargetPrepareResponse
			prepareError    error
			collect         bool
			expectedMessage string
		}{
			{name: "target preparation", prepareError: engineErr},
			{name: "process collection", prepareResponse: &apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOYED}, collect: true},
			{
				name:            "incomplete automatic target preparation",
				prepareResponse: &apapproto.TargetPrepareResponse{Result: apapproto.TargetPrepareResult_DEPLOY},
				expectedMessage: "the process search cannot continue because automatic target preparation failed to deploy all mandatory tools",
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				ctx := context.Background()
				tgt := newTestTarget("10.0.0.1")
				targets := &targetservice.MockTargetManager{}
				targets.On("GetTarget", "remote").Return(tgt, nil).Once()

				engine := apapprotomocks.NewApapClient(t)
				engine.On("TargetPrepare", mock.Anything, mock.Anything).
					Return(test.prepareResponse, test.prepareError).Once()
				if test.collect {
					engine.On("TargetInfoCollector", mock.Anything, mock.Anything).
						Return((*apapproto.TargetInfoResponse)(nil), engineErr).Once()
				}
				clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchTargetProcessesTool{}.Register)
				defer clientSession.Close()
				defer serverSession.Close()

				result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      "search_target_processes",
					Arguments: map[string]any{"target": "remote"},
				})

				require.NoError(t, err)
				require.True(t, result.IsError)
				content := decodeSearchTargetProcessesResult(t, result)
				require.NotNil(t, content.Error)
				if test.expectedMessage == "" {
					assert.Equal(t, message.EngineTargetSessionConnectionNotEstablished, content.Error.Code)
				} else {
					assert.Equal(t, test.expectedMessage, content.Error.Message)
				}
				if !test.collect {
					engine.AssertNotCalled(t, "TargetInfoCollector", mock.Anything, mock.Anything)
				}
			})
		}
	})

	t.Run("returns invalid target error", func(t *testing.T) {
		ctx := context.Background()
		targets := &targetservice.MockTargetManager{}
		targets.On("GetTarget", "missing").Return(newTestTarget("unused"), errors.New("target not found")).Once()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine, Targets: targets}, SearchTargetProcessesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_target_processes",
			Arguments: map[string]any{"target": "missing"},
		})

		require.NoError(t, err)
		require.True(t, result.IsError)
		content := decodeSearchTargetProcessesResult(t, result)
		assert.NotNil(t, content.Processes)
		assert.Empty(t, content.Processes)
		assert.Zero(t, content.TotalMatches)
		assert.False(t, content.ResultsTruncated)
		require.NotNil(t, content.Error)
		assert.Contains(t, content.Error.Message, "target not found")
	})

}

func TestSearchTargetProcessesResultFromProto(t *testing.T) {
	filteredInput := []listedTargetProcess{
		{PID: 100, Name: "worker", CommandLine: "/opt/worker --queue CRITICAL"},
		{PID: 200, Name: "worker", CommandLine: "/opt/worker --queue bulk"},
		{PID: 300, Name: "CriticalSupervisor", CommandLine: "/opt/supervisor"},
	}
	cappedInput := make([]listedTargetProcess, 0, maxTargetProcessResults+1)
	for pid := maxTargetProcessResults + 1; pid > 0; pid-- {
		cappedInput = append(cappedInput, listedTargetProcess{PID: uint32(pid), Name: "worker"})
	}
	cappedExpected := make([]listedTargetProcess, 0, maxTargetProcessResults)
	for pid := 1; pid <= maxTargetProcessResults; pid++ {
		cappedExpected = append(cappedExpected, listedTargetProcess{PID: uint32(pid), Name: "worker"})
	}

	for _, test := range []struct {
		name              string
		input             searchTargetProcessesInput
		processes         []listedTargetProcess
		expected          []listedTargetProcess
		expectedTotal     int
		expectedTruncated bool
	}{
		{
			name:          "filters name and command line case-insensitively",
			input:         searchTargetProcessesInput{Contains: "critical"},
			processes:     filteredInput,
			expected:      []listedTargetProcess{filteredInput[0], filteredInput[2]},
			expectedTotal: 2,
		},
		{
			name:              "sorts and caps results",
			processes:         cappedInput,
			expected:          cappedExpected,
			expectedTotal:     maxTargetProcessResults + 1,
			expectedTruncated: true,
		},
		{
			name:     "returns an empty non-nil result",
			expected: []listedTargetProcess{},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := searchTargetProcessesResultFromProto(targetProcessesResponse(test.processes...), test.input)

			assert.Equal(t, test.expected, result.Processes)
			assert.Equal(t, test.expectedTotal, result.TotalMatches)
			assert.Equal(t, test.expectedTruncated, result.ResultsTruncated)
		})
	}
}

func targetProcessesResponse(processes ...listedTargetProcess) *apapproto.TargetInfoResponse {
	protoProcesses := make([]*apapproto.TargetInfoPIDs, 0, len(processes))
	for _, process := range processes {
		protoProcesses = append(protoProcesses, &apapproto.TargetInfoPIDs{
			Pid:         proto.Uint32(process.PID),
			Name:        proto.String(process.Name),
			Username:    proto.String(process.Username),
			CommandLine: proto.String(process.CommandLine),
		})
	}

	return &apapproto.TargetInfoResponse{Info: map[string]*apapproto.TargetInfo{
		targetProcessesCollectorName: {
			Info: &apapproto.TargetInfo_Pids{Pids: &apapproto.TargetInfoPIDResponse{Process: protoProcesses}},
		},
	}}
}

func decodeSearchTargetProcessesResult(t *testing.T, result *mcp.CallToolResult) searchTargetProcessesResult {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var content searchTargetProcessesResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
	return content
}
