// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestOpenRenderSession(t *testing.T) {
	t.Run("forwards the prepared configuration", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{{
				Renderer: "hot-functions",
				Id:       &apapproto.RendererId{Value: "hot-functions-1"},
			}},
			Visualizations: []*apapproto.VisualizationConfig{{
				Type: "hot-functions-table",
				Id:   &apapproto.VisualizationId{Value: "hot-functions-table-1"},
			}},
		}
		expectRunQueryPrepare(engine, "run-1", prepared)
		invoked := successfulRenderInvocation("session-1")
		engine.On("InvokeRender", mock.Anything, mock.MatchedBy(func(req *apapproto.InvokeRenderRequest) bool {
			return len(req.GetContent().GetRuns()) == 1 &&
				req.GetContent().GetRuns()[0].GetValue() == "run-1" &&
				proto.Equal(&apapproto.InvokeRenderRequest{
					Content:             req.GetContent(),
					RendererConfig:      prepared.GetRenderers(),
					VisualizationConfig: prepared.GetVisualizations(),
				}, req)
		})).Return(invoked, nil).Once()

		session, err := openRenderSession(context.Background(), engine, "run-1", nil)

		require.NoError(t, err)
		assert.Same(t, prepared, session.Preparation)
		assert.Same(t, invoked, session.Invocation)
	})

	t.Run("closes a session returned with an invoke error", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(
			&apapproto.InvokeRenderResponse{SessionId: "session-partial"},
			errors.New("invoke failed"),
		).Once()
		expectRunQueryClose(engine, "session-partial")

		_, err := openRenderSession(context.Background(), engine, "run-1", nil)

		require.ErrorContains(t, err, "invoke failed")
		_, joined := err.(interface{ Unwrap() []error })
		assert.False(t, joined, "successful cleanup must not change the structured error tree")
	})

	t.Run("joins a rejected-render cleanup failure", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}},
		}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{
			SessionId: "session-rejected",
			InvocationStatuses: []*apapproto.RendererInvocationStatus{{
				Status: &apapproto.RendererInvocationStatus_Error{
					Error: &apapproto.Error{Message: "cannot render"},
				},
			}},
		}, nil).Once()
		engine.On("CloseRender", mock.Anything, &apapproto.CloseRenderRequest{
			SessionId: "session-rejected",
		}).Return(nil, errors.New("close failed")).Once()

		_, err := openRenderSession(context.Background(), engine, "run-1", nil)

		require.ErrorContains(t, err, "render failed")
		require.ErrorContains(t, err, "close failed")
		require.ErrorContains(t, err, `close render "session-rejected"`)
	})

	t.Run("uses an uncancelled context to reject a partial render", func(t *testing.T) {
		engine := apapprotomocks.NewApapClient(t)
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{{Renderer: "hot-functions"}},
		}
		expectRunQueryPrepare(engine, "run-1", prepared)
		engine.On("InvokeRender", mock.Anything, mock.Anything).Return(&apapproto.InvokeRenderResponse{
			SessionId: "session-pending",
			InvocationStatuses: []*apapproto.RendererInvocationStatus{{
				Status: &apapproto.RendererInvocationStatus_Pending{Pending: &apapproto.Pending{}},
			}},
		}, nil).Once()
		engine.On("CloseRender", mock.MatchedBy(func(ctx context.Context) bool {
			return ctx.Err() == nil
		}), &apapproto.CloseRenderRequest{SessionId: "session-pending"}).Return(&emptypb.Empty{}, nil).Once()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := openRenderSession(ctx, engine, "run-1", nil)

		require.ErrorContains(t, err, "render remained pending")
	})
}

func TestRenderSessionResultFromProto(t *testing.T) {
	prepared := &apapproto.PrepareRenderResponse{
		CompatibilityWarning: message.BuildErrorChain(errors.New("recipe compatibility warning")),
	}
	invoked := successfulRenderInvocation("session-1")
	invoked.ConnectionString = "duckdb:session-1"
	invoked.Manifest = &apapproto.RenderManifest{Entry: []*apapproto.RenderManifestEntry{{
		ComponentType:          "functions",
		ComponentSchemaVersion: "1.0",
		TableName:              "flat_table",
	}}}
	invoked.VisualizationResolvedTables = &apapproto.VisualizationResolvedTablesList{
		Entries: []*apapproto.VisualizationResolvedTables{{
			Id: &apapproto.VisualizationId{Value: "hot-functions-table-1"},
			Tables: map[string]*apapproto.StringArray{
				"functions": {Values: []string{"flat_table"}},
			},
		}},
	}

	result, err := renderSessionResultFromProto(prepared, invoked)

	require.NoError(t, err)
	assert.Equal(t, "session-1", result["session_id"])
	assert.Equal(t, "duckdb:session-1", result["connection_string"])
	assert.Contains(t, result, "manifest")
	assert.Contains(t, result, "invocation_statuses")
	assert.Contains(t, result, "visualization_resolved_tables")
	assert.NotContains(t, result, "connectionString")
	assert.NotContains(t, result, "invocationStatuses")
	statuses, ok := result["invocation_statuses"].([]any)
	require.True(t, ok)
	require.Len(t, statuses, 1)
	status, ok := statuses[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, status, "success")
	warning, ok := result["compatibility_warning"].(*toolError)
	require.True(t, ok)
	assert.Equal(t, "recipe compatibility warning", warning.Message)

	result, err = renderSessionResultFromProto(&apapproto.PrepareRenderResponse{}, invoked)

	require.NoError(t, err)
	assert.NotContains(t, result, "compatibility_warning")
}

func successfulRenderInvocation(sessionID string) *apapproto.InvokeRenderResponse {
	return &apapproto.InvokeRenderResponse{
		SessionId: sessionID,
		InvocationStatuses: []*apapproto.RendererInvocationStatus{{
			Status: &apapproto.RendererInvocationStatus_Success{Success: &apapproto.Success{}},
		}},
	}
}
