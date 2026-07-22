// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestGetVersion(t *testing.T) {
	service := VersionService{}

	t.Run("returns server version", func(t *testing.T) {
		mockClient := apapprotomocks.NewApapClient(t)
		mockClient.On(
			"GetVersion", context.Background(), &emptypb.Empty{},
		).Return(&apapproto.ServiceVersion{Version: "foo"}, nil)

		version, err := service.GetVersion(mockClient)

		assert.NoError(t, err)
		assert.Equal(t, "foo", version)
	})

	t.Run("returns error if get version fails", func(t *testing.T) {
		expectedError := errors.New("oh no")
		mockClient := apapprotomocks.NewApapClient(t)
		mockClient.On(
			"GetVersion", context.Background(), &emptypb.Empty{},
		).Return(nil, expectedError)

		_, err := service.GetVersion(mockClient)

		assert.ErrorIs(t, err, expectedError)
	})
}
