// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func (s *ApapServer) GetTelemetrySpecification(
	_ context.Context,
	request *apapproto.GetTelemetrySpecificationRequest,
) (*apapproto.GetTelemetrySpecificationResponse, error) {
	specification, ok := telemetry.GetSpecification(request.GetCpuName())
	if !ok {
		return &apapproto.GetTelemetrySpecificationResponse{}, nil
	}

	return &apapproto.GetTelemetrySpecificationResponse{
		Specification: &apapproto.TelemetrySpecification{
			CpuModel: specification.CPUModel,
			Json:     specification.JSON,
		},
	}, nil
}
