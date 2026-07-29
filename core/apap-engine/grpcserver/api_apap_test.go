// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Arm-Debug/apap-cli/apap-engine/agent"
	"github.com/Arm-Debug/apap-cli/apap-engine/agent/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe/runtime"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipeparser"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/render/sessionfactory"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool/deployer"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
	"github.com/Arm-Debug/apap-cli/atperf-compatibility/compatibility"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	targetagentmocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
	"github.com/Arm-Debug/apap-cli/clients/go/targetagentproto"
)

type MockSleeper struct {
}

func (m MockSleeper) Sleep(ms int) {
}

type mockParameterPipeline struct {
	mock.Mock
}

type MockRecipeReader struct {
	mock.Mock
}

type testRenderStage struct {
	outputFn func(renderParams map[string]any) recipe.RenderOutput
}

func (s *testRenderStage) Execute(ctx recipe.ExecutionContext, stageContext *recipe.StageContext) (func(), error) {
	renderParams := ctx.GetRecipeCtx().RenderParamValues
	output := s.outputFn(renderParams)
	return nil, stageContext.RendererNotifier.OnRender(output)
}

func (s *testRenderStage) Name() string { return "testRenderStage" }

func setupPrepareRenderServer(t *testing.T, outputFn func(renderParams map[string]any) recipe.RenderOutput) (*ApapServer, run.RunID) {
	t.Helper()

	rc, runID := createRunCollectionWithRun(t)
	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "thread_id", Type: parameters.RenderParameterValueTypeNumber, IsArray: false, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{outputFn: outputFn},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := &ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	return server, runID
}

func (s *MockRecipeReader) ReadRecipes(errHandler func(string, error)) (recipes map[string]recipe.Recipe, err error) {
	mockArgs := s.Called(errHandler)
	return mockArgs.Get(0).(map[string]recipe.Recipe), mockArgs.Error(1)
}

func (s *MockRecipeReader) ReadRecipe(file string) (recipeInfo recipe.Recipe, err error) {
	mockArgs := s.Called(file)
	return mockArgs.Get(0).(recipe.Recipe), mockArgs.Error(1)
}

func (s *MockRecipeReader) IsRecipeValidFile(name string) bool {
	return strings.Contains(name, ".js")
}

func (m *mockParameterPipeline) EvaluateOptions(ctx context.Context, sc *runtime.StageConfiguration, stageContext *recipe.StageContext) (recipe.ParameterOptions, error) {
	args := m.Called(ctx, sc, stageContext)
	return args.Get(0).(recipe.ParameterOptions), args.Error(1)
}

func (m *mockParameterPipeline) Validate(ctx context.Context, stageFactory runtime.ValidationStageFactory, sc *runtime.StageConfiguration) (recipe.ParamValidation, error) {
	args := m.Called(ctx, stageFactory, sc)
	return args.Get(0).(recipe.ParamValidation), args.Error(1)
}

const experimentalRecipeName = "experimental-recipe"

func createRunCollectionWithRun(t *testing.T) (*run.RunCollection, run.RunID) {
	t.Helper()

	rc, err := run.NewRunCollection(filepath.Join(t.TempDir(), "runs"))
	require.NoError(t, err)

	builder, err := rc.RunBuilder()
	require.NoError(t, err)

	metadata := &cdf.Metadata{
		EngineVersion: "1.0.0",
		Name:          "Test Run",
		StartTime:     util.CurrentTime(),
		EndTime:       util.CurrentTime(),
		RecipeName:    "cpu_microarchitecture",
		TargetName:    "local",
		TargetConfig:  target.JSONTarget{Value: &target.JSONLocalTarget{}},
		RunResult:     string(run.RecipeSuccess),
	}

	runID, err := rc.CreateRun(builder, metadata)
	require.NoError(t, err)
	return rc, runID
}

func newTestPackageManager(t *testing.T) *packages.PackageManager {
	t.Helper()

	root := t.TempDir()
	return packages.NewPackageManager(root, filepath.Join(root, "extensions"))
}

func TestGetVersion(t *testing.T) {
	server := ApapServer{}

	version, err := server.GetVersion(context.Background(), &emptypb.Empty{})

	assert.NoError(t, err)
	assert.NotEqual(t, "0.0.0-dev", version.GetVersion())
}

func TestPrepareRender_WithRenderParameters(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)

	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "thread_id", Type: parameters.RenderParameterValueTypeNumber, IsArray: false, Order: 0},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		RenderParameters: map[string]*structpb.Value{
			"thread_id": structpb.NewNumberValue(0.75),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestPrepareRender_ConvertsStringRenderParameterNumber(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		assert.Equal(t, float64(0.75), renderParams["thread_id"])

		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		RenderParameters: map[string]*structpb.Value{
			"thread_id": structpb.NewStringValue("0.75"),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)

	threadDetail, ok := resp.RenderParameters["thread_id"]
	require.True(t, ok)
	assert.Equal(t, structpb.NewNumberValue(0.75), threadDetail.GetValue())
}

func TestPrepareRender_AcceptsExplicitNullRenderParameter(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		require.Contains(t, renderParams, "thread_id")
		assert.Nil(t, renderParams["thread_id"])

		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		RenderParameters: map[string]*structpb.Value{
			"thread_id": structpb.NewNullValue(),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)

	threadDetail, ok := resp.RenderParameters["thread_id"]
	require.True(t, ok)
	assert.Equal(t, structpb.NullValue_NULL_VALUE, threadDetail.GetValue().GetNullValue())
}

func TestPrepareRender_ConvertsStringRenderParameterNumberArray(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)

	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "time_range", Type: parameters.RenderParameterValueTypeNumber, IsArray: true, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{outputFn: func(renderParams map[string]any) recipe.RenderOutput {
					assert.Equal(t, []any{float64(1000), float64(10000)}, renderParams["time_range"])

					return recipe.RenderOutput{
						Renderers: []recipe.RendererConfig{
							{Type: "test_renderer", ID: "render"},
						},
					}
				}},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		RenderParameters: map[string]*structpb.Value{
			"time_range": structpb.NewStringValue("[1000, 10000]"),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)

	timeRangeDetail, ok := resp.RenderParameters["time_range"]
	require.True(t, ok)
	assert.Equal(t, structpb.NewListValue(&structpb.ListValue{
		Values: []*structpb.Value{
			structpb.NewNumberValue(1000),
			structpb.NewNumberValue(10000),
		},
	}), timeRangeDetail.GetValue())
}

func TestPrepareRender_KeepsNumberLikeStringRenderParameterArray(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)

	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "thread_ids", Type: parameters.RenderParameterValueTypeString, IsArray: true, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{outputFn: func(renderParams map[string]any) recipe.RenderOutput {
					assert.Equal(t, []any{"1000", "10000"}, renderParams["thread_ids"])
					return recipe.RenderOutput{
						Renderers: []recipe.RendererConfig{
							{Type: "test_renderer", ID: "render"},
						},
					}
				}},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		RenderParameters: map[string]*structpb.Value{
			"thread_ids": structpb.NewStringValue(`["1000", "10000"]`),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)

	threadIDsDetail, ok := resp.RenderParameters["thread_ids"]
	require.True(t, ok)
	assert.Equal(t, structpb.NewListValue(&structpb.ListValue{
		Values: []*structpb.Value{
			structpb.NewStringValue("1000"),
			structpb.NewStringValue("10000"),
		},
	}), threadIDsDetail.GetValue())
}

func TestPrepareRender_IncludesRenderAndVisualizationParameters(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              "test_vis",
					ID:                "flat",
					RendererID:        "render",
					Title:             "static",
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)

	threadDetail, ok := resp.RenderParameters["thread_id"]
	require.True(t, ok)
	assert.Equal(t, structpb.NullValue_NULL_VALUE, threadDetail.GetValue().GetNullValue())
	assert.Equal(t, structpb.NullValue_NULL_VALUE, threadDetail.GetDefaultValue().GetNullValue())

	renderParam, ok := resp.VisualizationParameters["flat.selected_thread_id"]
	require.True(t, ok)
	assert.Equal(t, "thread_id", renderParam)
}

func TestPrepareRender_IncludesNullRenderParameterDetails(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)
	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "mode", Type: parameters.RenderParameterValueTypeString, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{
					outputFn: func(renderParams map[string]any) recipe.RenderOutput {
						return recipe.RenderOutput{
							Renderers: []recipe.RendererConfig{
								{Type: "test_renderer", ID: "render"},
							},
						}
					},
				},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := &ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)

	modeDetail, ok := resp.RenderParameters["mode"]
	require.True(t, ok)
	assert.Equal(t, structpb.NullValue_NULL_VALUE, modeDetail.GetValue().GetNullValue())
	assert.Equal(t, structpb.NullValue_NULL_VALUE, modeDetail.GetDefaultValue().GetNullValue())
	assert.Empty(t, modeDetail.GetOptions())
}

func TestPrepareRender_WithVisualizationParameters(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		// Use the visualization title as a simple proxy for the current render param value, so we can easily query if it has changed later.
		title := fmt.Sprintf("thread:%v", renderParams["thread_id"])
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              "test_vis",
					ID:                "flat",
					RendererID:        "render",
					Title:             title,
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id": structpb.NewNumberValue(5678),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Visualizations, 1)
	assert.Equal(t, "thread:5678", resp.Visualizations[0].GetTitle())
}

func TestPrepareRender_WithVisualizationParameters_IgnoresInvalidBindings(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		// Use the visualization title as a simple proxy for the current render param value, so we can easily query if it has changed later.
		title := fmt.Sprintf("thread:%v", renderParams["thread_id"])
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              "test_vis",
					ID:                "flat",
					RendererID:        "render",
					Title:             title,
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.unknown": structpb.NewNumberValue(9999),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Visualizations, 1)
	assert.Equal(t, "thread:<nil>", resp.Visualizations[0].GetTitle())
}

func TestPrepareRender_WithVisualizationParameters_IgnoresUnqualifiedKey(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		// Use the visualization title as a simple proxy for the current render param value, so we can easily query if it has changed later.
		title := fmt.Sprintf("thread:%v", renderParams["thread_id"])
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              "test_vis",
					ID:                "flat",
					RendererID:        "render",
					Title:             title,
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"selected_thread_id": structpb.NewNumberValue(5678),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Visualizations, 1)
	assert.Equal(t, "thread:<nil>", resp.Visualizations[0].GetTitle())
}

func TestPrepareRender_WithVisualizationParameters_IgnoresMissingRenderParam(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)
	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "thread_id", Type: parameters.RenderParameterValueTypeNumber, IsArray: false, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{
					outputFn: func(renderParams map[string]any) recipe.RenderOutput {
						title := fmt.Sprintf("thread:%v", renderParams["thread_id"])
						return recipe.RenderOutput{
							Renderers: []recipe.RendererConfig{
								{Type: "test_renderer", ID: "render"},
							},
							Widgets: []recipe.WidgetConfig{
								{
									Type:              "test_vis",
									ID:                "flat",
									RendererID:        "render",
									Title:             title,
									ParameterBindings: map[string]string{"selected_thread_id": "missing_param"},
								},
							},
						}
					},
				},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := &ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id": structpb.NewStringValue("5678"),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Visualizations, 1)
	assert.Equal(t, "thread:<nil>", resp.Visualizations[0].GetTitle())
}

