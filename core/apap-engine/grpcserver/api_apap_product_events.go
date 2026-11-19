// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/producttelemetry"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

func productEvent(request *apapproto.ProductTelemetryEvent) producttelemetry.Event {
	return producttelemetry.Event{
		Name: request.GetName(), Properties: request.GetProperties().AsMap(),
	}
}

func (s *ApapServer) SendTelemetryEvent(ctx context.Context, request *apapproto.ProductTelemetryEvent) (*emptypb.Empty, error) {
	if s.productTelemetry == nil {
		return nil, message.New(message.EngineProductTelemetryIdentityAccessFailed)
	}
	if err := s.productTelemetry.SendTelemetryEvent(ctx, productEvent(request)); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *ApapServer) SendFeedbackEvent(ctx context.Context, request *apapproto.ProductTelemetryEvent) (*emptypb.Empty, error) {
	if s.productTelemetry == nil {
		return nil, message.New(message.EngineProductTelemetryIdentityAccessFailed)
	}
	if err := s.productTelemetry.SendFeedbackEvent(ctx, productEvent(request)); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
