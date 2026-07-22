// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolimpl

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestListRecipesTool(t *testing.T) {
	t.Run("advertises read-only hint", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRecipesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		tools, err := clientSession.ListTools(ctx, nil)

		require.NoError(t, err)
		require.Len(t, tools.Tools, 1)
		assert.Equal(t, "list_recipes", tools.Tools[0].Name)
		require.NotNil(t, tools.Tools[0].Annotations)
		assert.True(t, tools.Tools[0].Annotations.ReadOnlyHint)
	})

	t.Run("returns engine recipe listing", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRecipes", mock.Anything, &emptypb.Empty{}).Return(&apapproto.RecipeNameListing{
			RecipeNames: []*apapproto.RecipeNameEntry{
				{
					Identifier: &apapproto.RecipeNameEntry_Name{Name: "code_hotspots"},
				},
				{
					Identifier: &apapproto.RecipeNameEntry_Path{Path: "/bad/recipe.js"},
					LoadError: &apapproto.ErrorChain{
						Root: &apapproto.ErrorNode{
							Error: "parse failed",
							Type:  "*errors.errorString",
							Message: &apapproto.MessageDetails{
								Code:     "engine.recipe.READ_FAILURE",
								Metadata: map[string]string{"path": "/bad/recipe.js"},
							},
						},
					},
				},
			},
		}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRecipesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_recipes",
		})

		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		var content listRecipesResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		require.Len(t, content.Recipes, 1)
		assert.Equal(t, "code_hotspots", content.Recipes[0].Name)
		require.Len(t, content.LoadErrors, 1)
		assert.Equal(t, "/bad/recipe.js", content.LoadErrors[0].Path)
		assert.NotEmpty(t, content.LoadErrors[0].Message)
		assert.Contains(t, content.LoadErrors[0].Explanation, "/bad/recipe.js")
		assert.Equal(t, "engine.recipe.READ_FAILURE", content.LoadErrors[0].MessageCode)
		structured, ok := result.StructuredContent.(map[string]any)
		require.True(t, ok)
		recipes, ok := structured["recipes"].([]any)
		require.True(t, ok)
		require.Len(t, recipes, 1)
		loadErrors, ok := structured["load_errors"].([]any)
		require.True(t, ok)
		require.Len(t, loadErrors, 1)
	})

	t.Run("returns empty recipe array when no recipes load", func(t *testing.T) {
		ctx := context.Background()
		engine := apapprotomocks.NewApapClient(t)
		engine.On("ListRecipes", mock.Anything, &emptypb.Empty{}).Return(&apapproto.RecipeNameListing{}, nil).Once()
		clientSession, serverSession := connectTestServer(t, ctx, ToolDependencies{Engine: engine}, ListRecipesTool{}.Register)
		defer clientSession.Close()
		defer serverSession.Close()

		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_recipes",
		})

		require.NoError(t, err)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)

		var content listRecipesResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &content))
		assert.NotNil(t, content.Recipes)
		assert.Empty(t, content.Recipes)

		structured, ok := result.StructuredContent.(map[string]any)
		require.True(t, ok)
		recipes, ok := structured["recipes"].([]any)
		require.True(t, ok)
		assert.Empty(t, recipes)
	})
}
