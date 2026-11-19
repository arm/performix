// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"path/filepath"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/settings"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

// GetSetting reads a saved setting from the engine's configuration directory.
// An unset setting is returned as a JSON null value.
func (s *ApapServer) GetSetting(ctx context.Context, request *apapproto.GetSettingRequest) (*apapproto.Setting, error) {
	path := filepath.Join(s.config.ConfigDirectory, settings.Filename)
	value, err := settings.Read(ctx, path, request.Name)
	if err != nil {
		return nil, err
	}
	encoded, err := structpb.NewValue(value)
	if err != nil {
		return nil, message.New(message.EngineSettingsParseFailed).WithMetadata(map[string]string{"path": path}).WithCause(err)
	}
	return &apapproto.Setting{Name: request.Name, Value: encoded}, nil
}

// SetSetting saves a setting in the engine's configuration directory.
// A JSON null value removes the saved setting.
func (s *ApapServer) SetSetting(ctx context.Context, setting *apapproto.Setting) (*emptypb.Empty, error) {
	if setting.Value == nil || setting.Value.Kind == nil {
		return nil, message.New(message.EngineSettingsInvalidValue).WithMetadata(map[string]string{"name": setting.Name})
	}
	if err := settings.Write(ctx, filepath.Join(s.config.ConfigDirectory, settings.Filename), setting.Name, setting.Value.AsInterface()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
