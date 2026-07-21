// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/cliversion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/commands"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/config"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/daemon"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/help"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mcp"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/recipe"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/render"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/run"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/ssh"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/support"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/resolvepath"
	"github.com/Arm-Debug/apap-cli/apap-cli/service"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

var cfgFile string

var initConfigOnce sync.Once

func defaultConfigFilePath() string {
	dir, err := userdirs.ConfigDir()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(dir, fmt.Sprintf("%v.yml", terminology.GetProductBinaryName()))
}

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use: terminology.GetProductBinaryName(),
		// Stops failing RunE from printing command usage a.k.a. --help
		SilenceUsage: true,
		Short:        fmt.Sprintf("%v CLI", terminology.GetProductFullName()),
		Long:         fmt.Sprintf(`A command-line interface and gRPC server daemon for %v.`, terminology.GetProductFullName()),
	}

	cobra.OnInitialize(initConfig)

	// Define your flags and configuration settings.
	configPath := defaultConfigFilePath()
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", configPath, "Path to config file")

	utils.SetPersistentFlags(rootCmd)

	rootCmd.AddCommand(
		cliversion.NewVersionCmd(client.NewAutostartClient(), service.VersionService{}, versions.GetVersion()),
	)

	rootCmd.AddCommand(daemon.RootCmd)
	rootCmd.AddCommand(config.RootCmd)
	rootCmd.AddCommand(commands.RootCmd)
	rootCmd.AddCommand(recipe.RootCmd)
	rootCmd.AddCommand(run.RootCmd)
	rootCmd.AddCommand(target.RootCmd)
	rootCmd.AddCommand(render.RootCmd)
	rootCmd.AddCommand(ssh.RootCmd)
	rootCmd.AddCommand(support.RootCmd)
	rootCmd.AddCommand(mcp.RootCmd)

	// Shell completion command
	rootCmd.AddCommand(completion.RootCompletionCmd)

	rootCmd.SetHelpCommand(help.NewHelpCmd(rootCmd))
	rootCmd.SetUsageTemplate(grouping.UsageMessage())
	rootCmd.SetHelpFunc(configAwareHelpFunc(rootCmd.HelpFunc()))
	rootCmd.SilenceErrors = true

	return rootCmd
}

func configAwareHelpFunc(next func(*cobra.Command, []string)) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		initConfig()
		target.RefreshTargetAddHelp()
		target.RefreshTargetUpdateHelp()
		target.RefreshTargetListHelp()
		recipe.RefreshAndroidLaunchHelp()
		next(cmd, args)
	}
}

// determineExitCodeForError figures out the correct exit code for the program
// based on the error type. Plain go errors and Messages of type Error are
// considered errors and exit with code 1. Messages of type Info or Warning
// exit with code 0.
func determineExitCodeForError(err error) int {
	if err == nil {
		return 0
	}

	if m := message.IsMessage(err); m != nil && m.IsInfoOrWarning() {
		// Message of type Info or Warning
		return 0
	}

	// Message of type Error or plain go error
	return 1
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(rootCmd *cobra.Command) {
	if err := rootCmd.Execute(); err != nil {
		clijson.HandleCLIError(rootCmd.OutOrStdout(), err)
		code := determineExitCodeForError(err)
		os.Exit(code)
	}
}

// initConfig reads in config file and ENV variables if set.
// It is guarded by a sync.Once so it runs at most once per process, even when
// called from both cobra.OnInitialize and configAwareHelpFunc.
func initConfig() {
	initConfigOnce.Do(func() {
		viper.SetConfigFile(resolvepath.ResolvePath(cfgFile))

		// Defaults to be used when given setting is not specified
		viper.SetDefault("jobs", serverconfig.Jobs)
		viper.SetDefault("server-hostname", serverconfig.DefaultServerHostname)
		viper.SetDefault("server-port", serverconfig.DefaultServerPort)
		viper.SetDefault("auth-port", serverconfig.DefaultAuthPort)
		viper.SetDefault("http-port", serverconfig.DefaultHTTPPort)
		viper.SetDefault("http-chunk-bytes", serverconfig.DefaultHTTPChunkBytes)
		viper.SetDefault("log-level", serverconfig.DefaultLogLevel)
		viper.SetDefault("log-file", serverconfig.DefaultLogFile)
		viper.SetDefault("data-dir", serverconfig.DefaultDataDir)
		viper.SetDefault("source-tools-dir", serverconfig.DefaultSrcToolsDirectory)
		viper.SetDefault("deployment-tools-dir", serverconfig.DefaultToolsDeploymentDirectory)
		viper.SetDefault("enable-on-demand-privilege", serverconfig.DefaultEnableOnDemandPrivilege)
		viper.SetDefault("agent-use-group-controller", serverconfig.DefaultAgentUseGroupController)
		viper.SetDefault("enable-full-capture-support", serverconfig.DefaultEnableFullCaptureSupport)
		viper.SetDefault("enable-rerendering", serverconfig.DefaultEnableRerendering)
		viper.SetDefault("enable-experimental-recipes", serverconfig.DefaultEnableExperimentalRecipes)
		viper.SetDefault("enable-secondary-run-paths", serverconfig.DefaultEnableSecondaryRunPaths)
		viper.SetDefault("enable-transfer-manager", serverconfig.DefaultEnableTransferManager)
		viper.SetDefault(serverconfig.EnableAndroidTargetsConfigKey, serverconfig.DefaultEnableAndroidTargets)
		viper.SetDefault("enable-render-db-sandbox", serverconfig.DefaultEnableRenderDBSandbox)
		viper.SetDefault("enable-neoprof-timeline", serverconfig.DefaultEnableNeoprofTimeline)

		// read in environment variables that match
		viper.AutomaticEnv()

		// Replace hyphens in command line option with underscores in env vars
		viper.SetEnvKeyReplacer(util.EnvVarReplacer)

		// Namespace env vars
		viper.SetEnvPrefix(terminology.GetEnvVarPrefix())

		// If a config file is found, read it in.
		if err := viper.ReadInConfig(); err == nil {
			log.Debugf("Using config file: %s", viper.ConfigFileUsed())
		}
	})
}
