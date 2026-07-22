// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	recipejson "github.com/Arm-Debug/apap-cli/apap-cli/service/clijson/recipe"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/targetlogin"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var InfoCmd = NewInfoCommand(
	client.NewAutostartClient(),
	engine_target.NewDefaultTargetManager(),
	targetlogin.NewDefaultTargetLoginService(),
)

func NewInfoCommand(cc client.ClientConnector, targetService target.TargetManagerService, loginService targetlogin.TargetLoginService) *cobra.Command {
	var target string
	infoCmd := &cobra.Command{
		Use:   "info [recipe name]",
		Short: "Show detailed information about a recipe",
		Long: `Show detailed information about a recipe, including the tools that it requires and the available input parameters.

To run parameter option functions, you must specify a target so that target information can be exposed to the function.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recipeName := args[0]
			return printInfo(cc, targetService, loginService, recipeName, target, cmd.OutOrStdout())
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRecipeSub,
		},
	}
	infoCmd.Flags().StringVar(&target, "target", "", "Specify the target for the specified action.")
	return infoCmd
}

func convertConditionTypeToPrettyString(condType string) string {
	switch condType {
	case "param_is_not_set":
		return "Parameter is not set:"
	case "param_is_set":
		return "Parameter is set:"
	default:
		return condType
	}
}

func prettyPrintConditions(conditions []*apapproto.PlatformSupportRequirementSpec) string {
	out := " (Conditions:\n"
	for _, cond := range conditions {
		out += "  " + convertConditionTypeToPrettyString(cond.Type) + " {"
		j := 0
		for k, v := range cond.Parameters {
			if j > 0 {
				out += ", "
			}
			out += fmt.Sprintf("%s: %s", k, v)
			j++
		}
		out += "}\n"
	}
	out += ")"
	return out
}

func extractPlatformSupportPrettyPrint(support []*apapproto.PlatformSupport, targetSupportMode bool) string {
	out := ""
	if targetSupportMode {
		out = recipejson.ConvertPlatformSupportResultToString(support[0].Result, true)
		if support[0].Result == apapproto.PlatformSupportResult_PLATFORM_CONDITIONALLY_SUPPORTED {
			out += prettyPrintConditions(support[0].ConditionList)
		}
		return out
	}
	// Non-target mode, print all supported platforms
	for _, ps := range support {
		if ps == nil {
			continue
		}
		if ps.Result == apapproto.PlatformSupportResult_PLATFORM_UNSUPPORTED {
			// Skip unsupported platforms in non-target mode
			continue
		}
		// Add a newline if this is not the first entry
		if out != "" {
			out += "\n"
		}
		out += fmt.Sprintf("    - os:\"%s\"  arch:\"%s\"", ps.Platform.Os, ps.Platform.Arch)
		if ps.Result == apapproto.PlatformSupportResult_PLATFORM_CONDITIONALLY_SUPPORTED {
			out += fmt.Sprintf(": %s", recipejson.ConvertPlatformSupportResultToString(ps.Result, true))
		}
		if len(ps.ConditionList) > 0 {
			out += prettyPrintConditions(ps.ConditionList)
		}
	}
	return out
}

func printInfo(cc client.ClientConnector, targetService target.TargetManagerService, loginService targetlogin.TargetLoginService, recipeName string, targetName string, out io.Writer) error {
	targetSupportMode := false
	client, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	parseMsg := &apapproto.ParseRecipeMessage{Name: recipeName}
	if targetName != "" {
		targetSupportMode = true
		tgt, err := targetService.GetTarget(targetName)
		if err != nil {
			return err
		}
		tgtDescription := grpcserver.TargetToProto(tgt)
		if tgtDescription == nil {
			return message.New(message.CliCmdRecipeInfoTargetConversionFailed).WithMetadata(map[string]string{"target": targetName})
		}
		parseMsg.Target = tgtDescription

		if err := loginService.LoginToTarget(context.Background(), tgt, serverconfig.FromViperForBackground()); err != nil {
			return err
		}
	}

	recipe, err := client.ParseRecipe(context.Background(), parseMsg)
	if err != nil {
		return err
	}

	if viper.GetBool("json") {
		return clijson.MarshalJSONCLIResponse(out, recipejson.ConvertRecipeStruct(recipe, targetSupportMode))
	} else {
		fmt.Fprintf(out, "Name: %s\n", recipe.Name)
		fmt.Fprintf(out, "Title: %s\n", recipe.Title)
		fmt.Fprintf(out, "Description: %s\n", recipe.Description)
		fmt.Fprintf(out, "Version: %s\n", recipe.Version)
		printParameterInfo(out, "Parameters", recipe.Parameters)
		if len(recipe.RenderParameters) > 0 {
			printRenderParameterInfo(out, "Render Parameters", recipe.RenderParameters)
		}
		if recipe.PlatformSupport != nil {
			if targetSupportMode {
				fmt.Fprintf(out, "Target Platform: ")
			} else {
				fmt.Fprintf(out, "Supported Platforms:\n")
			}
			fmt.Fprintf(out, "%s\n", extractPlatformSupportPrettyPrint(recipe.PlatformSupport, targetSupportMode))
		}
	}
	return nil
}

func printParameterInfo(out io.Writer, title string, params []*apapproto.ParameterList) {
	fmt.Fprintf(out, "%s:\n", title)
	for _, p := range params {
		switch v := p.Parameter.(type) {
		case *apapproto.ParameterList_SingleSelect:
			fmt.Fprintf(out, "  - %v - single_select\n", v.SingleSelect.Base.GetID())
			fmt.Fprintf(out, "      - Label: %s\n", v.SingleSelect.Base.GetLabel())
			fmt.Fprintf(out, "      - Description: %s\n", v.SingleSelect.Base.GetDescription())
			fmt.Fprintf(out, "      - Default: %v\n", v.SingleSelect.DefaultValue)
			if len(v.SingleSelect.Options) > 0 {
				fmt.Fprintf(out, "      - Options:\n")
				for _, option := range recipejson.ConvertOptionItems(v.SingleSelect.Options) {
					if option.Description != "" {
						fmt.Fprintf(out, "          - %s (%s): %s\n", option.Value, option.Label, option.Description)
					} else {
						fmt.Fprintf(out, "          - %s (%s)\n", option.Value, option.Label)
					}
				}
			}
		case *apapproto.ParameterList_MultiSelect:
			fmt.Fprintf(out, "  - %v - multi_select\n", v.MultiSelect.Base.GetID())
			fmt.Fprintf(out, "      - Label: %s\n", v.MultiSelect.Base.GetLabel())
			fmt.Fprintf(out, "      - Description: %s\n", v.MultiSelect.Base.GetDescription())
			fmt.Fprintf(out, "      - Default: %v\n", v.MultiSelect.DefaultValue)
			if len(v.MultiSelect.Options) > 0 {
				fmt.Fprintf(out, "      - Options:\n")
				for _, option := range recipejson.ConvertOptionItems(v.MultiSelect.Options) {
					if option.Description != "" {
						fmt.Fprintf(out, "          - %s (%s): %s\n", option.Value, option.Label, option.Description)
					} else {
						fmt.Fprintf(out, "          - %s (%s)\n", option.Value, option.Label)
					}
				}
			}
		case *apapproto.ParameterList_Checkbox:
			fmt.Fprintf(out, "  - %v - checkbox\n", v.Checkbox.Base.GetID())
			fmt.Fprintf(out, "      - Label: %s\n", v.Checkbox.Base.GetLabel())
			fmt.Fprintf(out, "      - Description: %s\n", v.Checkbox.Base.GetDescription())
			fmt.Fprintf(out, "      - Default: %v\n", v.Checkbox.DefaultValue)
		case *apapproto.ParameterList_Input:
			fmt.Fprintf(out, "  - %v - input\n", v.Input.Base.GetID())
			fmt.Fprintf(out, "      - Label: %s\n", v.Input.Base.GetLabel())
			fmt.Fprintf(out, "      - Description: %s\n", v.Input.Base.GetDescription())
			fmt.Fprintf(out, "      - Default: %s\n", v.Input.DefaultValue)
		case *apapproto.ParameterList_Radio:
			fmt.Fprintf(out, "  - %v - radio\n", v.Radio.Base.GetID())
			fmt.Fprintf(out, "      - Label: %s\n", v.Radio.Base.GetLabel())
			fmt.Fprintf(out, "      - Description: %s\n", v.Radio.Base.GetDescription())
			fmt.Fprintf(out, "      - Default: %s\n", v.Radio.DefaultValue)
			if len(v.Radio.Options) > 0 {
				fmt.Fprintf(out, "      - Options:\n")
				for _, option := range recipejson.ConvertOptionItems(v.Radio.Options) {
					if option.Description != "" {
						fmt.Fprintf(out, "          - %s (%s): %s\n", option.Value, option.Label, option.Description)
					} else {
						fmt.Fprintf(out, "          - %s (%s)\n", option.Value, option.Label)
					}
				}
			}
		default:
			fmt.Println("Unknown parameter type")
		}
	}
}

func printRenderParameterInfo(out io.Writer, title string, params []*apapproto.RenderParameter) {
	fmt.Fprintf(out, "%s:\n", title)
	for _, p := range params {
		if p == nil {
			continue
		}
		paramType, ok := recipejson.ConvertRenderParameterTypeToString(p.GetType())
		if !ok {
			paramType = "unknown"
		}
		fmt.Fprintf(out, "  - %v - %s\n", p.GetId(), paramType)
		fmt.Fprintf(out, "      - Array: %v\n", p.GetIsArray())
	}
}
