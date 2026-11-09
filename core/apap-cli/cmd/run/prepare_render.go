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
)

var PrepareRenderCmd = NewPrepareRenderCmd(client.NewAutostartClient(), run.RenderService{})

type prepareRenderCliParams struct {
	Runs           []string
	Visualizations []string
	recipeSelectionParams
	renderParamsList []string
}

type recipeSelectionParams struct {
	RecipeName string
}

func (p recipeSelectionParams) ToPreparationParams() run.RenderPreparationParams {
	return run.RenderPreparationParams{OverrideRecipeName: p.RecipeName}
}

func addRecipeSelectionFlags(cliParams *recipeSelectionParams, cmd *cobra.Command) {
	cmd.Flags().StringVar(&cliParams.RecipeName, "recipe", "", "Recipe to use for rendering. If unset, use the installed recipe with the name from the latest run.")
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

	params := cliParams.ToPreparationParams()

	params.RenderParameterValues, err = recipe.ParamListToMap(cliParams.renderParamsList)
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
