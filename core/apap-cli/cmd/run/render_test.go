// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-cli/test"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockRenderPreparer struct {
	mock.Mock
}

func (m *mockRenderPreparer) PrepareRender(client apapproto.ApapClient, content *apapproto.ContentSelection, params run.RenderPreparationParams) (*apapproto.PrepareRenderResponse, error) {
	args := m.Called(client, content, params)
	return args.Get(0).(*apapproto.PrepareRenderResponse), args.Error(1)
}

func TestRenderCommand(t *testing.T) {
	t.Run("renders preparation and rendered output together", func(t *testing.T) {
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
					Id:     &apapproto.VisualizationId{Value: "viz_123"},
					Type:   "bar_chart",
					Title:  proto.String("My Chart"),
					Config: &structpb.Struct{},
				},
			},
		}
		preparer.On("PrepareRender", client, content, mock.Anything).Return(prepared, nil)

		// Mock InvokeRender
		invoker := &mockRenderInvoker{}
		expectedRender := &apapproto.InvokeRenderResponse{
			SessionId:        "session_abc",
			ConnectionString: "conn_str_xyz",
			Manifest: &apapproto.RenderManifest{
				Entry: []*apapproto.RenderManifestEntry{
					{
						ComponentType:          "function_profile",
						ComponentSchemaVersion: "v1",
						TableName:              "profile_table",
						RendererIndex:          &apapproto.NumericIndex{Value: 0},
					},
				},
			},
		}
		invoker.On("InvokeRender", client, content, &run.RenderInvocationParams{RendererConfig: prepared.Renderers, VisualizationConfig: prepared.Visualizations}).Return(expectedRender, nil)

		cmd := NewRenderCmd(cc, preparer, invoker)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{runID.Value})
		err := cmd.Execute()

		assert.NoError(t, err)

		output := cmdBuf.String()

		// High-level check: Must contain fields from both preparation and render output
		assert.Contains(t, output, "function_profile", "should contain prepared renderer name")
		assert.Contains(t, output, "profile_main", "should contain prepared renderer ID")
		assert.Contains(t, output, "viz_123", "should contain visualization ID")
		assert.Contains(t, output, "session_abc", "should contain session ID")
		assert.Contains(t, output, "conn_str_xyz", "should contain connection string")
		assert.Contains(t, output, "profile_table", "should contain manifest table name")
	})

	t.Run("returns error when client connector fails", func(t *testing.T) {
		expectedError := errors.New("connect_fail")
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, expectedError)

		cmd := NewRenderCmd(cc, &mockRenderPreparer{}, &mockRenderInvoker{})
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

		cmd := NewRenderCmd(cc, preparer, &mockRenderInvoker{})
		cmd.SetArgs([]string{runID.Value})
		err := cmd.Execute()

		assert.Equal(t, expectedError, err)
	})

	t.Run("returns error when invoke render fails", func(t *testing.T) {
		expectedError := errors.New("invoke_fail")
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "9012"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		preparer := &mockRenderPreparer{}
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{
				{Renderer: "testRenderer", Config: nil},
			},
		}
		preparer.On("PrepareRender", client, content, mock.Anything).Return(prepared, nil)

		invoker := &mockRenderInvoker{}
		invoker.On("InvokeRender", client, content, &run.RenderInvocationParams{RendererConfig: prepared.Renderers}).Return(&apapproto.InvokeRenderResponse{}, expectedError)

		cmd := NewRenderCmd(cc, preparer, invoker)
		cmd.SetArgs([]string{runID.Value})
		err := cmd.Execute()

		assert.ErrorContains(t, err, "invoke_fail")
	})

	t.Run("passes render params via --param", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "1234"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		preparer := &mockRenderPreparer{}
		prepared := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{
				{Renderer: "testRenderer", Config: nil},
			},
		}
		preparer.On("PrepareRender", client, content,
			mock.MatchedBy(func(params run.RenderPreparationParams) bool {
				if assert.NotNil(t, params.RenderParameterValues) {
					assert.Equal(t, "true",
						params.RenderParameterValues["enabled"].GetStringValue())
				}
				return true
			})).Return(prepared, nil)

		invoker := &mockRenderInvoker{}
		invoker.On("InvokeRender", client, content,
			&run.RenderInvocationParams{
				RendererConfig: prepared.Renderers,
			}).Return(&apapproto.InvokeRenderResponse{}, nil)

		cmd := NewRenderCmd(cc, preparer, invoker)
		cmd.SetArgs([]string{
			runID.Value,
			"--param=enabled=true",
		})
		err := cmd.Execute()

		assert.NoError(t, err)
	})

	t.Run("returns error on duplicate --param keys", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		cmd := NewRenderCmd(cc, &mockRenderPreparer{},
			&mockRenderInvoker{})
		cmd.SetArgs([]string{
			"1234",
			"--param=dup=1",
			"--param=dup=2",
		})

		err := cmd.Execute()
		assert.ErrorContains(t, err, "duplicate parameter: dup")
	})
}

