// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package rpcclient

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"

	"github.com/Arm-Debug/apap-cli/atperf-agent/grpcserver"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
	targetagentproto "github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

func TestConcreteClientSupplier(t *testing.T) {
	// Create a grpc server on a port
	port := 5678

	mpw := &grpcserver.MockPortWriter{}
	mpw.On("Write", port).Return(nil).Once()
	mpw.On("Remove").Return(nil)

	server, err := grpcserver.NewServer(grpcserver.GrpcServerConfig{
		TransportConfig: grpcserver.TCPTransportConfig{Port: port},
		LogBuffer:       grpcserver.NewLogBuffer(512),
		PortWriter:      mpw,
	})
	assert.NoError(t, err)
	errChan := server.Start()

	// Wait for server to start properly
	time.Sleep(50 * time.Millisecond)

	// Establish client connection to the grpc server
	client, conn, err := ConcreteClientSupplier(port)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	assert.NotNil(t, conn)

	// Close client connection
	err = conn.Close()
	assert.NoError(t, err)

	// Stop the grpc server
	server.Stop(false)

	select {
	case serverErr := <-errChan:
		assert.NoError(t, serverErr)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for server to stop")
	}

	mpw.AssertExpectations(t)
}

func TestInvokeRPC(t *testing.T) {
	registry := NewRegistry()
	args := json.RawMessage("{}")
	handler, err := registry.NewRPCHandler("GetVersion")
	assert.NoError(t, err)
	assert.Equal(t, handler.Name, "GetVersion")
	t.Run("InvokeRpc succeeds with ConcreteClientSupplierr", func(t *testing.T) {
		// Create a grpc server
		port := 5678

		mpw := &grpcserver.MockPortWriter{}
		mpw.On("Write", port).Return(nil).Once()
		mpw.On("Remove").Return(nil)

		server, err := grpcserver.NewServer(grpcserver.GrpcServerConfig{
			TransportConfig: grpcserver.TCPTransportConfig{Port: port},
			LogBuffer:       grpcserver.NewLogBuffer(512),
			PortWriter:      mpw,
		})
		assert.NoError(t, err)
		errChan := server.Start()

		// Wait for server to start properly
		time.Sleep(50 * time.Millisecond)
		out, err := InvokeRPC(handler, port, ConcreteClientSupplier, args)
		assert.NoError(t, err)
		expected := fmt.Sprintf(`{"version":"%s"}`, versions.GetVersion())
		assert.JSONEq(t, expected, out)

		// Stop the grpc server
		server.Stop(false)
		select {
		case serverErr := <-errChan:
			assert.NoError(t, serverErr)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for server to stop")
		}

		mpw.AssertExpectations(t)
	})
	t.Run("InvokeRpc fails when ClientSupplier errors", func(t *testing.T) {
		clientSupplier := func(port int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return nil, nil, fmt.Errorf("fake error")
		}
		out, err := InvokeRPC(handler, -1, clientSupplier, args)
		assert.EqualError(t, err, "fake error")
		assert.Empty(t, out)
	})
	t.Run("InvokeRpc fails when ClientSupplier returns an error", func(t *testing.T) {
		clientSupplier := func(port int) (targetagentproto.TargetAgentClient, *grpc.ClientConn, error) {
			return nil, nil, fmt.Errorf("fake error")
		}
		out, err := InvokeRPC(handler, -1, clientSupplier, args)
		assert.EqualError(t, err, "fake error")
		assert.Empty(t, out)
	})
}