func TestPrepareRender_WithVisualizationParameters_RejectsRenderParamConflict(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)
	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "thread_id", Type: parameters.RenderParameterValueTypeNumber, IsArray: false, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{
					outputFn: func(renderParams map[string]any) recipe.RenderOutput {
						return recipe.RenderOutput{
							Renderers: []recipe.RendererConfig{
								{Type: "test_renderer", ID: "render"},
							},
							Widgets: []recipe.WidgetConfig{
								{
									Type:              "test_vis",
									ID:                "flat",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
								},
								{
									Type:              "test_vis",
									ID:                "timeline",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
								},
							},
						}
					},
				},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := &ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id":     structpb.NewStringValue("123"),
			"timeline.selected_thread_id": structpb.NewStringValue("234"),
		},
	}

	_, err := server.PrepareRender(context.Background(), request)
	require.Error(t, err)
	var msg *message.MessageImpl
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineGrpcserverApiApapRenderParamConflict, msg.Code())
}

func TestPrepareRender_WithVisualizationParameters_AllowsMatchingValues(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)
	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "thread_id", Type: parameters.RenderParameterValueTypeNumber, IsArray: false, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{
					outputFn: func(renderParams map[string]any) recipe.RenderOutput {
						return recipe.RenderOutput{
							Renderers: []recipe.RendererConfig{
								{Type: "test_renderer", ID: "render"},
							},
							Widgets: []recipe.WidgetConfig{
								{
									Type:              "test_vis",
									ID:                "flat",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
								},
								{
									Type:              "test_vis",
									ID:                "timeline",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
								},
								{
									Type:              "test_vis",
									ID:                "cpu",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
								},
							},
						}
					},
				},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := &ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id":     structpb.NewNumberValue(123),
			"timeline.selected_thread_id": structpb.NewNumberValue(123),
			"cpu.selected_thread_id":      structpb.NewNumberValue(123),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestPrepareRender_WithVisualizationParameters_RejectsMismatchedValues(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)
	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "thread_id", Type: parameters.RenderParameterValueTypeNumber, IsArray: false, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{
					outputFn: func(renderParams map[string]any) recipe.RenderOutput {
						return recipe.RenderOutput{
							Renderers: []recipe.RendererConfig{
								{Type: "test_renderer", ID: "render"},
							},
							Widgets: []recipe.WidgetConfig{
								{
									Type:              "test_vis",
									ID:                "flat",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
								},
								{
									Type:              "test_vis",
									ID:                "timeline",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
								},
								{
									Type:              "test_vis",
									ID:                "cpu",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
								},
							},
						}
					},
				},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := &ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id":     structpb.NewStringValue("123"),
			"timeline.selected_thread_id": structpb.NewStringValue("123"),
			"cpu.selected_thread_id":      structpb.NewStringValue("456"),
		},
	}

	_, err := server.PrepareRender(context.Background(), request)
	require.Error(t, err)
	var msg *message.MessageImpl
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineGrpcserverApiApapRenderParamConflict, msg.Code())
}

func TestPrepareRender_WithVisualizationParameters_DoesNotOverrideExplicitRenderParams(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		// Use the visualization title as a simple proxy for the current render param value, so we can easily query if it has changed later.
		title := fmt.Sprintf("thread:%v", renderParams["thread_id"])
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              "test_vis",
					ID:                "flat",
					RendererID:        "render",
					Title:             title,
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		RenderParameters: map[string]*structpb.Value{
			"thread_id": structpb.NewNumberValue(1111),
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id": structpb.NewNumberValue(9999),
		},
	}

	resp, err := server.PrepareRender(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Visualizations, 1)
	assert.Equal(t, "thread:1111", resp.Visualizations[0].GetTitle())
}

func TestPrepareRender_WithVisualizationParameters_RejectsTopologyChange(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		rendererID := "render"
		if renderParams["thread_id"] == float64(9999) {
			rendererID = "render_changed"
		}
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: rendererID},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              "test_vis",
					ID:                "flat",
					RendererID:        rendererID,
					Title:             "static",
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id": structpb.NewNumberValue(9999),
		},
	}

	_, err := server.PrepareRender(context.Background(), request)
	require.Error(t, err)
	var msg *message.MessageImpl
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineGrpcserverApiApapRenderTopologyChanged, msg.Code())
}

func TestPrepareRender_WithVisualizationParameters_RejectsBindingChange(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)
	reader := MockRecipeReader{}
	recipes := map[string]recipe.Recipe{
		"cpu_microarchitecture": {
			Name: "cpu_microarchitecture",
			RenderParameters: parameters.RenderParameters{
				{ID: "thread_id", Type: parameters.RenderParameterValueTypeNumber, IsArray: false, Order: 0},
			},
			RenderStages: []recipe.ScriptedStage{
				&testRenderStage{
					outputFn: func(renderParams map[string]any) recipe.RenderOutput {
						binding := "thread_id"
						if renderParams["thread_id"] == float64(9999) {
							binding = "other_thread_id"
						}
						return recipe.RenderOutput{
							Renderers: []recipe.RendererConfig{
								{Type: "test_renderer", ID: "render"},
							},
							Widgets: []recipe.WidgetConfig{
								{
									Type:              "test_vis",
									ID:                "flat",
									RendererID:        "render",
									Title:             "static",
									ParameterBindings: map[string]string{"selected_thread_id": binding},
								},
							},
						}
					},
				},
			},
		},
	}
	reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)

	server := &ApapServer{
		runs:         rc,
		recipeReader: &reader,
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			recipeInfo := recipes[recipeName]
			return &recipeInfo, nil
		},
		packageManager:       newTestPackageManager(t),
		compatibilityChecker: &compatibility.ConcreteCompatibilityChecker{},
	}

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id": structpb.NewNumberValue(9999),
		},
	}

	_, err := server.PrepareRender(context.Background(), request)
	require.Error(t, err)
	var msg *message.MessageImpl
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineGrpcserverApiApapRenderBindingsChanged, msg.Code())
}

func TestPrepareRender_WithVisualizationParameters_RejectsVisualizationTopologyChange(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		visType := "test_vis"
		if renderParams["thread_id"] == float64(9999) {
			visType = "test_vis_changed"
		}
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              visType,
					ID:                "flat",
					RendererID:        "render",
					Title:             "static",
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id": structpb.NewNumberValue(9999),
		},
	}

	_, err := server.PrepareRender(context.Background(), request)
	require.Error(t, err)
	var msg *message.MessageImpl
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineGrpcserverApiApapRenderTopologyChanged, msg.Code())
}

func TestPrepareRender_WithVisualizationParameters_RejectsRendererTypeChange(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		rendererType := "test_renderer"
		if renderParams["thread_id"] == float64(9999) {
			rendererType = "test_renderer_changed"
		}
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: rendererType, ID: "render"},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              "test_vis",
					ID:                "flat",
					RendererID:        "render",
					Title:             "static",
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id": structpb.NewNumberValue(9999),
		},
	}

	_, err := server.PrepareRender(context.Background(), request)
	require.Error(t, err)
	var msg *message.MessageImpl
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineGrpcserverApiApapRenderTopologyChanged, msg.Code())
}

func TestPrepareRender_WithVisualizationParameters_RejectsVisualizationRendererIDChange(t *testing.T) {
	server, runID := setupPrepareRenderServer(t, func(renderParams map[string]any) recipe.RenderOutput {
		visRendererID := "render"
		if renderParams["thread_id"] == float64(9999) {
			visRendererID = "render_alt"
		}
		return recipe.RenderOutput{
			Renderers: []recipe.RendererConfig{
				{Type: "test_renderer", ID: "render"},
				{Type: "test_renderer_alt", ID: "render_alt"},
			},
			Widgets: []recipe.WidgetConfig{
				{
					Type:              "test_vis",
					ID:                "flat",
					RendererID:        visRendererID,
					Title:             "static",
					ParameterBindings: map[string]string{"selected_thread_id": "thread_id"},
				},
			},
		}
	})

	request := &apapproto.PrepareRenderRequest{
		Content: &apapproto.ContentSelection{
			Runs: []*apapproto.RunId{{Value: runID.Value}},
		},
		VisualizationParameters: map[string]*structpb.Value{
			"flat.selected_thread_id": structpb.NewNumberValue(9999),
		},
	}

	_, err := server.PrepareRender(context.Background(), request)
	require.Error(t, err)
	var msg *message.MessageImpl
	require.ErrorAs(t, err, &msg)
	assert.Equal(t, message.EngineGrpcserverApiApapRenderTopologyChanged, msg.Code())
}

func TestShutdown(t *testing.T) {
	called := false
	serverShutdown := func() { called = true }
	server := ApapServer{shutdownCb: serverShutdown}

	result, err := server.Shutdown(context.Background(), &emptypb.Empty{})

	assert.True(t, called)
	assert.NoError(t, err)
	assert.Equal(t, result, &emptypb.Empty{})
}

