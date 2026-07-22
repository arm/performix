// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/recipe"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var PrepareRenderCmd = NewPrepareRenderCmd(client.NewAutostartClient(), run.RenderService{})

type prepareRenderCliParams struct {
	Runs           []string
	Visualizations []string
	recipeSelectionParams
	renderParamsList        []string
	visualizationParamsList []string
}

type recipeSelectionParams struct {
	RecipeSelectionPolicyName string
	RecipeName                string
}

func (p recipeSelectionParams) ToPreparationParams() (run.RenderPreparationParams, error) {
	params := run.RenderPreparationParams{OverrideRecipeName: p.RecipeName}
	if p.RecipeSelectionPolicyName != "" {
		policy, err := run.ParseRecipeSelectionPolicy(p.RecipeSelectionPolicyName)
		if err != nil {
			return params, err
		}
		if policy == apapproto.RecipeSelectionPolicyType_OVERRIDE_BY_NAME && p.RecipeName == "" {
			return params, message.New(message.CliCmdRunPrepareRenderNoOverrideRecipe)
		}
		params.RecipeSelectionPolicy = &policy
	} else {
		if p.RecipeName != "" {
			params.RecipeSelectionPolicy = util.Ptr(apapproto.RecipeSelectionPolicyType_OVERRIDE_BY_NAME)
		}
	}
	return params, nil
}

func addRecipeSelectionFlags(cliParams *recipeSelectionParams, cmd *cobra.Command) {
	cmd.Flags().StringVar(&cliParams.RecipeName, "recipe", "", "Recipe to call; if unset, the recipe code is loaded from the on-disk run data. The latest run is used.")
	cmd.Flags().StringVar(&cliParams.RecipeSelectionPolicyName, "recipe-selection-policy", "", "Sets the policy for how to choose which recipe to invoke. If not supplied, and no --recipe is supplied, the engine will use a default policy. If no policy is supplied but --recipe is supplied, the policy is set to override-by-name. If a policy is explicitly supplied, that policy will be used. "+run.RecipeSelectionPolicyHelp())
}

func NewPrepareRenderCmd(cc client.ClientConnector, preparer run.RenderPreparer) *cobra.Command {
	cliParams := prepareRenderCliParams{}

	cmd := &cobra.Command{
		Use:   "prepare-render run_id [run_id...] [--visualization ID] [--recipe NAME]",
		Short: "Primitive to prepare renderers and visualizations for a previous run without invoking them.",
		Long: "This command fetches renderer and visualization information determined by a run " +
			"and outputs the preparation results. It does not invoke rendering.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.Runs = args
			return prepareRender(cc, preparer, &cliParams, cmd.OutOrStdout())
		},
	}

	cmd.Args = cobra.MinimumNArgs(1)
	cmd.Flags().StringArrayVar(&cliParams.Visualizations, "visualization", []string{}, "Visualization IDs to include; if omitted, all visualizations will be included.")
	cmd.Flags().StringArrayVar(&cliParams.renderParamsList, "param", nil, "Render parameter value in key=value form. Repeatable. Duplicate keys are not permitted.")
	cmd.Flags().StringArrayVar(&cliParams.visualizationParamsList, "visualization-param", nil, "Visualization parameter value in key=value form. Repeatable. Duplicate keys are not permitted.")
	addRecipeSelectionFlags(&cliParams.recipeSelectionParams, cmd)
	cmd.ValidArgsFunction = completion.CompleteTargetNames

	return cmd
}

func prepareRender(cc client.ClientConnector, preparer run.RenderPreparer, cliParams *prepareRenderCliParams, out io.Writer) error {
	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	content := marshalContentSelection(cliParams.Runs)

	params, err := cliParams.ToPreparationParams()
	if err != nil {
		return err
	}

	params.RenderParameterValues, err = recipe.ParamListToMap(cliParams.renderParamsList)
	if err != nil {
		return err
	}
	params.VisualizationParameterValues, err = recipe.ParamListToMap(cliParams.visualizationParamsList)
	if err != nil {
		return err
	}

	preparation, err := preparer.PrepareRender(connector, content, params)
	if err != nil {
		return err
	}

	filtered, err := filterPreparedRender(cliParams.Visualizations, preparation)
	if err != nil {
		return err
	}

	jsonOutput := clijson.PrepareRenderResponseToJSON(filtered)

	if err = clijson.MarshalJSONCLIResponse(out, jsonOutput); err != nil {
		return err
	}

	// Print warning message below in non-JSON mode
	if !viper.GetBool("json") {
		reconstructedWarning := message.ReconstructFromChain(filtered.CompatibilityWarning)
		if reconstructedWarning != nil {
			fmt.Fprintln(out)
			clijson.HandleCLIError(out, reconstructedWarning)
		}
	}
	return nil
}
