// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/settings"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

func TestSettingRPCReadStopsWaitingWhenTheRequestExpires(t *testing.T) {
	// Given
	directory := t.TempDir()
	path := filepath.Join(directory, settings.Filename)
	require.NoError(t, settings.Write(context.Background(), path, settings.ProfilingFocus, "mobile"))
	lock := flock.New(path + ".lock")
	require.NoError(t, lock.Lock())
	t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// When
	_, err := server.GetSetting(ctx, &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})
	// Then
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, lock.Unlock())
	value, err := settings.Read(context.Background(), path, settings.ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

func TestSettingRPCSaveStopsWaitingWhenTheRequestExpires(t *testing.T) {
	// Given
	directory := t.TempDir()
	path := filepath.Join(directory, settings.Filename)
	require.NoError(t, settings.Write(context.Background(), path, settings.ProfilingFocus, "mobile"))
	lock := flock.New(path + ".lock")
	require.NoError(t, lock.Lock())
	t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// When
	_, err := server.SetSetting(ctx, &apapproto.Setting{Name: settings.ProfilingFocus, Value: structpb.NewStringValue("server")})
	// Then
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, lock.Unlock())
	value, err := settings.Read(context.Background(), path, settings.ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

func TestSettingRPCClearStopsWaitingWhenTheRequestExpires(t *testing.T) {
	// Given
	directory := t.TempDir()
	path := filepath.Join(directory, settings.Filename)
	require.NoError(t, settings.Write(context.Background(), path, settings.ProfilingFocus, "mobile"))
	lock := flock.New(path + ".lock")
	require.NoError(t, lock.Lock())
	t.Cleanup(func() { require.NoError(t, lock.Unlock()) })
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// When
	_, err := server.SetSetting(ctx, &apapproto.Setting{Name: settings.ProfilingFocus, Value: structpb.NewNullValue()})
	// Then
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, lock.Unlock())
	value, err := settings.Read(context.Background(), path, settings.ProfilingFocus)
	require.NoError(t, err)
	assert.Equal(t, "mobile", value)
}

func TestSettingRPCSavedValueIsAvailableToNewServer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value *structpb.Value
	}{
		{name: "mobile", value: structpb.NewStringValue("mobile")},
		{name: "server", value: structpb.NewStringValue("server")},
		{name: "explicit opt-out", value: structpb.NewStringValue("none")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			directory := filepath.Join(t.TempDir(), "config")
			server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
			fresh := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
			require.NoError(t, settings.Write(context.Background(), filepath.Join(directory, settings.Filename), settings.ProfilingFocus, "mobile"))
			// When
			_, err := server.SetSetting(context.Background(), &apapproto.Setting{Name: settings.ProfilingFocus, Value: tc.value})
			actual, readErr := fresh.GetSetting(context.Background(), &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})
			// Then
			require.NoError(t, err)
			require.NoError(t, readErr)
			require.NotNil(t, actual.Value)
			assert.Equal(t, tc.value.AsInterface(), actual.Value.AsInterface())
		})
	}
}

func TestSettingRPCClearedValueIsUnsetForNewServer(t *testing.T) {
	// Given
	directory := filepath.Join(t.TempDir(), "config")
	path := filepath.Join(directory, settings.Filename)
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	fresh := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	require.NoError(t, settings.Write(context.Background(), path, settings.ProfilingFocus, "mobile"))
	// When
	_, err := server.SetSetting(context.Background(), &apapproto.Setting{Name: settings.ProfilingFocus, Value: structpb.NewNullValue()})
	actual, readErr := fresh.GetSetting(context.Background(), &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})
	// Then
	require.NoError(t, err)
	require.NoError(t, readErr)
	require.NotNil(t, actual.Value)
	assert.IsType(t, &structpb.Value_NullValue{}, actual.Value.Kind)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(data))
}

func TestSettingRPCUnsetWhenFileIsMissing(t *testing.T) {
	// Given
	directory := filepath.Join(t.TempDir(), "config")
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	// When
	initial, err := server.GetSetting(context.Background(), &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})
	// Then
	require.NoError(t, err)
	require.NotNil(t, initial.Value)
	assert.IsType(t, &structpb.Value_NullValue{}, initial.Value.Kind)
}

func TestSettingRPCReadsLatestFileContents(t *testing.T) {
	// Given
	directory := t.TempDir()
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	require.NoError(t, settings.Write(context.Background(), filepath.Join(directory, settings.Filename), settings.ProfilingFocus, "mobile"))
	_, err := server.GetSetting(context.Background(), &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})
	require.NoError(t, err)
	require.NoError(t, settings.Write(context.Background(), filepath.Join(directory, settings.Filename), settings.ProfilingFocus, "server"))
	// When
	actual, err := server.GetSetting(context.Background(), &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})
	// Then
	require.NoError(t, err)
	assert.Equal(t, "server", actual.Value.GetStringValue())
}