func TestRecipeParameterValidation(t *testing.T) {
	testMsgA := message.New(message.EngineRecipeparserJsRecipeStageReadinessMessage)
	testMsgB := message.New(message.CommonUnsupportedTargetType)

	t.Run("experimental recipe disabled returns not found", func(t *testing.T) {
		server := ApapServer{
			config: ApapServerConfig{EnableExperimentalRecipes: false},
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				assert.Equal(t, experimentalRecipeName, recipeName)
				return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
			},
		}

		_, err := server.RecipeValidateParameters(context.Background(), &apapproto.RecipeValidateParametersRequest{
			RecipeName: experimentalRecipeName,
		})
		expectedErr := message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": experimentalRecipeName})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("experimental recipe allowed when enabled", func(t *testing.T) {
		paramPipeline := &mockParameterPipeline{}
		paramPipeline.On("Validate", mock.Anything, mock.Anything, mock.Anything).
			Return(recipe.ParamValidation{}, nil)

		server := ApapServer{
			config:            ApapServerConfig{EnableExperimentalRecipes: true},
			parameterPipeline: paramPipeline,
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				assert.Equal(t, experimentalRecipeName, recipeName)
				return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
			},
		}

		_, err := server.RecipeValidateParameters(context.Background(), &apapproto.RecipeValidateParametersRequest{
			RecipeName: experimentalRecipeName,
		})
		assert.NoError(t, err)
	})

	t.Run("getRecipe blocks experimental when disabled", func(t *testing.T) {
		server := ApapServer{
			config: ApapServerConfig{EnableExperimentalRecipes: false},
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				assert.Equal(t, experimentalRecipeName, recipeName)
				return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
			},
		}

		_, err := server.getRecipe(experimentalRecipeName)
		expectedErr := message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": experimentalRecipeName})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("getRecipe allows experimental when enabled", func(t *testing.T) {
		server := ApapServer{
			config: ApapServerConfig{EnableExperimentalRecipes: true},
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				assert.Equal(t, experimentalRecipeName, recipeName)
				return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
			},
		}

		parsedRecipe, err := server.getRecipe(experimentalRecipeName)
		require.NoError(t, err)
		assert.Equal(t, experimentalRecipeName, parsedRecipe.Name)
	})

	t.Run("unknown recipe reports error", func(t *testing.T) {
		server := ApapServer{
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return nil, errors.New("rekt")
			},
		}
		_, err := server.RecipeValidateParameters(context.Background(), &apapproto.RecipeValidateParametersRequest{})
		require.ErrorContains(t, err, "rekt")
	})

	t.Run("single select validation rejects multiple submitted values", func(t *testing.T) {
		testRecipe := &recipe.Recipe{
			Name: "test-recipe",
			Parameters: parameters.Parameters{
				SingleSelect: []parameters.SingleSelectParameter{
					{Parameter: parameters.Parameter{ID: "select1"}},
				},
			},
		}

		server := ApapServer{
			parameterPipeline: &mockParameterPipeline{},
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
		}

		_, err := server.RecipeValidateParameters(context.Background(), &apapproto.RecipeValidateParametersRequest{
			RecipeName: "test-recipe",
			Parameters: map[string]*structpb.Value{
				"select1": structpb.NewListValue(&structpb.ListValue{Values: []*structpb.Value{
					structpb.NewStringValue("a"),
					structpb.NewStringValue("b"),
				}}),
			},
		})
		expectedErr := message.New(message.EngineParametersInvalidSingleSelectValue).WithMetadata(map[string]string{
			"paramName": "select1",
			"value":     "[a b]",
			"source":    "test-recipe",
		})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("test recipe parameter validation happy path", func(t *testing.T) {
		paramPipeline := &mockParameterPipeline{}

		testRecipe := &recipe.Recipe{
			Name: "test-recipe",
			Parameters: parameters.Parameters{Input: []parameters.InputParameter{
				{Parameter: parameters.Parameter{ID: "param1"}},
			}},
		}

		paramPipeline.On("Validate", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			// Verify the parameters flow to the validator
			sc := args.Get(2).(*runtime.StageConfiguration)
			assert.Equal(t, sc.Ctx.ParamValues, parameters.BoundParameters{
				Parameters: testRecipe.Parameters,
				Values: parameters.ParameterValues{
					Input:        []string{"value1"},
					Checkbox:     []bool{},
					Radio:        []string{},
					SingleSelect: []string{},
					MultiSelect:  [][]string{},
				},
			})
		}).Return(recipe.ParamValidation{Errors: []recipe.ParameterValidationError{{ParameterId: "A", Message: testMsgA}, {ParameterId: "B", Message: testMsgB}}}, nil)

		server := ApapServer{
			parameterPipeline: paramPipeline,
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
		}

		resp, err := server.RecipeValidateParameters(context.Background(), &apapproto.RecipeValidateParametersRequest{
			RecipeName: "test-recipe",
			Parameters: map[string]*structpb.Value{"param1": structpb.NewStringValue("value1")},
			Target:     &apapproto.Target{Connection: &apapproto.Target_LocalConfig{}},
			Workload:   &apapproto.RecipeWorkload{SpecificWorkload: &apapproto.RecipeWorkload_SystemWideWorkload{}},
		})
		require.NoError(t, err)
		assert.Equal(t, resp, &apapproto.RecipeValidateParametersResponse{Messages: []*apapproto.ParameterValidationResult{
			{ParameterId: "A", Message: message.BuildErrorChain(testMsgA)},
			{ParameterId: "B", Message: message.BuildErrorChain(testMsgB)},
		}})

		paramPipeline.AssertExpectations(t)
	})

	t.Run("test recipe parameter validation passes with no target if no parameter options stages", func(t *testing.T) {
		paramPipeline := &mockParameterPipeline{}

		testRecipe := &recipe.Recipe{
			Name: "test-recipe",
			Parameters: parameters.Parameters{Input: []parameters.InputParameter{
				{Parameter: parameters.Parameter{ID: "param1"}},
			}},
		}

		paramPipeline.On("Validate", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
			// Verify the parameters flow to the validator
			sc := args.Get(2).(*runtime.StageConfiguration)
			assert.Equal(t, sc.Ctx.ParamValues, parameters.BoundParameters{
				Parameters: testRecipe.Parameters,
				Values: parameters.ParameterValues{
					Input:        []string{"value1"},
					Checkbox:     []bool{},
					Radio:        []string{},
					SingleSelect: []string{},
					MultiSelect:  [][]string{},
				}})
		}).Return(recipe.ParamValidation{Errors: []recipe.ParameterValidationError{{ParameterId: "A", Message: testMsgA}, {ParameterId: "B", Message: testMsgB}}}, nil)

		server := ApapServer{
			parameterPipeline: paramPipeline,
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
		}

		resp, err := server.RecipeValidateParameters(context.Background(), &apapproto.RecipeValidateParametersRequest{
			RecipeName: "test-recipe",
			Parameters: map[string]*structpb.Value{"param1": structpb.NewStringValue("value1")},
		})
		require.NoError(t, err)
		assert.Equal(t, resp, &apapproto.RecipeValidateParametersResponse{Messages: []*apapproto.ParameterValidationResult{
			{ParameterId: "A", Message: message.BuildErrorChain(testMsgA)},
			{ParameterId: "B", Message: message.BuildErrorChain(testMsgB)},
		}})

		paramPipeline.AssertExpectations(t)
	})

	t.Run("test recipe parameter validation fails with no target if there are parameter options stages", func(t *testing.T) {

		testRecipe := &recipe.Recipe{
			Name: "test-recipe",
			Parameters: parameters.Parameters{Input: []parameters.InputParameter{
				{Parameter: parameters.Parameter{ID: "param1"}},
			}},
			// Param's valid options are computed dynamically
			ParameterOptionsStages: []recipe.ScriptedStage{&recipeparser.GojaRadioParameterOptionStage{}},
		}

		server := ApapServer{
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
		}

		_, err := server.RecipeValidateParameters(context.Background(), &apapproto.RecipeValidateParametersRequest{
			RecipeName: "test-recipe",
			Parameters: map[string]*structpb.Value{"param1": structpb.NewStringValue("value1")},
		})
		expectedMetadata := map[string]string{"recipeName": "test-recipe"}
		expectedErr := message.New(message.EngineGrpcserverApiApapTargetRequired).WithMetadata(expectedMetadata)
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("returns error if workload command is empty", func(t *testing.T) {
		testRecipe := &recipe.Recipe{
			Name: "test-recipe",
			Parameters: parameters.Parameters{Input: []parameters.InputParameter{
				{Parameter: parameters.Parameter{ID: "param1"}},
			}},
		}

		server := ApapServer{
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
		}

		_, err := server.RecipeValidateParameters(context.Background(), &apapproto.RecipeValidateParametersRequest{
			RecipeName: "test-recipe",
			Target:     &apapproto.Target{Connection: &apapproto.Target_LocalConfig{}},
			Workload: &apapproto.RecipeWorkload{SpecificWorkload: &apapproto.RecipeWorkload_LaunchWorkload{
				LaunchWorkload: &apapproto.LaunchWorkload{},
			}},
		})
		require.Error(t, err)

		expectedErr := message.New(message.EngineRecipeWorkloadCommandEmpty).WithMetadata(map[string]string{"recipe": "test-recipe"})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})
}

func TestRecipeList(t *testing.T) {
	s := ApapServer{}
	testErr := errors.New("testError")
	t.Run("returns an error if recipe files couldn't be located", func(t *testing.T) {
		reader := MockRecipeReader{}
		reader.On("ReadRecipes", mock.Anything).Return(map[string]recipe.Recipe{}, testErr)
		s.recipeReader = &reader

		_, err := s.ListRecipes(context.Background(), &emptypb.Empty{})
		assert.Equal(t, testErr, err)
	})
	t.Run("returns recipes correctly if only invalid ones are provided", func(t *testing.T) {
		reader := MockRecipeReader{}
		reader.On("ReadRecipes", mock.Anything).Run(func(args mock.Arguments) {
			handler := args.Get(0).(func(filename string, err error))
			handler("BadRecipe1", testErr)
			handler("BadRecipe2", testErr)
		}).Return(map[string]recipe.Recipe{}, nil)
		s.recipeReader = &reader

		response, err := s.ListRecipes(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		assert.Len(t, response.RecipeNames, 2)

		// Build a map path per failed recipe to not depend on ordering
		byPath := map[string]*apapproto.RecipeNameEntry{}
		for _, recipe := range response.RecipeNames {
			if pathIdent, ok := recipe.Identifier.(*apapproto.RecipeNameEntry_Path); ok {
				byPath[pathIdent.Path] = recipe
			}
		}

		require.Contains(t, byPath, "BadRecipe1")
		require.Contains(t, byPath, "BadRecipe2")

		recipe1 := byPath["BadRecipe1"]
		assert.NotNil(t, recipe1.LoadError)
		reconstructedErr1 := message.ReconstructFromChain(recipe1.LoadError)
		assert.EqualError(t, reconstructedErr1, testErr.Error())

		recipe2 := byPath["BadRecipe2"]
		assert.NotNil(t, recipe2.LoadError)
		reconstructedErr2 := message.ReconstructFromChain(recipe2.LoadError)
		assert.EqualError(t, reconstructedErr2, testErr.Error())
	})
	t.Run("returns recipe info correctly for both valid and invalid recipes", func(t *testing.T) {
		reader := MockRecipeReader{}

		recipes := map[string]recipe.Recipe{
			"BrilliantRecipe": {},
			"SillyRecipe":     {},
		}

		reader.On("ReadRecipes", mock.Anything).Run(func(args mock.Arguments) {
			handler := args.Get(0).(func(filename string, err error))
			handler("BadRecipe1", testErr)
			handler("BadRecipe2", testErr)
		}).Return(recipes, nil)
		s.recipeReader = &reader

		response, err := s.ListRecipes(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		assert.Len(t, response.RecipeNames, 4)
		for name := range recipes {
			assert.Contains(t, response.RecipeNames, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Name{Name: name}})
		}

		hasOne := false
		hasTwo := false
		for _, recipe := range response.RecipeNames {
			switch identifier := recipe.Identifier.(type) {
			case *apapproto.RecipeNameEntry_Path:
				assert.NotNil(t, recipe.LoadError)
				reconstructedErr := message.ReconstructFromChain(recipe.LoadError)
				assert.EqualError(t, reconstructedErr, testErr.Error())
				switch identifier.Path {
				case "BadRecipe1":
					hasOne = true
				case "BadRecipe2":
					hasTwo = true
				}
			}
		}
		assert.True(t, hasOne)
		assert.True(t, hasTwo)
	})
	t.Run("returns recipes correctly if only valid ones are provided", func(t *testing.T) {
		reader := MockRecipeReader{}

		recipes := map[string]recipe.Recipe{
			"BrilliantRecipe": {},
			"SillyRecipe":     {},
		}
		reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		s.recipeReader = &reader

		response, err := s.ListRecipes(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		assert.Len(t, response.RecipeNames, len(recipes))
		for name := range recipes {
			assert.Contains(t, response.RecipeNames, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Name{Name: name}})
		}
	})

	t.Run("filters experimental recipe when disabled", func(t *testing.T) {
		reader := MockRecipeReader{}
		recipes := map[string]recipe.Recipe{
			experimentalRecipeName: {Status: recipe.RecipeStatusExperimental},
			"BrilliantRecipe":      {},
		}
		reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		server := ApapServer{
			config:       ApapServerConfig{EnableExperimentalRecipes: false},
			recipeReader: &reader,
		}

		response, err := server.ListRecipes(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		for _, entry := range response.RecipeNames {
			if nameEntry, ok := entry.Identifier.(*apapproto.RecipeNameEntry_Name); ok {
				assert.NotEqual(t, experimentalRecipeName, nameEntry.Name)
			}
		}
	})

	t.Run("includes experimental recipe when enabled", func(t *testing.T) {
		reader := MockRecipeReader{}
		recipes := map[string]recipe.Recipe{
			experimentalRecipeName: {Status: recipe.RecipeStatusExperimental},
			"BrilliantRecipe":      {},
		}
		reader.On("ReadRecipes", mock.Anything).Return(recipes, nil)
		server := ApapServer{
			config:       ApapServerConfig{EnableExperimentalRecipes: true},
			recipeReader: &reader,
		}

		response, err := server.ListRecipes(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		assert.Contains(t, response.RecipeNames, &apapproto.RecipeNameEntry{
			Identifier: &apapproto.RecipeNameEntry_Name{Name: experimentalRecipeName},
		})
	})
}

func TestListRuns(t *testing.T) {
	rc, runID := createRunCollectionWithRun(t)
	require.NoError(t, run.WriteRunCategorization(filepath.Join(rc.GetRunPath(runID), run.CategorizationFilename), &run.RunCategorization{
		Group: "test-group",
		Tags:  []string{"tag-a", "tag-b"},
	}))
	server := ApapServer{runs: rc}

	resp, err := server.ListRuns(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.Runs, 1)

	listed := resp.Runs[0]
	assert.Equal(t, runID.Value, listed.Id)
	desc := listed.GetDescription()
	require.NotNil(t, desc)
	require.NotNil(t, desc.Metadata)
	assert.Equal(t, "cpu_microarchitecture", desc.Metadata.RecipeName)
	assert.Equal(t, "test-group", desc.Metadata.Group)
	assert.Equal(t, []string{"tag-a", "tag-b"}, desc.Metadata.Tags)
}

func TestListRenders(t *testing.T) {
	s := ApapServer{sessions: render.NewSessionStorage()}
	// Initialising DB
	loader := render.MockRunLoader{}
	loader.On("LoadRun", mock.Anything).Return(&cdf.OnDiskModel{}, nil)

	renderer := &render.MockRenderer{}
	renderer.On("Configure", &render.Config{}).Return(nil)
	renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
	renderer.On("GetInputSpec").Return(render.InputSpec{})
	renderer.On("GetOutputSpec").Return(render.OutputSpec{})

	rendererFactory := render.MockRendererFactory{}
	rendererFactory.On("NewRenderer", mock.Anything).Return(renderer, nil)

	t.Run("returns list of active sessions and memory usage upon success", func(t *testing.T) {
		// Note that an exponentially larger amount of data is added to each extra session (to ensure that each has a
		// tangibly higher memory usage than the previous) - more than 4 sessions will likely not work
		numSessions := 3
		defer s.sessions.CloseAllRenderSessions()
		// Creating the specified amount of new sessions, and adding data to each one
		for i := 0; i < numSessions; i++ {
			session, invocationErrors, err := render.StartRenderSession(
				context.Background(),
				&sessionfactory.Impl{},
				&s.sessions,
				&rendererFactory,
				&loader,
				[]run.RunID{{Value: fmt.Sprintf("test-run-%d", i)}},
				render.RendererConfigList{{}},
				render.WidgetConfigList{{}},
				&render.DuckDBFactory{},
				nil,
				nil,
			)
			assert.NoError(t, err)
			assert.NoError(t, errors.Join(invocationErrors...))

			_, err = session.Database().Conn.ExecContext(context.Background(), "CREATE TABLE test (val VARCHAR)")
			assert.NoError(t, err)

			data := strings.Repeat("againand", 1<<(10*i))
			insertQuery := fmt.Sprint(`INSERT INTO test VALUES ('`, data, `')`)
			_, err = session.Database().Conn.ExecContext(context.Background(), insertQuery)
			assert.NoError(t, err)
		}

		result, err := s.ListRenders(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		sessions := result.Sessions
		dbUsage := make(map[string]float64, numSessions)
		sessionDbKeys := make(map[string]string, numSessions)
		for i := 0; i < len(sessions); i++ {
			sessionDbKeys[sessions[i].SessionId] = sessions[i].DbKey
		}

		assert.Equal(t, numSessions, len(sessions))
		for _, session := range sessions {
			assert.NotEmpty(t, session.DbKey)
		}

		for _, instance := range result.DbInstances {
			dbUsage[instance.DbKey] = instance.MemoryUsageGib
		}
		assert.Equal(t, numSessions, len(dbUsage))
		for key, usage := range dbUsage {
			assert.Greater(t, usage, 0.0)

			var sessionId string
			for id, dbKey := range sessionDbKeys {
				if dbKey == key {
					sessionId = id
					break
				}
			}
			assert.NotEmpty(t, sessionId)

			var num int
			num, err = strconv.Atoi(sessionId[1:])
			assert.NoError(t, err)
			nextSessionId := fmt.Sprintf("s%v", num+1)
			nextKey := sessionDbKeys[nextSessionId]
			if nextKey != "" {
				nextUsage, ok := dbUsage[nextKey]
				if ok {
					assert.Greater(t, nextUsage, usage)
				}
			}
		}
	})
	t.Run("ignores sessions whose database is closed", func(t *testing.T) {
		session, invocationErrors, err := render.StartRenderSession(
			context.Background(),
			&sessionfactory.Impl{},
			&s.sessions,
			&rendererFactory,
			&loader,
			[]run.RunID{{Value: "test-run"}},
			render.RendererConfigList{{}},
			render.WidgetConfigList{{}},
			&render.DuckDBFactory{},
			nil,
			nil,
		)
		assert.NoError(t, err)
		assert.NoError(t, errors.Join(invocationErrors...))

		session.Close()

		result, err := s.ListRenders(context.Background(), &emptypb.Empty{})
		assert.NoError(t, err)
		assert.Equal(t, 0, len(result.Sessions))
		assert.Equal(t, 0, len(result.DbInstances))
	})
}

func TestCloseSession(t *testing.T) {
	server := ApapServer{sessions: render.NewSessionStorage()}

	t.Run("returns an error if session id not found", func(t *testing.T) {
		_, err := server.CloseRender(context.Background(), &apapproto.CloseRenderRequest{SessionId: "i-dont-exist"})
		assert.Error(t, err)
	})

	t.Run("returns success if session id found", func(t *testing.T) {
		dbfactory := render.DuckDBFactory{}

		factory := sessionfactory.Impl{}
		s, err := factory.NewSession(nil, nil, &dbfactory, nil, nil)
		require.NoError(t, err)
		defer s.Close()
		require.NoError(t, server.sessions.AddRenderSession(s))

		_, err = server.CloseRender(context.Background(), &apapproto.CloseRenderRequest{SessionId: s.ID()})
		assert.NoError(t, err)
	})
}

func TestParseRecipe(t *testing.T) {
	const toolDescr1 = `
	let tool = {
		name: "core-tool-1",
		version: "0.1",
	    description: {
		  short: "Short description",
		  long: "Long description",
	    },
	    deployments: [{
		  appliesTo: [
		    {architecture: "x86_64", os: "Linux"}
		  ],
	    }],
	};`

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "performix-test-")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(tempDir)) }()

	// Create a single tool bundle file
	executableDir := filepath.Join(tempDir, terminology.GetProductBinaryName())
	toolBundlePath := filepath.Join(executableDir, "tools", "core-tool-1", "1.1", "core-tool-1.tar.gz")
	require.NoError(t, os.MkdirAll(filepath.Dir(toolBundlePath), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(toolBundlePath, nil, perms.LocalFilePerm))

	// Create a single tool integration file
	toolIntPath := filepath.Join(executableDir, "tool-integrations", "core-tool-integration-1.js")
	toolIntData := []byte(toolDescr1)
	require.NoError(t, os.MkdirAll(filepath.Dir(toolIntPath), perms.LocalDirPerm))
	require.NoError(t, os.WriteFile(toolIntPath, toolIntData, perms.LocalFilePerm))

	pm := packages.NewPackageManager(executableDir, "")

	t.Run("experimental recipe disabled returns not found", func(t *testing.T) {
		server := ApapServer{
			config: ApapServerConfig{EnableExperimentalRecipes: false},
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				assert.Equal(t, experimentalRecipeName, recipeName)
				return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
			},
		}

		_, err := server.ParseRecipe(context.Background(), &apapproto.ParseRecipeMessage{Name: experimentalRecipeName})
		expectedErr := message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": experimentalRecipeName})
		assert.Equal(t, expectedErr, err)
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
	})

	t.Run("experimental recipe allowed when enabled", func(t *testing.T) {
		server := ApapServer{
			config: ApapServerConfig{EnableExperimentalRecipes: true},
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				assert.Equal(t, experimentalRecipeName, recipeName)
				return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
			}, packageManager: pm,
		}

		_, err := server.ParseRecipe(context.Background(), &apapproto.ParseRecipeMessage{Name: experimentalRecipeName})
		assert.NoError(t, err)
	})

	t.Run("unknown recipe reports error", func(t *testing.T) {
		server := ApapServer{
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return nil, errors.New("rekt")
			},
		}
		_, err := server.ParseRecipe(context.Background(), &apapproto.ParseRecipeMessage{})
		require.ErrorContains(t, err, "rekt")
	})

	t.Run("test recipe parameter validation happy path", func(t *testing.T) {

		testRecipe := &recipe.Recipe{
			Name:   "test-recipe",
			Status: recipe.RecipeStatusPreview,
			Parameters: parameters.Parameters{
				Input: []parameters.InputParameter{
					{Parameter: parameters.Parameter{
						ID:          "param1",
						Label:       "labelA",
						Description: "descB",
						Required:    false,
						Order:       0,
						VisibleWhen: []parameters.ParameterDependency{{ParameterID: "checkbox1", Value: "value"}},
					}, DefaultValue: "defaultA"},
				},
				Checkbox: []parameters.CheckboxParameter{
					{Parameter: parameters.Parameter{ID: "checkbox1", Order: 1}, DefaultValue: true},
				},
				Radio: []parameters.RadioParameter{
					{Parameter: parameters.Parameter{ID: "radio1", Order: 3}, Options: []string{"option1", "option2"}, DefaultValue: "option1"},
				},
				MultiSelect: []parameters.MultiSelectParameter{
					{Parameter: parameters.Parameter{ID: "select1", Order: 2}, Options: []string{"optionA", "optionB", "optionC"}, DefaultValue: []string{"optionA", "opbionB"}},
				},
			},
		}

		server := ApapServer{
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			}, packageManager: pm,
		}

		resp, err := server.ParseRecipe(context.Background(), &apapproto.ParseRecipeMessage{Name: "test-recipe"})
		require.NoError(t, err)
		assert.Equal(t, resp, &apapproto.ParseRecipeResponse{
			Name:   "test-recipe",
			Status: apapproto.RecipeStatus_RECIPE_STATUS_PREVIEW,
			PlatformSupport: []*apapproto.PlatformSupport{
				{Platform: &apapproto.PlatformConfiguration{Os: "Linux", Arch: "aarch64"}},
				{Platform: &apapproto.PlatformConfiguration{Os: "Linux", Arch: "x86_64"}},
				{Platform: &apapproto.PlatformConfiguration{Os: "Windows", Arch: "aarch64"}},
				{Platform: &apapproto.PlatformConfiguration{Os: "Windows", Arch: "x86_64"}},
				{Platform: &apapproto.PlatformConfiguration{Os: "Darwin", Arch: "aarch64"}},
				{Platform: &apapproto.PlatformConfiguration{Os: "Darwin", Arch: "x86_64"}},
				{Platform: &apapproto.PlatformConfiguration{Os: "Android", Arch: "aarch64"}},
			},
			Parameters: []*apapproto.ParameterList{
				{
					Parameter: &apapproto.ParameterList_Input{Input: &apapproto.InputParameter{
						Base: &apapproto.RecipeParameterBase{
							ID:          "param1",
							Label:       "labelA",
							Description: "descB",
							VisibleWhen: []*apapproto.ParameterDependency{
								{ParameterId: "checkbox1", Value: "value"},
							},
						},
						DefaultValue: "defaultA",
					}},
				},
				{
					Parameter: &apapproto.ParameterList_Checkbox{Checkbox: &apapproto.CheckboxParameter{
						Base: &apapproto.RecipeParameterBase{
							ID:          "checkbox1",
							VisibleWhen: []*apapproto.ParameterDependency{},
						},
						DefaultValue: true,
					}},
				},
				{
					Parameter: &apapproto.ParameterList_MultiSelect{MultiSelect: &apapproto.MultiSelectParameter{
						Base: &apapproto.RecipeParameterBase{
							ID:          "select1",
							VisibleWhen: []*apapproto.ParameterDependency{},
						},
						DefaultValue: []string{"optionA", "opbionB"},
						Options: []*apapproto.ParameterOption{
							{Value: "optionA", Label: "optionA"},
							{Value: "optionB", Label: "optionB"},
							{Value: "optionC", Label: "optionC"},
						},
					}},
				},
				{
					Parameter: &apapproto.ParameterList_Radio{Radio: &apapproto.RadioParameter{
						Base: &apapproto.RecipeParameterBase{
							ID:          "radio1",
							VisibleWhen: []*apapproto.ParameterDependency{},
						},
						DefaultValue: "option1",
						Options: []*apapproto.ParameterOption{
							{Value: "option1", Label: "option1"},
							{Value: "option2", Label: "option2"},
						},
					}},
				},
			},
		})
	})

	t.Run("parse recipe encodes single select parameters as single_select", func(t *testing.T) {
		testRecipe := &recipe.Recipe{
			Name:   "test-recipe",
			Status: recipe.RecipeStatusStable,
			Parameters: parameters.Parameters{
				SingleSelect: []parameters.SingleSelectParameter{
					{Parameter: parameters.Parameter{ID: "select1", Order: 0}, Options: []string{"optionA", "optionB"}, DefaultValue: "optionB"},
				},
			},
		}

		server := ApapServer{
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
			packageManager: pm,
		}

		resp, err := server.ParseRecipe(context.Background(), &apapproto.ParseRecipeMessage{Name: "test-recipe"})
		require.NoError(t, err)
		require.Len(t, resp.Parameters, 1)
		require.NotNil(t, resp.Parameters[0].GetSingleSelect())
		assert.Equal(t, "select1", resp.Parameters[0].GetSingleSelect().GetBase().GetID())
		assert.Equal(t, "optionB", resp.Parameters[0].GetSingleSelect().GetDefaultValue())
		assert.Equal(t, []*apapproto.ParameterOption{
			{Value: "optionA", Label: "optionA"},
			{Value: "optionB", Label: "optionB"},
		}, resp.Parameters[0].GetSingleSelect().GetOptions())
	})

	t.Run("parse recipe omits render parameters when rerendering is disabled", func(t *testing.T) {
		testRecipe := &recipe.Recipe{
			Name:   "test-recipe",
			Status: recipe.RecipeStatusStable,
			RenderParameters: parameters.RenderParameters{
				{ID: "process_name", Type: parameters.RenderParameterValueTypeString, IsArray: false, Order: 0},
				{ID: "time_range", Type: parameters.RenderParameterValueTypeNumber, IsArray: true, Order: 1},
			},
		}

		server := ApapServer{
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
			packageManager: pm,
		}

		resp, err := server.ParseRecipe(context.Background(), &apapproto.ParseRecipeMessage{Name: "test-recipe"})
		require.NoError(t, err)
		assert.Empty(t, resp.RenderParameters)
	})

	t.Run("parse recipe includes render parameters when rerendering is enabled", func(t *testing.T) {
		testRecipe := &recipe.Recipe{
			Name:   "test-recipe",
			Status: recipe.RecipeStatusStable,
			RenderParameters: parameters.RenderParameters{
				{ID: "process_name", Type: parameters.RenderParameterValueTypeString, IsArray: false, Order: 0},
				{ID: "time_range", Type: parameters.RenderParameterValueTypeNumber, IsArray: true, Order: 1},
			},
		}

		server := ApapServer{
			config: ApapServerConfig{EnableRerendering: true},
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
			packageManager: pm,
		}

		resp, err := server.ParseRecipe(context.Background(), &apapproto.ParseRecipeMessage{Name: "test-recipe"})
		require.NoError(t, err)
		require.Len(t, resp.RenderParameters, 2)

		assert.Equal(t, "process_name", resp.RenderParameters[0].Id)
		assert.Equal(t, apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_STRING, resp.RenderParameters[0].Type)
		assert.False(t, resp.RenderParameters[0].IsArray)

		assert.Equal(t, "time_range", resp.RenderParameters[1].Id)
		assert.Equal(t, apapproto.RenderParameterType_RENDER_PARAMETER_TYPE_NUMBER, resp.RenderParameters[1].Type)
		assert.True(t, resp.RenderParameters[1].IsArray)
	})

	t.Run("target parse skips nil parameter options evaluator", func(t *testing.T) {
		testRecipe := &recipe.Recipe{
			Name:   "test-recipe",
			Status: recipe.RecipeStatusStable,
			Parameters: parameters.Parameters{Input: []parameters.InputParameter{
				{Parameter: parameters.Parameter{ID: "param1", Order: 0}, DefaultValue: "defaultA"},
			}},
		}

		server := ApapServer{
			recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
				return testRecipe, nil
			},
		}

		resp, err := server.ParseRecipe(context.Background(), &apapproto.ParseRecipeMessage{
			Name:   "test-recipe",
			Target: &apapproto.Target{Connection: &apapproto.Target_LocalConfig{LocalConfig: &apapproto.LocalConnectionConfig{}}},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Parameters, 1)
		assert.Len(t, resp.PlatformSupport, 1)
	})
}

