// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func executeAndCheck(t *testing.T, command *cobra.Command, arguments []string) {
	command.SetArgs(arguments)
	err := command.Execute()
	assert.NoError(t, err)
}

func TestFlagsDoNotConflict(t *testing.T) {
	command := NewRootCmd()

	executeAndCheck(t, command, []string{"-h"})

	executeAndCheck(t, command, []string{"commands", "-h"})

	executeAndCheck(t, command, []string{"config", "-h"})
	executeAndCheck(t, command, []string{"config", "print", "-h"})

	executeAndCheck(t, command, []string{"daemon", "-h"})
	executeAndCheck(t, command, []string{"daemon", "start", "-h"})
	executeAndCheck(t, command, []string{"daemon", "stop", "-h"})

	executeAndCheck(t, command, []string{"version", "-h"})

	executeAndCheck(t, command, []string{"target", "-h"})
}

func TestDetermineExitCodeForError(t *testing.T) {
	t.Run("nil error returns exit code 0", func(t *testing.T) {
		code := determineExitCodeForError(nil)
		assert.Equal(t, code, 0)
	})

	t.Run("plain error returns exit code 1", func(t *testing.T) {
		plainErr := errors.New("boom")
		code := determineExitCodeForError(plainErr)
		assert.Equal(t, code, 1)
	})

	t.Run("info message returns exit code 0", func(t *testing.T) {
		infoMsg := message.New(message.CliCmdTargetPrepareAlreadyPrepared)
		code := determineExitCodeForError(infoMsg)
		assert.Equal(t, code, 0)
	})

	// Disabled for now until we have some Warning message examples in the catalog
	//t.Run("warning message returns exit code 0", func(t *testing.T) {
	//	warningMsg := message.New(message.CliCmdTargetPrepareAlreadyPrepared)
	//	code := determineExitCodeForError(warningMsg)
	//	assert.Equal(t, code, 0)
	//})

	t.Run("error message returns exit code 1", func(t *testing.T) {
		errorMsg := message.New(message.CommonUnknownError)
		code := determineExitCodeForError(errorMsg)
		assert.Equal(t, code, 1)
	})
}

func TestInitConfigSetsDefaults(t *testing.T) {
	viper.Reset()
	initConfigOnce = sync.Once{}
	oldCfgFile := cfgFile
	t.Cleanup(func() {
		cfgFile = oldCfgFile
		viper.Reset()
		initConfigOnce = sync.Once{}
	})

	tempDir := t.TempDir()
	cfgFile = filepath.Join(tempDir, "apx.yml")
	t.Setenv(util.ApplyEnvPrefix("CONFIG_DIR"), tempDir)
	t.Setenv(util.ApplyEnvPrefix("ENABLE_CMN_RECIPE"), "")

	initConfig()

	assert.Equal(t, serverconfig.DefaultEnableExperimentalRecipes, viper.GetBool("enable-experimental-recipes"))
	assert.Equal(t, serverconfig.DefaultEnableTransferManager, viper.GetBool("enable-transfer-manager"))
	assert.Equal(t, serverconfig.DefaultEnableAndroidTargets, viper.GetBool(serverconfig.EnableAndroidTargetsConfigKey))
	assert.Equal(t, serverconfig.DefaultEnableRenderDBSandbox, viper.GetBool("enable-render-db-sandbox"))
	assert.Equal(t, serverconfig.DefaultEnableNeoprofTimeline, viper.GetBool("enable-neoprof-timeline"))
}