func TestFilterPreparedRender(t *testing.T) {
	t.Run("filters visualizations and prunes unused renderers and visualization parameters", func(t *testing.T) {
		// Setup: three visualizations, two renderers
		preparation := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{
				{Renderer: "function_profile", Config: nil, Id: &apapproto.RendererId{Value: "profile_main"}},
				{Renderer: "summary_table", Config: nil, Id: &apapproto.RendererId{Value: "summary_main"}},
			},
			Visualizations: []*apapproto.VisualizationConfig{
				{Id: &apapproto.VisualizationId{Value: "viz1"}, Type: "bar_chart", RendererId: &apapproto.RendererId{Value: "profile_main"}},
				{Id: &apapproto.VisualizationId{Value: "viz2"}, Type: "table", RendererId: &apapproto.RendererId{Value: "summary_main"}},
				{Id: &apapproto.VisualizationId{Value: "viz3"}, Type: "line_chart", RendererId: &apapproto.RendererId{Value: "profile_main"}},
			},
			VisualizationParameters: map[string]string{
				"viz1.foo": "boo",
				"viz2.bar": "car",
				"viz3.baz": "shaz",
			},
		}

		// Simulate CLI requesting only viz1 and viz3
		requestedVisualizations := []string{"viz1", "viz3"}

		filtered, _ := filterPreparedRender(requestedVisualizations, preparation)

		// Check: only viz1 and viz3 should remain
		assert.Len(t, filtered.Visualizations, 2)
		vizIDs := map[string]bool{}
		for _, viz := range filtered.Visualizations {
			vizIDs[viz.Id.GetValue()] = true
		}
		assert.Contains(t, vizIDs, "viz1")
		assert.Contains(t, vizIDs, "viz3")
		assert.NotContains(t, vizIDs, "viz2")

		// Check: only profile_main renderer remains, because summary_main is only used by viz2 (filtered out)
		assert.Len(t, filtered.Renderers, 1)
		assert.Equal(t, "profile_main", filtered.Renderers[0].GetId().GetValue())

		// Check: only visualization parameters for viz1 and viz3 remain
		assert.Equal(t, filtered.VisualizationParameters, map[string]string{
			"viz1.foo": "boo",
			"viz3.baz": "shaz",
		})
	})
}

func TestFilterPreparedRender_NoFilter(t *testing.T) {
	t.Run("returns everything when no filter is provided", func(t *testing.T) {
		// Setup: two renderers, two visualizations
		preparation := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{
				{Renderer: "function_profile", Config: nil, Id: &apapproto.RendererId{Value: "profile_main"}},
				{Renderer: "summary_table", Config: nil, Id: &apapproto.RendererId{Value: "summary_main"}},
			},
			Visualizations: []*apapproto.VisualizationConfig{
				{Id: &apapproto.VisualizationId{Value: "viz1"}, Type: "bar_chart", RendererId: &apapproto.RendererId{Value: "profile_main"}},
				{Id: &apapproto.VisualizationId{Value: "viz2"}, Type: "table", RendererId: &apapproto.RendererId{Value: "summary_main"}},
			},
		}

		requestedVisualizations := []string{} // No specific visualizations requested

		filtered, _ := filterPreparedRender(requestedVisualizations, preparation)

		// Expect: all visualizations preserved
		assert.Equal(t, len(preparation.Visualizations), len(filtered.Visualizations))
		// Expect: all renderers preserved
		assert.Equal(t, len(preparation.Renderers), len(filtered.Renderers))
	})
}