func TestRecipeIssueCommandDisabled(t *testing.T) {
	server := ApapServer{
		config: ApapServerConfig{EnableExperimentalRecipes: false},
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			assert.Equal(t, experimentalRecipeName, recipeName)
			return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
		},
	}
	stream := &fakeRecipeIssueCommandServer{ctx: context.Background()}

	err := server.RecipeIssueCommand(&apapproto.RecipeCommand{
		SpecificCommand: &apapproto.RecipeCommand_StartCommand{
			StartCommand: &apapproto.RecipeStartCommand{Name: experimentalRecipeName},
		},
	}, stream)

	expectedErr := message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": experimentalRecipeName})
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestRecipeReadyDisabled(t *testing.T) {
	server := ApapServer{
		config: ApapServerConfig{EnableExperimentalRecipes: false},
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			assert.Equal(t, experimentalRecipeName, recipeName)
			return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
		},
	}

	_, err := server.RecipeReady(context.Background(), &apapproto.RecipeReadyRequest{
		RecipeInfo: &apapproto.RecipeStartCommand{Name: experimentalRecipeName},
	})

	expectedErr := message.New(message.EngineRecipeDoesNotExist).WithMetadata(map[string]string{"recipe": experimentalRecipeName})
	assert.Equal(t, expectedErr, err)
	assert.NoError(t, message.ValidateMetadataPlaceholders(err))
}

