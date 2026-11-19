// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package producttelemetry

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/Arm-Debug/apap-cli/apap-engine/clientcontext"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
)

func rpcContext(t *testing.T, raw string) context.Context {
	t.Helper()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(clientcontext.MetadataKey, raw))
	_, err := clientcontext.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/apap.Apap/ExampleAction"}, func(requestContext context.Context, _ any) (any, error) {
		ctx = requestContext
		return nil, nil
	})
	require.NoError(t, err)
	return ctx
}

func guiContext(t *testing.T) context.Context {
	return rpcContext(t, `{"client":"gui","version":"1.2.3","runtime":"packaged","updateChannel":"latest","legacyGuiUUID":"old-gui-id"}`)
}

func TestEventPreservesGUIPropertiesWithEngineIdentity(t *testing.T) {
	// Given properties passed to the old trackEvent API, including structured recipe data.
	directory := t.TempDir()
	require.NoError(t, Mark(directory, true, true))
	id, err := GetOrCreateUserID(directory)
	require.NoError(t, err)
	event := Event{Name: "recipe.run.start"}
	event.Properties = map[string]any{
		"recipeName": "cpu_microarchitecture", "targetCpuCoreCount": 8,
		"readinessChecks": map[string]any{"warnings": []string{"warning"}, "errors": []string{}},
		"client":          "cli", "emissionSource": "gui", "userIdSource": "gui", "isCoreDev": 0,
		"isArmInternal": false, "version": "wrong", "legacyGuiUUID": "wrong", "hostArch": "wrong",
	}
	// When the central service builds the Azure envelope.
	prepared, err := (&Service{directory: directory}).prepareEvent(guiContext(t), event)
	require.NoError(t, err)
	payload, err := envelope(prepared)
	// Then the SDK mapping is preserved, with engine-owned identity and classification.
	require.NoError(t, err)
	var result eventEnvelope
	require.NoError(t, json.Unmarshal(payload, &result))
	assert.Equal(t, id, result.Tags["ai.user.id"])
	assert.Equal(t, "Browser", result.Tags["ai.device.type"])
	assert.Equal(t, instrumentationKey, result.IKey)
	assert.Equal(t, "EventData", result.Data.BaseType)
	assert.Equal(t, "recipe.run.start", result.Data.BaseData.Name)
	assert.Equal(t, map[string]float64{"targetCpuCoreCount": 8}, result.Data.BaseData.Measurements)
	assert.Equal(t, map[string]string{
		"recipeName": "cpu_microarchitecture", "readinessChecks": `{"errors":[],"warnings":["warning"]}`,
		"client": "gui", "emissionSource": "engine", "userIdSource": "engine", "isCoreDev": "true",
		"hostArch":      runtime.GOARCH,
		"isArmInternal": "true", "version": "1.2.3", "runtime": "packaged", "updateChannel": "latest", "legacyGuiUUID": "old-gui-id",
	}, result.Data.BaseData.Properties)
}

func TestTelemetryConsentAndExplicitFeedback(t *testing.T) {
	// Given telemetry disabled deep in the service, without GUI-side checks.
	var delivered []eventEnvelope
	service := NewService(t.TempDir(), func() bool { return false }, func(_ context.Context, payload []byte) error {
		var event eventEnvelope
		if err := json.Unmarshal(payload, &event); err != nil {
			return err
		}
		delivered = append(delivered, event)
		return nil
	})
	t.Cleanup(func() { require.NoError(t, service.Close(context.Background())) })
	// When ordinary events and explicit feedback use their respective paths.
	require.NoError(t, service.SendTelemetryEvent(guiContext(t), Event{Name: "application.startup"}))
	require.NoError(t, service.SendTelemetryEvent(guiContext(t), Event{Name: "feedback.after-run"}))
	require.NoError(t, service.SendFeedbackEvent(guiContext(t), Event{Name: "feedback.after-run"}))
	require.NoError(t, service.SendFeedbackEvent(guiContext(t), Event{Name: "feedback.insight"}))
	err := service.SendFeedbackEvent(guiContext(t), Event{Name: "application.startup"})
	require.NoError(t, service.Close(context.Background()))
	// Then only recognised explicit feedback is delivered using the engine identity.
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineProductTelemetryEventInvalid, msg.Code())
	require.Len(t, delivered, 2)
	assert.Equal(t, "feedback.after-run", delivered[0].Data.BaseData.Name)
	assert.Equal(t, "feedback.insight", delivered[1].Data.BaseData.Name)
	assert.Equal(t, delivered[0].Tags["ai.user.id"], delivered[1].Tags["ai.user.id"])
	for _, event := range delivered {
		assert.Equal(t, runtime.GOARCH, event.Data.BaseData.Properties["hostArch"])
	}
}

