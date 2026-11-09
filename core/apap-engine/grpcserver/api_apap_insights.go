// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"strconv"

	"github.com/Arm-Debug/apap-cli/apap-engine/insights"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

// runSummaryBundleTextLimitBytes caps the total size of the run summaries produced
// by GetRunSummaryBundle. This has been chosen to keep bundle sizes within gRPC
// message size limits.
const runSummaryBundleTextLimitBytes = 1024 * 1024

func (s *ApapServer) GetRunSummaryBundle(
	ctx context.Context,
	req *apapproto.RunSummaryBundleRequest,
) (*apapproto.RunSummaryBundleResponse, error) {
	if req == nil || req.GetRunId() == nil {
		return nil, message.New(message.EngineGrpcserverApiApapInsightsRunIdRequired)
	}

	runID := run.RunID{Value: req.GetRunId().GetValue()}
	desc, err := s.runs.RunDescription(ctx, runID)
	if err != nil {
		return nil, err
	}

	return getRunSummaryBundle(
		ctx,
		desc,
		func(ctx context.Context, fn func(render.Session) error) error {
			return s.withRenderedRunContent(ctx, req.GetRunId(), fn)
		},
		runSummaryBundleTextLimitBytes,
	)
}

// getRunSummaryBundle returns the run details in the existing bundle format.
func getRunSummaryBundle(
	ctx context.Context,
	desc *run.RunDescription,
	withRenderedRunContent func(context.Context, func(render.Session) error) error,
	bundleLimitBytes int,
) (*apapproto.RunSummaryBundleResponse, error) {
	if desc.RunResult != string(run.RecipeSuccess) {
		return nil, message.New(message.EngineGrpcserverApiApapInsightsRunNotSuccessful).
			WithMetadata(map[string]string{
				"runID":     desc.ID,
				"runResult": desc.RunResult,
			})
	}

	var summary insights.RunSummary
	err := withRenderedRunContent(ctx, func(session render.Session) error {
		logger := logx.FromContext(ctx).WithField("runID", desc.ID)
		logger.Info("Run details summary starting.")
		var err error
		summary, err = insights.SummarizeRunDetails(ctx, desc, session)
		if err != nil {
			logger.WithError(err).Warn("Failed to produce run details summary.")
			return err
		}
		logger.Info("Run details summary stopping.")
		return nil
	})
	if err != nil {
		return nil, err
	}

	totalBytes := len(summary.Name) + len(summary.PromptFragment) + len(summary.Payload)
	if totalBytes > bundleLimitBytes {
		return nil, message.New(message.EngineGrpcserverApiApapInsightsBundleSizeExceeded).
			WithMetadata(map[string]string{
				"totalBytes": strconv.Itoa(totalBytes),
				"limitBytes": strconv.Itoa(bundleLimitBytes),
			})
	}

	return &apapproto.RunSummaryBundleResponse{
		Payloads: []*apapproto.RunSummaryPayload{{
			Name:           summary.Name,
			PromptFragment: summary.PromptFragment,
			Payload:        summary.Payload,
		}},
	}, nil
}