func TestRecipeReadyExperimentalAllowed(t *testing.T) {
	server := ApapServer{
		config: ApapServerConfig{EnableExperimentalRecipes: true},
		recipeFinder: func(recipeName string) (*recipe.Recipe, error) {
			assert.Equal(t, experimentalRecipeName, recipeName)
			return &recipe.Recipe{Name: recipeName, Status: recipe.RecipeStatusExperimental}, nil
		},
	}

	_, err := server.RecipeReady(context.Background(), &apapproto.RecipeReadyRequest{
		RecipeInfo: &apapproto.RecipeStartCommand{Name: experimentalRecipeName},
	})

	assert.Error(t, err)
	assert.NotErrorIs(t, err, message.New(message.EngineRecipeDoesNotExist))
}

type fakeRecipeIssueCommandServer struct {
	ctx context.Context
}

func (f *fakeRecipeIssueCommandServer) SetHeader(metadata.MD) error          { return nil }
func (f *fakeRecipeIssueCommandServer) SendHeader(metadata.MD) error         { return nil }
func (f *fakeRecipeIssueCommandServer) SetTrailer(metadata.MD)               {}
func (f *fakeRecipeIssueCommandServer) Context() context.Context             { return f.ctx }
func (f *fakeRecipeIssueCommandServer) SendMsg(interface{}) error            { return nil }
func (f *fakeRecipeIssueCommandServer) RecvMsg(interface{}) error            { return nil }
func (f *fakeRecipeIssueCommandServer) Send(*apapproto.RecipeResponse) error { return nil }

func TestAwaitRecipeJob(t *testing.T) {
	t.Run("returns when phase 1 completes while the job continues", func(t *testing.T) {
		done := make(chan error, 1)
		phase1Done := make(chan struct{})
		close(phase1Done)

		jobCompleted, err := awaitRecipeJob(
			context.Background(),
			done,
			phase1Done,
			func() { t.Fatal("detached job was cancelled") },
			true,
		)

		require.NoError(t, err)
		assert.False(t, jobCompleted)
	})

	t.Run("returns the final job error", func(t *testing.T) {
		for _, detachBackgroundTransfers := range []bool{false, true} {
			t.Run(fmt.Sprintf("detach=%t", detachBackgroundTransfers), func(t *testing.T) {
				wantErr := errors.New("recipe failed")
				done := make(chan error, 1)
				done <- wantErr

				jobCompleted, err := awaitRecipeJob(
					context.Background(),
					done,
					make(chan struct{}),
					func() { t.Fatal("completed job was cancelled") },
					detachBackgroundTransfers,
				)

				require.ErrorIs(t, err, wantErr)
				assert.True(t, jobCompleted)
			})
		}
	})

	t.Run("cancels and waits for cleanup when the client disconnects before phase 1", func(t *testing.T) {
		for _, detachBackgroundTransfers := range []bool{false, true} {
			t.Run(fmt.Sprintf("detach=%t", detachBackgroundTransfers), func(t *testing.T) {
				ctx, cancelCtx := context.WithCancel(context.Background())
				cancelCtx()
				done := make(chan error, 1)
				cancelled := false

				jobCompleted, err := awaitRecipeJob(ctx, done, make(chan struct{}), func() {
					cancelled = true
					done <- context.Canceled
				}, detachBackgroundTransfers)

				require.ErrorIs(t, err, context.Canceled)
				assert.False(t, jobCompleted)
				assert.True(t, cancelled)
				assert.Empty(t, done)
			})
		}
	})
}

