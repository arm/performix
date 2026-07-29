// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/recipe"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

var ReadyCmd = NewReadyCommand(
	client.NewAutostartClient(),
	recipeparser.FileRecipeReader{},
	recipe.RecipeReady{},
	engine_target.NewDefaultTargetManager(),
	targetlogin.NewDefaultTargetLoginService(),
)

func NewReadyCommand(cc client.ClientConnector,
	readerService recipeparser.RecipeReader,
	readyService recipe.Ready,
	targetService engine_target.TargetManagerService,
	loginService targetlogin.TargetLoginService) *cobra.Command {
	var params []string
	var workingDir string
	var useShell bool
	var pid = NewCountedInt64Flag(-2)
	var workload string
	var androidPackage string
	var androidActivity string
	var systemWide bool
	var target string

	readyCmd := &cobra.Command{
		Use:   "ready [recipe]",
		Short: "Check if all the dependencies for a recipe are installed on the target.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRecipeSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAndroidLaunchFeature(cmd); err != nil {
				return err
			}

			recipeName := args[0]

			if pid.Repeated() {
				return message.New(message.CliCmdRecipeCommonDuplicateSingularFlags).WithMetadata(map[string]string{"flag": "--pid"})
			}

			// recipe ready might want to support --timeout
			// https://jira.arm.com/browse/APAP-660
			workloadCtx, err := recipe.ValidateWorkloadFlags(cmd.Flags(), int32(pid.Value()), workload, recipe.ReadyCommandType) // #nosec G115
			if err != nil {
				return err
			}
			workloadCtx.AndroidPackageName = androidPackage
			workloadCtx.AndroidActivityName = androidActivity

			// Must specify one workload mode.
			if !cmd.Flags().Changed("pid") && !cmd.Flags().Changed("workload") && !cmd.Flags().Changed("system-wide") && !workloadCtx.AndroidLaunch {
				return message.New(message.CliCmdRecipeReadyNoWorkloadSpecified)
			}

			if cmd.Flags().Changed("use-shell") && !cmd.Flags().Changed("workload") {
				return message.New(message.CliCmdRecipeCommonNonLaunchUseShell)
			}
			workloadCtx.UseShell = useShell

			if cmd.Flags().Changed("working-dir") && !cmd.Flags().Changed("workload") {
				return message.New(message.CliCmdRecipeCommonNonLaunchWorkingDir)
			}
			workloadCtx.WorkingDir = workingDir

			return readyRecipe(cc, readerService, readyService, recipeName, workloadCtx, params, cmd.OutOrStdout(), targetService, loginService, target)
		},
	}

	// TODO - support --deploy-tools
	readyCmd.Flags().StringArrayVar(&params, "param", nil, "Changes the default recipe parameters")
	readyCmd.Flags().StringVar(&workingDir, "working-dir", "", "Specifies the working directory for the launch workload (defaults to your home directory on the target)")
	readyCmd.Flags().BoolVar(&useShell, "use-shell", false, "Runs the workload through the default shell on the target")
	readyCmd.Flags().Var(&pid, "pid", "Specifies the process ID to profile on the target")
	readyCmd.Flags().StringVar(&workload, "workload", "", "Check that the workload at the provided path can be run on the target")
	readyCmd.Flags().StringVar(&androidPackage, "android-package", "", "Check that the Android package can be launched on the target")
	readyCmd.Flags().StringVar(&androidActivity, "android-activity", "", "Check that the Android activity can be launched on the target")
	readyCmd.Flags().BoolVar(&systemWide, "system-wide", false, "Check that system-wide profiling can be run on the target")
	readyCmd.Flags().StringVar(&target, "target", "", "Specify the target for the specified action.")
	setAndroidLaunchFlagVisibility(readyCmd)
	return readyCmd
}

func readyRecipe(cc client.ClientConnector,
	readerService recipeparser.RecipeReader,
	readyService recipe.Ready,
	recipeName string,
	workload recipe.Workload,
	params []string,
	out io.Writer,
	targetService engine_target.TargetManagerService,
	loginService targetlogin.TargetLoginService,
	target string) error {
	return recipe.ProcessReadyRecipe(cc, readerService, readyService, recipeName, workload, params, out, targetService, loginService, target)
}
