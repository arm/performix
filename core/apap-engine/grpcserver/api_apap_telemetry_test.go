// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
//
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestGetTelemetrySpecification(t *testing.T) {
	response, err := (&ApapServer{}).GetTelemetrySpecification(
		context.Background(),
		&apapproto.GetTelemetrySpecificationRequest{CpuName: "Neoverse-V3AE"},
	)

	require.NoError(t, err)
	require.NotNil(t, response.Specification)
	assert.Equal(t, "Neoverse-V3AE", response.Specification.CpuModel)
	assert.Contains(t, response.Specification.Json, "Telemetry Specification (PMU Events, Metrics and Methodology) for Neoverse V3 processor")
}

func TestGetTelemetrySpecificationReturnsEmptyResponseForUnsupportedCPU(t *testing.T) {
	response, err := (&ApapServer{}).GetTelemetrySpecification(
		context.Background(),
		&apapproto.GetTelemetrySpecificationRequest{CpuName: "Cobalt 100"},
	)

	require.NoError(t, err)
	assert.Nil(t, response.Specification)
}
