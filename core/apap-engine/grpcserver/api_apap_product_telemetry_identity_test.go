// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/producttelemetry"
)

func TestProductTelemetryIdentityCreatesAndReusesID(t *testing.T) {
	// Given per-user state that has never been used by the GUI.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", home)
	t.Setenv("XDG_STATE_HOME", home)
	server := &ApapServer{}
	// When clients request the identity through the RPC implementation.
	first, err := server.GetOrCreateProductTelemetryUserId(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	second, err := server.GetOrCreateProductTelemetryUserId(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	// Then clients share the engine-created identity.
	assert.NoError(t, uuid.Validate(first.Value))
	assert.Equal(t, first.Value, second.Value)
}

func TestProductTelemetryIdentityRejectsInvalidStoredID(t *testing.T) {
	// Given an invalid persisted identity.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", home)
	t.Setenv("XDG_STATE_HOME", home)
	server := &ApapServer{}
	directory, err := producttelemetry.MarkerDirectory()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(directory, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "telemetry-user-id"), []byte("invalid"), 0o600))
	// When a client requests the identity.
	result, err := server.GetOrCreateProductTelemetryUserId(context.Background(), &emptypb.Empty{})
	// Then it receives a structured engine error and no fallback identity.
	assert.Nil(t, result)
	require.Error(t, err)
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineProductTelemetryIdentityAccessFailed, msg.Code())
}
