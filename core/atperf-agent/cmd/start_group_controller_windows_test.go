// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package cmd

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestStartGroupController_UnsupportedOnWindows(t *testing.T) {
	cmd := NewStartGroupControllerCmd()

	err := cmd.Execute()
	require.NotNil(t, err)

	expectedErr := message.New(message.AgentLifecycleGroupControllerUnsupportedPlatform).
		WithMetadata(map[string]string{
			"os":   runtime.GOOS,
			"arch": runtime.GOARCH,
		})
	assert.Equal(t, err, expectedErr)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}
