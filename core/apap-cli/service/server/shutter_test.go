// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/pidfiles"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestShutdown(t *testing.T) {
	service := shutter{}

	t.Run("returns error if grpc request fails", func(t *testing.T) {
		expectedErr := errors.New("🤷")
		client := apapprotomocks.NewApapClient(t)

		client.On(
			"Shutdown", context.Background(), &emptypb.Empty{},
		).Return(&emptypb.Empty{}, expectedErr)

		err := service.Shutdown(client)

		assert.Error(t, err)
	})

	t.Run("returns no error if grpc request succeeds", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)

		client.On(
			"Shutdown", context.Background(), &emptypb.Empty{},
		).Return(&emptypb.Empty{}, nil)

		err := service.Shutdown(client)

		assert.NoError(t, err)
	})

	t.Run("returns ENOENT when killing fake client", func(t *testing.T) {
		path, err := pidfiles.ConstructPidFilePath("1.2.3.4", 22)
		assert.NoError(t, err)
		if err != nil {
			return
		}
		dir := filepath.Dir(path)
		err = os.MkdirAll(dir, 0777)
		assert.NoError(t, err)
		if err != nil {
			return
		}

		client := apapprotomocks.NewApapClient(t)

		client.On(
			"Shutdown", context.Background(), &emptypb.Empty{},
		).Return(&emptypb.Empty{}, nil)

		err = service.Kill("1.2.3.4", 22)
		assert.Equal(t, err.(*os.PathError).Err, syscall.ENOENT)

		// Need to correctly shutdown
		_ = service.Shutdown(client)
	})
}
