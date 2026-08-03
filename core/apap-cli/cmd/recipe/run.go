// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/recipe"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	engine_run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

var RunCmd = NewRunCommand(
	client.NewAutostartClient(),
	recipeparser.FileRecipeReader{},
	recipe.RecipeRunner{},
	engine_target.NewDefaultTargetManager(),
	targetlogin.NewDefaultTargetLoginService(),
)

func NewRunCommand(cc client.ClientConnector,
	readerService recipeparser.RecipeReader,
	runnerService recipe.Runner,
	targetService target.TargetManagerService,
	loginService targetlogin.TargetLoginService) *cobra.Command {
	var params []string
	var envs []string
	var workingDir string
	var deployTools bool
	var forceDeployTools bool
	var pid = NewCountedInt64Flag(-2)
	var workload string
	var androidPackage string
	var androidActivity string
	var timeout uint32
	var systemWide = false
	var sourceCodePaths string
	var target string
	var noCleanup bool
	var useShell bool
	var detachBackgroundTransfers bool

	runCmd := &cobra.Command{
		Use:   runCommandUse(),
		Short: "Run a recipe on a target.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRecipeSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAndroidLaunchFeature(cmd); err != nil {
				return err
			}

			deploymentType, err := recipe.DeploymentTypeFromFlags(deployTools, forceDeployTools)
			if err != nil {
				return err
			}

			if pid.Repeated() {
				return message.New(message.CliCmdRecipeCommonDuplicateSingularFlags).WithMetadata(map[string]string{"flag": "--pid"})
			}

			workloadCtx, err := recipe.ValidateWorkloadFlags(cmd.Flags(), int32(pid.Value()), workload, recipe.RunCommandType) // #nosec G115
			if err != nil {
				return err
			}
			workloadCtx.AndroidPackageName = androidPackage
			workloadCtx.AndroidActivityName = androidActivity

			// Must specify one workload mode.
			if !cmd.Flags().Changed("pid") && !cmd.Flags().Changed("workload") && !cmd.Flags().Changed("system-wide") && !workloadCtx.AndroidLaunch {
				return message.New(message.CliCmdRecipeRunNoWorkloadSpecified)
			}

			if cmd.Flags().Changed("use-shell") && !cmd.Flags().Changed("workload") {
				return message.New(message.CliCmdRecipeCommonNonLaunchUseShell)
			}
			workloadCtx.UseShell = useShell

			if cmd.Flags().Changed("env") && !cmd.Flags().Changed("workload") {
				return message.New(message.CliCmdRecipeRunNonLaunchEnvVars)
			}

			if cmd.Flags().Changed("working-dir") && !cmd.Flags().Changed("workload") {
				return message.New(message.CliCmdRecipeCommonNonLaunchWorkingDir)
			}
			workloadCtx.WorkingDir = workingDir

			envVars, err := recipe.ParseEnvVars(envs)
			if err != nil {
				return err
			}
			workloadCtx.Environment = envVars

			workloadCtx.Timeout = timeout

			hostSourceCodePath := engine_run.HostSourceCodePath{}
			if cmd.Flags().Changed("source") {
				// Split the source code paths by the OS path list separator.
				hostSourceCodePath.Paths = utils.FilterPaths(strings.Split(sourceCodePaths, string(os.PathListSeparator)))
			}

			return recipe.ProcessRunRecipe(
				cc,
				readerService,
				runnerService,
				args[0],
				workloadCtx,
				params,
				cmd.OutOrStdout(),
				targetService,
				loginService,
				deploymentType,
				hostSourceCodePath,
				noCleanup,
				detachBackgroundTransfers,
				target)
		},
	}
	runCmd.Flags().StringArrayVar(&params, "param", nil, "Changes the default recipe parameters")
	runCmd.Flags().StringArrayVar(&envs, "env", nil, "Specifies the environment variables to expose to the launched workload (repeat for multiple variables)")
	runCmd.Flags().StringVar(&workingDir, "working-dir", "", "Specifies the working directory for the launch workload (defaults to your home directory on the target)")
	runCmd.Flags().BoolVar(&useShell, "use-shell", false, "Runs the workload through the default shell on the target")
	runCmd.Flags().BoolVar(&deployTools, "deploy-tools", false, "Automatically deploys any missing or out of date tools to the target")
	runCmd.Flags().BoolVar(&forceDeployTools, "deploy-tools-force", false, "Automatically deploys any missing or out of date tools to the target, removing any existing deployment")
	runCmd.Flags().Var(&pid, "pid", "Specifies the process ID to profile on the target")
	runCmd.Flags().StringVar(&workload, "workload", "", "Specifies the workload to run on the target")
	runCmd.Flags().StringVar(&androidPackage, "android-package", "", "Specifies the Android package to launch on the target")
	runCmd.Flags().StringVar(&androidActivity, "android-activity", "", "Specifies the Android activity to launch on the target")
	runCmd.Flags().Uint32Var(&timeout, "timeout", 0, "Specifies the number of seconds to profile the workload for")
	runCmd.Flags().BoolVar(&systemWide, "system-wide", false, "Specifies that system-wide profiling should be used")
	runCmd.Flags().StringVar(&sourceCodePaths, "source", "", "Specifies the host-based source code path(s) that will be used for source code attribution")
	runCmd.Flags().StringVar(&target, "target", "", "Specify the target for the specified action.")
	runCmd.Flags().BoolVar(&noCleanup, "no-cleanup", false, "If set, the working area on the target will not be removed after the run completes")
	runCmd.Flags().BoolVar(&detachBackgroundTransfers, "background-transfer", false, "Return after required transfers complete while background transfers continue in the engine")
	setAndroidLaunchFlagVisibility(runCmd)

	return runCmd
}
