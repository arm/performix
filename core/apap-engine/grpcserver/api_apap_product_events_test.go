// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/clientcontext"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/producttelemetry"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

func TestProductEventsOverGRPC(t *testing.T) {
	// Given the real gRPC handlers and consent service with a controlled delivery sink.
	directory := t.TempDir()
	config := &ApapServerConfig{ConfigFile: filepath.Join(directory, "apx.yml"), TelemetryEnabled: false}
	var deliveries []map[string]any
	service := producttelemetry.NewService(directory, func() bool { enabled, _ := config.GetTelemetryPreference(); return enabled }, func(_ context.Context, payload []byte) error {
		var envelope map[string]any
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return err
		}
		deliveries = append(deliveries, envelope)
		return nil
	})
	t.Cleanup(func() { require.NoError(t, service.Close(context.Background())) })
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(clientcontext.UnaryServerInterceptor(), message.ErrorHandlingServerInterceptor()))
	apapproto.RegisterApapServer(server, &ApapServer{config: config, productTelemetry: service})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///telemetry-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	client := apapproto.NewApapClient(connection)
	properties, err := structpb.NewStruct(map[string]any{"targetCpuCoreCount": 8, "readinessChecks": map[string]any{"warnings": []any{"warning"}}, "isCoreDev": true})
	require.NoError(t, err)
	event := &apapproto.ProductTelemetryEvent{Name: "recipe.run.start", Properties: properties}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(clientcontext.MetadataKey, `{"client":"gui","version":"1.2.3","runtime":"packaged","updateChannel":"latest","legacyGuiUUID":"legacy-id"}`))
	// When clients submit before and after opt-in, including invalid feedback and origins.
	_, err = client.SendTelemetryEvent(ctx, event)
	require.NoError(t, err)
	_, err = client.SendFeedbackEvent(ctx, event)
	var msg message.Message
	require.ErrorAs(t, message.FromGRPCStatus(err), &msg)
	assert.Equal(t, message.EngineProductTelemetryEventInvalid, msg.Code())
	event.Name = "feedback.insight"
	_, err = client.SendFeedbackEvent(ctx, event)
	require.NoError(t, err)
	_, err = client.SetProductTelemetryPreference(context.Background(), &apapproto.ProductTelemetryPreference{Enabled: true})
	require.NoError(t, err)
	event.Name = "recipe.run.start"
	_, err = client.SendTelemetryEvent(ctx, event)
	require.NoError(t, err)
	for _, origin := range []string{"cli", "mcp"} {
		ctx = metadata.NewOutgoingContext(context.Background(), metadata.Pairs(clientcontext.MetadataKey, `{"client":"`+origin+`"}`))
		_, err = client.SendTelemetryEvent(ctx, event)
		require.NoError(t, err)
	}
	ctx = metadata.NewOutgoingContext(context.Background(), metadata.Pairs(clientcontext.MetadataKey, `{"client":"engine"}`))
	_, err = client.SendTelemetryEvent(ctx, event)
	require.ErrorAs(t, message.FromGRPCStatus(err), &msg)
	assert.Equal(t, message.EngineProductTelemetryEventInvalid, msg.Code())
	event.Name = "feedback.insight"
	_, err = client.SendFeedbackEvent(ctx, event)
	require.ErrorAs(t, message.FromGRPCStatus(err), &msg)
	assert.Equal(t, message.EngineProductTelemetryEventInvalid, msg.Code())
	server.GracefulStop()
	require.NoError(t, service.Close(context.Background()))
	// Then only explicit feedback and opted-in events from recognised clients are delivered.
	require.Len(t, deliveries, 4)
	for i, origin := range []string{"gui", "gui", "cli", "mcp"} {
		base := deliveries[i]["data"].(map[string]any)["baseData"].(map[string]any)
		name := "recipe.run.start"
		if i == 0 {
			name = "feedback.insight"
		}
		assert.Equal(t, name, base["name"])
		assert.Equal(t, map[string]any{"targetCpuCoreCount": float64(8)}, base["measurements"])
		props := base["properties"].(map[string]any)
		tags := deliveries[i]["tags"].(map[string]any)
		assert.Equal(t, "false", props["isCoreDev"])
		assert.Equal(t, origin, props["client"])
		assert.Equal(t, "engine", props["emissionSource"])
		if origin == "gui" {
			assert.Equal(t, "1.2.3", props["version"])
			assert.Equal(t, "legacy-id", props["legacyGuiUUID"])
			assert.Equal(t, "Browser", tags["ai.device.type"])
			assert.NotContains(t, tags, "ai.session.id")
		} else {
			assert.NotContains(t, props, "version")
			assert.NotContains(t, props, "legacyGuiUUID")
			assert.NotContains(t, tags, "ai.device.type")
		}
		assert.JSONEq(t, `{"warnings":["warning"]}`, props["readinessChecks"].(string))
		assert.Equal(t, deliveries[0]["tags"].(map[string]any)["ai.user.id"], tags["ai.user.id"])
	}
}
