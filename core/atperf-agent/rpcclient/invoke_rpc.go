// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package rpcclient

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

// formatResponse marshals a proto.Message to indented JSON string
func formatResponse(msg proto.Message) (string, error) {
	opts := protojson.MarshalOptions{
		Multiline:       true, // enable multi-line output
		Indent:          "  ", // two-space indentation
		EmitUnpopulated: true, // include fields with zero values (e.g. false, 0, "")
	}
	outputString, err := opts.Marshal(msg)
	return string(outputString), err
}

// ConcreteClientSupplier establishes the connection to the grpc server on localhost.
// Returns a TargetAgentClient and the connection.
func ConcreteClientSupplier(port int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
	// Dial the server
	conn, err := grpc.NewClient(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect: %v", err)
	}
	client := targetagentproto.NewTargetAgentClient(conn)
	return client, conn, nil
}

// InvokeRPC takes an RPCHandler, port, and clientSupplier, then runs the Invoke method of the rpcHandler.
// Returns the output from the gRPC server.
func InvokeRPC(rpcHandler *RPCHandler, port int, clientSupplier ClientSupplier, args json.RawMessage) (string, error) {
	client, conn, err := clientSupplier(port)
	if err != nil {
		return "", err
	}
	if conn != nil {
		defer conn.Close()
	}

	respMsg, err := rpcHandler.Invoke(context.Background(), client, args)
	if err != nil {
		return "", fmt.Errorf("RPC error: %v", err)
	}
	out, err := formatResponse(respMsg)
	if err != nil {
		return "", fmt.Errorf("JSON marshal error: %v", err)
	}
	return out, nil
}

type ClientSupplier func(int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error)
