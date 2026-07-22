// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// validateInvokeRenderSuccess returns an error if any render response returned an error status,
// or if the response is missing a session ID.
func validateInvokeRenderSuccess(
	req *apapproto.InvokeRenderRequest,
	resp *apapproto.InvokeRenderResponse,
) error {
	if resp == nil {
		return message.New(message.EngineGrpcserverApiApapRenderFailed).
			WithCause(errors.New("render returned no response"))
	}

	var failures []string
	var pending []string
	for i, status := range resp.GetInvocationStatuses() {
		name := fmt.Sprintf("renderer[%d]", i)
		if req != nil && i < len(req.RendererConfig) {
			config := req.RendererConfig[i]
			idStr := ""
			if id := config.GetId(); id != nil {
				idStr = fmt.Sprintf("=%s", id.Value)
			}
			name = fmt.Sprintf("%s[%d%s]", config.GetRenderer(), i, idStr)
		} else if id := status.GetId().GetValue(); id != "" {
			name = fmt.Sprintf("%s=%s", name, id)
		}

		renderErr := status.GetError()
		if renderErr != nil {
			failures = append(failures, fmt.Sprintf("%s %s", name, renderErr.GetMessage()))
			continue
		}

		isPending := status.GetPending()
		if isPending != nil {
			pending = append(pending, fmt.Sprintf("%s %s", name, cdf.ErrComponentPending))
			continue
		}
	}
	if len(failures) > 0 {
		return message.New(message.EngineGrpcserverApiApapRenderFailed).
			WithCause(errors.New(strings.Join(failures, "; ")))
	} else if len(pending) > 0 {
		return message.New(message.EngineGrpcserverApiApapRenderFailed).
			WithCause(errors.New(strings.Join(pending, "; ")))
	}

	if resp.GetSessionId() == "" {
		return message.New(message.EngineGrpcserverApiApapRenderFailed).
			WithCause(errors.New("render did not produce a session"))
	}

	return nil
}

// withRenderedRunContent prepares and invokes a render for the specified run,
// calls fn with the resulting render session, and closes the session before
// returning.
func (s *ApapServer) withRenderedRunContent(
	ctx context.Context,
	runID *apapproto.RunId,
	fn func(render.Session) error,
) error {
	return withRenderedRunContent(ctx, runID, s.PrepareRender, s.InvokeRender, &s.sessions, fn)
}

func withRenderedRunContent(
	ctx context.Context,
	runID *apapproto.RunId,
	prepareRender func(context.Context, *apapproto.PrepareRenderRequest) (*apapproto.PrepareRenderResponse, error),
	invokeRender func(context.Context, *apapproto.InvokeRenderRequest) (*apapproto.InvokeRenderResponse, error),
	sessions *render.SessionStorage,
	fn func(render.Session) error,
) error {
	content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}
	standardRender, err := prepareRender(ctx, &apapproto.PrepareRenderRequest{
		Content: content,
	})
	if err != nil {
		return err
	}

	renderReq := &apapproto.InvokeRenderRequest{
		Content:             content,
		RendererConfig:      standardRender.GetRenderers(),
		VisualizationConfig: standardRender.GetVisualizations(),
	}
	renderResp, err := invokeRender(ctx, renderReq)
	if err != nil {
		return err
	}

	sessionID := renderResp.GetSessionId()
	if err := validateInvokeRenderSuccess(renderReq, renderResp); err != nil {
		if sessionID != "" {
			sessions.CloseRenderSession(sessionID)
		}
		return err
	}

	sessionAccess, err := sessions.GetSessionByID(sessionID)
	if err != nil {
		sessions.CloseRenderSession(sessionID)
		return message.New(message.EngineGrpcserverApiApapRenderFailed).WithCause(err)
	}
	defer func() {
		sessionAccess.Done()
		sessions.CloseRenderSession(sessionID)
	}()

	return fn(sessionAccess.S)
}
