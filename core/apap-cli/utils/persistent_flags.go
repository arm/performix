// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/config"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
)

func SetPersistentFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String("adb-path", serverconfig.DefaultADBPath, "Path to the Android Debug Bridge (adb) executable.")
	config.ViperBindPFlag(cmd, "adb-path", true)
	setADBPathFlagVisibility(cmd)

	cmd.PersistentFlags().StringP("server-hostname", "s", serverconfig.DefaultServerHostname, "The gRPC host name for communication between the client (CLI) and the server (daemon).")
	config.ViperBindPFlag(cmd, "server-hostname", true)

	cmd.PersistentFlags().IntP("server-port", "p", serverconfig.DefaultServerPort, "The gRPC port number for communication between the client (CLI) and the server (daemon).")
	config.ViperBindPFlag(cmd, "server-port", true)

	cmd.PersistentFlags().Int("auth-port", serverconfig.DefaultAuthPort, "The gRPC port number used for authentication between the client (CLI) and the server (daemon).")
	config.ViperBindPFlag(cmd, "auth-port", true)

	cmd.PersistentFlags().Bool("json", false, "Request the output in JSON format.")
	config.ViperBindPFlag(cmd, "json", true)
}

func RefreshPersistentFlagsHelp(cmd *cobra.Command) {
	setADBPathFlagVisibility(cmd)
}

func setADBPathFlagVisibility(cmd *cobra.Command) {
	if flag := cmd.PersistentFlags().Lookup("adb-path"); flag != nil {
		flag.Hidden = !viper.GetBool(serverconfig.EnableAndroidTargetsConfigKey)
	}
}
