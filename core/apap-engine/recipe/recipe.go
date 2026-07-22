// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"fmt"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/parameters"
)

type RecipeStatus string

const (
	RecipeStatusStable       RecipeStatus = "stable"
	RecipeStatusExperimental RecipeStatus = "experimental"
	RecipeStatusPreview      RecipeStatus = "preview"
)

func ParseRecipeStatus(status string) (RecipeStatus, error) {
	switch RecipeStatus(strings.ToLower(strings.TrimSpace(status))) {
	case "":
		return RecipeStatusPreview, nil
	case RecipeStatusStable:
		return RecipeStatusStable, nil
	case RecipeStatusExperimental:
		return RecipeStatusExperimental, nil
	case RecipeStatusPreview:
		return RecipeStatusPreview, nil
	default:
		return "", fmt.Errorf("invalid recipe status %q", status)
	}
}

type Recipe struct {
	Name        string
	Title       string
	Description string
	// MCPGuidance is agent-facing recipe advice. Keep it separate from
	// Description so GUI and CLI recipe summaries stay user-facing.
	MCPGuidance              string
	Version                  string
	APIVersion               string
	Status                   RecipeStatus
	Parameters               parameters.Parameters
	RenderParameters         parameters.RenderParameters
	RunStages                []ScriptedStage
	ReadyStages              []ScriptedStage
	RenderStages             []ScriptedStage
	ParameterOptionsStages   []ScriptedStage
	ParameterValidationStage ScriptedStage
	Deployments              []deploymentsupport.DeploymentDeclaration
	ToolVersions             map[string]string
}

type Parser interface {
	ParseRecipe(sourceName string, content string) (Recipe, error)
}
