// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// render_session.go contains the code shared by MCP tools that open and close
// render sessions. Keeping it here ensures that every caller handles render
// failures and cleanup in the same way.

package toolimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

const renderSessionCleanupTimeout = 5 * time.Second

// renderSessionResult is the JSON response for an opened render session. It
// matches InvokeRenderResponse and may include a compatibility warning from
// PrepareRender.
type renderSessionResult map[string]any

// openedRenderSession keeps both daemon responses. The invocation describes
// the opened session, while the preparation may contain a compatibility
// warning that must be returned with it.
type openedRenderSession struct {
	Preparation *apapproto.PrepareRenderResponse
	Invocation  *apapproto.InvokeRenderResponse
}

// openRenderSession prepares and invokes the installed recipe for one run. If
// invocation returns an error or a renderer does not complete successfully, it
// closes any returned session before returning the error.
func openRenderSession(
	ctx context.Context,
	engine apapproto.ApapClient,
	runID string,
	renderParameters map[string]*structpb.Value,
) (openedRenderSession, error) {
	var session openedRenderSession
	content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{{Value: runID}}}
	prepared, err := engine.PrepareRender(ctx, &apapproto.PrepareRenderRequest{
		Content:          content,
		RenderParameters: renderParameters,
	})
	if err != nil {
		return session, fmt.Errorf("prepare render: %w", err)
	}
	if prepared == nil {
		return session, errors.New("prepare render returned no configuration")
	}
	session.Preparation = prepared

	invoked, invokeErr := engine.InvokeRender(ctx, &apapproto.InvokeRenderRequest{
		Content:             content,
		RendererConfig:      prepared.GetRenderers(),
		VisualizationConfig: prepared.GetVisualizations(),
	})
	if invoked != nil {
		session.Invocation = invoked
	}
	if invokeErr != nil {
		err = fmt.Errorf("invoke render: %w", invokeErr)
		if sessionID := strings.TrimSpace(invoked.GetSessionId()); sessionID != "" {
			err = joinRenderSessionCleanupError(ctx, engine, sessionID, err)
		}
		return session, err
	}
	if invoked == nil {
		return session, errors.New("invoke render returned no response")
	}

	sessionID := strings.TrimSpace(invoked.GetSessionId())
	if sessionID == "" {
		return session, errors.New("invoke render returned no session ID")
	}

	if err := renderInvocationError(prepared, invoked); err != nil {
		err = joinRenderSessionCleanupError(ctx, engine, sessionID, err)
		return session, err
	}

	return session, nil
}

// openRenderSessionResult opens a session and builds the response returned to
// MCP clients. It closes the session if response conversion fails so callers
// never have to manage a session they did not receive.
func openRenderSessionResult(
	ctx context.Context,
	engine apapproto.ApapClient,
	runID string,
	renderParameters map[string]*structpb.Value,
) (openedRenderSession, renderSessionResult, error) {
	session, err := openRenderSession(ctx, engine, runID, renderParameters)
	if err != nil {
		return session, nil, err
	}

	result, err := renderSessionResultFromProto(session.Preparation, session.Invocation)
	if err == nil {
		return session, result, nil
	}

	sessionID := strings.TrimSpace(session.Invocation.GetSessionId())
	err = joinRenderSessionCleanupError(ctx, engine, sessionID, err)
	return session, nil, err
}

// renderInvocationError checks that InvokeRender returned one successful
// status for every prepared renderer. This prevents callers from using a
// session whose data may be incomplete.
func renderInvocationError(
	prepared *apapproto.PrepareRenderResponse,
	invoked *apapproto.InvokeRenderResponse,
) error {
	renderers := prepared.GetRenderers()
	statuses := invoked.GetInvocationStatuses()
	if len(statuses) != len(renderers) {
		return fmt.Errorf(
			"render returned %d invocation statuses for %d renderers",
			len(statuses),
			len(renderers),
		)
	}

	params := &run.RenderInvocationParams{
		RendererConfig:      renderers,
		VisualizationConfig: prepared.GetVisualizations(),
	}
	if run.AnyRenderError(invoked) {
		return fmt.Errorf("render failed: %s", strings.Join(run.ListFailedRenderersForDisplay(params, invoked), "; "))
	}
	if run.AnyRendererPending(invoked) {
		return fmt.Errorf("render remained pending: %s", strings.Join(run.ListPendingRenderersForDisplay(params, invoked), "; "))
	}
	for index, status := range statuses {
		if status.GetSuccess() == nil {
			return fmt.Errorf("render returned an unknown status for renderer %d", index)
		}
	}

	return nil
}

// renderSessionResultFromProto converts the daemon responses to the JSON
// returned to MCP clients. It keeps the InvokeRenderResponse field names and
// adds the preparation compatibility warning when present.
func renderSessionResultFromProto(
	prepared *apapproto.PrepareRenderResponse,
	invoked *apapproto.InvokeRenderResponse,
) (renderSessionResult, error) {
	encoded, err := (protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   true,
	}).Marshal(invoked)
	if err != nil {
		return nil, fmt.Errorf("convert invoke render response: %w", err)
	}

	result := renderSessionResult{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("convert invoke render response: %w", err)
	}
	if warning := compatibilityWarningFromProto(prepared); warning != nil {
		result["compatibility_warning"] = warning
	}
	return result, nil
}

// compatibilityWarningFromProto converts the daemon warning to the structured
// error format used in MCP responses so callers retain its cause and advice.
func compatibilityWarningFromProto(prepared *apapproto.PrepareRenderResponse) *toolError {
	warning := message.ReconstructFromChain(prepared.GetCompatibilityWarning())
	if warning == nil {
		return nil
	}
	return newToolError(warning)
}

// cleanupRenderSession closes a session even if the request was cancelled. Its
// own timeout gives cleanup a chance to finish without blocking indefinitely.
func cleanupRenderSession(ctx context.Context, engine apapproto.ApapClient, sessionID string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), renderSessionCleanupTimeout)
	defer cancel()
	return closeRenderSession(cleanupCtx, engine, sessionID)
}

// joinRenderSessionCleanupError closes a rejected session and reports both
// errors if closing also fails. When closing succeeds, it returns the original
// error without wrapping it.
func joinRenderSessionCleanupError(
	ctx context.Context,
	engine apapproto.ApapClient,
	sessionID string,
	err error,
) error {
	if cleanupErr := cleanupRenderSession(ctx, engine, sessionID); cleanupErr != nil {
		return errors.Join(err, cleanupErr)
	}
	return err
}

func closeRenderSession(ctx context.Context, engine apapproto.ApapClient, sessionID string) error {
	_, err := engine.CloseRender(ctx, &apapproto.CloseRenderRequest{SessionId: sessionID})
	if err != nil {
		return fmt.Errorf("close render %q: %w", sessionID, err)
	}
	return nil
}