func TestFilterPreparedRender_IgnoresNonVisualizationPlacements(t *testing.T) {
	preparation := &apapproto.PrepareRenderResponse{
		Renderers: []*apapproto.RendererConfig{
			{Renderer: "function_profile", Config: nil, Id: &apapproto.RendererId{Value: "profile_main"}},
			{Renderer: "controls", Config: nil, Id: &apapproto.RendererId{Value: "control_main"}},
		},
		Visualizations: []*apapproto.VisualizationConfig{
			{Id: &apapproto.VisualizationId{Value: "viz1"}, Type: "bar_chart", Placement: proto.String("visualizations"), RendererId: &apapproto.RendererId{Value: "profile_main"}},
			{Id: &apapproto.VisualizationId{Value: "control1"}, Type: "single_select_dropdown", Placement: proto.String("top_bar_filters"), RendererId: &apapproto.RendererId{Value: "control_main"}},
		},
	}

	filtered, err := filterPreparedRender([]string{}, preparation)
	require.NoError(t, err)
	require.Len(t, filtered.Visualizations, 1)
	assert.Equal(t, "viz1", filtered.Visualizations[0].GetId().GetValue())
	// This will change when pruning logic can extract render dependencies from visualizations' config.data_source info
	assert.Len(t, filtered.Renderers, 2)
	assert.Equal(t, "profile_main", filtered.Renderers[0].GetId().GetValue())
	assert.Equal(t, "control_main", filtered.Renderers[1].GetId().GetValue())

	filtered, err = filterPreparedRender([]string{"control1"}, preparation)
	assert.Nil(t, filtered)
	assert.Equal(t, message.New(message.CliCmdRunRenderVisualizationNotFound).WithMetadata(map[string]string{"id": "control1"}), err)
}

func TestFilterPreparedRender_MissingVisualizationFails(t *testing.T) {
	t.Run("returns error when requested visualization is not found", func(t *testing.T) {
		preparation := &apapproto.PrepareRenderResponse{
			Renderers: []*apapproto.RendererConfig{
				{Renderer: "function_profile", Config: nil, Id: &apapproto.RendererId{Value: "profile_main"}},
			},
			Visualizations: []*apapproto.VisualizationConfig{
				{Id: &apapproto.VisualizationId{Value: "viz1"}, Type: "bar_chart", RendererId: &apapproto.RendererId{Value: "profile_main"}},
			},
		}

		requestedVisualizations := []string{"nonexistent_viz"}

		filtered, err := filterPreparedRender(requestedVisualizations, preparation)

		assert.Nil(t, filtered)
		expectedMetadata := map[string]string{
			"id": requestedVisualizations[0],
		}
		expectedErr := message.New(message.CliCmdRunRenderVisualizationNotFound).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
	})
}

