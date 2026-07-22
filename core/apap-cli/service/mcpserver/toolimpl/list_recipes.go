// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

type ListRecipesTool struct{}

type listRecipesInput struct{}

type listRecipesResult struct {
	Recipes    []recipeListingEntry `json:"recipes"`
	LoadErrors []recipeLoadError    `json:"load_errors,omitempty"`
	Error      *toolError           `json:"error,omitempty"`
}

type recipeListingEntry struct {
	Name string `json:"name"`
}

type recipeLoadError struct {
	Path        string `json:"path"`
	Message     string `json:"message,omitempty"`
	MessageCode string `json:"message_code,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Advice      string `json:"advice,omitempty"`
}

var listRecipesOutputSchema = &jsonschema.Schema{
	Type:     "object",
	Required: []string{"recipes"},
	Properties: map[string]*jsonschema.Schema{
		"recipes": {
			Type:        "array",
			Description: "Recipes that loaded successfully and can be run with run_recipe.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"name"},
				Properties: map[string]*jsonschema.Schema{
					"name": {Type: "string", Description: "Name of the recipe. Pass this as the recipe argument to run_recipe."},
				},
			},
		},
		"load_errors": {
			Type:        "array",
			Description: "Recipes that failed to load, with error detail. Omitted when every recipe loaded successfully.",
			Items: &jsonschema.Schema{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]*jsonschema.Schema{
					"path":         {Type: "string", Description: "Filesystem path of the recipe that failed to load."},
					"message":      {Type: "string", Description: "Human-readable summary of the load failure."},
					"message_code": {Type: "string", Description: "Catalog message code, when the failure maps to a known catalog message."},
					"severity":     {Type: "string", Description: "Catalog severity (for example Error or Warning), when available."},
					"explanation":  {Type: "string", Description: "Detailed explanation of the cause, when available from the catalog."},
					"advice":       {Type: "string", Description: "Suggested next steps to resolve the problem, when available from the catalog."},
				},
			},
		},
		"error": toolErrorSchema(),
	},
}

func (ListRecipesTool) Register(server *mcp.Server, toolDeps ToolDependencies) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_recipes",
		Description: "Lists available " + terminology.GetProductFullName() + " recipes. Recipes that failed to load are included with error details.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
		},
		OutputSchema: listRecipesOutputSchema,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listRecipesInput) (*mcp.CallToolResult, listRecipesResult, error) {
		listing, err := toolDeps.Engine.ListRecipes(ctx, &emptypb.Empty{})
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, listRecipesResult{Recipes: []recipeListingEntry{}, Error: newToolError(err)}, nil
		}

		return nil, newListRecipesResult(listing), nil
	})
}

func newListRecipesResult(listing *apapproto.RecipeNameListing) listRecipesResult {
	result := listRecipesResult{
		Recipes: []recipeListingEntry{},
	}
	for _, recipe := range listing.GetRecipeNames() {
		switch identifier := recipe.GetIdentifier().(type) {
		case *apapproto.RecipeNameEntry_Name:
			result.Recipes = append(result.Recipes, recipeListingEntry{Name: identifier.Name})
		case *apapproto.RecipeNameEntry_Path:
			errorMessage := message.ReconstructFromChain(recipe.GetLoadError())
			if errorMessage != nil {
				result.LoadErrors = append(result.LoadErrors, newRecipeLoadError(identifier.Path, errorMessage))
			}
		}
	}
	return result
}

func newRecipeLoadError(path string, err error) recipeLoadError {
	loadError := recipeLoadError{
		Path: path,
	}

	if catalogMsg, lookupErr := message.LookupMessage(err); lookupErr == nil {
		loadError.Message = catalogMsg.Message
		loadError.MessageCode = catalogMsg.Code
		loadError.Severity = catalogMsg.Severity
		loadError.Explanation = catalogMsg.Explanation
		loadError.Advice = catalogMsg.Advice
		return loadError
	}
	loadError.Message = err.Error()
	return loadError
}
