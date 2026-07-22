// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
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

var RenderCmd = NewRenderCmd(client.NewAutostartClient(), run.RenderService{}, run.RenderService{})

// Assume that we only want to see widgets that are data visualizations -
// in the normal sense - and that these can be determined from the placement.
const visualizationsPlacement = "visualizations"

type renderCliParams struct {
	Runs           []string
	Visualizations []string
	recipeSelectionParams
	renderParamsList []string
}

func NewRenderCmd(cc client.ClientConnector, preparer run.RenderPreparer, invoker run.RenderInvoker) *cobra.Command {
	cliParams := renderCliParams{}

	cmd := &cobra.Command{
		Use:   "render [run_id...]",
		Short: "Start a render session for a previous run.",
		Long: "Determine renderers and visualizations invoked by the recipe, as if via prepare-render, and then invoke " +
			"renderers, as if by invoke-render.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.Runs = args

			return render(cc, preparer, invoker, &cliParams, cmd.OutOrStdout())
		},
	}

	cmd.Args = cobra.MinimumNArgs(1)
	cmd.Flags().StringArrayVar(&cliParams.renderParamsList, "param", nil, "Render parameter value in key=value form. Repeatable. Duplicate keys are not permitted.")
	addRecipeSelectionFlags(&cliParams.recipeSelectionParams, cmd)

	cmd.ValidArgsFunction = completion.CompleteRunIDs
	return cmd
}

func filterPreparedRender(visualizations []string, preparation *apapproto.PrepareRenderResponse) (*apapproto.PrepareRenderResponse, error) {
	if !needToFilter(visualizations, preparation.Visualizations) {
		return preparation, nil
	}

	vizSet := make(map[string]struct{})
	for _, id := range visualizations {
		vizSet[id] = struct{}{}
	}

	// Track which requested IDs we actually matched, ignoring
	// widgets that aren't traditional data visualizations
	matchedVizIDs := make(map[string]struct{})
	var filteredViz []*apapproto.VisualizationConfig
	usedRenderers := make(map[string]struct{})

	shouldInclude := func(viz *apapproto.VisualizationConfig) bool {
		if !isDataVisualization(viz) {
			return false
		} else if len(visualizations) == 0 {
			return true
		} else {
			_, ok := vizSet[viz.Id.GetValue()]
			return ok
		}
	}

	for _, viz := range preparation.Visualizations {
		vizID := viz.Id.GetValue()
		if shouldInclude(viz) {
			filteredViz = append(filteredViz, viz)
			matchedVizIDs[vizID] = struct{}{}
			if viz.RendererId != nil {
				usedRenderers[viz.RendererId.GetValue()] = struct{}{}
			}
		}
	}

	// After scanning, check if any requested IDs were missing
	for id := range vizSet {
		if _, ok := matchedVizIDs[id]; !ok {
			metadata := map[string]string{
				"id": id,
			}
			return nil, message.New(message.CliCmdRunRenderVisualizationNotFound).WithMetadata(metadata)
		}
	}

	var filteredRenderers []*apapproto.RendererConfig
	if len(visualizations) == 0 {
		// TODO Pruning logic should take account of dependencies in
		// config.data_source for filtered visualizations (see APAP-4701).
		// Until it does, play it safe by including *all* renderers if the
		// filtering was purely due to the presence of non-visualization widgets.
		filteredRenderers = preparation.Renderers
	} else {
		for _, r := range preparation.Renderers {
			if r.Id != nil {
				if _, ok := usedRenderers[r.Id.GetValue()]; ok {
					filteredRenderers = append(filteredRenderers, r)
				}
			}
		}
	}

	filteredVisualizationParameters := make(map[string]string)
	for key, renderParamId := range preparation.VisualizationParameters {
		vizID, _, ok := util.SplitVisualizationParameterKey(key)
		_, matched := matchedVizIDs[vizID]
		if ok && matched {
			filteredVisualizationParameters[key] = renderParamId
		}
	}

	return &apapproto.PrepareRenderResponse{
		Renderers:               filteredRenderers,
		Visualizations:          filteredViz,
		CompatibilityWarning:    preparation.CompatibilityWarning,
		RenderParameters:        preparation.RenderParameters,
		VisualizationParameters: filteredVisualizationParameters,
	}, nil
}

func isDataVisualization(config *apapproto.VisualizationConfig) bool {
	return config.GetPlacement() == "" || config.GetPlacement() == visualizationsPlacement
}

func needToFilter(visualizations []string, configs []*apapproto.VisualizationConfig) bool {
	if len(visualizations) != 0 {
		return true
	}
	for _, config := range configs {
		if !isDataVisualization(config) {
			return true
		}
	}

	return false
}

