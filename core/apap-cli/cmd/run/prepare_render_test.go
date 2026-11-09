// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-cli/test"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/api/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/api/mocks"
)

func TestPrepareRenderCommand(t *testing.T) {
	t.Run("prepares render output successfully", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "1234"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		// Mock PrepareRender
		preparer := &mockRenderPreparer{}
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{
				{
					Renderer: "function_profile",
					Config:   nil,
					Id:       &apapproto.RendererId{Value: "profile_main"},
				},
			},
			Visualizations: []*apapproto.VisualizationConfig{
				{
					Id:    &apapproto.VisualizationId{Value: "viz_123"},
					Type:  "bar_chart",
					Title: proto.String("My Chart"),
				},
			},
		}
		preparer.On("PrepareRender", client, content, mock.Anything).Return(prepared, nil)

		cmd := NewPrepareRenderCmd(cc, preparer)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{runID.Value})
		err := cmd.Execute()

		assert.NoError(t, err)

		output := cmdBuf.String()

		assert.Contains(t, output, "function_profile", "should contain prepared renderer name")
		assert.Contains(t, output, "profile_main", "should contain prepared renderer ID")
		assert.Contains(t, output, "viz_123", "should contain visualization ID")
	})

	t.Run("passes render params via --param", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "1234"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		preparer := &mockRenderPreparer{}
		prepared := &apapproto.PrepareRenderResponse{}

		preparer.On("PrepareRender", client, content,
			mock.MatchedBy(func(params run.RenderPreparationParams) bool {
				if assert.NotNil(t, params.RenderParameterValues) {
					assert.Equal(t, "10",
						params.RenderParameterValues["threshold"].GetStringValue())
					assert.Equal(t, "fast",
						params.RenderParameterValues["mode"].GetStringValue())
				}
				return true
			})).Return(prepared, nil)

		cmd := NewPrepareRenderCmd(cc, preparer)
		cmd.SetArgs([]string{
			runID.Value,
			"--param=threshold=10",
			"--param=mode=fast",
		})
		err := cmd.Execute()

		assert.NoError(t, err)
	})

	t.Run("returns error on duplicate --param keys", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewPrepareRenderCmd(cc, &mockRenderPreparer{})
		cmd.SetArgs([]string{
			"1234",
			"--param=dup=1",
			"--param=dup=2",
		})

		err := cmd.Execute()
		assert.ErrorContains(t, err, "duplicate parameter: dup")
	})

	t.Run("returns error when client connector fails", func(t *testing.T) {
		expectedError := errors.New("connect_fail")
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, expectedError)

		cmd := NewPrepareRenderCmd(cc, &mockRenderPreparer{})
		cmd.SetArgs([]string{"some_run_id"})
		err := cmd.Execute()

		assert.Equal(t, expectedError, err)
	})

	t.Run("returns error when prepare render fails", func(t *testing.T) {
		expectedError := errors.New("prepare_fail")
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "5678"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		preparer := &mockRenderPreparer{}
		preparer.On("PrepareRender", client, content, mock.Anything).Return(&apapproto.PrepareRenderResponse{}, expectedError)

		cmd := NewPrepareRenderCmd(cc, preparer)
		cmd.SetArgs([]string{runID.Value})
		err := cmd.Execute()

		assert.Equal(t, expectedError, err)
	})

	t.Run("includes render parameters in JSON output", func(t *testing.T) {
		test.SetViperJSON(t, true)
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "1234"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		preparer := &mockRenderPreparer{}
		prepared := &apapproto.PrepareRenderResponse{
			RenderParameters: map[string]*apapproto.RenderParameterDetails{
				"thread_id": {
					Value:        structpb.NewStringValue("1234"),
					DefaultValue: structpb.NewStringValue("1234"),
				},
				"mode": {
					Value:        structpb.NewStringValue("auto"),
					DefaultValue: structpb.NewStringValue("auto"),
					Options:      []string{"auto", "manual"},
				},
			},
		}
		preparer.On("PrepareRender", client, content, mock.Anything).Return(prepared, nil)

		cmd := NewPrepareRenderCmd(cc, preparer)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{runID.Value})

		err := cmd.Execute()
		assert.NoError(t, err)

		output := cmdBuf.String()
		assert.True(t, utils.IsValidJSON(output))

		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(output), &decoded))

		data, ok := decoded["data"].(map[string]any)
		require.True(t, ok)

		renderParams, ok := data["render_parameters"].(map[string]any)
		require.True(t, ok)

		threadParams, ok := renderParams["thread_id"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "1234", threadParams["value"])
		assert.Equal(t, "1234", threadParams["default_value"])

		modeParams, ok := renderParams["mode"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "auto", modeParams["value"])
		assert.Equal(t, "auto", modeParams["default_value"])
		assert.ElementsMatch(t, []any{"auto", "manual"}, modeParams["options"])

		assert.NotContains(t, data, "visualization_parameters")
	})
}

