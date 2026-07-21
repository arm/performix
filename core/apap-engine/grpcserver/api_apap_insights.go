// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"fmt"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/insights"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
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
		insights.SummarizersForRecipe,
		runSummaryBundleTextLimitBytes,
	)
}

// getRunSummaryBundle calls the summarizer functions for the given run and returns a bundle of summaries.
func getRunSummaryBundle(
	ctx context.Context,
	desc *run.RunDescription,
	withRenderedRunContent func(context.Context, func(render.Session) error) error,
	summarizersForRecipe func(string) (insights.RecipeRunSummarizers, error),
	bundleLimitBytes int,
) (*apapproto.RunSummaryBundleResponse, error) {
	if desc.RunResult != string(run.RecipeSuccess) {
		return nil, message.New(message.EngineGrpcserverApiApapInsightsRunNotSuccessful).
			WithMetadata(map[string]string{
				"runID":     desc.ID,
				"runResult": desc.RunResult,
			})
	}

	summarizers, err := summarizersForRecipe(desc.RecipeName)
	if err != nil {
		return nil, err
	}

	summaries := make([]insights.RunSummary, 0, len(summarizers.Unbudgeted)+len(summarizers.Budgeted))
	err = withRenderedRunContent(ctx, func(session render.Session) error {
		for _, summarizer := range summarizers.Unbudgeted {
			logger := logx.FromContext(ctx).WithFields(log.Fields{
				"runID":          desc.ID,
				"summarizer":     summarizer.Name,
				"summarizerType": "unbudgeted",
			})
			logger.Info("Summarizer starting.")
			summary, err := summarizer.Summarize(ctx, desc, session)
			if err != nil {
				logger.WithError(err).Warn("Summarizer failed to produce summary.")
				return err
			}
			logger.Info("Summarizer stopping.")
			summaries = append(summaries, summary)
		}

		budgetedSummaryBytes := remainingRunSummaryBundleBytes(summaries, bundleLimitBytes)
		for _, summarizer := range summarizers.Budgeted {
			byteLimit := budgetedSummarizerByteLimit(budgetedSummaryBytes, summarizer, summarizers.Budgeted)
			logger := logx.FromContext(ctx).WithFields(log.Fields{
				"byteLimit":      byteLimit,
				"runID":          desc.ID,
				"summarizer":     summarizer.Name,
				"summarizerType": "budgeted",
			})
			logger.Info("Summarizer starting.")
			summary, err := summarizer.Summarize(ctx, desc, session, byteLimit)
			if err != nil {
				logger.WithError(err).Warn("Summarizer failed to produce summary.")
				return err
			}
			logger.Info("Summarizer stopping.")
			summaries = append(summaries, summary)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := validateRunSummaryBundle(summaries); err != nil {
		return nil, err
	}

	if err := limitRunSummaryBundleSize(summaries, bundleLimitBytes); err != nil {
		return nil, err
	}

	return &apapproto.RunSummaryBundleResponse{
		Payloads: runSummariesToProto(summaries),
	}, nil
}

// budgetedSummarizerByteLimit calculates the byte limit for a budgeted summarizer based on the available budget
// and the summarizer's weight relative to all budgeted summarizers.
func budgetedSummarizerByteLimit(
	availableBudgetBytes int,
	summarizer insights.BudgetedRunSummarizerConfig,
	summarizers []insights.BudgetedRunSummarizerConfig,
) int {
	totalWeight := 0
	for _, configuredSummarizer := range summarizers {
		totalWeight += configuredSummarizer.Weight
	}

	return availableBudgetBytes * summarizer.Weight / totalWeight
}

// runSummariesToProto converts run summaries to proto.
func runSummariesToProto(summaries []insights.RunSummary) []*apapproto.RunSummaryPayload {
	payloads := make([]*apapproto.RunSummaryPayload, 0, len(summaries))
	for _, summary := range summaries {
		payloads = append(payloads, &apapproto.RunSummaryPayload{
			Name:           summary.Name,
			PromptFragment: summary.PromptFragment,
			Payload:        summary.Payload,
		})
	}

	return payloads
}

// validateRunSummaryBundle validates that each run summary has a unique name.
func validateRunSummaryBundle(summaries []insights.RunSummary) error {
	names := map[string]struct{}{}
	for _, summary := range summaries {
		if summary.Name == "" {
			return fmt.Errorf("produced run summary is invalid as a summarizer name is missing")
		}
		if _, ok := names[summary.Name]; ok {
			return fmt.Errorf("produced run summary is invalid as summarizers have duplicated names %q", summary.Name)
		}
		names[summary.Name] = struct{}{}
	}

	return nil
}

// remainingRunSummaryBundleBytes calculates the remaining bytes available for the run
// summary bundle given the summaries so far and the overall limit.
func remainingRunSummaryBundleBytes(summaries []insights.RunSummary, limitBytes int) int {
	totalBytes := runSummaryBundleSizeBytes(summaries)
	remainingBytes := limitBytes - totalBytes
	if remainingBytes < 0 {
		return 0
	}

	return remainingBytes
}

// limitRunSummaryBundleSize returns an error if the total size of the run summaries
// exceeds runSummaryBundleTextLimitBytes. Provided the unbudgeted summarizers are small
// and the budgeted summarizers respect their byte limits, this should not be hit, but is
// kept as a safeguard to ensure we do not exceed gRPC message size limits.
func limitRunSummaryBundleSize(
	summaries []insights.RunSummary,
	limitBytes int,
) error {
	totalBytes := runSummaryBundleSizeBytes(summaries)

	if totalBytes > limitBytes {
		return message.New(message.EngineGrpcserverApiApapInsightsBundleSizeExceeded).
			WithMetadata(map[string]string{
				"totalBytes": strconv.Itoa(totalBytes),
				"limitBytes": strconv.Itoa(limitBytes),
			})
	}

	return nil
}

// runSummaryBundleSizeBytes calculates the total size of the run summaries in bytes.
func runSummaryBundleSizeBytes(summaries []insights.RunSummary) int {
	totalBytes := 0
	for _, summary := range summaries {
		totalBytes += len(summary.Name) + len(summary.PromptFragment) + len(summary.Payload)
	}

	return totalBytes
}
