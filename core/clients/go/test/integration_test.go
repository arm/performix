// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const (
	address = "localhost:9000"
)

func getVersion(client apapproto.ApapClient) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	response, err := client.GetVersion(ctx, &emptypb.Empty{})
	if err != nil {
		log.Errorf("Could not get version: %v", err)
		return "", err
	}

	return response.GetVersion(), nil
}

// An integration test which connects to a running instance of the gRPC server,
// makes an RPC to get the server version, then makes an RPC to request shutdown of
// the server.
func TestIntegration(t *testing.T) {
	credentials := insecure.NewCredentials()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials))
	assert.NoError(t, err, "Failed to create client")
	defer conn.Close()

	apapClient := apapproto.NewApapClient(conn)

	// Semver regex from https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
	version, err := getVersion(apapClient)
	assert.NoError(t, err, "Failed to get version")
	regex := `^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`
	assert.Regexp(t, regex, version)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	_, err = apapClient.Shutdown(shutdownCtx, &emptypb.Empty{})
	assert.NoError(t, err, "Failed to shutdown server")

	time.Sleep(time.Second)

	_, err = getVersion(apapClient)
	assert.Error(t, err)
}
