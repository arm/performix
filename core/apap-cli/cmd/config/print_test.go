// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/config"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

func TestPrintCmd(t *testing.T) {
	var rootFlagVar, childFlagVar string

	viper.Reset()
	t.Cleanup(viper.Reset)

	newRootCmd := &cobra.Command{
		Use: "root",
	}
	newRootCmd.Flags().StringVar(&rootFlagVar, "log-file", "stdout", "Root command with special case for log-file")
	config.ViperBindPFlag(newRootCmd, "log-file", false)

	newChildCmd := &cobra.Command{
		Use: "child",
	}
	newChildCmd.Flags().StringVar(&childFlagVar, "a-flag", "./file.yaml", "A child command, the description of which should be wrapped in a table.")
	config.ViperBindPFlag(newChildCmd, "a-flag", false)

	newRootCmd.AddCommand(newChildCmd)

	t.Run("test layout and contents of the table output", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}

		printCmd := config.NewPrintCmd()
		printCmd.SetOut(cmdBuf)
		newRootCmd.AddCommand(printCmd)
		t.Cleanup(func() { newRootCmd.RemoveCommand(printCmd) })
		newRootCmd.SetArgs([]string{"print"})
		err := newRootCmd.Execute()

		assert.NoError(t, err)

		expected := `┌────────────┬──────────────────────┬──────────────────────┬────────────────────┬─────────────────────────────────────────────────┐
│ COMMAND    │ CONFIG FILE VARIABLE │ ENVIRONMENT VARIABLE │ CURRENT VALUE      │ DESCRIPTION                                     │
├────────────┼──────────────────────┼──────────────────────┼────────────────────┼─────────────────────────────────────────────────┤
│ root <all> │ log-file             │ APXD_LOG_FILE        │ <depends on usage> │ Root command with special case for log-file     │
├────────────┼──────────────────────┼──────────────────────┼────────────────────┼─────────────────────────────────────────────────┤
│ root child │ a-flag               │ APXD_A_FLAG          │ ./file.yaml        │ A child command, the description of which       │
│            │                      │                      │                    │ should be wrapped in a table.                   │
└────────────┴──────────────────────┴──────────────────────┴────────────────────┴─────────────────────────────────────────────────┘

Note: You must restart the daemon for some configuration changes to take effect.
`
		assert.Equal(t, expected, cmdBuf.String())
	})

	t.Run("test layout and contents of the narrow list output", func(t *testing.T) {
		cmdBuf := &bytes.Buffer{}

		printCmd := config.NewPrintCmd()
		printCmd.SetOut(cmdBuf)
		newRootCmd.AddCommand(printCmd)
		t.Cleanup(func() { newRootCmd.RemoveCommand(printCmd) })
		newRootCmd.SetArgs([]string{"print", "--list"})
		err := newRootCmd.Execute()

		assert.NoError(t, err)

		expected := `Command: root <all>
Config File Variable: log-file
Environment Variable: APXD_LOG_FILE
Current Value: <depends on usage>
Description: Root command with special case for log-file

Command: root child
Config File Variable: a-flag
Environment Variable: APXD_A_FLAG
Current Value: ./file.yaml
Description: A child command, the description of which should be wrapped in a table.

Note: You must restart the daemon for some configuration changes to take effect.
`
		assert.Equal(t, expected, cmdBuf.String())
	})
}

func TestPrintIncludeAllViperKeys(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	expected := map[string]string{}

	// Given
	rootCmd := &cobra.Command{Use: "apx"}
	utils.SetPersistentFlags(rootCmd)
	rootCmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		expected[flag.Name] = flag.DefValue
	})

	// i. Config via `ViperBindPFlag`
	// e.g., CLI flags like `--server-port`
	expected["test-local"] = "local value"
	childCmd := &cobra.Command{Use: "child"}
	childCmd.Flags().String("test-local", expected["test-local"], "A local APX flag")
	config.ViperBindPFlag(childCmd, "test-local", false)
	rootCmd.AddCommand(childCmd)

	// ii. Config via `SetDefault`
	// e.g., default values like feature flags
	expected["test-default"] = "false"
	viper.SetDefault("test-default", false)

	// iii. Config via YAML file
	// e.g., things defined in the config file
	expected["test-yaml"] = "yaml value"
	viper.SetConfigType("yaml")
	require.NoError(t, viper.ReadConfig(strings.NewReader("test-yaml: "+expected["test-yaml"])))

	// iv. Config via `AutomaticEnv`
	// e.g., env. overrides like `APXD_SERVER_PORT=8081`
	viper.SetDefault("env-var", false)
	viper.SetEnvPrefix(terminology.GetEnvVarPrefix())
	viper.SetEnvKeyReplacer(util.EnvVarReplacer)
	viper.AutomaticEnv()
	t.Setenv(terminology.GetEnvVarPrefix()+"_ENV_VAR", "true")
	expected["env-var"] = "true"

	// When
	configCmd := &cobra.Command{Use: "config"}
	configCmd.AddCommand(config.NewPrintCmd())
	rootCmd.AddCommand(configCmd)

	var output bytes.Buffer
	rootCmd.SetOut(&output)
	rootCmd.SetArgs([]string{"config", "print"})

	require.NoError(t, rootCmd.Execute())

	// Then
	for key, value := range expected {
		envName := terminology.GetEnvVarPrefix() + "_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		assert.Contains(t, output.String(), key)
		assert.Contains(t, output.String(), envName)
		assert.Contains(t, output.String(), value)
	}
}
