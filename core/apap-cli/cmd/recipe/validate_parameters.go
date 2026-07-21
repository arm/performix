// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	recipe_service "github.com/Arm-Debug/apap-cli/apap-cli/service/recipe"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var ValidateCmd = NewValidateCommand(
	client.NewAutostartClient(),
	engine_target.NewDefaultTargetManager(),
	targetlogin.NewDefaultTargetLoginService(),
)

// ParamListToMap translates parameters of format "param=value" into a map[string]*structpb.Value.
func ParamListToMap(cliParams []string) (map[string]*structpb.Value, error) {
	paramMap := map[string]*structpb.Value{}
	for _, cliParam := range cliParams {
		cliParamName, cliParamValue, _ := strings.Cut(cliParam, "=")
		_, alreadyExists := paramMap[cliParamName]
		if alreadyExists {
			return nil, errors.New("duplicate parameter: " + cliParamName)
		}
		paramMap[cliParamName] = structpb.NewStringValue(cliParamValue)
	}
	return paramMap, nil
}

func NewValidateCommand(cc client.ClientConnector, targetService engine_target.TargetManagerService, loginService targetlogin.TargetLoginService) *cobra.Command {
	var params []string
	var workingDir string
	var useShell bool
	var workload string
	var androidPackage string
	var androidActivity string
	var target string
	readyCmd := &cobra.Command{
		Use:   "validate-parameters [recipe] <workload>",
		Short: "Validate the input parameters conform to the recipe specification.",
		Long:  "Validate the input parameters conform to the recipe specification before running the recipe.",
		Args:  cobra.RangeArgs(1, 3),
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRecipeSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAndroidLaunchFeature(cmd); err != nil {
				return err
			}

			androidLaunchChanged, err := recipe_service.ValidateAndroidLaunchFlags(cmd.Flags())
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("workload") && androidLaunchChanged {
				return message.New(message.CliCmdValidationMutuallyExclusiveFlags).WithMetadata(map[string]string{
					"cmd":   "recipe validate-parameters",
					"flag1": "--workload",
					"flag2": "--android-package/--android-activity",
				})
			}
			if cmd.Flags().Changed("use-shell") && !cmd.Flags().Changed("workload") {
				return message.New(message.CliCmdRecipeCommonNonLaunchUseShell)
			}
			if cmd.Flags().Changed("working-dir") && !cmd.Flags().Changed("workload") {
				return message.New(message.CliCmdRecipeCommonNonLaunchWorkingDir)
			}
			return validateRecipeParameters(cmd, args[0], params, workload, androidLaunchChanged, androidPackage, androidActivity, workingDir, useShell, cc, targetService, loginService, target)
		},
	}

	readyCmd.Flags().StringArrayVar(&params, "param", nil, "Changes the default recipe parameters")
	readyCmd.Flags().StringVar(&workingDir, "working-dir", "", "Specifies the working directory for the launch workload (defaults to your home directory on the target)")
	readyCmd.Flags().BoolVar(&useShell, "use-shell", false, "Runs the workload through the default shell on the target")
	readyCmd.Flags().StringVar(&workload, "workload", "", "The workload to validate parameters for")
	readyCmd.Flags().StringVar(&androidPackage, "android-package", "", "The Android package to validate parameters for")
	readyCmd.Flags().StringVar(&androidActivity, "android-activity", "", "The Android activity to validate parameters for")
	readyCmd.Flags().StringVar(&target, "target", "", "Specify the target for the specified action.")
	setAndroidLaunchFlagVisibility(readyCmd)
	return readyCmd
}

func validateRecipeParameters(cmd *cobra.Command, recipeName string, params []string, workload string, androidLaunch bool, androidPackage string, androidActivity string, workingDir string, useShell bool, cc client.ClientConnector, targetService engine_target.TargetManagerService, loginService targetlogin.TargetLoginService, target string) error {
	paramMap, err := ParamListToMap(params)
	if err != nil {
		return err
	}

	req := apapproto.RecipeValidateParametersRequest{
		RecipeName: recipeName,
		Parameters: paramMap,
	}

	// Pass --workload only if set
	if cmd.Flags().Changed("workload") {
		req.Workload = &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_LaunchWorkload{
				LaunchWorkload: &apapproto.LaunchWorkload{Command: workload, WorkingDir: workingDir, UseShell: useShell},
			},
		}
	} else if androidLaunch {
		req.Workload = &apapproto.RecipeWorkload{
			SpecificWorkload: &apapproto.RecipeWorkload_AndroidLaunchWorkload{
				AndroidLaunchWorkload: &apapproto.AndroidLaunchWorkload{
					PackageName:  androidPackage,
					ActivityName: androidActivity,
				},
			},
		}
	}

	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	targetName := target
	if targetName != "" {
		req.TargetName = &targetName
		tgt, err := targetService.GetTarget(targetName)
		if err != nil {
			return err
		}
		tgtDescription := grpcserver.TargetToProto(tgt)
		if tgtDescription == nil {
			return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("failed to convert target description to proto: %v", tgt.String()))
		}
		req.Target = tgtDescription

		if err := loginService.LoginToTarget(context.Background(), tgt, serverconfig.FromViperForBackground()); err != nil {
			return err
		}
	}

	resp, err := client.RecipeValidateParameters(context.Background(), &req)
	if err != nil {
		return err
	}

	jsonOut := viper.GetBool("json")
	out := cmd.OutOrStdout()
	if jsonOut {
		return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), clijson.ValidateParamsResponseToJSON(resp))
	} else {
		if len(resp.Messages) > 0 {
			clijson.HandleCLIError(out, message.New(message.CliCmdRecipeValidateParametersValidationIssues))
		}
		printValidationErrors(out, resp.Messages)
	}
	return nil
}

func printValidationErrors(out io.Writer, messages []*apapproto.ParameterValidationResult) {
	indent := 2
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		err := message.ReconstructFromChain(msg.Message)

		fmt.Fprintln(out)
		clijson.HandlePlaintextCLIErrorWithIndent(out, err, indent)
	}
}
