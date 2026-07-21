// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

const runUse = "run [recipe] [--workload OR --pid OR --system-wide]"
const runUseWithAndroid = "run [recipe] [--workload OR --pid OR --system-wide OR --android-package --android-activity]"

var androidLaunchFlagNames = []string{"android-package", "android-activity"}

func androidTargetsEnabled() bool {
	return viper.GetBool(serverconfig.EnableAndroidTargetsConfigKey)
}

func runCommandUse() string {
	if androidTargetsEnabled() {
		return runUseWithAndroid
	}
	return runUse
}

func setAndroidLaunchFlagVisibility(cmd *cobra.Command) {
	hidden := !androidTargetsEnabled()
	for _, flagName := range androidLaunchFlagNames {
		if flag := cmd.Flags().Lookup(flagName); flag != nil {
			flag.Hidden = hidden
		}
	}
}

func validateAndroidLaunchFeature(cmd *cobra.Command) error {
	if androidTargetsEnabled() {
		return nil
	}

	for _, flagName := range androidLaunchFlagNames {
		if cmd.Flags().Changed(flagName) {
			return message.New(message.CommonUnsupportedTargetType).WithMetadata(map[string]string{"targetType": "android"})
		}
	}
	return nil
}

// RefreshAndroidLaunchHelp updates command help after configuration has been
// loaded, since the package-level commands are constructed before Viper reads
// the user's configuration.
func RefreshAndroidLaunchHelp() {
	for _, cmd := range []*cobra.Command{RunCmd, ReadyCmd, ValidateCmd} {
		setAndroidLaunchFlagVisibility(cmd)
	}
	RunCmd.Use = runCommandUse()
}