func TestQueuedTelemetryIsDiscardedAfterOptOut(t *testing.T) {
	// Given a request held in flight while more events enter the queue.
	var enabled atomic.Bool
	enabled.Store(true)
	entered, release := make(chan struct{}), make(chan struct{})
	var names []string
	service := NewService(t.TempDir(), enabled.Load, func(ctx context.Context, payload []byte) error {
		if len(names) == 0 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		var event eventEnvelope
		if err := json.Unmarshal(payload, &event); err != nil {
			return err
		}
		names = append(names, event.Data.BaseData.Name)
		return nil
	})
	t.Cleanup(func() { require.NoError(t, service.Close(context.Background())) })
	require.NoError(t, service.SendFeedbackEvent(guiContext(t), Event{Name: "feedback.after-run"}))
	<-entered
	require.NoError(t, service.SendTelemetryEvent(guiContext(t), Event{Name: "application.startup"}))
	require.NoError(t, service.SendFeedbackEvent(guiContext(t), Event{Name: "feedback.insight"}))
	// When consent is revoked before queued events are dispatched.
	enabled.Store(false)
	close(release)
	require.NoError(t, service.Close(context.Background()))
	// Then shutdown drains feedback and discards queued telemetry.
	assert.Equal(t, []string{"feedback.after-run", "feedback.insight"}, names)
}

func TestQueueIsBoundedAndShutdownCancelsDelivery(t *testing.T) {
	// Given a blocked transport and a full queue.
	entered := make(chan struct{})
	service := NewService(t.TempDir(), func() bool { return true }, func(ctx context.Context, _ []byte) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	require.NoError(t, service.SendTelemetryEvent(guiContext(t), Event{Name: "application.startup"}))
	<-entered
	for range queueCapacity {
		require.NoError(t, service.SendTelemetryEvent(guiContext(t), Event{Name: "application.startup"}))
	}
	// When another event arrives and the shutdown deadline expires.
	err := service.SendTelemetryEvent(guiContext(t), Event{Name: "application.startup"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closeErr := service.Close(ctx)
	// Then overflow is reported, the request is cancelled and further submissions fail.
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineProductTelemetryEventSubmissionFailed, msg.Code())
	require.ErrorIs(t, closeErr, context.Canceled)
	assert.Error(t, service.SendFeedbackEvent(guiContext(t), Event{Name: "feedback.after-run"}))
	require.NoError(t, service.Close(context.Background()))
}

func TestInvalidEventsNeverReachSender(t *testing.T) {
	huge := make(map[string]any)
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		huge[key] = strings.Repeat(key, 8192)
	}
	for name, event := range map[string]Event{
		"missing name":        {Name: ""},
		"non-JSON properties": {Name: "event", Properties: map[string]any{"bad": math.NaN()}},
		"oversized event":     {Name: "event", Properties: huge},
	} {
		t.Run(name, func(t *testing.T) {
			// Given an invalid submission.
			service := NewService(t.TempDir(), func() bool { return true }, func(context.Context, []byte) error { t.Error("invalid event delivered"); return nil })
			// When the client submits it.
			err := service.SendTelemetryEvent(guiContext(t), event)
			require.NoError(t, service.Close(context.Background()))
			// Then validation rejects it before delivery.
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineProductTelemetryEventInvalid, msg.Code())
		})
	}
}

