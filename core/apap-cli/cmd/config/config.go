// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package config

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
)

type CommandFlag struct {
	CobraCommand *cobra.Command
	FlagName     string
}

var configurableOptions []CommandFlag

func ViperBindPFlag(cobraCommand *cobra.Command, flagName string, persistent bool) {
	var flag *pflag.Flag
	if persistent {
		flag = cobraCommand.PersistentFlags().Lookup(flagName)
	} else {
		flag = cobraCommand.Flags().Lookup(flagName)
	}
	err := viper.BindPFlag(flagName, flag)

	if err == nil {
		configurableOptions = append(configurableOptions, CommandFlag{CobraCommand: cobraCommand, FlagName: flagName})
	} else {
		log.Errorf("Unable to bind configuration for `%s`, values set using files or env vars might be incorrect.", flagName)
	}
}

var RootCmd = &cobra.Command{
	Use:   "config",
	Short: "View configuration information.",
	Long:  `This command provides information on configuration options and current values.`,
	Annotations: map[string]string{
		grouping.GroupAnnotation: grouping.GroupConfig,
	},
}

func init() {
	RootCmd.AddCommand(PrintCmd)
}