func TestWriteRenderCLIResponse(t *testing.T) {
	t.Run("WithError_JSONEnabled", func(t *testing.T) {
		test.SetViperJSON(t, true)

		invocationRequest := &run.RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "bar",
					Id:       &apapproto.RendererId{Value: "beta"},
				},
			},
		}

		invocationResponse := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Error{
						Error: &apapproto.Error{Message: "bar failed"},
					},
				},
			},
		}

		var buf bytes.Buffer
		err := WriteRenderCLIResponse(invocationRequest, renderResult{Invocation: invocationResponse}, &buf)
		assert.ErrorIs(t, clijson.ErrorAlreadyHandled, err)

		output := buf.String()
		fmt.Printf("%s\n", output)
		assert.Contains(t, output, `"code":"-1"`)
		assert.Contains(t, output, `The following renderers failed: \nbar[0=beta]`)
		assert.Contains(t, output, `"invocation_statuses":[{"Status":{"Error":{"message":"bar failed"}}}]`)
		assert.NotContains(t, output, `"preparation":null`)
	})

	t.Run("WithError_JSONDisabled", func(t *testing.T) {
		test.SetViperJSON(t, false)

		invocationRequest := &run.RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "alpha",
					Id:       &apapproto.RendererId{Value: "x1"},
				},
				{
					Renderer: "beta",
					Id:       &apapproto.RendererId{Value: "x2"},
				},
			},
		}

		invocationResponse := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Error{
						Error: &apapproto.Error{Message: "internal renderer error"},
					},
				},
				{
					Status: &apapproto.RendererInvocationStatus_Error{
						Error: &apapproto.Error{Message: "internal renderer error"},
					},
				},
			},
		}

		var buf bytes.Buffer
		err := WriteRenderCLIResponse(invocationRequest, renderResult{Invocation: invocationResponse}, &buf)
		require.Error(t, err)
		msg, err := message.LookupMessage(err)
		require.NoError(t, err)
		assert.Contains(t, msg.String(), "The following renderers failed: \nalpha[0=x1]")
		assert.Contains(t, msg.String(), "\nbeta[1=x2]")

		output := buf.String()
		assert.Contains(t, output, `"code":"-1"`)
		assert.Contains(t, output, `The following renderers failed: \nalpha[0=x1]`)
		assert.Contains(t, output, `beta[1=x2]`)
		assert.Contains(t, output, `invocation_statuses":[{"Status":{"Error":{"message":"internal renderer error"}}},{"Status":{"Error":{"message":"internal renderer error"}}}]`)
		assert.NotContains(t, output, `"preparation":null`)
	})

	t.Run("WithPending_JSONDisabled", func(t *testing.T) {
		test.SetViperJSON(t, false)

		invocationRequest := &run.RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "pending-renderer",
					Id:       &apapproto.RendererId{Value: "pending-id"},
				},
			},
		}

		invocationResponse := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Pending{
						Pending: &apapproto.Pending{},
					},
				},
			},
		}

		var buf bytes.Buffer
		err := WriteRenderCLIResponse(invocationRequest, renderResult{Invocation: invocationResponse}, &buf)
		require.Error(t, err)
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		assert.Equal(t, message.CliCmdRunRenderRendererPending, msg.Code())
		assert.Equal(t, map[string]string{"renderers": "\npending-renderer[0=pending-id]"}, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		output := buf.String()
		assert.Contains(t, output, `"code":"-1"`)
		assert.Contains(t, output, `"message_code":"cli.cmd.run.render.RENDERER_PENDING"`)
		assert.Contains(t, output, `The following renderers did not run, because they depend on data which has not yet been transferred from the target machine: \npending-renderer[0=pending-id]`)
		assert.Contains(t, output, `"invocation_statuses":[{"Status":{"Pending":{}}}]`)
		assert.NotContains(t, output, `"preparation":null`)
	})

	t.Run("WithPending_JSONEnabled", func(t *testing.T) {
		test.SetViperJSON(t, true)

		invocationRequest := &run.RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "pending-renderer",
					Id:       &apapproto.RendererId{Value: "pending-id"},
				},
			},
		}

		invocationResponse := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Pending{
						Pending: &apapproto.Pending{},
					},
				},
			},
		}

		var buf bytes.Buffer
		err := WriteRenderCLIResponse(invocationRequest, renderResult{Invocation: invocationResponse}, &buf)
		require.Equal(t, clijson.ErrorAlreadyHandled, err)

		output := buf.String()
		assert.Contains(t, output, `"code":"-1"`)
		assert.Contains(t, output, `"severity":"Error"`)
		assert.Contains(t, output, `"message_code":"cli.cmd.run.render.RENDERER_PENDING"`)
		assert.NotContains(t, output, `"preparation":null`)
	})

	t.Run("WithErrorAndPending_JSONDisabled_ReturnsFailure", func(t *testing.T) {
		test.SetViperJSON(t, false)

		invocationRequest := &run.RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "failed-renderer",
					Id:       &apapproto.RendererId{Value: "failed-id"},
				},
				{
					Renderer: "pending-renderer",
					Id:       &apapproto.RendererId{Value: "pending-id"},
				},
			},
		}

		invocationResponse := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Error{
						Error: &apapproto.Error{Message: "renderer failed"},
					},
				},
				{
					Status: &apapproto.RendererInvocationStatus_Pending{
						Pending: &apapproto.Pending{},
					},
				},
			},
		}

		var buf bytes.Buffer
		err := WriteRenderCLIResponse(invocationRequest, renderResult{Invocation: invocationResponse}, &buf)
		require.Error(t, err)
		var msg message.Message
		require.True(t, errors.As(err, &msg))
		assert.Equal(t, message.CliCmdRunRenderRendererFailed, msg.Code())
		assert.Equal(t, map[string]string{"failures": "\nfailed-renderer[0=failed-id] renderer failed"}, msg.Metadata())
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))

		output := buf.String()
		assert.Contains(t, output, `"code":"-1"`)
		assert.Contains(t, output, `"message_code":"cli.cmd.run.render.RENDERER_FAILED"`)
		assert.Contains(t, output, `The following renderers failed: \nfailed-renderer[0=failed-id] renderer failed`)
		assert.Contains(t, output, `"invocation_statuses":[{"Status":{"Error":{"message":"renderer failed"}}},{"Status":{"Pending":{}}}]`)
		assert.NotContains(t, output, `cli.cmd.run.render.RENDERER_PENDING`)
		assert.NotContains(t, output, `pending-renderer[1=pending-id]`)
	})

	t.Run("NoError_JSONDisabled", func(t *testing.T) {
		test.SetViperJSON(t, false)

		invocationRequest := &run.RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "good-renderer",
				},
			},
		}

		invocationResponse := &apapproto.InvokeRenderResponse{
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Success{
						Success: &apapproto.Success{},
					},
				},
			},
		}

		var buf bytes.Buffer
		err := WriteRenderCLIResponse(invocationRequest, renderResult{Invocation: invocationResponse}, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, `"code":"0"`)
		assert.Contains(t, output, `"error":{"message_code":"","severity":"","message":"","explanation":"","advice":"","locale":"","metadata":null}`)
		assert.Contains(t, output, `"invocation_statuses":[{"Status":{"Success":{}}}]`)
		assert.NotContains(t, output, `"preparation":null`)
	})
}