func TestEventsWithoutValidClientContextAreRejected(t *testing.T) {
	for _, raw := range []string{"", `{"client":"unknown"}`, `{"client":"engine"}`} {
		// Given a missing or invalid originating client.
		ctx := rpcContext(t, raw)
		// When the handler emits an event.
		_, err := (&Service{directory: t.TempDir()}).prepareEvent(ctx, Event{Name: "event"})
		// Then attribution is required rather than invented.
		var msg message.Message
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, message.EngineProductTelemetryEventInvalid, msg.Code())
	}
}

func TestInvalidIdentityIsNotReplaced(t *testing.T) {
	// Given a corrupt existing identity.
	directory := t.TempDir()
	path := filepath.Join(directory, userIDFile)
	require.NoError(t, os.WriteFile(path, []byte("invalid"), 0o600))
	service := NewService(directory, func() bool { return true }, func(context.Context, []byte) error { t.Error("unexpected send"); return nil })
	// When feedback is submitted.
	err := service.SendFeedbackEvent(guiContext(t), Event{Name: "feedback.insight"})
	require.NoError(t, service.Close(context.Background()))
	// Then it cannot fall back to another identity or overwrite the persisted value.
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineProductTelemetryIdentityAccessFailed, msg.Code())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "invalid", string(data))
}

func TestEngineEventsInheritRPCContext(t *testing.T) {
	expectedOS, err := systeminfo.NewSystemInfo().GetOSDescription()
	if err != nil || strings.TrimSpace(expectedOS) == "" {
		expectedOS = runtime.GOOS
	}
	for _, origin := range []clientcontext.Client{clientcontext.GUI, clientcontext.CLI, clientcontext.MCP} {
		t.Run(string(origin), func(t *testing.T) {
			// Given application metadata, including an attempted client OS override.
			raw, _ := json.Marshal(map[string]string{"client": string(origin), "os": "client OS 42", "version": "client-version", "runtime": "dev", "updateChannel": "unknown", "legacyGuiUUID": "old-gui"})
			ctx := rpcContext(t, string(raw))
			var received eventEnvelope
			service := NewService(t.TempDir(), func() bool { return true }, func(_ context.Context, payload []byte) error { return json.Unmarshal(payload, &received) })
			// When an ordinary engine RPC emits only an event name and event properties.
			err := service.SendTelemetryEvent(ctx, Event{Name: "engine.action", Properties: map[string]any{"count": 8, "client": "wrong", "version": "wrong", "runtime": "wrong", "updateChannel": "wrong", "legacyGuiUUID": "wrong", "hostArch": 42}})
			require.NoError(t, err)
			require.NoError(t, service.Close(context.Background()))
			// Then all common metadata arrives without RPC-specific client changes.
			props := received.Data.BaseData.Properties
			assert.Equal(t, string(origin), props["client"])
			assert.Equal(t, "client-version", props["version"])
			assert.Equal(t, "engine", props["emissionSource"])
			assert.Equal(t, runtime.GOARCH, props["hostArch"])
			assert.Equal(t, map[string]float64{"count": 8}, received.Data.BaseData.Measurements)
			assert.Equal(t, strings.TrimSpace(expectedOS), received.Tags["ai.device.osVersion"])
			assert.NotEqual(t, "client OS 42", received.Tags["ai.device.osVersion"])
			assert.Equal(t, "/apap.Apap/ExampleAction", received.Tags["ai.operation.name"])
			assert.Len(t, received.Tags["ai.operation.id"], 32)
			assert.Len(t, received.Tags["ai.operation.parentId"], 16)
			for _, key := range []string{"ai.session.id", "ai.device.model", "ai.internal.sdkVersion"} {
				assert.NotContains(t, received.Tags, key)
			}
			if origin == clientcontext.GUI {
				assert.Equal(t, "dev", props["runtime"])
				assert.Equal(t, "unknown", props["updateChannel"])
				assert.Equal(t, "old-gui", props["legacyGuiUUID"])
			} else {
				assert.NotContains(t, props, "legacyGuiUUID")
				assert.NotContains(t, props, "runtime")
				assert.NotContains(t, props, "updateChannel")
				assert.NotContains(t, received.Tags, "ai.device.type")
			}
		})
	}
}