func TestRecipeIssueCommandCancelAndStop(t *testing.T) {
	tests := []struct {
		name          string
		runID         string
		cmd           func(string) *apapproto.RecipeCommand
		expectedState cmdsync.CommandStateType
		expectedLog   string
	}{
		{
			name:  "cancel command successfully writes cancel flag and logs it",
			runID: "cancel-command",
			cmd: func(id string) *apapproto.RecipeCommand {
				return &apapproto.RecipeCommand{
					SpecificCommand: &apapproto.RecipeCommand_CancelCommand{
						CancelCommand: &apapproto.RecipeCancelCommand{
							Id: &apapproto.RunId{Value: id},
						},
					},
				}
			},
			expectedState: cmdsync.CommandCancel,
			expectedLog:   "Cancel request received",
		},
		{
			name:  "stop command successfully writes stop flag and logs it",
			runID: "stop-command",
			cmd: func(id string) *apapproto.RecipeCommand {
				return &apapproto.RecipeCommand{
					SpecificCommand: &apapproto.RecipeCommand_StopCommand{
						StopCommand: &apapproto.RecipeStopCommand{
							Id: &apapproto.RunId{Value: id},
						},
					},
				}
			},
			expectedState: cmdsync.CommandStop,
			expectedLog:   "Stop request received",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger, hook := test.NewNullLogger()
			ctx := logx.CtxWithLogger(context.Background(), logger)

			runID := run.RunID{Value: tc.runID}
			commandMap := cmdsync.NewCommandStateMap()
			state := commandMap.CreateCommandState(runID)
			server := ApapServer{recipeCommandMap: commandMap}
			out := &fakeRecipeIssueCommandServer{ctx: ctx}

			err := server.RecipeIssueCommand(tc.cmd(runID.Value), out)
			require.NoError(t, err)

			assert.Equal(t, tc.expectedState, state.Read())

			// Check the logs
			found := false
			for _, entry := range hook.AllEntries() {
				if entry.Message == tc.expectedLog &&
					entry.Level == logrus.InfoLevel &&
					entry.Data["runID"] == runID.Value {
					found = true
					break
				}
			}
			assert.True(t, found)
		})
	}
}

func createRunCollection(t *testing.T) (*run.RunCollection, run.RunID) {
	td := t.TempDir()
	creator := &recipe.CollectionState{}
	recipeDir := filepath.Join(td, "/recipe.json")
	runs, err := run.NewRunCollection(td)
	assert.NoError(t, err)
	rc := recipe.RecipeCtx{RecipePath: recipeDir, Target: &target.SSHTarget{Jumps: []target.SSHHostConfig{
		{Host: "1.2.3.4", Port: 345, Username: "Blobby", PrivateKeyFilename: "/a/real/file"}}}}
	require.NoError(t, os.WriteFile(recipeDir, []byte{}, perms.LocalDirPerm))
	rec := &recipe.Recipe{
		Parameters: parameters.Parameters{
			Input:       []parameters.InputParameter{{Parameter: parameters.Parameter{ID: "input1"}}},
			Checkbox:    []parameters.CheckboxParameter{{Parameter: parameters.Parameter{ID: "checkbox1"}}},
			MultiSelect: []parameters.MultiSelectParameter{{Parameter: parameters.Parameter{ID: "select1"}}},
			Radio:       []parameters.RadioParameter{{Parameter: parameters.Parameter{ID: "radio1"}}},
		},
	}
	rc.ParamValues, err = parameters.BindRecipeParameters(map[string]any{
		"input1":    "inVal",
		"checkbox1": true,
		"radio1":    "radioVal",
		"select1":   []string{"selA", "selB"},
	}, rec.Parameters, "test-recipe")
	require.NoError(t, err)
	runId, release, err := creator.CreateRun(context.Background(), runs, &rc)
	require.NoError(t, err)
	require.NotNil(t, release)
	t.Cleanup(func() { _ = release() })
	return runs, runId
}

func TestGetRunDescription(t *testing.T) {

	t.Run("run parameters are stored and returned correctly", func(t *testing.T) {
		rc, runId := createRunCollection(t)

		server := ApapServer{runs: rc}

		runDescription, err := server.GetRunDescription(context.Background(), &apapproto.GetRunDescriptionRequest{Id: &apapproto.RunId{Value: runId.Value}})
		require.NoError(t, err)
		assert.Equal(t, map[string]*structpb.Value{
			"input1":    structpb.NewStringValue("inVal"),
			"checkbox1": structpb.NewBoolValue(true),
			"radio1":    structpb.NewStringValue("radioVal"),
			"select1":   structpb.NewListValue(&structpb.ListValue{Values: []*structpb.Value{structpb.NewStringValue("selA"), structpb.NewStringValue("selB")}}),
		}, runDescription.Metadata.Parameters)
	})

	t.Run("returns error when run ID does not exist", func(t *testing.T) {
		rc, _ := createRunCollection(t)
		server := ApapServer{runs: rc}

		_, err := server.GetRunDescription(
			context.Background(),
			&apapproto.GetRunDescriptionRequest{
				Id: &apapproto.RunId{Value: "does-not-exist"},
			},
		)

		require.Error(t, err)
		expectedErr := message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "does-not-exist"})
		assert.NoError(t, message.ValidateMetadataPlaceholders(err))
		assert.Equal(t, expectedErr, err)
	})

	t.Run("returns error when an unknown extra is requested", func(t *testing.T) {
		rc, runID := createRunCollection(t)
		server := ApapServer{runs: rc}

		unknown := apapproto.StandardRunDescriptionExtras(9999)
		_, err := server.GetRunDescription(
			context.Background(),
			&apapproto.GetRunDescriptionRequest{
				Id:               &apapproto.RunId{Value: runID.Value},
				ExtrasRequestStd: []apapproto.StandardRunDescriptionExtras{unknown},
			},
		)

		require.Error(t, err)
		cause := fmt.Errorf("unrecognized extra request: %v", unknown)
		expectedErr := message.New(message.CommonUnknownError).WithCause(cause)
		assert.Equal(t, err, expectedErr)
	})
	t.Run("returns error when a run conversion fails", func(t *testing.T) {
		desc := &run.RunDescription{
			Parameters: map[string]any{
				// 123 is non-string = conversion error
				"bad": []any{"ok", 123},
			},
		}

		_, err := DescToProto(desc)
		require.Error(t, err)
		_, innerCause := AnyToProto([]any{"ok", 123})
		require.Error(t, innerCause)

		cause := fmt.Errorf("failed to convert run description to proto: %w", innerCause)
		expectedErr := message.New(message.CommonUnknownError).WithCause(cause)
		assert.Equal(t, expectedErr, err)
	})
}

func TestLookupMessage(t *testing.T) {
	server := ApapServer{}

	t.Run("returns expected response when message lookup succeeds", func(t *testing.T) {
		// Simulate incoming proto request with an arbitrary known message from the catalog
		req := &apapproto.LookupMessageRequest{
			Msg: &apapproto.MessageDetails{
				Code:     "engine.run.DOES_NOT_EXIST",
				Metadata: map[string]string{"runID": "abc"},
				Locale:   message.LocaleEnglish,
			},
		}

		expectedRsp := &apapproto.LookupMessageResponse{
			CatalogMsg: &apapproto.CatalogMessage{
				Code:        "engine.run.DOES_NOT_EXIST",
				Severity:    "Error",
				Message:     "The run with the ID `abc` does not exist.",
				Explanation: "This run does not exist in the system.",
				Advice:      fmt.Sprintf("Check that the run ID `abc` is correct. If %v still cannot find the run, contact Arm support.", terminology.GetProductFullName()),
			},
		}

		rsp, err := server.LookupMessage(context.Background(), req)

		assert.Nil(t, err)
		assert.Equal(t, expectedRsp, rsp)
	})

	t.Run("returns expected response when message lookup fails", func(t *testing.T) {
		// Simulate incoming proto request that will fail to find a catalog message
		req := &apapproto.LookupMessageRequest{
			Msg: &apapproto.MessageDetails{
				Code:     "this.is.a.fake.domain.FAKE_REASON",
				Metadata: map[string]string{"key1": "value1"},
				Locale:   message.LocaleEnglish,
			},
		}

		// The catalog message in the response should be unknownCatalogMessage
		// The error should always be nil
		expectedRsp := &apapproto.LookupMessageResponse{
			CatalogMsg: &apapproto.CatalogMessage{
				Code:        "engine.message.LOOKUP_FAILED",
				Severity:    message.SeverityError,
				Message:     "There was an issue finding this message.",
				Explanation: "An internal error occurred while trying to find the message with code 'this.is.a.fake.domain.FAKE_REASON'.",
				Advice:      "Contact Arm support.",
			},
		}

		rsp, err := server.LookupMessage(context.Background(), req)

		assert.Nil(t, err)
		assert.Equal(t, expectedRsp, rsp)
	})
}

type stubCommandRunner struct{}

func (stubCommandRunner) RunCommand(cmd string) (string, string, error) {
	return "C:\\\\Users\\\\runner\\\\AppData\\\\Local\n", "", nil
}