func createMockPreparer(compatibilityWarning message.Message, client *apapprotomocks.ApapClient, content *apapproto.ContentSelection) (*mockRenderPreparer, []*apapproto.RendererConfig) {
	preparer := &mockRenderPreparer{}
	prepared := &apapproto.PrepareRenderResponse{
		Renderers: []*apapproto.RendererConfig{
			{Renderer: "testRenderer", Config: nil},
		},
		CompatibilityWarning: message.BuildErrorChain(compatibilityWarning),
	}
	preparer.On("PrepareRender", client, content, mock.Anything).Return(prepared, nil)
	return preparer, prepared.Renderers
}

func createMockInvoker(errMsg string, client *apapprotomocks.ApapClient, content *apapproto.ContentSelection, renderers []*apapproto.RendererConfig) *mockRenderInvoker {
	invoker := &mockRenderInvoker{}
	invocationResponse := &apapproto.InvokeRenderResponse{SessionId: "session_abc"}
	if errMsg != "" {
		invocationResponse.InvocationStatuses = []*apapproto.RendererInvocationStatus{
			{
				Status: &apapproto.RendererInvocationStatus_Error{
					Error: &apapproto.Error{Message: "bar failed"},
				},
			},
		}
	}

	invoker.On("InvokeRender", client, content, &run.RenderInvocationParams{RendererConfig: renderers}).Return(invocationResponse, nil)
	return invoker
}