func WriteRenderCLIResponse(
	invocationRequest *run.RenderInvocationParams,
	cliResponse renderResult,
	out io.Writer,
) error {
	var failureMsg message.Message
	var indeterminabilityMsg message.Message

	if run.AnyRenderError(cliResponse.Invocation) {
		failures := run.ListFailedRenderersForDisplay(invocationRequest, cliResponse.Invocation)
		metadata := map[string]string{
			"failures": "\n" + strings.Join(failures, ",\n"),
		}
		failureMsg = message.New(message.CliCmdRunRenderRendererFailed).WithMetadata(metadata)
	} else if run.AnyRendererPending(cliResponse.Invocation) {
		renderers := run.ListPendingRenderersForDisplay(invocationRequest, cliResponse.Invocation)
		metadata := map[string]string{
			"renderers": "\n" + strings.Join(renderers, ",\n"),
		}
		failureMsg = message.New(message.CliCmdRunRenderRendererPending).WithMetadata(metadata)
	}

	if cliResponse.Preparation != nil && cliResponse.Preparation.CompatibilityWarning != nil {
		warning := message.ReconstructFromChain(cliResponse.Preparation.CompatibilityWarning)
		var warningMsg message.Message
		if !errors.As(warning, &warningMsg) {
			return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("compatibility warning couldn't be converted to a Message"))
		}

		if isIncompatibilityMessage(warningMsg) && failureMsg != nil {
			return handleIncompatibilityMsg(cliResponse, warningMsg, out)
		} else {
			indeterminabilityMsg = warningMsg
		}
	}

	if err := writeRenderCLIJSON(cliResponse, failureMsg, out); err != nil {
		return err
	}

	if failureMsg == nil {
		return nil
	}

	if viper.GetBool("json") {
		// The error has been reported above (combined with cliResponse), so don't report it again in HandleCLIError
		return clijson.ErrorAlreadyHandled
	} else {
		fmt.Fprintln(out)
		if indeterminabilityMsg != nil {
			clijson.HandleCLIError(out, message.New(message.CliCmdRunRenderIndeterminableMessage))
			indentSpaces := 2
			if catalogMsg, lookupErr := clijson.LookupMsg(indeterminabilityMsg); lookupErr == nil && catalogMsg != nil {
				fmt.Fprintln(out, catalogMsg.StringWithIndent(indentSpaces))
				fmt.Fprintln(out)
			} else {
				fmt.Fprintf(out, "%v%v\n", strings.Repeat(" ", indentSpaces), indeterminabilityMsg.Error())
				fmt.Fprintln(out)
			}
		}
	}

	return failureMsg
}

func writeRenderCLIJSON(cliResponse renderResult, errorMsg message.Message, out io.Writer) error {
	// If we're only supplied with the invocation result (i.e. we've come from invoke-render), only print this out
	if cliResponse.Preparation == nil {
		return clijson.MarshalJSONCLIResponseWithError(out, cliResponse.Invocation, errorMsg)
	}

	jsonPrepared := clijson.PrepareRenderResponseToJSON(cliResponse.Preparation)
	jsonCLIResponse := jsonRenderResult{Preparation: jsonPrepared, Invocation: cliResponse.Invocation}

	return clijson.MarshalJSONCLIResponseWithError(out, jsonCLIResponse, errorMsg)
}

func handleIncompatibilityMsg(cliResponse renderResult, warningMsg message.Message, out io.Writer) error {
	jsonPrepared := clijson.PrepareRenderResponseToJSON(cliResponse.Preparation)
	jsonCLIResponse := jsonRenderResult{Preparation: jsonPrepared, Invocation: cliResponse.Invocation}

	if err := clijson.MarshalJSONCLIResponseWithErrorAndSeverity(out, jsonCLIResponse, warningMsg, message.SeverityError); err != nil {
		return err
	}

	if !viper.GetBool("json") {
		fmt.Fprintln(out)
		if catalogMsg, lookupErr := message.LookupMessage(warningMsg); lookupErr == nil {
			catalogMsg.Severity = message.SeverityError
			fmt.Fprintln(out, catalogMsg.String())
		} else {
			// Fallback to the raw error message if lookup fails
			fmt.Fprintf(out, "%v\n", warningMsg.Error())
		}
	}

	return clijson.ErrorAlreadyHandled
}

// The domain indicative of an incompatibility message which should be escalated to 'Error' in this context.
const incompatibleCodeDomain = "engine.render.compatibility.incompatible"

// isIncompatibilityMessage returns true if a compatibility message should be treated as an error **in the context of `run render`**.
// Messages in the 'incompatible' domain should be escalated to 'Error'; others should stay as 'Warning's.
func isIncompatibilityMessage(msg message.Message) bool {
	return msg.Domain() == incompatibleCodeDomain
}

type renderResult struct {
	Preparation *apapproto.PrepareRenderResponse `json:"preparation"`
	Invocation  *apapproto.InvokeRenderResponse  `json:"invocation"`
}

type jsonRenderResult struct {
	Preparation clijson.PrepareRenderResponseJSON `json:"preparation"`
	Invocation  *apapproto.InvokeRenderResponse   `json:"invocation"`
}

func render(cc client.ClientConnector, preparer run.RenderPreparer, invoker run.RenderInvoker, cliParams *renderCliParams, out io.Writer) error {
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

	preparation, err := preparer.PrepareRender(connector, content, params)
	if err != nil {
		return err
	}

	filtered, err := filterPreparedRender(cliParams.Visualizations, preparation)
	if err != nil {
		return err
	}

	invocationParams := &run.RenderInvocationParams{RendererConfig: filtered.Renderers, VisualizationConfig: filtered.Visualizations}
	rendered, err := invoker.InvokeRender(connector, content, invocationParams)
	if err != nil {
		return clierror.DecorateError(clierror.Run.Render.InvokeRenderFailed, err)
	}

	result := renderResult{filtered, rendered}

	return WriteRenderCLIResponse(invocationParams, result, out)
}