func TestTargetInfoCollector(t *testing.T) {
	t.Run("no target returns error", func(t *testing.T) {
		_, err := (&ApapServer{}).TargetInfoCollector(context.Background(), &apapproto.TargetInfoRequest{})
		require.ErrorIs(t, err, message.New(message.CommonUnknownError))
	})

	t.Run("returns error when connection can't be made", func(t *testing.T) {
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		session := &targetsessionmocks.MockTargetSession{}
		provider.On("TargetSession", mock.Anything).Return(session, nil).Once()
		session.On("Connect", mock.Anything).Return(nil, errors.New("rekt")).Once()
		server := ApapServer{
			targetSessions: provider,
		}
		_, err := server.TargetInfoCollector(context.Background(), &apapproto.TargetInfoRequest{Collectors: []string{}, Target: &apapproto.Target{Connection: &apapproto.Target_LocalConfig{}}})
		require.ErrorContains(t, err, "rekt")
	})

	t.Run("returns pids and target info", func(t *testing.T) {
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		session := &targetsessionmocks.MockTargetSession{}
		provider.On("TargetSession", mock.Anything).Return(session, nil).Once()
		conn := &targetsessionmocks.MockTargetConnection{}
		conn.On("CommandRunner").Return(&conductor.LocalCommandRunner{})
		conn.On("Filesystem").Return(nil)
		conn.On("Dialer").Return(nil)
		session.On("Connect", mock.Anything).Return(conn, nil).Once()
		session.On("ResolveToolsDir").Return("/tmp/tools").Once()
		session.On("TargetPlatform").Return(&conductor.TargetPlatform{Path: &conductor.LinuxPathUtils{}, PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Linux}}, nil).Once()
		agentMock := &targetagentmocks.TargetAgentClient{}
		agentConn := &agent.AgentConn{Client: agentMock}
		session.On("TargetAgent", mock.Anything).Return(agentConn, nil).Once()
		server := ApapServer{
			targetSessions: provider,
		}

		pinfo := []*targetagentproto.ProcessInfo{
			{Pid: 123, User: "root", Name: "proc1", CommandLine: "proc1 -arg1 -arg2"},
			{Pid: 456, User: "root", Name: "alice", CommandLine: "proc2 -arg3 -arg4"},
		}

		agentMock.On("ListProcesses", mock.Anything, mock.Anything).Return(&targetagentproto.ProcessList{
			Processes: pinfo}, nil)

		agentMock.On("GetTargetInfo", mock.Anything, mock.Anything).Return(&targetagentproto.TargetInfo{
			Os: "linux", OsDescription: "Ubuntu 22.04.5 LTS", Arch: "arm64", KernelVersion: "6.8.0", IsRoot: true,
			CpuTopology: &targetagentproto.CPUTopology{
				PrimaryCPUName: "Neoverse-N1",
				CPUs: []*targetagentproto.CPUDescription{
					{
						CoreNumber: 0,
						ClusterID:  60,
						Midr:       "0x413fd0c1",
						Name:       "Neoverse-N1",
					},
					{
						CoreNumber: 1,
						ClusterID:  60,
						Midr:       "0x413fd0c1",
						Name:       "Neoverse-N1",
					},
				},
				ClusterInfo: []*targetagentproto.ClusterDescription{
					{
						ClusterID: 60,
						Name:      "Cluster 0",
					},
				},
			},
		}, nil)

		holdStream := &mocks.MockHoldLockStream{}
		holdStream.On("Recv").Return(&targetagentproto.LockGranted{}, nil).Once()
		agentMock.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
			Return(holdStream, nil).
			Once()

		resp, err := server.TargetInfoCollector(context.Background(), &apapproto.TargetInfoRequest{Collectors: []string{"sl-collect-target-info", "sl-collect-target-pids"}, Target: &apapproto.Target{Connection: &apapproto.Target_LocalConfig{}}})
		require.NoError(t, err)

		pid0, pid1 := uint32(pinfo[0].Pid), uint32(pinfo[1].Pid) // #nosec G115
		assert.Equal(t, []*apapproto.TargetInfoPIDs{
			{Pid: &pid0, Name: &pinfo[0].Name, Username: &pinfo[0].User, CommandLine: &pinfo[0].CommandLine},
			{Pid: &pid1, Name: &pinfo[1].Name, Username: &pinfo[1].User, CommandLine: &pinfo[1].CommandLine},
		}, resp.Info["sl-collect-target-pids"].GetPids().Process)

		// Check target info
		assert.Equal(t, apapproto.OsFamily_OS_FAMILY_LINUX, resp.Info["sl-collect-target-info"].GetSystem().Os.Family)
		assert.Equal(t, "6.8.0", *resp.Info["sl-collect-target-info"].GetSystem().Os.KernelVersion)
		assert.Equal(t, "Ubuntu 22.04.5 LTS", *resp.Info["sl-collect-target-info"].GetSystem().Os.Description)

		id0, id1, clusterId, name, midr, clusterName := uint32(0), uint32(1), uint32(60), "Neoverse-N1", "0x413fd0c1", "Cluster 0"
		assert.Equal(t, []*apapproto.TargetInfoCPU{
			{CoreNumber: &id0, ClusterId: &clusterId, Name: &name, Midr: &midr},
			{CoreNumber: &id1, ClusterId: &clusterId, Name: &name, Midr: &midr},
		}, resp.Info["sl-collect-target-info"].GetSystem().Cpus)
		assert.Equal(t, []*apapproto.TargetInfoCluster{{Id: &clusterId, Name: &clusterName}}, resp.Info["sl-collect-target-info"].GetSystem().Clusters)
	})
	t.Run("handles missing OSDescription", func(t *testing.T) {
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		session := &targetsessionmocks.MockTargetSession{}
		provider.On("TargetSession", mock.Anything).Return(session, nil).Once()
		conn := &targetsessionmocks.MockTargetConnection{}
		conn.On("CommandRunner").Return(stubCommandRunner{})
		conn.On("Filesystem").Return(nil)
		conn.On("Dialer").Return(nil)
		session.On("Connect", mock.Anything).Return(conn, nil).Once()
		session.On("ResolveToolsDir").Return(fmt.Sprintf(`C:\%v\tools`, terminology.GetProductBinaryName())).Once()
		session.On("TargetPlatform").Return(&conductor.TargetPlatform{Path: &conductor.WindowsPathUtils{}, PlatformConfiguration: conductor.PlatformConfiguration{OS: conductor.Win}}, nil).Once()
		agentMock := &targetagentmocks.TargetAgentClient{}
		agentConn := &agent.AgentConn{Client: agentMock}
		session.On("TargetAgent", mock.Anything).Return(agentConn, nil).Once()
		server := ApapServer{
			targetSessions: provider,
		}

		agentMock.On("GetTargetInfo", mock.Anything, mock.Anything).Return(&targetagentproto.TargetInfo{
			Os: "windows", Arch: "arm64", KernelVersion: "10.0.26000", IsRoot: true, CpuTopology: &targetagentproto.CPUTopology{
				PrimaryCPUName: "Cobalt 100",
				CPUs: []*targetagentproto.CPUDescription{
					{
						CoreNumber: 0,
						ClusterID:  0,
						Midr:       "",
						Name:       "Cobalt 100",
					},
				},
				ClusterInfo: []*targetagentproto.ClusterDescription{
					{
						ClusterID: 0,
						Name:      "Cluster 0",
					},
				},
			},
		}, nil)

		holdStream := &mocks.MockHoldLockStream{}
		holdStream.On("Recv").Return(&targetagentproto.LockGranted{}, nil).Once()
		agentMock.On("HoldLock", mock.Anything, mock.Anything, mock.Anything).
			Return(holdStream, nil).
			Once()

		resp, err := server.TargetInfoCollector(context.Background(), &apapproto.TargetInfoRequest{Collectors: []string{"sl-collect-target-info"}, Target: &apapproto.Target{Connection: &apapproto.Target_LocalConfig{}}})
		require.NoError(t, err)

		// Check target info
		assert.Equal(t, apapproto.OsFamily_OS_FAMILY_WINDOWS, resp.Info["sl-collect-target-info"].GetSystem().Os.Family)
		assert.Equal(t, "10.0.26000", *resp.Info["sl-collect-target-info"].GetSystem().Os.KernelVersion)
		// This field should be empty because OsDescription was missing from the target agent response
		assert.Equal(t, "", *resp.Info["sl-collect-target-info"].GetSystem().Os.Description)
	})
}

func runUpdatePatch(operations ...*apapproto.RunUpdateOperation) *apapproto.RunUpdatePatch {
	return &apapproto.RunUpdatePatch{Operations: operations}
}

func updateRunsRequest(runID run.RunID, patch *apapproto.RunUpdatePatch) *apapproto.UpdateRunsRequest {
	return &apapproto.UpdateRunsRequest{
		RunIds: []*apapproto.RunId{{Value: runID.Value}},
		Patch:  patch,
	}
}

func setHostSourceCodePathsOperation(paths ...string) *apapproto.RunUpdateOperation {
	return &apapproto.RunUpdateOperation{
		Operation: &apapproto.RunUpdateOperation_SetHostSourceCodePaths{
			SetHostSourceCodePaths: &apapproto.SetHostSourceCodePaths{
				Value: &apapproto.HostSourceCodePaths{Paths: paths},
			},
		},
	}
}

func setRunGroupOperation(group string) *apapproto.RunUpdateOperation {
	return &apapproto.RunUpdateOperation{
		Operation: &apapproto.RunUpdateOperation_SetGroup{
			SetGroup: &apapproto.SetRunGroup{Value: group},
		},
	}
}

func setRunTagsOperation(tags ...string) *apapproto.RunUpdateOperation {
	return &apapproto.RunUpdateOperation{
		Operation: &apapproto.RunUpdateOperation_SetTags{
			SetTags: &apapproto.SetRunTags{Value: &apapproto.StringArray{Values: tags}},
		},
	}
}

func TestUpdateRunsSingleRun(t *testing.T) {
	tmpDir := t.TempDir()
	rc, err := run.NewRunCollection(tmpDir)
	require.NoError(t, err)
	server := ApapServer{runs: rc}

	builder, err := server.runs.RunBuilder()
	require.NoError(t, err)
	runId, err := server.runs.CreateRun(builder, &cdf.Metadata{})
	require.NoError(t, err)

	t.Run("successfully creates host source code paths", func(t *testing.T) {
		req := updateRunsRequest(runId, runUpdatePatch(setHostSourceCodePathsOperation("/foo", "/bar")))

		resp, err := server.UpdateRuns(context.Background(), req)
		assert.NoError(t, err)
		require.Len(t, resp.Statuses, 1)
		assert.Nil(t, resp.Statuses[0].Error)

		r, err := server.runs.LoadRun(runId)
		require.NoError(t, err)

		component, err := r.ResolveComponent(run.SourceCodeFilename)
		require.NoError(t, err)
		require.NotEmpty(t, component)

		hscp, err := util.ReadJSONFile[run.HostSourceCodePath](component.AbsolutePath)
		require.NoError(t, err)
		assert.Equal(t, []string{"/foo", "/bar"}, hscp.Paths)
	})

	t.Run("successfully updates host source code paths", func(t *testing.T) {
		req := updateRunsRequest(runId, runUpdatePatch(setHostSourceCodePathsOperation("/baz")))

		resp, err := server.UpdateRuns(context.Background(), req)
		assert.NoError(t, err)
		require.Len(t, resp.Statuses, 1)
		assert.Nil(t, resp.Statuses[0].Error)

		r, err := server.runs.LoadRun(runId)
		require.NoError(t, err)

		component, err := r.ResolveComponent(run.SourceCodeFilename)
		require.NoError(t, err)
		require.NotEmpty(t, component)

		hscp, err := util.ReadJSONFile[run.HostSourceCodePath](component.AbsolutePath)
		require.NoError(t, err)
		assert.Equal(t, []string{"/baz"}, hscp.Paths)
	})

	t.Run("successfully updates categorization through patch", func(t *testing.T) {
		req := updateRunsRequest(runId, runUpdatePatch(
			setRunGroupOperation("test-group"),
			setRunTagsOperation("tag-a", "tag-b"),
		))

		resp, err := server.UpdateRuns(context.Background(), req)
		assert.NoError(t, err)
		require.Len(t, resp.Statuses, 1)
		assert.Nil(t, resp.Statuses[0].Error)

		categorization, err := run.ReadRunCategorization(filepath.Join(server.runs.GetRunPath(runId), run.CategorizationFilename))
		require.NoError(t, err)
		assert.Equal(t, "test-group", categorization.Group)
		assert.Equal(t, []string{"tag-a", "tag-b"}, categorization.Tags)
	})

	t.Run("locks the run when updating host source code paths", func(t *testing.T) {
		req := updateRunsRequest(runId, runUpdatePatch(setHostSourceCodePathsOperation("/baz")))

		// Lock the run to simulate it being in use
		unlock, err := server.runs.LockRun(context.Background(), runId)
		require.NoError(t, err)

		// We pass a relatively short-lived `ctx` which will cause the
		// underlying lock acquisition to time out quickly.
		// When that happens it'll return 'engine.run.BUSY' error
		// Which means the update tried to acquire the lock (as expected)
		ctx, cancel := context.WithTimeout(
			context.Background(), 10*time.Millisecond)
		defer cancel()

		expectedErr := message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runId.Value})

		resp, err := server.UpdateRuns(ctx, req)
		require.NoError(t, err)
		require.Len(t, resp.Statuses, 1)
		reconstructed := message.ReconstructFromChain(resp.Statuses[0].Error)
		assert.True(t, errors.Is(reconstructed, expectedErr))
		assert.NoError(t, message.ValidateMetadataPlaceholders(reconstructed))

		require.NoError(t, unlock())

		// Now that the run is unlocked, the update should succeed
		resp, err = server.UpdateRuns(context.Background(), req)
		assert.NoError(t, err)
		require.Len(t, resp.Statuses, 1)
		assert.Nil(t, resp.Statuses[0].Error)
	})

	t.Run("fails when run id does not exist", func(t *testing.T) {
		req := updateRunsRequest(run.RunID{Value: "i-dont-exist"}, runUpdatePatch(setHostSourceCodePathsOperation("/foo", "/bar")))

		resp, err := server.UpdateRuns(context.Background(), req)
		require.NoError(t, err)
		require.Len(t, resp.Statuses, 1)
		assert.True(t, errors.Is(message.ReconstructFromChain(resp.Statuses[0].Error), message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "i-dont-exist"})))
	})
}

