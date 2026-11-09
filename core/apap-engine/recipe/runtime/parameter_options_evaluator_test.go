// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cmdsync"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	targetsessionmocks "github.com/Arm-Debug/apap-cli/apap-engine/targetsession/mocks"
)

func TestParameterOptionsEvaluatorMissingConfig(t *testing.T) {
	evaluator := &ParameterOptionsEvaluatorConcrete{}

	_, err := evaluator.Evaluate(context.Background(), nil, nil)
	require.Error(t, err)

	_, err = evaluator.Evaluate(context.Background(), &StageConfiguration{}, nil)
	require.Error(t, err)
}

func TestParameterOptionsEvaluatorRequiresStageContext(t *testing.T) {
	evaluator := &ParameterOptionsEvaluatorConcrete{}

	recipeDef := &recipe.Recipe{
		Parameters: parameters.Parameters{
			SingleSelect: []parameters.SingleSelectParameter{{Parameter: parameters.Parameter{ID: "select1"}}},
			Radio: []parameters.RadioParameter{
				{Parameter: parameters.Parameter{ID: "radio1"}},
				{Parameter: parameters.Parameter{ID: "radio2"}},
			},
		},
	}
	sc := &StageConfiguration{
		Recipe: recipeDef,
		Ctx:    &recipe.RecipeCtx{Target: &target.LocalTarget{}},
	}

	_, err := evaluator.Evaluate(context.Background(), sc, nil)
	require.Error(t, err)
}

func TestParameterOptionsEvaluatorStageContextValidation(t *testing.T) {
	evaluator := &ParameterOptionsEvaluatorConcrete{}

	recipeDef := &recipe.Recipe{
		Parameters: parameters.Parameters{
			SingleSelect: []parameters.SingleSelectParameter{{Parameter: parameters.Parameter{ID: "select1"}}},
			MultiSelect:  []parameters.MultiSelectParameter{{Parameter: parameters.Parameter{ID: "multi1"}}},
			Radio:        []parameters.RadioParameter{{Parameter: parameters.Parameter{ID: "radio1"}}},
		},
	}
	sc := &StageConfiguration{
		Recipe: recipeDef,
		Ctx:    &recipe.RecipeCtx{Target: &target.LocalTarget{}},
	}

	base := &recipe.StageContext{
		Context:               context.Background(),
		CommandState:          &cmdsync.CommandState{},
		ReadinessNotifier:     &recipe.NullReadinessNotifier{},
		RendererNotifier:      &recipe.NullRenderNotifier{},
		StageNotifier:         &recipe.NullStageNotifier{},
		TargetSupportNotifier: &recipe.NullTargetSupportNotifier{},
		ParameterOptions: recipe.ParameterOptions{
			SingleSelectOptions: make([][]parameters.ParameterOption, len(recipeDef.Parameters.SingleSelect)),
			MultiSelectOptions:  make([][]parameters.ParameterOption, len(recipeDef.Parameters.MultiSelect)),
			RadioOptions:        make([][]parameters.ParameterOption, len(recipeDef.Parameters.Radio)),
		},
	}

	cases := []struct {
		name  string
		stage *recipe.StageContext
	}{
		{name: "missing context", stage: func() *recipe.StageContext { c := *base; c.Context = nil; return &c }()},
		{name: "missing command state", stage: func() *recipe.StageContext { c := *base; c.CommandState = nil; return &c }()},
		{name: "missing readiness notifier", stage: func() *recipe.StageContext { c := *base; c.ReadinessNotifier = nil; return &c }()},
		{name: "missing renderer notifier", stage: func() *recipe.StageContext { c := *base; c.RendererNotifier = nil; return &c }()},
		{name: "missing stage notifier", stage: func() *recipe.StageContext { c := *base; c.StageNotifier = nil; return &c }()},
		{name: "missing target support notifier", stage: func() *recipe.StageContext { c := *base; c.TargetSupportNotifier = nil; return &c }()},
		{name: "invalid single select options size", stage: func() *recipe.StageContext {
			c := *base
			c.ParameterOptions.SingleSelectOptions = [][]parameters.ParameterOption{}
			return &c
		}()},
		{name: "invalid multi select options size", stage: func() *recipe.StageContext {
			c := *base
			c.ParameterOptions.MultiSelectOptions = [][]parameters.ParameterOption{}
			return &c
		}()},
		{name: "invalid radio options size", stage: func() *recipe.StageContext {
			c := *base
			c.ParameterOptions.RadioOptions = [][]parameters.ParameterOption{}
			return &c
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := evaluator.Evaluate(context.Background(), sc, tc.stage)
			require.Error(t, err)
		})
	}
}

