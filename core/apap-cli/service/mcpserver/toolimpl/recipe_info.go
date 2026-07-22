// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file exposes the engine ParseRecipe RPC through MCP so clients can
// inspect recipe-specific parameters and support information before run_recipe.

package toolimpl

import (
	"context"
	"errors"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	recipejson "github.com/Arm-Debug/apap-cli/apap-cli/service/clijson/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/grpcserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type RecipeInfoTool struct{}

type recipeInfoInput struct {
	Recipe string `json:"recipe"`
	Target string `json:"target,omitempty"`
}

type recipeInfoResult struct {
	Name               string                              `json:"name,omitempty"`
	Title              string                              `json:"title,omitempty"`
	Description        string                              `json:"description,omitempty"`
	MCPGuidance        string                              `json:"mcp_guidance,omitempty"`
	Version            string                              `json:"version,omitempty"`
	Status             string                              `json:"status,omitempty"`
	Parameters         []recipejson.ParameterJSON          `json:"parameters,omitempty"`
	RenderParameters   []recipejson.RenderParameterJSON    `json:"render_parameters,omitempty"`
	SupportedPlatforms []recipejson.SupportedPlatformsJSON `json:"supported_platforms,omitempty"`
	TargetSupport      *recipejson.TargetSupportJSON       `json:"target_support,omitempty"`
	Error              *toolError                          `json:"error,omitempty"`
}

var recipeInfoInputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"recipe"},
	Properties: map[string]*jsonschema.Schema{
		"recipe": {
			Type:        "string",
			Description: "Recipe name to inspect. Use list_recipes to discover available recipe names.",
		},
		"target": {
			Type:        "string",
			Description: "Optional configured target name. When supplied, recipe_info returns target-specific parameter options and target support.",
		},
	},
}

var recipeInfoOutputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"name":                {Type: "string", Description: "Recipe name. Pass this as the recipe argument to run_recipe."},
		"title":               {Type: "string", Description: "Human-readable recipe title."},
		"description":         {Type: "string", Description: "Recipe description."},
		"mcp_guidance":        {Type: "string", Description: "MCP-specific recipe guidance, such as recommended timeout or parameter choices."},
		"version":             {Type: "string", Description: "Recipe version."},
		"status":              {Type: "string", Description: "Recipe stability status."},
		"parameters":          recipeInfoParametersSchema(),
		"render_parameters":   recipeInfoRenderParametersSchema(),
		"supported_platforms": recipeInfoSupportedPlatformsSchema(),
		"target_support":      recipeInfoTargetSupportSchema(),
		"error":               toolErrorSchema(),
	},
}

func (RecipeInfoTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "recipe_info",
		Description: "Returns detailed " + terminology.GetProductFullName() + " recipe metadata, including recipe-specific parameters, defaults, options and platform support. " +
			"Call this before run_recipe to look up recipe-specific parameters, or to check target compatibility.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		InputSchema:  recipeInfoInputSchema,
		OutputSchema: recipeInfoOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input recipeInfoInput) (*mcp.CallToolResult, recipeInfoResult, error) {
		request := &apapproto.ParseRecipeMessage{Name: input.Recipe}
		targetSupportMode := false
		if input.Target != "" {
			if toolDeps.Targets == nil {
				return recipeInfoError(errors.New("target configuration is not available"))
			}
			tgt, err := toolDeps.Targets.GetTarget(input.Target)
			if err != nil {
				return recipeInfoError(err)
			}
			protoTarget := grpcserver.TargetToProto(tgt)
			if protoTarget == nil {
				return recipeInfoError(errors.New("target could not be converted to the engine target format"))
			}
			request.Target = protoTarget
			targetSupportMode = true
		}

		recipeInfo, err := toolDeps.Engine.ParseRecipe(ctx, request)
		if err != nil {
			return recipeInfoError(err)
		}

		return nil, recipeInfoResultFromProto(recipeInfo, targetSupportMode), nil
	})
}

func recipeInfoError(err error) (*mcp.CallToolResult, recipeInfoResult, error) {
	return &mcp.CallToolResult{IsError: true}, recipeInfoResult{Error: newToolError(err)}, nil
}

