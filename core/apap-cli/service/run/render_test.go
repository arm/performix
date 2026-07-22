// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestListFailedRenderersForDisplay(t *testing.T) {
	t.Run("returns failed renderers with index id and error message", func(t *testing.T) {
		request := &RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "foo",
					Id:       &apapproto.RendererId{Value: "foo-id"},
				},
				{
					Renderer: "bar",
					Id:       &apapproto.RendererId{Value: "bar-id"},
				},
				{
					Renderer: "baz",
					Id:       &apapproto.RendererId{Value: "baz-id"},
				},
			},
		}
		response := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Success{
						Success: &apapproto.Success{},
					},
				},
				{
					Status: &apapproto.RendererInvocationStatus_Error{
						Error: &apapproto.Error{Message: "bar failed"},
					},
				},
				{
					Status: &apapproto.RendererInvocationStatus_Error{
						Error: &apapproto.Error{Message: "baz failed"},
					},
				},
			},
		}

		result := ListFailedRenderersForDisplay(request, response)

		require.Equal(t, []string{"bar[1=bar-id] bar failed", "baz[2=baz-id] baz failed"}, result)
	})

	t.Run("omits renderer id when absent", func(t *testing.T) {
		request := &RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "bar",
				},
			},
		}
		response := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Error{
						Error: &apapproto.Error{Message: "bar failed"},
					},
				},
			},
		}

		result := ListFailedRenderersForDisplay(request, response)

		require.Equal(t, []string{"bar[0] bar failed"}, result)
	})

	t.Run("returns empty list when no renderers failed", func(t *testing.T) {
		request := &RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "foo",
				},
				{
					Renderer: "bar",
				},
			},
		}
		response := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Success{
						Success: &apapproto.Success{},
					},
				},
				{
					Status: &apapproto.RendererInvocationStatus_Pending{
						Pending: &apapproto.Pending{},
					},
				},
			},
		}

		result := ListFailedRenderersForDisplay(request, response)

		require.Empty(t, result)
	})
}

func TestListPendingRenderersForDisplay(t *testing.T) {
	t.Run("returns pending renderers with index and id", func(t *testing.T) {
		request := &RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "foo",
					Id:       &apapproto.RendererId{Value: "foo-id"},
				},
				{
					Renderer: "bar",
					Id:       &apapproto.RendererId{Value: "bar-id"},
				},
				{
					Renderer: "baz",
					Id:       &apapproto.RendererId{Value: "baz-id"},
				},
			},
		}
		response := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Success{
						Success: &apapproto.Success{},
					},
				},
				{
					Status: &apapproto.RendererInvocationStatus_Pending{
						Pending: &apapproto.Pending{},
					},
				},
				{
					Status: &apapproto.RendererInvocationStatus_Pending{
						Pending: &apapproto.Pending{},
					},
				},
			},
		}

		result := ListPendingRenderersForDisplay(request, response)

		require.Equal(t, []string{"bar[1=bar-id]", "baz[2=baz-id]"}, result)
	})
}

func TestRenderServicePrepareRender(t *testing.T) {
	t.Run("includes render parameters", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		service := RenderService{}
		content := &apapproto.ContentSelection{}

		policy := apapproto.RecipeSelectionPolicyType_OVERRIDE_BY_NAME
		params := RenderPreparationParams{
			RecipeSelectionPolicy: &policy,
			OverrideRecipeName:    "my-recipe",
			RenderParameterValues: map[string]*structpb.Value{
				"threshold": structpb.NewStringValue("10"),
			},
		}

		client.On("PrepareRender", mock.Anything, mock.MatchedBy(func(req *apapproto.PrepareRenderRequest) bool {
			if req == nil || req.RecipeSelectionPolicy == nil {
				return false
			}
			if req.RecipeSelectionPolicy.GetPolicy() != policy {
				return false
			}
			if req.RecipeSelectionPolicy.GetOverrideName() != "my-recipe" {
				return false
			}
			if req.RenderParameters == nil {
				return false
			}
			return req.RenderParameters["threshold"].GetStringValue() == "10"
		})).Return(&apapproto.PrepareRenderResponse{}, nil)

		_, err := service.PrepareRender(client, content, params)
		require.NoError(t, err)
	})

	t.Run("omits render parameters when empty", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		service := RenderService{}
		content := &apapproto.ContentSelection{}

		params := RenderPreparationParams{}

		client.On("PrepareRender", mock.Anything, mock.MatchedBy(func(req *apapproto.PrepareRenderRequest) bool {
			return req != nil && req.RenderParameters == nil
		})).Return(&apapproto.PrepareRenderResponse{}, nil)

		_, err := service.PrepareRender(client, content, params)
		require.NoError(t, err)
	})
}