func TestPrepareRenderCommand_FilteringViaCLI(t *testing.T) {
	t.Run("filters visualizations via --visualization arg", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "run_1"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		preparer := &mockRenderPreparer{}
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{
				{Renderer: "profile", Config: nil, Id: &apapproto.RendererId{Value: "profile_renderer"}},
				{Renderer: "summary", Config: nil, Id: &apapproto.RendererId{Value: "summary_renderer"}},
			},
			Visualizations: []*apapproto.VisualizationConfig{
				{Id: &apapproto.VisualizationId{Value: "viz_profile"}, Type: "bar_chart", RendererId: &apapproto.RendererId{Value: "profile_renderer"}},
				{Id: &apapproto.VisualizationId{Value: "viz_summary"}, Type: "table", RendererId: &apapproto.RendererId{Value: "summary_renderer"}},
			},
		}
		preparer.On("PrepareRender", client, content, mock.Anything).Return(prepared, nil)

		cmd := NewPrepareRenderCmd(cc, preparer)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"run_1", "--visualization=viz_profile"})

		err := cmd.Execute()

		assert.NoError(t, err)

		output := cmdBuf.String()

		assert.Contains(t, output, "viz_profile")
		assert.NotContains(t, output, "viz_summary")
	})
}

func TestRenderCommandsRejectRecipeSelectionPolicy(t *testing.T) {
	for _, command := range []string{"prepare-render", "render"} {
		t.Run(command, func(t *testing.T) {
			// Given
			cmd := NewPrepareRenderCmd(nil, nil)
			if command == "render" {
				cmd = NewRenderCmd(nil, nil, nil)
			}

			// When
			err := cmd.ParseFlags([]string{"--recipe-selection-policy", "from-content"})

			// Then
			require.EqualError(t, err, "unknown flag: --recipe-selection-policy")
			require.NotContains(t, cmd.UsageString(), "recipe-selection-policy")
		})
	}
}

func TestPrepareRenderCompatibilityWarning(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	runID := &apapproto.RunId{Value: "5678"}
	content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

	metadata := map[string]string{
		"runID":                      runID.Value,
		"runVersion":                 "0.5.0",
		"currentEngineVersion":       "0.10.0",
		"currentMinSupportedVersion": "0.8.0",
		"minCompatibleVersion":       "0.5.0",
		"maxCompatibleVersion":       "0.7.0",
	}
	warning := message.New(message.EngineRenderCompatibilityIncompatibleTooOld).WithCause(fmt.Errorf("some internal error")).WithMetadata(metadata)

	response := &apapproto.PrepareRenderResponse{
		Renderers: []*apapproto.RendererConfig{
			{Renderer: "profile", Config: nil, Id: &apapproto.RendererId{Value: "profile_renderer"}},
			{Renderer: "summary", Config: nil, Id: &apapproto.RendererId{Value: "summary_renderer"}},
		},
		Visualizations: []*apapproto.VisualizationConfig{
			{Id: &apapproto.VisualizationId{Value: "viz_profile"}, Type: "bar_chart", RendererId: &apapproto.RendererId{Value: "profile_renderer"}},
			{Id: &apapproto.VisualizationId{Value: "viz_summary"}, Type: "table", RendererId: &apapproto.RendererId{Value: "summary_renderer"}},
		},
		CompatibilityWarning: message.BuildErrorChain(warning),
	}

	preparer := &mockRenderPreparer{}
	preparer.On("PrepareRender", client, content, mock.Anything).Return(response, nil)

	t.Run("if a compatibility warning is returned, it is shown to the user", func(t *testing.T) {
		cmd := NewPrepareRenderCmd(cc, preparer)
		cmd.SetArgs([]string{runID.Value})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		output := cmdBuf.String()

		// no error, exit code should be 0
		assert.NoError(t, err)
		assert.Contains(t, output, `{"code":"0","error":{"message_code":""`)

		// should contain the JSON preparation output
		assert.Contains(t, output, `"data":{"renderers":[{"renderer":"profile","id":{"value":"profile_renderer"}}`)

		// should contain a compatibility warning in the prepareRender JSON
		assert.Contains(t, output, `"compatibilityWarning":{"message_code":"engine.render.compatibility.incompatible.TOO_OLD"`)

		// should contain the error message output as plaintext
		assert.Contains(t, output, "[Warning]:")
		assert.Contains(t, output, fmt.Sprintf("Your current version of %v cannot render run", terminology.GetProductFullName()))
	})
	t.Run("if a compatibility warning is returned, it is shown to the user in JSON", func(t *testing.T) {
		test.SetViperJSON(t, true)
		cmd := NewPrepareRenderCmd(cc, preparer)
		cmd.SetArgs([]string{runID.Value})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		output := cmdBuf.String()

		// no error, exit code should be 0
		assert.NoError(t, err)
		assert.Contains(t, output, `{"code":"0","error":{"message_code":""`)

		assert.True(t, utils.IsValidJSON(output))

		// should contain the JSON preparation output
		assert.Contains(t, output, `"data":{"renderers":[{"renderer":"profile","id":{"value":"profile_renderer"}}`)

		// should contain a compatibility warning in the prepareRender JSON
		assert.Contains(t, output, `"compatibilityWarning":{"message_code":"engine.render.compatibility.incompatible.TOO_OLD"`)
	})
}
