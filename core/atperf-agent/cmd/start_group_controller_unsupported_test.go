// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build darwin || windows

package cmd

import (
	"os"
	"runtime"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestStartGroupController_UnsupportedPlatform(t *testing.T) {
	t.Chdir(t.TempDir())
	logger := log.StandardLogger()
	output, formatter := logger.Out, logger.Formatter
	hooks := make(log.LevelHooks)
	for level, entries := range logger.Hooks {
		hooks[level] = append([]log.Hook(nil), entries...)
	}
	hookSetup := isLogHookSetup
	// Run before TempDir cleanup: Windows cannot remove an open log file.
	t.Cleanup(func() {
		if file, ok := logger.Out.(*os.File); ok && file != output {
			assert.NoError(t, file.Close())
		}
		logger.SetOutput(output)
		logger.SetFormatter(formatter)
		logger.ReplaceHooks(hooks)
		isLogHookSetup = hookSetup
	})
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
