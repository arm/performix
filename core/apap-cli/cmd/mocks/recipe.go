// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mocks

import (
	"io"
	"strings"

	"github.com/stretchr/testify/mock"

	"github.com/Arm-Debug/apap-cli/apap-cli/service/recipe"
	engine_recipe "github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type MockReadyRecipe struct {
	mock.Mock
}

type MockRecipeParser struct {
	mock.Mock
}

type MockRecipeRunner struct {
	mock.Mock
}

type MockRecipeRunResponse struct {
	mock.Mock
}

type MockRecipeFileReader struct {
	mock.Mock
}

func (m *MockRecipeRunner) SendRecipeRunToEngine(client apapproto.ApapClient, recipeInfo *engine_recipe.Recipe, recipeCtx *recipe.RecipeExecutionCtx, out io.Writer) (recipe.RunResponse, error) {
	mockArgs := m.Called(client, recipeInfo, recipeCtx)
	mockResult := mockArgs.Get(0).(recipe.RunResponse)
	return mockResult, mockArgs.Error(1)
}

func (m *MockRecipeRunResponse) ToJSON() (string, error) {
	mockArgs := m.Called()
	return mockArgs.String(0), mockArgs.Error(1)
}

type MockRecipeReader struct {
	mock.Mock
}

func (s *MockRecipeReader) ReadRecipes(errHandler func(string, error)) (recipes map[string]engine_recipe.Recipe, err error) {
	mockArgs := s.Called(errHandler)
	return mockArgs.Get(0).(map[string]engine_recipe.Recipe), mockArgs.Error(1)
}

func (s *MockRecipeReader) ReadRecipe(file string) (recipeInfo engine_recipe.Recipe, err error) {
	mockArgs := s.Called(file)
	return mockArgs.Get(0).(engine_recipe.Recipe), mockArgs.Error(1)
}

func (s *MockRecipeReader) IsRecipeValidFile(name string) bool {
	return strings.Contains(name, ".js")
}

func (m *MockReadyRecipe) ReadyRecipe(client apapproto.ApapClient, recipeInfo *engine_recipe.Recipe, recipeCtx *recipe.RecipeExecutionCtx) (*recipe.RecipeReadyResponse, error) {
	mockArgs := m.Called(client, recipeInfo, recipeCtx)
	return mockArgs.Get(0).(*recipe.RecipeReadyResponse), mockArgs.Error(1)
}