func TestRenderCompatibilityWarningHandling(t *testing.T) {
	client := apapprotomocks.NewApapClient(t)
	cc := &mocks.MockAutostartClientConnector{}
	cc.SetClient(client, nil)

	runID := &apapproto.RunId{Value: "9012"}
	content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

	warning := message.New(message.EngineRenderCompatibilityIncompatibleTooOld).WithCause(fmt.Errorf("some internal error"))

	t.Run("doesn't return error when PrepareRender returns 'incompatible' compatibility warning, but InvokeRender succeeds", func(t *testing.T) {
		preparer, renderers := createMockPreparer(warning, client, content)
		invoker := createMockInvoker("", client, content, renderers)

		cmd := NewRenderCmd(cc, preparer, invoker)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{runID.Value})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		output := cmdBuf.String()

		assert.NoError(t, err)
		assert.Contains(t, output, `{"code":"0","error":{"message_code":""`)

		// should still contain a compatibility warning in the prepareRender JSON
		assert.Contains(t, output, `"compatibilityWarning":{"message_code":"engine.render.compatibility.incompatible.TOO_OLD"`)
	})
	t.Run("compatibility warning from PrepareRender takes precedence over InvokeRender failures", func(t *testing.T) {
		preparer, renderers := createMockPreparer(warning, client, content)
		invoker := createMockInvoker("bar failed", client, content, renderers)

		cmd := NewRenderCmd(cc, preparer, invoker)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{runID.Value})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		output := cmdBuf.String()

		// should result in error, JSON preparation / invocation output should contain non-zero code
		assert.ErrorIs(t, err, clijson.ErrorAlreadyHandled)
		assert.Contains(t, output, `{"code":"-1","error":{"message_code":"engine.render.compatibility.incompatible.TOO_OLD"`)

		// should contain full JSON preparation / invocation output
		assert.Contains(t, output, `"preparation":{"renderers":[{"renderer":"testRenderer"}]`)
		assert.Contains(t, output, `"compatibilityWarning":{"message_code":"engine.render.compatibility.incompatible.TOO_OLD"`)
		assert.Contains(t, output, `"invocation":{"session_id":"session_abc","invocation_statuses":[{"Status":{"Error":{"message":"bar failed"}}}]}`)

		// should print warning message **as an error** below
		assert.Contains(t, output, "[Error]:")
		assert.Contains(t, output, fmt.Sprintf("cannot be rendered in your current version of %v", terminology.GetProductFullName()))
		assert.Contains(t, output, "[Code]: engine.render.compatibility.incompatible.TOO_OLD")
	})
	t.Run("doesn't show indeterminability warning if rendering succeeds", func(t *testing.T) {
		preparer, renderers := createMockPreparer(message.New(message.EngineRenderCompatibilityIndeterminableTooNew), client, content)
		invoker := createMockInvoker("", client, content, renderers)

		cmd := NewRenderCmd(cc, preparer, invoker)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{runID.Value})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		output := cmdBuf.String()

		// should be no error
		assert.NoError(t, err)
		assert.Contains(t, output, `{"code":"0","error":{"message_code":""`)

		// should contain a compatibility warning in the prepareRender JSON
		assert.Contains(t, output, `"compatibilityWarning":{"message_code":"engine.render.compatibility.indeterminable.TOO_NEW"`)

		// should not print warning below
		assert.NotContains(t, output, "[Warning]:")
	})
	t.Run("prints indeterminability warning and failure message if rendering fails", func(t *testing.T) {
		preparer, renderers := createMockPreparer(message.New(message.EngineRenderCompatibilityIndeterminableTooNew), client, content)
		invoker := createMockInvoker("bar failed", client, content, renderers)

		cmd := NewRenderCmd(cc, preparer, invoker)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{runID.Value})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		output := cmdBuf.String()

		// main error should be failure message
		assert.NotNil(t, err)
		var msg message.Message
		assert.True(t, errors.As(err, &msg))
		assert.Equal(t, message.CliCmdRunRenderRendererFailed, msg.Code())
		assert.Contains(t, output, `{"code":"-1","error":{"message_code":"cli.cmd.run.render.RENDERER_FAILED"`)

		// should contain a compatibility warning in the prepareRender JSON
		assert.Contains(t, output, `"compatibilityWarning":{"message_code":"engine.render.compatibility.indeterminable.TOO_NEW"`)

		// should print warning below
		assert.Contains(t, output, fmt.Sprintf("[Warning]: %v cannot verify", terminology.GetProductFullName()))
	})
}
