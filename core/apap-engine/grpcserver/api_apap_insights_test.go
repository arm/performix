// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	run "github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
)

func runDescriptionFor(recipeName string, result run.RunResult) *run.RunDescription {
	return &run.RunDescription{
		ID:           "run-id",
		Name:         "Test Run",
		RecipeName:   recipeName,
		RunResult:    string(result),
		WorkloadType: "launch",
		Cmdline:      "ls",
		TargetName:   "local",
		Timeout:      60,
	}
}

func TestGetRunSummaryBundle(t *testing.T) {
	t.Run("returns error message when run_id is required but not provided", func(t *testing.T) {
		tests := map[string]*apapproto.RunSummaryBundleRequest{
			"nil request":    nil,
			"missing run id": {},
		}

		for name, req := range tests {
			t.Run(name, func(t *testing.T) {
				server := ApapServer{}

				_, err := server.GetRunSummaryBundle(context.Background(), req)

				require.Error(t, err)
				expectedErr := message.New(message.EngineGrpcserverApiApapInsightsRunIdRequired)
				assert.Equal(t, expectedErr, err)
				assert.NoError(t, message.ValidateMetadataPlaceholders(err))
			})
		}
	})
}

func TestGetRunSummaryBundleInternal(t *testing.T) {
	t.Run("returns run details summary", func(t *testing.T) {
		resp, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeSuccess),
			func(_ context.Context, fn func(render.Session) error) error {
				return fn(nil)
			},
			runSummaryBundleTextLimitBytes,
		)

		require.NoError(t, err)
		require.Len(t, resp.GetPayloads(), 1)
		assert.Equal(t, "run_details", resp.GetPayloads()[0].GetName())
		assert.NotEmpty(t, resp.GetPayloads()[0].GetPromptFragment())
		assert.JSONEq(t, `{"RunId":"run-id","RunName":"Test Run","RecipeName":"code_hotspots","RunResult":"success","RunError":"","WorkloadType":"launch","WorkloadCmdline":"ls","TargetName":"local","StartTime":"","EndTime":"","Timeout":60}`, resp.GetPayloads()[0].GetPayload())
	})

	t.Run("returns run details for another recipe", func(t *testing.T) {
		resp, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("instruction_mix", run.RecipeSuccess),
			func(_ context.Context, fn func(render.Session) error) error {
				return fn(nil)
			},
			runSummaryBundleTextLimitBytes,
		)

		require.NoError(t, err)
		require.Len(t, resp.GetPayloads(), 1)
		assert.Equal(t, "run_details", resp.GetPayloads()[0].GetName())
	})

	t.Run("returns error when run was not successful", func(t *testing.T) {
		renderCalled := false

		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeFailureProfiling),
			func(context.Context, func(render.Session) error) error {
				renderCalled = true
				return nil
			},
			runSummaryBundleTextLimitBytes,
		)

		require.Error(t, err)
		expectedErr := message.New(message.EngineGrpcserverApiApapInsightsRunNotSuccessful).
			WithMetadata(map[string]string{
				"runID":     "run-id",
				"runResult": string(run.RecipeFailureProfiling),
			})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.False(t, renderCalled)
	})

	t.Run("returns error when render fails", func(t *testing.T) {
		renderErr := errors.New("render failed")

		_, err := getRunSummaryBundle(
			context.Background(),
			runDescriptionFor("code_hotspots", run.RecipeSuccess),
			func(context.Context, func(render.Session) error) error {
				return renderErr
			},
			runSummaryBundleTextLimitBytes,
		)

		require.Error(t, err)
		assert.Equal(t, renderErr, err)
	})

	t.Run("returns error when summarizer fails", func(t *testing.T) {
		desc := runDescriptionFor("code_hotspots", run.RecipeSuccess)
		desc.Parameters = map[string]any{"invalid": func() {}}

		_, err := getRunSummaryBundle(
			context.Background(),
			desc,
			func(_ context.Context, fn func(render.Session) error) error {
				return fn(nil)
			},
			runSummaryBundleTextLimitBytes,
		)

		require.Error(t, err)
		msg, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineInsightsRunSummaryMarshalFailed, msg.Code())
	})

	t.Run("enforces the bundle size limit", func(t *testing.T) {
		renderContent := func(_ context.Context, fn func(render.Session) error) error {
			return fn(nil)
		}
		desc := runDescriptionFor("code_hotspots", run.RecipeSuccess)
		resp, err := getRunSummaryBundle(
			context.Background(),
			desc, renderContent, runSummaryBundleTextLimitBytes,
		)
		require.NoError(t, err)
		payload := resp.GetPayloads()[0]
		size := len(payload.GetName()) + len(payload.GetPromptFragment()) + len(payload.GetPayload())

		_, err = getRunSummaryBundle(context.Background(), desc, renderContent, size)
		require.NoError(t, err)
		_, err = getRunSummaryBundle(context.Background(), desc, renderContent, size-1)

		require.Error(t, err)
		msg, ok := err.(*message.MessageImpl)
		require.True(t, ok)
		assert.Equal(t, message.EngineGrpcserverApiApapInsightsBundleSizeExceeded, msg.Code())
		assert.Equal(t, map[string]string{
			"totalBytes": strconv.Itoa(size),
			"limitBytes": strconv.Itoa(size - 1),
		}, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}
