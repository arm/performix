// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/clients/go/healthproto"
)

type healthPacksServiceMock struct {
	mock.Mock
}

func (sps *healthPacksServiceMock) IsCacheReady() bool {
	args := sps.Called()
	return args.Get(0).(bool)
}

func TestHealthCheckServer(t *testing.T) {
	t.Run("health probe returns SERVING when cache is ready", func(t *testing.T) {
		packsServiceMock := &healthPacksServiceMock{}
		packsServiceMock.On("IsCacheReady").Return(true)

		server := HealthServer{}

		response, err := server.Check(context.Background(), &healthproto.HealthCheckRequest{})

		require.NoError(t, err)
		assert.Equal(t, healthproto.HealthCheckResponse_SERVING, response.GetStatus())
	})
}
