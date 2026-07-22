// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/clierror"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var InvokeRenderCmd = NewInvokeRenderCmd(client.NewAutostartClient(), run.RenderService{})

// Should be able to invoke specific renderers like so -
// run invoke-render
//
//			0ab12345
//			"--renderer=function_profile=<JSON_STR>"
//	     "--renderer=drilldown_table=<JSON_STR>"
type invokeRenderCliParams struct {
	Runs      []string
	Renderers []string
}

// parseRendererCLIConfig parses CLI config string into struct for next stage of processing.
// config contains a string of the format
//
//	RendererName[:RendererID]=JSONStr
//
// where JSONStr is a JSON-formatted string containing a JSON dictionary representing the configuration of the
// given renderer.
func parseRendererCLIConfig(config string) (*apapproto.RendererConfig, error) {
	rendererAndMaybeID, configJSON, found := strings.Cut(config, "=")
	if !found {
		return nil, errors.New("invalid renderer config: expected name[optional-id]=json")
	}

	rendererName := rendererAndMaybeID
	var rendererID *apapproto.RendererId

	// See if a ':' is present inside the rendererAndMaybeID
	if name, id, hasID := strings.Cut(rendererAndMaybeID, ":"); hasID {
		rendererName = name
		rendererID = &apapproto.RendererId{Value: id}
	}

	configStruct, err := util.UnmarshalJSONToStruct(configJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config JSON: %w", err)
	}

	return &apapproto.RendererConfig{
		Renderer: rendererName,
		Id:       rendererID,
		Config:   configStruct,
	}, nil
}

func (p *invokeRenderCliParams) parseRendererConfigs() ([]*apapproto.RendererConfig, error) {
	var configs []*apapproto.RendererConfig
	for _, rendererConfigStr := range p.Renderers {
		config, err := parseRendererCLIConfig(rendererConfigStr)
		if err != nil {
			return configs, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

func NewInvokeRenderCmd(cc client.ClientConnector, re run.RenderInvoker) *cobra.Command {
	cliParams := invokeRenderCliParams{}

	cmd := &cobra.Command{
		Use:   "invoke-render run_id [run_id...]",
		Short: "Primitive to start a render session for a previous run with custom configuration.",
		Long: "This command invokes specific renderers on the selected run(s) and provides full control over the " +
			"rendering pipeline to a CLI user. This is intended as a programming-level command. " +
			"Use run render instead, for a more user friendly experience.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRunSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.Runs = args

			return invokeRender(cc, re, &cliParams, cmd.OutOrStdout())
		},
	}

	cmd.Args = cobra.MinimumNArgs(1)
	cmd.Flags().StringArrayVar(&cliParams.Renderers, "renderer", []string{}, "Renderer configs; provide one per renderer; format is --renderer=Name=ConfigJSON")

	cmd.ValidArgsFunction = completion.CompleteRunIDs
	return cmd
}

func marshalContentSelection(runIDs []string) *apapproto.ContentSelection {
	protoRuns := util.Map(runIDs, func(s string) *apapproto.RunId { return &apapproto.RunId{Value: s} })
	return &apapproto.ContentSelection{Runs: protoRuns}
}

func WriteInvokeRenderCLIResponse(
	invocationRequest *run.RenderInvocationParams,
	invocationResponse *apapproto.InvokeRenderResponse,
	out io.Writer,
) error {
	return WriteRenderCLIResponse(invocationRequest, renderResult{Invocation: invocationResponse}, out)
}

func invokeRender(cc client.ClientConnector, re run.RenderInvoker, cliParams *invokeRenderCliParams, out io.Writer) error {
	connector, err := cc.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return clierror.DecorateError(clierror.Common.ConnectFailed, err)
	}

	content := marshalContentSelection(cliParams.Runs)

	configs, err := cliParams.parseRendererConfigs()
	if err != nil {
		return clierror.DecorateError(clierror.Run.Render.InvalidRendererConfig, err)
	}
	params := run.RenderInvocationParams{RendererConfig: configs}

	render, err := re.InvokeRender(connector, content, &params)
	if err != nil {
		return clierror.DecorateError(clierror.Run.Render.InvokeRenderFailed, err)
	}

	return WriteInvokeRenderCLIResponse(&params, render, out)
}