func recipeInfoResultFromProto(in *apapproto.ParseRecipeResponse, targetSupportMode bool) recipeInfoResult {
	info := recipejson.ConvertRecipeStruct(in, targetSupportMode)
	out := recipeInfoResult{
		Name:               info.Name,
		Title:              info.Title,
		Description:        info.Description,
		MCPGuidance:        info.MCPGuidance,
		Version:            info.Version,
		Status:             recipeStatusString(in.GetStatus()),
		Parameters:         recipeInfoParameters(info.Parameters),
		RenderParameters:   info.RenderParameters,
		SupportedPlatforms: info.SupportedPlatforms,
	}
	if targetSupportMode {
		out.TargetSupport = &info.TargetSupport
	}
	return out
}

func recipeStatusString(status apapproto.RecipeStatus) string {
	return strings.TrimPrefix(strings.ToLower(status.String()), "recipe_status_")
}

func recipeInfoParameters(parameters []recipejson.ParameterJSON) []recipejson.ParameterJSON {
	for i := range parameters {
		parameters[i].Config.Options = nil
	}
	return parameters
}

func recipeInfoParametersSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "array",
		Description: "Input parameters accepted by run_recipe tool.",
		Items: &jsonschema.Schema{
			Type:     "object",
			Required: []string{"id", "config"},
			Properties: map[string]*jsonschema.Schema{
				"id":          {Type: "string", Description: "Parameter id to use as the key in run_recipe parameters."},
				"required":    {Type: "boolean", Description: "Whether this parameter must be supplied."},
				"label":       {Type: "string", Description: "Human-readable parameter label."},
				"description": {Type: "string", Description: "Parameter description."},
				"config": {
					Type:        "object",
					Description: "Parameter type, options and default value.",
					Properties: map[string]*jsonschema.Schema{
						"type":         {Type: "string", Enum: []any{"single_select", "multi_select", "radio", "checkbox", "input"}},
						"optionItems":  recipeInfoOptionItemsSchema(),
						"defaultValue": {Description: "Default parameter value."},
					},
				},
			},
		},
	}
}

func recipeInfoOptionItemsSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "array",
		Description: "Rich option metadata for select-style parameters.",
		Items: &jsonschema.Schema{
			Type:     "object",
			Required: []string{"value", "label"},
			Properties: map[string]*jsonschema.Schema{
				"value":       {Type: "string"},
				"label":       {Type: "string"},
				"description": {Type: "string"},
			},
		},
	}
}

func recipeInfoRenderParametersSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "array",
		// TODO: Revisit this description when MCP render tooling is added.
		Description: "Render parameters exposed by runs produced by this recipe.",
		Items: &jsonschema.Schema{
			Type:     "object",
			Required: []string{"id", "type"},
			Properties: map[string]*jsonschema.Schema{
				"id":       {Type: "string"},
				"type":     {Type: "string", Enum: []any{"number", "string"}},
				"is_array": {Type: "boolean"},
			},
		},
	}
}

func recipeInfoSupportedPlatformsSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "array",
		Description: "If the target input parameter for the recipe_info tool is empty, this schema will show a complete list of platforms supported by the selected recipe.",
		Items: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"platform": {
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"architecture": {Type: "string"},
						"os":           {Type: "string"},
					},
				},
				"result":     {Type: "string", Enum: []any{"supported", "conditionally_supported", "unsupported", "unknown"}},
				"conditions": recipeInfoConditionsSchema(),
			},
		},
	}
}

func recipeInfoTargetSupportSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "object",
		Description: "Support status of the selected recipe on the selected target.",
		Properties: map[string]*jsonschema.Schema{
			"result":     {Type: "string", Enum: []any{"supported", "conditionally_supported", "unsupported", "unknown"}},
			"conditions": recipeInfoConditionsSchema(),
		},
	}
}

func recipeInfoConditionsSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "array",
		Items: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"type":       {Type: "string"},
				"parameters": {Type: "object", AdditionalProperties: &jsonschema.Schema{Type: "string"}},
			},
		},
	}
}
