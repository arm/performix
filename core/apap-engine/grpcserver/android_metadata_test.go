// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

type metadataADBRunner struct {
	unreadable  atomic.Bool
	discoveries atomic.Int32
}

type reconnectingMetadataADBRunner struct {
	address        string
	serialNumber   string
	connected      atomic.Bool
	connectAddress atomic.Value
	identityTarget atomic.Value
	stateTarget    atomic.Value
}

func (r *reconnectingMetadataADBRunner) Run(args ...string) (string, string, error) {
	if len(args) == 1 && args[0] == "devices" {
		if !r.connected.Load() {
			return "List of devices attached\n", "", nil
		}
		return "List of devices attached\n" + r.address + "\tdevice\n", "", nil
	}
	if len(args) == 2 && args[0] == "connect" {
		r.connectAddress.Store(args[1])
		r.connected.Store(true)
		return "connected", "", nil
	}
	if len(args) == 5 && args[0] == "-s" && args[2] == "shell" && args[3] == "getprop" && args[4] == "ro.serialno" {
		r.identityTarget.Store(args[1])
		if args[1] == r.address {
			return r.serialNumber, "", nil
		}
		return "different-physical-device", "", nil
	}
	if len(args) == 3 && args[0] == "-s" && args[2] == "get-state" {
		r.stateTarget.Store(args[1])
		return "device", "", nil
	}
	return "", "", errors.New("platform probe failed after serial verification")
}

type metadataNoLocalhostVerifier struct{}

func (*metadataNoLocalhostVerifier) IsLocalhostSupported() bool { return false }

func (r *metadataADBRunner) Run(args ...string) (string, string, error) {
	if len(args) == 1 && args[0] == "devices" {
		r.discoveries.Add(1)
		return "engine:5556 device\n", "", nil
	}
	if len(args) == 5 && args[3] == "getprop" {
		if r.unreadable.Load() {
			return "", "", nil
		}
		return "phone", "", nil
	}
	if len(args) == 3 && args[2] == "get-state" {
		return "device", "", nil
	}
	return "", "", errors.New("platform probe failed after serial verification")
}

func TestAndroidChecksReturnVerifiedAddressOnFailure(t *testing.T) {
	for _, operation := range []string{"test", "prepare"} {
		t.Run(operation, func(t *testing.T) {
			// Given: an actual engine session and gRPC transport, with no real ADB process.
			runner := &metadataADBRunner{}
			provider := targetsession.NewTargetSessionProvider(t.TempDir(), runner)
			t.Cleanup(func() { require.NoError(t, provider.Shutdown()) })
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			apapproto.RegisterApapServer(server, &ApapServer{targetSessions: provider})
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			conn, err := grpc.NewClient("passthrough:///android-metadata",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			client := apapproto.NewApapClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tgt := TargetToProto(&target.AndroidTarget{SerialNumber: "phone"})
			call := func() metadata.MD {
				var trailers metadata.MD
				if operation == "test" {
					response, err := client.TargetTest(ctx, &apapproto.TargetTestRequest{Target: tgt}, grpc.Trailer(&trailers))
					require.NoError(t, err)
					require.Equal(t, apapproto.ConnectionStatus_CONNECTION_STATUS_ERROR, response.Connection.Status)
				} else {
					_, err := client.TargetPrepare(ctx, &apapproto.TargetPrepareRequest{Target: tgt, DeploymentType: apapproto.ToolDeploy_CHECK}, grpc.Trailer(&trailers))
					require.Equal(t, codes.Unknown, status.Code(err))
				}
				return trailers
			}

			// When: identity resolution succeeds, then platform detection fails.
			trailers := call()

			// Then: the verified address survives both application and RPC failures.
			assert.Equal(t, []string{"engine:5556"}, trailers.Get(ResolvedADBAddress))
			assert.EqualValues(t, 1, runner.discoveries.Load())

			// When: a later call cannot verify any identity.
			runner.unreadable.Store(true)
			trailers = call()

			// Then: no stale address from the previous request is reported.
			assert.Empty(t, trailers.Get(ResolvedADBAddress))
		})
	}
}

func TestTargetTestReconnectsUsingSavedAndroidAddress(t *testing.T) {
	// Given a saved discovered remote target that is absent from initial ADB discovery.
	address := "192.0.2.10:5555"
	serial := "physical-phone-serial"
	targetManager := target.NewTargetManager(filepath.Join(t.TempDir(), "targets.json"), &metadataNoLocalhostVerifier{})
	require.NoError(t, targetManager.AddTarget("my-phone", &target.AndroidTarget{
		SerialNumber:        serial,
		LastKnownADBAddress: &address,
	}))
	savedTarget, err := targetManager.GetTarget("my-phone")
	require.NoError(t, err)
	protoTarget := TargetToProto(savedTarget)
	require.Equal(t, address, protoTarget.GetAndroidConfig().GetDeviceIpAddress())

	runner := &reconnectingMetadataADBRunner{address: address, serialNumber: serial}
	provider := targetsession.NewTargetSessionProvider(t.TempDir(), runner)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown()) })
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	apapproto.RegisterApapServer(server, &ApapServer{targetSessions: provider})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///saved-android-address",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	client := apapproto.NewApapClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// When the target is tested by the engine.
	var trailers metadata.MD
	response, err := client.TargetTest(ctx, &apapproto.TargetTestRequest{Target: protoTarget}, grpc.Trailer(&trailers))

	// Then ADB connects to the saved address and confirms the requested hardware serial.
	require.NoError(t, err)
	require.Equal(t, apapproto.ConnectionStatus_CONNECTION_STATUS_ERROR, response.Connection.Status)
	assert.Equal(t, address, runner.connectAddress.Load())
	assert.Equal(t, address, runner.identityTarget.Load())
	assert.Equal(t, address, runner.stateTarget.Load())
	assert.Equal(t, []string{address}, trailers.Get(ResolvedADBAddress))
}