func TestSettingRPCRejectsInvalidFocusValue(t *testing.T) {
	// Given
	directory := t.TempDir()
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	// When
	_, err := server.SetSetting(context.Background(), &apapproto.Setting{Name: settings.ProfilingFocus, Value: structpb.NewStringValue("cloud")})
	// Then
	assertSettingRPCError(t, err, message.EngineSettingsInvalidValue, map[string]string{"name": settings.ProfilingFocus})
}

func TestSettingRPCReportsUnreadableSettingsFile(t *testing.T) {
	// Given
	directory := t.TempDir()
	path := filepath.Join(directory, settings.Filename)
	require.NoError(t, os.Mkdir(path, perms.LocalDirPerm))
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	// When
	_, err := server.GetSetting(context.Background(), &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})
	// Then
	assertSettingRPCError(t, err, message.EngineSettingsReadFailed, map[string]string{"path": path})
}

func TestSettingRPCReportsInvalidSettingsFileContents(t *testing.T) {
	// Given
	directory := t.TempDir()
	path := filepath.Join(directory, settings.Filename)
	require.NoError(t, os.WriteFile(path, []byte(`{"profilingFocus":"cloud"}`), perms.LocalFilePerm))
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	// When
	_, err := server.GetSetting(context.Background(), &apapproto.GetSettingRequest{Name: settings.ProfilingFocus})
	// Then
	assertSettingRPCError(t, err, message.EngineSettingsParseFailed, map[string]string{"path": path})
}

func TestSettingRPCSaveReportsUnreadableSettingsFile(t *testing.T) {
	// Given
	directory := t.TempDir()
	path := filepath.Join(directory, settings.Filename)
	require.NoError(t, os.Mkdir(path, perms.LocalDirPerm))
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
	// When
	_, err := server.SetSetting(context.Background(), &apapproto.Setting{Name: settings.ProfilingFocus, Value: structpb.NewStringValue("mobile")})
	// Then
	assertSettingRPCError(t, err, message.EngineSettingsReadFailed, map[string]string{"path": path})
}

// assertSettingRPCError checks that a catalogue error keeps its details when sent to a client.
func assertSettingRPCError(t *testing.T, err error, code message.MessageCode, metadata map[string]string) {
	t.Helper()
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, code, msg.Code())
	assert.Equal(t, metadata, msg.Metadata())
	rpcStatus := status.Convert(message.AsGRPCStatus(err))
	assert.Equal(t, codes.FailedPrecondition, rpcStatus.Code())
	require.NotEmpty(t, rpcStatus.Details())
	info, ok := rpcStatus.Details()[0].(*errdetails.ErrorInfo)
	require.True(t, ok)
	assert.Equal(t, msg.Domain(), info.Domain)
	assert.Equal(t, msg.Reason(), info.Reason)
	assert.Equal(t, msg.Metadata(), info.Metadata)
}

func TestSettingRPCReadRejectsUnknownName(t *testing.T) {
	// Given
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: t.TempDir()}}
	// When
	_, err := server.GetSetting(context.Background(), &apapproto.GetSettingRequest{Name: "unknown"})
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsInvalidName, msg.Code())
	assert.Equal(t, "unknown", msg.Metadata()["name"])
}

func TestSettingRPCSaveRejectsUnknownName(t *testing.T) {
	// Given
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: t.TempDir()}}
	// When
	_, err := server.SetSetting(context.Background(), &apapproto.Setting{Name: "unknown", Value: structpb.NewBoolValue(true)})
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsInvalidName, msg.Code())
	assert.Equal(t, "unknown", msg.Metadata()["name"])
}

func TestSettingRPCClearRejectsUnknownName(t *testing.T) {
	// Given
	server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: t.TempDir()}}
	// When
	_, err := server.SetSetting(context.Background(), &apapproto.Setting{Name: "unknown", Value: structpb.NewNullValue()})
	// Then
	var msg message.Message
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineSettingsInvalidName, msg.Code())
	assert.Equal(t, "unknown", msg.Metadata()["name"])
}

func TestSettingRPCRejectsInvalidTypes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value *structpb.Value
	}{
		{name: "missing value", value: nil},
		{name: "missing value kind", value: &structpb.Value{}},
		{name: "empty string", value: structpb.NewStringValue("")},
		{name: "boolean", value: structpb.NewBoolValue(true)},
		{name: "number", value: structpb.NewNumberValue(42)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			directory := t.TempDir()
			path := filepath.Join(directory, settings.Filename)
			require.NoError(t, settings.Write(context.Background(), path, settings.ProfilingFocus, "mobile"))
			server := &ApapServer{config: &ApapServerConfig{ConfigDirectory: directory}}
			// When
			_, err := server.SetSetting(context.Background(), &apapproto.Setting{Name: settings.ProfilingFocus, Value: tc.value})
			// Then
			var msg message.Message
			require.ErrorAs(t, err, &msg)
			assert.Equal(t, message.EngineSettingsInvalidValue, msg.Code())
			assert.Equal(t, settings.ProfilingFocus, msg.Metadata()["name"])
			value, readErr := settings.Read(context.Background(), path, settings.ProfilingFocus)
			require.NoError(t, readErr)
			assert.Equal(t, "mobile", value)
		})
	}
}
