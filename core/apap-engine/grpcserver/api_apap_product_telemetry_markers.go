// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/producttelemetry"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

func (s *ApapServer) SetProductTelemetryMarkers(_ context.Context, markers *apapproto.ProductTelemetryUserClassification) (*emptypb.Empty, error) {
	directory, err := producttelemetry.MarkerDirectory()
	if err == nil {
		err = producttelemetry.Mark(directory, markers.IsArmInternal, markers.IsCoreDev)
	}
	if err != nil {
		return nil, message.New(message.EngineProductTelemetryMarkersAccessFailed).WithCause(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ApapServer) GetProductTelemetryUserClassification(context.Context, *emptypb.Empty) (*apapproto.ProductTelemetryUserClassification, error) {
	directory, err := producttelemetry.MarkerDirectory()
	if err != nil {
		return nil, message.New(message.EngineProductTelemetryMarkersAccessFailed).WithCause(err)
	}
	armInternal, coreDev := producttelemetry.Classify(directory, time.Now())
	return &apapproto.ProductTelemetryUserClassification{IsArmInternal: armInternal, IsCoreDev: coreDev}, nil
}
