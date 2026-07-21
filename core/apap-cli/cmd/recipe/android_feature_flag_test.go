// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func withAndroidTargetsEnabled(t *testing.T, enabled bool) {
	t.Helper()
	original := viper.GetBool(serverconfig.EnableAndroidTargetsConfigKey)
	viper.Set(serverconfig.EnableAndroidTargetsConfigKey, enabled)
	t.Cleanup(func() {
		viper.Set(serverconfig.EnableAndroidTargetsConfigKey, original)
		RefreshAndroidLaunchHelp()
	})
}

func androidRecipeCommands() []struct {
	name string
	new  func() *cobra.Command
} {
	return []struct {
		name string
		new  func() *cobra.Command
	}{
		{name: "run", new: func() *cobra.Command { return NewRunCommand(nil, nil, nil, nil, nil) }},
		{name: "ready", new: func() *cobra.Command { return NewReadyCommand(nil, nil, nil, nil, nil) }},
		{name: "validate-parameters", new: func() *cobra.Command { return NewValidateCommand(nil, nil, nil) }},
	}
}

func TestAndroidLaunchFeatureFlagControlsCommandHelp(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		expectedUse string
	}{
		{name: "disabled", enabled: false, expectedUse: runUse},
		{name: "enabled", enabled: true, expectedUse: runUseWithAndroid},
	}
	for _, featureTest := range tests {
		t.Run(featureTest.name, func(t *testing.T) {
			withAndroidTargetsEnabled(t, featureTest.enabled)

			for _, test := range androidRecipeCommands() {
				t.Run(test.name, func(t *testing.T) {
					cmd := test.new()
					for _, flagName := range androidLaunchFlagNames {
						flag := cmd.Flags().Lookup(flagName)
						require.NotNil(t, flag)
						assert.Equal(t, !featureTest.enabled, flag.Hidden)
					}
				})
			}

			runCmd := NewRunCommand(nil, nil, nil, nil, nil)
			assert.Equal(t, featureTest.expectedUse, runCmd.Use)
		})
	}
}

func TestAndroidLaunchFlagsFailWhenFeatureIsDisabled(t *testing.T) {
	withAndroidTargetsEnabled(t, false)

	expectedErr := message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "android"})
	for _, test := range androidRecipeCommands() {
		t.Run(test.name, func(t *testing.T) {
			cmd := test.new()
			cmd.SetArgs([]string{"test_recipe", "--android-package", "com.example.app", "--android-activity", ".MainActivity"})

			_, err := cmd.ExecuteC()

			assert.Equal(t, expectedErr, err)
			assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		})
	}
}

func TestRefreshAndroidLaunchHelpUsesLoadedFeatureFlag(t *testing.T) {
	withAndroidTargetsEnabled(t, true)

	RefreshAndroidLaunchHelp()

	assert.Equal(t, runUseWithAndroid, RunCmd.Use)
	for _, cmd := range []*cobra.Command{RunCmd, ReadyCmd, ValidateCmd} {
		for _, flagName := range androidLaunchFlagNames {
			assert.False(t, cmd.Flags().Lookup(flagName).Hidden)
		}
	}
}
