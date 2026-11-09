// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

func (s *ApapServer) GetProductTelemetryPreference(context.Context, *emptypb.Empty) (*apapproto.ProductTelemetryPreference, error) {
	enabled, configured := s.config.GetTelemetryPreference()
	return &apapproto.ProductTelemetryPreference{
		Enabled:    enabled,
		Configured: configured,
	}, nil
}

func (s *ApapServer) SetProductTelemetryPreference(_ context.Context, preference *apapproto.ProductTelemetryPreference) (*emptypb.Empty, error) {
	if err := s.config.SetTelemetryPreference(preference.Enabled); err != nil {
		return nil, message.New(message.EngineProductTelemetryPreferenceUpdateFailed).WithCause(err)
	}
	return &emptypb.Empty{}, nil
}
