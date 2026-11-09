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

func (s *ApapServer) GetOrCreateProductTelemetryUserId(context.Context, *emptypb.Empty) (*apapproto.ProductTelemetryUserId, error) {
	directory, err := producttelemetry.MarkerDirectory()
	var id string
	if err == nil {
		id, err = producttelemetry.GetOrCreateUserID(directory)
	}
	if err != nil {
		return nil, message.New(message.EngineProductTelemetryIdentityAccessFailed).WithCause(err)
	}
	return &apapproto.ProductTelemetryUserId{Value: id}, nil
}