func TestUpdateRuns(t *testing.T) {
	tmpDir := t.TempDir()
	rc, err := run.NewRunCollection(tmpDir)
	require.NoError(t, err)
	server := ApapServer{runs: rc}

	createRun := func(t *testing.T) run.RunID {
		t.Helper()
		builder, err := server.runs.RunBuilder()
		require.NoError(t, err)
		runID, err := server.runs.CreateRun(builder, &cdf.Metadata{})
		require.NoError(t, err)
		return runID
	}

	readCategorization := func(t *testing.T, runID run.RunID) *run.RunCategorization {
		t.Helper()
		categorization, err := run.ReadRunCategorization(filepath.Join(server.runs.GetRunPath(runID), run.CategorizationFilename))
		require.NoError(t, err)
		return categorization
	}

	t.Run("reports partial failures per run", func(t *testing.T) {
		runID1 := createRun(t)
		runID2 := createRun(t)

		unlock, err := server.runs.LockRun(context.Background(), runID2)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		resp, err := server.UpdateRuns(ctx, &apapproto.UpdateRunsRequest{
			RunIds: []*apapproto.RunId{
				{Value: runID1.Value},
				{Value: runID2.Value},
				{Value: "i-dont-exist"},
			},
			Patch: runUpdatePatch(setRunGroupOperation("new-group")),
		})
		require.NoError(t, err)
		require.Len(t, resp.Statuses, 3)
		assert.Nil(t, resp.Statuses[0].Error)
		assert.True(t, errors.Is(message.ReconstructFromChain(resp.Statuses[1].Error), message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID2.Value})))
		assert.True(t, errors.Is(message.ReconstructFromChain(resp.Statuses[2].Error), message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": "i-dont-exist"})))

		categorization := readCategorization(t, runID1)
		assert.Equal(t, "new-group", categorization.Group)

		require.NoError(t, unlock())
	})

	t.Run("accepts empty update as no-op", func(t *testing.T) {
		resp, err := server.UpdateRuns(context.Background(), &apapproto.UpdateRunsRequest{
			RunIds: []*apapproto.RunId{{Value: createRun(t).Value}},
		})
		require.NoError(t, err)
		require.Len(t, resp.Statuses, 1)
		assert.Nil(t, resp.Statuses[0].Error)
	})
}

func TestTargetTest(t *testing.T) {
	t.Run("successful connection", func(t *testing.T) {
		session := &targetsessionmocks.MockTargetSession{}
		session.On("Connect", mock.Anything).Return(nil, nil).Once()

		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(session, nil).Once()

		server := ApapServer{targetSessions: provider}
		req := &apapproto.TargetTestRequest{
			Target: TargetToProto(&target.LocalTarget{}),
		}

		resp, err := server.TargetTest(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, apapproto.ConnectionStatus_CONNECTION_STATUS_OK, resp.Connection.Status)
		provider.AssertExpectations(t)
		session.AssertExpectations(t)
	})
	t.Run("bad connection", func(t *testing.T) {
		session := &targetsessionmocks.MockTargetSession{}
		session.On("Connect", mock.Anything).Return(nil, errors.New("unable to connect to target")).Once()

		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(session, nil).Once()

		server := ApapServer{targetSessions: provider}
		req := &apapproto.TargetTestRequest{
			Target: TargetToProto(&target.LocalTarget{}),
		}

		resp, err := server.TargetTest(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, apapproto.ConnectionStatus_CONNECTION_STATUS_ERROR, resp.Connection.Status)
		require.NotNil(t, resp.Connection.Error)
		provider.AssertExpectations(t)
		session.AssertExpectations(t)
	})
	t.Run("target session provider error", func(t *testing.T) {
		tgtSessionErr := errors.New("target session provider not available")
		provider := &targetsessionmocks.MockTargetSessionProvider{}
		provider.On("TargetSession", mock.Anything).Return(&targetsessionmocks.MockTargetSession{}, tgtSessionErr).Once()

		server := ApapServer{targetSessions: provider}
		req := &apapproto.TargetTestRequest{
			Target: TargetToProto(&target.LocalTarget{}),
		}

		resp, err := server.TargetTest(context.Background(), req)
		require.Nil(t, resp)
		require.ErrorIs(t, err, tgtSessionErr)
		provider.AssertExpectations(t)
	})
}

func TestListDirectories(t *testing.T) {
	server := ApapServer{}

	expectedLogDir, err := userdirs.StateDir()
	require.NoError(t, err)
	expectedDefaultDataDir, err := userdirs.DefaultDataDir(terminology.GetDaemonDirName())
	require.NoError(t, err)

	resp, err := server.ListDirectories(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, expectedLogDir, resp.GetLogDir())
	assert.Equal(t, expectedDefaultDataDir, resp.GetDefaultDataDir())
}

func TestBuildSupportPackageConfigIncludesAllFields(t *testing.T) {
	server := &ApapServer{
		config: ApapServerConfig{
			ParallelJobs:              3,
			DataDirectory:             "/var/lib/apap",
			IsRootWorkerEnabled:       true,
			EnableFullCaptureSupport:  true,
			EnableRerendering:         true,
			EnableExperimentalRecipes: true,
			EnableSecondaryRunPaths:   true,
			EnableTransferManager:     true,
			EnableRenderDBSandbox:     false,
			EnableNeoprofTimeline:     true,
			ServerHostname:            "127.0.0.1",
			ServerGRPCPort:            9000,
			ServerAuthPort:            9001,
			ServerHTTPPort:            8080,
			ServerHTTPChunkBytes:      4096,
			LogLevel:                  "debug",
			LogFile:                   "/tmp/apap.log",
			SourceToolsDir:            "/opt/apap/tools/src",
			ConfigDirectory:           "/etc/apap",
		},
		deploymentPaths: deployer.BaseToolDeploymentPaths{
			DeployedToolsDirectory: "/opt/apap/tools/deployed",
		},
	}

	// Numeric values are represented as float64 in the resulting map due to the conversion function which utilises JSON marshalling.
	// This structure is used to generate a JSON blob so the intermediate float64 isn't a problem. float64 can map to integers precisely
	// so long as they're less than 9,007,199,254,740,992 (2^53).
	expected := map[string]any{
		"server-hostname":             "127.0.0.1",
		"server-port":                 float64(9000),
		"auth-port":                   float64(9001),
		"http-port":                   float64(8080),
		"http-chunk-bytes":            float64(4096),
		"jobs":                        float64(3),
		"data-dir":                    "/var/lib/apap",
		"log-level":                   "debug",
		"log-file":                    "/tmp/apap.log",
		"enable-on-demand-privilege":  true,
		"enable-full-capture-support": true,
		"enable-rerendering":          true,
		"enable-experimental-recipes": true,
		"enable-secondary-run-paths":  true,
		"enable-transfer-manager":     true,
		"enable-render-db-sandbox":    false,
		"enable-neoprof-timeline":     true,
		"deployment-tools-dir":        "/opt/apap/tools/deployed",
		"source-tools-dir":            "/opt/apap/tools/src",
		"config-dir":                  "/etc/apap",
	}

	require.Equal(t, expected, server.buildSupportPackageConfig())
}

func TestNewSessionFactoryUsesRenderDBSandboxFlag(t *testing.T) {
	server := &ApapServer{
		config: ApapServerConfig{EnableRenderDBSandbox: false},
	}

	factory, ok := server.newSessionFactory().(*sessionfactory.Impl)
	require.True(t, ok)
	require.NotNil(t, factory.Flags)
	require.Equal(t, render.SessionFlags{EnableDuckDBSandbox: false}, *factory.Flags)
}

func TestBuildSupportPackageConfigIncludesEmptyFields(t *testing.T) {
	server := &ApapServer{
		config: ApapServerConfig{},
	}

	cfg := server.buildSupportPackageConfig()

	require.Contains(t, cfg, "server-hostname")
	require.Equal(t, "", cfg["server-hostname"])
	require.Contains(t, cfg, "deployment-tools-dir")
	require.Equal(t, "", cfg["deployment-tools-dir"])
	require.Contains(t, cfg, "jobs")
	require.Equal(t, float64(0), cfg["jobs"])
}

func TestCreateSupportPackage_ReturnsRunExportUnavailableWhenRunsMissing(t *testing.T) {
	rc, err := run.NewRunCollection(t.TempDir())
	require.NoError(t, err)
	server := &ApapServer{runs: rc}

	req := &apapproto.CreateSupportPackageRequest{
		RunIds: []*apapproto.RunId{
			{Value: "run-123"},
		},
		OutputDirectory: t.TempDir(),
		LogCount:        1,
	}

	resp, err := server.CreateSupportPackage(context.Background(), req)

	require.Error(t, err)
	msg := message.IsMessage(err)
	require.NotNil(t, msg)
	require.Equal(t, message.EngineRunDoesNotExist, msg.Code())
	require.NotNil(t, resp)
	require.Zero(t, resp.GetPackageSizeBytes())
}

func TestCreateSupportPackage_SucceedsWhenNoEngineLogs(t *testing.T) {
	tmp := t.TempDir()
	stateHome := filepath.Join(tmp, "state")
	dataHome := filepath.Join(tmp, "data")
	configHome := filepath.Join(tmp, "config")

	require.NoError(t, os.MkdirAll(stateHome, os.ModePerm))
	require.NoError(t, os.MkdirAll(dataHome, os.ModePerm))
	require.NoError(t, os.MkdirAll(configHome, os.ModePerm))

	daemonDir := terminology.GetDaemonDirName()
	require.NoError(t, os.MkdirAll(filepath.Join(stateHome, daemonDir), os.ModePerm))
	require.NoError(t, os.MkdirAll(filepath.Join(dataHome, daemonDir), os.ModePerm))
	require.NoError(t, os.MkdirAll(filepath.Join(configHome, daemonDir), os.ModePerm))

	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	rc, err := run.NewRunCollection(t.TempDir())
	require.NoError(t, err)
	server := &ApapServer{runs: rc}

	req := &apapproto.CreateSupportPackageRequest{
		OutputDirectory: t.TempDir(),
		LogCount:        1,
	}

	resp, err := server.CreateSupportPackage(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.FileExists(t, resp.GetPackagePath())
}
