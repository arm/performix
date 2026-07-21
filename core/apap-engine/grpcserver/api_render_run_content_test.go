// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func TestValidateInvokeRenderSuccess(t *testing.T) {
	t.Run("accepts successful render response with session", func(t *testing.T) {
		err := validateInvokeRenderSuccess(nil, &apapproto.InvokeRenderResponse{
			SessionId: "session-1",
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Success{
						Success: &apapproto.Success{},
					},
				},
			},
		})

		require.NoError(t, err)
	})

	t.Run("returns render failed when response is nil", func(t *testing.T) {
		err := validateInvokeRenderSuccess(nil, nil)

		requireRenderFailedWithCause(t, err, "render returned no response")
	})

	t.Run("returns render failed when session is missing", func(t *testing.T) {
		err := validateInvokeRenderSuccess(nil, &apapproto.InvokeRenderResponse{})

		requireRenderFailedWithCause(t, err, "render did not produce a session")
	})

	t.Run("includes renderer config details for invocation errors", func(t *testing.T) {
		err := validateInvokeRenderSuccess(
			&apapproto.InvokeRenderRequest{
				RendererConfig: []*apapproto.RendererConfig{
					{
						Renderer: "CSV",
						Id:       &apapproto.RendererId{Value: "summary"},
					},
				},
			},
			&apapproto.InvokeRenderResponse{
				SessionId: "session-1",
				InvocationStatuses: []*apapproto.RendererInvocationStatus{
					{
						Status: &apapproto.RendererInvocationStatus_Error{
							Error: &apapproto.Error{Message: "missing component"},
						},
					},
				},
			},
		)

		requireRenderFailedWithCause(t, err, "CSV[0=summary] missing component")
	})

	t.Run("falls back to status renderer id for invocation errors", func(t *testing.T) {
		err := validateInvokeRenderSuccess(
			nil,
			&apapproto.InvokeRenderResponse{
				SessionId: "session-1",
				InvocationStatuses: []*apapproto.RendererInvocationStatus{
					{
						Id: &apapproto.RendererId{Value: "summary"},
						Status: &apapproto.RendererInvocationStatus_Error{
							Error: &apapproto.Error{Message: "missing component"},
						},
					},
				},
			},
		)

		requireRenderFailedWithCause(t, err, "renderer[0]=summary missing component")
	})

	t.Run("includes pending errors", func(t *testing.T) {
		err := validateInvokeRenderSuccess(
			&apapproto.InvokeRenderRequest{
				RendererConfig: []*apapproto.RendererConfig{
					{
						Renderer: "CSV",
						Id:       &apapproto.RendererId{Value: "summary"},
					},
				},
			},
			&apapproto.InvokeRenderResponse{
				SessionId: "session-1",
				InvocationStatuses: []*apapproto.RendererInvocationStatus{
					{
						Status: &apapproto.RendererInvocationStatus_Pending{Pending: &apapproto.Pending{}},
					},
				},
			},
		)

		requireRenderFailedWithCause(t, err, "CSV[0=summary] component pending transfer")
	})
}

func TestWithRenderedRunContent(t *testing.T) {
	t.Run("prepares invokes and calls callback with render session", func(t *testing.T) {
		runID := &apapproto.RunId{Value: "run-id"}
		renderer := &apapproto.RendererConfig{
			Renderer: "test-renderer",
			Id:       &apapproto.RendererId{Value: "renderer-1"},
		}
		visualization := &apapproto.VisualizationConfig{
			Id:   &apapproto.VisualizationId{Value: "visualization-1"},
			Type: "test-visualization",
		}
		sessionStorage := render.NewSessionStorage()
		session := &testSession{id: "session-1"}
		require.NoError(t, sessionStorage.AddRenderSession(session))

		prepareCalled := false
		invokeCalled := false
		callbackCalled := false

		err := withRenderedRunContent(
			context.Background(),
			runID,
			func(_ context.Context, req *apapproto.PrepareRenderRequest) (*apapproto.PrepareRenderResponse, error) {
				prepareCalled = true
				require.Len(t, req.GetContent().GetRuns(), 1)
				assert.Same(t, runID, req.GetContent().GetRuns()[0])

				return &apapproto.PrepareRenderResponse{
					Renderers:      []*apapproto.RendererConfig{renderer},
					Visualizations: []*apapproto.VisualizationConfig{visualization},
				}, nil
			},
			func(_ context.Context, req *apapproto.InvokeRenderRequest) (*apapproto.InvokeRenderResponse, error) {
				invokeCalled = true
				require.Len(t, req.GetContent().GetRuns(), 1)
				assert.Same(t, runID, req.GetContent().GetRuns()[0])
				require.Len(t, req.GetRendererConfig(), 1)
				assert.Same(t, renderer, req.GetRendererConfig()[0])
				require.Len(t, req.GetVisualizationConfig(), 1)
				assert.Same(t, visualization, req.GetVisualizationConfig()[0])

				return &apapproto.InvokeRenderResponse{
					SessionId: "session-1",
					InvocationStatuses: []*apapproto.RendererInvocationStatus{
						{
							Status: &apapproto.RendererInvocationStatus_Success{
								Success: &apapproto.Success{},
							},
						},
					},
				}, nil
			},
			&sessionStorage,
			func(renderedSession render.Session) error {
				callbackCalled = true
				assert.Same(t, session, renderedSession)
				assert.True(t, sessionStorage.SessionRegistered("session-1"))
				return nil
			},
		)

		require.NoError(t, err)
		assert.True(t, prepareCalled)
		assert.True(t, invokeCalled)
		assert.True(t, callbackCalled)
		assert.False(t, sessionStorage.SessionRegistered("session-1"))
	})

	t.Run("closes render session when callback returns error", func(t *testing.T) {
		sessionStorage := render.NewSessionStorage()
		require.NoError(t, sessionStorage.AddRenderSession(&testSession{id: "session-1"}))
		callbackErr := errors.New("callback failed")

		err := withRenderedRunContent(
			context.Background(),
			&apapproto.RunId{Value: "run-id"},
			func(context.Context, *apapproto.PrepareRenderRequest) (*apapproto.PrepareRenderResponse, error) {
				return &apapproto.PrepareRenderResponse{}, nil
			},
			func(context.Context, *apapproto.InvokeRenderRequest) (*apapproto.InvokeRenderResponse, error) {
				return &apapproto.InvokeRenderResponse{
					SessionId: "session-1",
					InvocationStatuses: []*apapproto.RendererInvocationStatus{
						{
							Status: &apapproto.RendererInvocationStatus_Success{
								Success: &apapproto.Success{},
							},
						},
					},
				}, nil
			},
			&sessionStorage,
			func(render.Session) error {
				assert.True(t, sessionStorage.SessionRegistered("session-1"))
				return callbackErr
			},
		)

		assert.Equal(t, callbackErr, err)
		assert.False(t, sessionStorage.SessionRegistered("session-1"))
	})
}

func requireRenderFailedWithCause(t *testing.T, err error, causeContains string) {
	t.Helper()

	require.Error(t, err)
	msg, ok := err.(*message.MessageImpl)
	require.True(t, ok)
	assert.Equal(t, message.EngineGrpcserverApiApapRenderFailed, msg.Code())
	require.Error(t, msg.Unwrap())
	assert.Contains(t, msg.Unwrap().Error(), causeContains)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}