func TestParameterOptionsEvaluatorReturnsOptions(t *testing.T) {
	evaluator := &ParameterOptionsEvaluatorConcrete{}

	recipeDef := &recipe.Recipe{
		Parameters: parameters.Parameters{
			SingleSelect: []parameters.SingleSelectParameter{{Parameter: parameters.Parameter{ID: "select1"}}},
			MultiSelect:  []parameters.MultiSelectParameter{{Parameter: parameters.Parameter{ID: "multi1"}}},
			Radio: []parameters.RadioParameter{
				{Parameter: parameters.Parameter{ID: "radio1"}},
				{Parameter: parameters.Parameter{ID: "radio2"}},
			},
		},
	}
	ctx := context.Background()
	stageContext := &recipe.StageContext{
		Context:               ctx,
		CommandState:          &cmdsync.CommandState{},
		ReadinessNotifier:     &recipe.NullReadinessNotifier{},
		RendererNotifier:      &recipe.NullRenderNotifier{},
		StageNotifier:         &recipe.NullStageNotifier{},
		TargetSupportNotifier: &recipe.NullTargetSupportNotifier{},
		ParameterOptions: recipe.ParameterOptions{
			SingleSelectOptions: make([][]parameters.ParameterOption, len(recipeDef.Parameters.SingleSelect)),
			MultiSelectOptions:  make([][]parameters.ParameterOption, len(recipeDef.Parameters.MultiSelect)),
			RadioOptions:        make([][]parameters.ParameterOption, len(recipeDef.Parameters.Radio)),
		},
	}
	sc := &StageConfiguration{
		Recipe: recipeDef,
		Ctx:    &recipe.RecipeCtx{},
	}

	options, err := evaluator.Evaluate(ctx, sc, stageContext)
	require.NoError(t, err)
	require.Len(t, options.SingleSelectOptions, 1)
	require.Len(t, options.MultiSelectOptions, 1)
	require.Len(t, options.RadioOptions, 2)
}

func TestParameterOptionsEvaluatorChecksTargetSupportWithoutDynamicOptions(t *testing.T) {
	evaluator := &ParameterOptionsEvaluatorConcrete{}
	linux := conductor.Linux
	aarch64 := conductor.AArch64
	recipeDef := &recipe.Recipe{
		Deployments: []deploymentsupport.DeploymentDeclaration{
			{
				AppliesTo: []deploymentsupport.PlatformConfigurationFilter{
					{OS: &linux, Architecture: &aarch64},
				},
			},
		},
	}
	targetPlatform := &conductor.TargetPlatform{
		PlatformConfiguration: conductor.PlatformConfiguration{
			OS:           conductor.Linux,
			Architecture: conductor.X86_64,
		},
	}
	targetSession := &targetsessionmocks.MockTargetSession{}
	targetSession.On("Connect", mock.Anything).Return(
		&stubTargetConnection{fs: afero.NewMemMapFs()},
		nil,
	)
	targetSession.On("TargetPlatform").Return(targetPlatform, nil)
	targetSessionProvider := &stubTargetSessionProvider{session: targetSession}
	target := &target.LocalTarget{}
	collector := &TargetSupportCollector{}
	packageRoot := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(packageRoot, "tools"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(packageRoot, "tool-integrations"), 0o755))
	ctx := context.Background()
	stageContext := &recipe.StageContext{
		Context:               ctx,
		CommandState:          &cmdsync.CommandState{},
		ReadinessNotifier:     &recipe.NullReadinessNotifier{},
		RendererNotifier:      &recipe.NullRenderNotifier{},
		StageNotifier:         &recipe.NullStageNotifier{},
		TargetSupportNotifier: collector,
		ParameterOptions: recipe.ParameterOptions{
			SingleSelectOptions: [][]parameters.ParameterOption{},
			MultiSelectOptions:  [][]parameters.ParameterOption{},
			RadioOptions:        [][]parameters.ParameterOption{},
		},
	}
	stageConfiguration := &StageConfiguration{
		Recipe:         recipeDef,
		Ctx:            &recipe.RecipeCtx{Target: target},
		TargetSessions: targetSessionProvider,
		PackageManager: packages.NewPackageManager(packageRoot, ""),
	}

	_, err := evaluator.Evaluate(ctx, stageConfiguration, stageContext)

	require.NoError(t, err)
	require.Equal(t, deploymentsupport.PlatformNotSupported, collector.PlatformSupport.Result)
	require.Equal(t, targetPlatform.PlatformConfiguration, collector.PlatformSupport.Platform)
	targetSession.AssertExpectations(t)
}
