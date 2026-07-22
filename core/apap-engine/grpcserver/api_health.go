// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"

	"github.com/Arm-Debug/apap-cli/clients/go/healthproto"
)

type HealthServer struct {
	healthproto.UnimplementedHealthServer
}

func NewHealthServer() *HealthServer {
	return &HealthServer{}
}

func (s *HealthServer) Check(ctx context.Context, in *healthproto.HealthCheckRequest) (*healthproto.HealthCheckResponse, error) {
	return &healthproto.HealthCheckResponse{Status: healthproto.HealthCheckResponse_SERVING}, nil
}
