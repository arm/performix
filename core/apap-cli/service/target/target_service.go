// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type ConcreteTargetTester struct {
}

type ConcreteTargetInfoCollector struct {
}

type TargetTestConnection struct {
	ConnectionStatus apapproto.ConnectionStatus `json:"status"`
	Error            error                      `json:"error"`
}
type TestTargetResponse struct {
	ConnectionStatus TargetTestConnection `json:"connection"`
}

func (dt *ConcreteTargetTester) TestTarget(ctx context.Context, client apapproto.ApapClient, target engine_target.Target) (TestTargetResponse, error) {
	targetProto := grpcserver.TargetToProto(target)

	targetTestReq := &apapproto.TargetTestRequest{Target: targetProto}

	targetTestResponse, err := client.TargetTest(ctx, targetTestReq)
	if err != nil {
		return TestTargetResponse{}, err
	}

	connection := targetTestResponse.Connection
	connectionErr := message.ReconstructFromChain(connection.Error)

	response := TestTargetResponse{
		ConnectionStatus: TargetTestConnection{connection.Status, connectionErr},
	}
	return response, nil
}

func (c *ConcreteTargetInfoCollector) CollectTargetInfo(client apapproto.ApapClient, target engine_target.Target, collectors []string) (*apapproto.TargetInfoResponse, error) {
	targetProto := grpcserver.TargetToProto(target)

	targetInfoCollectReq := &apapproto.TargetInfoRequest{Target: targetProto, Collectors: collectors}

	targetInfoCollectResponse, err := client.TargetInfoCollector(context.Background(), targetInfoCollectReq)
	if err != nil {
		return &apapproto.TargetInfoResponse{}, err
	}

	return targetInfoCollectResponse, nil
}

// TargetManagerService For now just an alias to the engine version
type TargetManagerService engine_target.TargetManagerService
