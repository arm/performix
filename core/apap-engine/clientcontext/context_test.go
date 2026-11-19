// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package clientcontext

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestMalformedOrMissingMetadataDoesNotBreakRPCs(t *testing.T) {
	for _, values := range [][]string{nil, {"invalid"}, {`{"client":"engine"}`}, {`{"client":"gui","version":"line\nbreak"}`}, {strings.Repeat("x", 4097)}, {`{"client":"gui","version":"` + strings.Repeat("x", 257) + `"}`}, {`{"client":"gui"}`, `{"client":"cli"}`}} {
		// Given a legacy or malformed request.
		ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{MetadataKey: values})
		called := false
		// When the interceptor dispatches the ordinary RPC.
		_, err := UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test"}, func(ctx context.Context, _ any) (any, error) {
			called = true
			// Then no context is invented, and the user's action still runs.
			_, ok := FromContext(ctx)
			assert.False(t, ok)
			return nil, nil
		})
		require.NoError(t, err)
		assert.True(t, called)
	}
}

func TestNonGUIClientsCannotSupplyGUIFieldsOrOperations(t *testing.T) {
	// Given client metadata claiming GUI-only and engine-owned fields.
	raw, _ := json.Marshal(map[string]string{"client": "cli", "runtime": "packaged", "updateChannel": "latest", "legacyGuiUUID": "gui-id", "OperationID": "injected", "version": "cli-version", "os": "Linux"})
	// When the description is parsed.
	info, ok := Parse(string(raw))
	// Then only the CLI's application fields survive.
	require.True(t, ok)
	assert.Equal(t, Info{Client: CLI, Version: "cli-version"}, info)
}
