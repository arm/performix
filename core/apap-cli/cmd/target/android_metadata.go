// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"context"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

// withAndroidAddressPersistence records only addresses verified by the engine.
// Other RPCs and target types retain their existing client behavior.
func withAndroidAddressPersistence(client apapproto.ApapClient, name string, tgt engine_target.Target, manager target.TargetManagerService) apapproto.ApapClient {
	android, ok := tgt.(*engine_target.AndroidTarget)
	if !ok {
		return client
	}
	return &androidAddressClient{ApapClient: client, name: name, target: android, manager: manager}
}

type androidAddressClient struct {
	apapproto.ApapClient
	name    string
	target  *engine_target.AndroidTarget
	manager target.TargetManagerService
}

func (c *androidAddressClient) TargetTest(ctx context.Context, request *apapproto.TargetTestRequest, opts ...grpc.CallOption) (*apapproto.TargetTestResponse, error) {
	var trailers metadata.MD
	response, err := c.ApapClient.TargetTest(ctx, request, append(opts, grpc.Trailer(&trailers))...)
	return response, c.recordAddress(trailers, err)
}

func (c *androidAddressClient) TargetPrepare(ctx context.Context, request *apapproto.TargetPrepareRequest, opts ...grpc.CallOption) (*apapproto.TargetPrepareResponse, error) {
	var trailers metadata.MD
	response, err := c.ApapClient.TargetPrepare(ctx, request, append(opts, grpc.Trailer(&trailers))...)
	return response, c.recordAddress(trailers, err)
}

func (c *androidAddressClient) recordAddress(trailers metadata.MD, callErr error) error {
	addresses := trailers.Get(grpcserver.ResolvedADBAddress)
	if len(addresses) == 0 {
		return callErr
	}
	address := addresses[len(addresses)-1]
	matched, err := c.manager.UpdateAndroidTargetLastKnownADBAddress(c.name, c.target.SerialNumber, address)
	if err != nil {
		log.Warnf("Unable to persist the resolved Android ADB address for target %q: %v", c.name, err)
		return callErr
	}
	if !matched {
		log.Warnf("Skipped persisting the resolved Android ADB address because target %q changed during the RPC", c.name)
		return callErr
	}
	c.target.LastKnownADBAddress = &address
	return callErr
}
