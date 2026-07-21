// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// ClientExample creates a connection to the gRPC server and then constructs a
// client with the connection. It then makes a call to GetVersion and prints
// the result to stdout.
//
// Before running this code make sure the apap-engine gRPC server
// is running at specified address.
func ClientExample() error {
	address := "localhost:9000"

	credentials := insecure.NewCredentials()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials))
	if err != nil {
		return err
	}
	defer conn.Close()

	apapClient := apapproto.NewApapClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	response, err := apapClient.GetVersion(ctx, &emptypb.Empty{})
	if err != nil {
		return err
	}

	log.Printf("Version: %s", response.GetVersion())

	return nil
}
