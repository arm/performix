// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/run"
	"github.com/Arm-Debug/apap-cli/apap-cli/test"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

type mockRenderInvoker struct {
	mock.Mock
}

func (m *mockRenderInvoker) InvokeRender(client apapproto.ApapClient, content *apapproto.ContentSelection, params *run.RenderInvocationParams) (*apapproto.InvokeRenderResponse, error) {
	mockArgs := m.Called(client, content, params)
	return mockArgs.Get(0).(*apapproto.InvokeRenderResponse), mockArgs.Error(1)
}

func TestInvokeRenderCommand(t *testing.T) {

	t.Run("returns rendered data when called by id", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "1234"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		renderer := mockRenderInvoker{}
		expectedConnectionString := "myConnString"
		renderer.On("InvokeRender", client, content, &run.RenderInvocationParams{}).Return(&apapproto.InvokeRenderResponse{ConnectionString: expectedConnectionString}, nil)

		cmd := NewInvokeRenderCmd(cc, &renderer)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{runID.Value})
		err := cmd.Execute()

		assert.NoError(t, err)

		assert.Contains(t, cmdBuf.String(), expectedConnectionString)
	})

	t.Run("passes renderer config through to service", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "123"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}
		renderParams := run.RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{Renderer: "foo", Config: &structpb.Struct{Fields: map[string]*structpb.Value{"bar": structpb.NewStringValue("baz")}}},
				{Renderer: "baz", Config: &structpb.Struct{Fields: map[string]*structpb.Value{"waz": structpb.NewStringValue("maz")}}},
			},
		}

		renderer := mockRenderInvoker{}
		expectedConnectionString := "myConnString"
		renderer.On("InvokeRender", client, content, &renderParams).Return(&apapproto.InvokeRenderResponse{ConnectionString: expectedConnectionString}, nil)

		cmd := NewInvokeRenderCmd(cc, &renderer)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"123", `--renderer=foo={"bar": "baz"}`, `--renderer=baz={"waz": "maz"}`})
		err := cmd.Execute()

		assert.NoError(t, err)

		assert.Contains(t, cmdBuf.String(), expectedConnectionString)
	})

	t.Run("returns error when client connector fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(nil, expectedError)

		cmd := NewInvokeRenderCmd(cc, &mockRenderInvoker{})
		cmd.SetArgs([]string{"asdf"})
		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("returns error when service fails", func(t *testing.T) {
		expectedError := errors.New("rekt")
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "123"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		renderer := &mockRenderInvoker{}
		renderer.On("InvokeRender", client, content, &run.RenderInvocationParams{}).Return(&apapproto.InvokeRenderResponse{}, expectedError)

		cmd := NewInvokeRenderCmd(cc, renderer)
		cmd.SetArgs([]string{"123"})
		err := cmd.Execute()

		assert.ErrorContains(t, err, "rekt")
	})

	t.Run("parses renderer ID when supplied", func(t *testing.T) {
		client := apapprotomocks.NewApapClient(t)
		cc := &mocks.MockAutostartClientConnector{}
		cc.SetClient(client, nil)

		runID := &apapproto.RunId{Value: "456"}
		content := &apapproto.ContentSelection{Runs: []*apapproto.RunId{runID}}

		expectedConfigs := []*apapproto.RendererConfig{
			{Renderer: "foo", Config: &structpb.Struct{Fields: map[string]*structpb.Value{"bar": structpb.NewStringValue("baz")}}},
			{Renderer: "baz", Config: &structpb.Struct{Fields: map[string]*structpb.Value{"waz": structpb.NewStringValue("maz")}}, Id: &apapproto.RendererId{Value: "my-id"}},
		}

		renderParams := run.RenderInvocationParams{
			RendererConfig: expectedConfigs,
		}

		renderer := mockRenderInvoker{}
		expectedConnectionString := "someConnectionString"
		renderer.On("InvokeRender", client, content, &renderParams).Return(&apapproto.InvokeRenderResponse{ConnectionString: expectedConnectionString}, nil)

		cmd := NewInvokeRenderCmd(cc, &renderer)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{
			"456",
			`--renderer=foo={"bar": "baz"}`,
			`--renderer=baz:my-id={"waz": "maz"}`,
		})
		err := cmd.Execute()

		assert.NoError(t, err)
		assert.Contains(t, cmdBuf.String(), expectedConnectionString)
	})
}

func TestWriteInvokeRenderCLIResponse(t *testing.T) {
	t.Run("EquivalentToWriteRenderCLIResponse", func(t *testing.T) {
		test.SetViperJSON(t, false)

		invocationRequest := &run.RenderInvocationParams{
			RendererConfig: []*apapproto.RendererConfig{
				{
					Renderer: "foo",
					Id:       &apapproto.RendererId{Value: "alpha"},
				},
			},
		}

		invocationResponse := &apapproto.InvokeRenderResponse{
			SessionId: "abc123",
			Manifest:  &apapproto.RenderManifest{},
			InvocationStatuses: []*apapproto.RendererInvocationStatus{
				{
					Status: &apapproto.RendererInvocationStatus_Success{
						Success: &apapproto.Success{},
					},
				},
			},
		}

		var bufA, bufB bytes.Buffer

		// This function now internally passes the request and response
		errA := WriteInvokeRenderCLIResponse(invocationRequest, invocationResponse, &bufA)
		errB := WriteRenderCLIResponse(invocationRequest, renderResult{Invocation: invocationResponse}, &bufB)

		require.NoError(t, errA)
		require.NoError(t, errB)
		assert.Equal(t, bufA.String(), bufB.String(), "WriteInvokeRenderCLIResponse should delegate exactly to WriteRenderCLIResponse")
	})
}
