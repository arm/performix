// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func ParseRecipeSelectionPolicy(input string) (apapproto.RecipeSelectionPolicyType, error) {
	switch strings.ToLower(input) {
	case "use-installed-version":
		return apapproto.RecipeSelectionPolicyType_USE_INSTALLED_VERSION, nil
	case "from-content":
		return apapproto.RecipeSelectionPolicyType_FROM_CONTENT, nil
	case "override-by-name":
		return apapproto.RecipeSelectionPolicyType_OVERRIDE_BY_NAME, nil
	default:
		metadata := map[string]string{
			"flag":  "--recipe-selection-policy",
			"value": input,
		}
		return apapproto.RecipeSelectionPolicyType(0), message.New(message.CliCmdValidationInvalidFlagValue).WithMetadata(metadata)
	}
}

func RecipeSelectionPolicyHelp() string {
	return "Valid values: use-installed-version, from-content, override-by-name"
}

type RenderPreparationParams struct {
	RecipeSelectionPolicy        *apapproto.RecipeSelectionPolicyType
	OverrideRecipeName           string
	RenderParameterValues        map[string]*structpb.Value
	VisualizationParameterValues map[string]*structpb.Value
}

type RenderInvocationParams struct {
	RendererConfig      []*apapproto.RendererConfig
	VisualizationConfig []*apapproto.VisualizationConfig
}

type RenderPreparer interface {
	PrepareRender(client apapproto.ApapClient, content *apapproto.ContentSelection, params RenderPreparationParams) (*apapproto.PrepareRenderResponse, error)
}

type RenderInvoker interface {
	InvokeRender(client apapproto.ApapClient, content *apapproto.ContentSelection, params *RenderInvocationParams) (*apapproto.InvokeRenderResponse, error)
}

func AnyRenderError(response *apapproto.InvokeRenderResponse) bool {
	if response == nil {
		return false
	}
	return slices.ContainsFunc(response.GetInvocationStatuses(), func(status *apapproto.RendererInvocationStatus) bool {
		return status.GetError() != nil
	})
}

func AnyRendererPending(response *apapproto.InvokeRenderResponse) bool {
	if response == nil {
		return false
	}
	return slices.ContainsFunc(response.GetInvocationStatuses(), func(status *apapproto.RendererInvocationStatus) bool {
		return status.GetPending() != nil
	})
}

func ListFailedRenderersForDisplay(request *RenderInvocationParams, response *apapproto.InvokeRenderResponse) []string {
	return listRenderersForDisplay(request, response, func(status *apapproto.RendererInvocationStatus) (string, bool) {
		if renderError := status.GetError(); renderError != nil {
			return renderError.Message, true
		}

		return "", false
	})
}

func ListPendingRenderersForDisplay(request *RenderInvocationParams, response *apapproto.InvokeRenderResponse) []string {
	return listRenderersForDisplay(request, response, func(status *apapproto.RendererInvocationStatus) (string, bool) {
		return "", status.GetPending() != nil
	})
}

func listRenderersForDisplay(
	request *RenderInvocationParams,
	response *apapproto.InvokeRenderResponse,
	statusDetails func(*apapproto.RendererInvocationStatus) (string, bool),
) []string {
	var renderers []string
	for i, status := range response.InvocationStatuses {
		details, ok := statusDetails(status)
		if !ok {
			continue
		}

		renderer := rendererNameForDisplay(request.RendererConfig[i], i)
		if details != "" {
			renderer = fmt.Sprintf("%s %s", renderer, details)
		}
		renderers = append(renderers, renderer)
	}

	return renderers
}

func rendererNameForDisplay(renderer *apapproto.RendererConfig, index int) string {
	// Print in format name[idx=id] but omit =id if not present
	idStr := ""
	if id := renderer.GetId(); id != nil {
		idStr = fmt.Sprintf("=%s", id.Value)
	}

	return fmt.Sprintf("%s[%d%s]", renderer.Renderer, index, idStr)
}

type RenderService struct{}

func (s RenderService) PrepareRender(client apapproto.ApapClient, content *apapproto.ContentSelection, params RenderPreparationParams) (*apapproto.PrepareRenderResponse, error) {
	request := apapproto.PrepareRenderRequest{Content: content}

	if params.RecipeSelectionPolicy != nil {
		request.RecipeSelectionPolicy = &apapproto.RecipeSelectionPolicyOptions{
			Policy: *params.RecipeSelectionPolicy,
		}

		if params.OverrideRecipeName != "" {
			request.RecipeSelectionPolicy.OverrideName = &params.OverrideRecipeName
		}
	}

	if len(params.RenderParameterValues) > 0 {
		request.RenderParameters = params.RenderParameterValues
	}
	if len(params.VisualizationParameterValues) > 0 {
		request.VisualizationParameters = params.VisualizationParameterValues
	}

	return client.PrepareRender(context.Background(), &request)
}

func (s RenderService) InvokeRender(client apapproto.ApapClient, content *apapproto.ContentSelection, params *RenderInvocationParams) (*apapproto.InvokeRenderResponse, error) {
	request := apapproto.InvokeRenderRequest{
		Content:             content,
		RendererConfig:      params.RendererConfig,
		VisualizationConfig: params.VisualizationConfig,
	}

	return client.InvokeRender(context.Background(), &request)
}
