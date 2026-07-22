// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/mocks"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/utils"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
	apapprotomocks "github.com/Arm-Debug/apap-cli/clients/go/mocks"
)

func TestListRecipe(t *testing.T) {
	t.Run("rejects wrong number of args", func(t *testing.T) {
		clientConnector := mocks.MockAutostartClientConnector{}

		cmd := NewListCommand(&clientConnector)

		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SetArgs([]string{"cpu_microarchitecture"})
		err := cmd.Execute()

		require.Error(t, err)
		assert.Equal(t, "accepts 0 arg(s), received 1", err.Error())
	})

	t.Run("printErrorMessage does nothing on nil error", func(t *testing.T) {
		buf := &bytes.Buffer{}
		printErrorMessage(buf, nil, 2)
		assert.Equal(t, "", buf.String())
	})

	t.Run("successfully lists recipes", func(t *testing.T) {
		var recipes []*apapproto.RecipeNameEntry
		recipes = append(recipes, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Name{Name: "SillyRecipe"}})
		recipes = append(recipes, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Name{Name: "BrilliantRecipe"}})

		protoRecipes := &apapproto.RecipeNameListing{RecipeNames: recipes}
		cl := apapprotomocks.ApapClient{}
		cl.On("ListRecipes", context.Background(), &emptypb.Empty{}).Return(protoRecipes, nil)

		clientConnector := mocks.MockAutostartClientConnector{}
		clientConnector.On("ApapClient", mock.AnythingOfType("grpcserver.GrpcServerConfig")).Return(&cl, nil)

		cmd := NewListCommand(&clientConnector)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		err := cmd.Execute()
		assert.NoError(t, err)
		assert.Equal(t, "BrilliantRecipe\nSillyRecipe\n", cmdBuf.String())
	})

	t.Run("returns error when ListRecipes fails", func(t *testing.T) {
		cl := apapprotomocks.ApapClient{}
		cl.On("ListRecipes", context.Background(), &emptypb.Empty{}).
			Return((*apapproto.RecipeNameListing)(nil), errors.New("list failed"))

		clientConnector := mocks.MockAutostartClientConnector{}
		clientConnector.On("ApapClient", mock.AnythingOfType("grpcserver.GrpcServerConfig")).Return(&cl, nil)

		cmd := NewListCommand(&clientConnector)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		require.Error(t, err)
		assert.ErrorContains(t, err, "list failed")
	})

	t.Run("prints info summary and per-file errors on recipe failures", func(t *testing.T) {
		var recipes []*apapproto.RecipeNameEntry
		recipes = append(recipes,
			&apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Name{Name: "BrilliantRecipe"}},
		)
		perFileMsg1 := message.
			New(message.EngineRecipeparserRecipeReaderParseRecipe).
			WithMetadata(map[string]string{"path": "BadRecipe"})

		perFileMsg2 := message.
			New(message.EngineRecipeparserRecipeReaderReadRecipe).
			WithMetadata(map[string]string{"path": "BadRecipe2"})

		recipes = append(recipes,
			&apapproto.RecipeNameEntry{
				Identifier: &apapproto.RecipeNameEntry_Path{Path: "BadRecipe"},
				LoadError:  message.BuildErrorChain(perFileMsg1),
			},
			&apapproto.RecipeNameEntry{
				Identifier: &apapproto.RecipeNameEntry_Path{Path: "BadRecipe2"},
				LoadError:  message.BuildErrorChain(perFileMsg2),
			},
		)

		protoRecipes := &apapproto.RecipeNameListing{RecipeNames: recipes}
		cl := apapprotomocks.ApapClient{}
		cl.On("ListRecipes", context.Background(), &emptypb.Empty{}).Return(protoRecipes, nil)

		clientConnector := mocks.MockAutostartClientConnector{}
		clientConnector.On("ApapClient", mock.AnythingOfType("grpcserver.GrpcServerConfig")).Return(&cl, nil)

		cmd := NewListCommand(&clientConnector)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		require.NoError(t, err)
		out := cmdBuf.String()
		// Valid recipes returned
		assert.Contains(t, out, "BrilliantRecipe")
		// Failed info message
		assert.Contains(t, out, fmt.Sprintf("[Info]: %v cannot process one or more recipe files:", terminology.GetProductFullName()))
		assert.NotContains(t, out, "cli.cmd.recipe.list.RECIPE_FILES_FAILED")
		// Per-file errors should be indented, mention the bad path and per-file catalog code
		assert.Contains(t, out, "BadRecipe")
		assert.Contains(t, out, "BadRecipe2")
		assert.Contains(t, out, "  [Code]: engine.recipeparser.recipe_reader.PARSE_RECIPE")
		assert.Contains(t, out, "  [Code]: engine.recipeparser.recipe_reader.READ_RECIPE")
		assert.Contains(t, out, "\n  [Error]:")
		assert.Equal(t, 2, strings.Count(out, "\n  [Error]:"))
	})

	t.Run("skips printing per-file error when reconstructed error is nil", func(t *testing.T) {
		recipes := []*apapproto.RecipeNameEntry{
			{Identifier: &apapproto.RecipeNameEntry_Name{Name: "BrilliantRecipe"}},
			{
				Identifier: &apapproto.RecipeNameEntry_Path{Path: "WeirdRecipe"},
				// Non-nil load error, but should reconstruct to nil
				LoadError: &apapproto.ErrorChain{},
			},
		}

		protoRecipes := &apapproto.RecipeNameListing{RecipeNames: recipes}
		cl := apapprotomocks.ApapClient{}
		cl.On("ListRecipes", context.Background(), &emptypb.Empty{}).Return(protoRecipes, nil)

		clientConnector := mocks.MockAutostartClientConnector{}
		clientConnector.On("ApapClient", mock.AnythingOfType("grpcserver.GrpcServerConfig")).Return(&cl, nil)

		cmd := NewListCommand(&clientConnector)
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)

		err := cmd.Execute()
		require.NoError(t, err)

		out := cmdBuf.String()
		assert.Contains(t, out, "BrilliantRecipe")
		// Still print the summary as len(recipeFilesFailed)>0
		assert.Contains(t, out, fmt.Sprintf("[Info]: %v cannot process one or more recipe files:", terminology.GetProductFullName()))
		// Should NOT print an indented per-file [Error] block
		assert.NotContains(t, out, "  [Error]:")
	})

	t.Run("verify --json, outputs valid json", func(t *testing.T) {
		var recipes []*apapproto.RecipeNameEntry
		recipes = append(recipes, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Name{Name: "SillyRecipe"}})
		stringErr := fmt.Errorf("Aah!")
		recipes = append(recipes, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Path{Path: "BadRecipe"}, LoadError: message.BuildErrorChain(stringErr)})
		recipes = append(recipes, &apapproto.RecipeNameEntry{Identifier: &apapproto.RecipeNameEntry_Name{Name: "BrilliantRecipe"}})
		protoRecipes := &apapproto.RecipeNameListing{RecipeNames: recipes}
		cl := apapprotomocks.ApapClient{}
		cl.On("ListRecipes", context.Background(), &emptypb.Empty{}).Return(protoRecipes, nil)

		clientConnector := mocks.MockAutostartClientConnector{}
		clientConnector.On("ApapClient", mock.AnythingOfType("grpcserver.GrpcServerConfig")).Return(&cl, nil)

		cmd := NewListCommand(&clientConnector)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"--json"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		_, err := cmd.ExecuteC()

		require.NoError(t, err)
		utils.IsValidJSON(cmdBuf.String())
		assert.Contains(t, cmdBuf.String(), "BrilliantRecipe")
		assert.Contains(t, cmdBuf.String(), "SillyRecipe")
		assert.Contains(t, cmdBuf.String(), "BadRecipe")
		assert.Contains(t, cmdBuf.String(), "Aah!")
		assert.Less(t, strings.Index(cmdBuf.String(), "BrilliantRecipe"), strings.Index(cmdBuf.String(), "SillyRecipe"))
		assert.Less(t, strings.Index(cmdBuf.String(), "SillyRecipe"), strings.Index(cmdBuf.String(), "BadRecipe"))
	})

	t.Run("verify --json, error outputs valid json", func(t *testing.T) {
		clientConnector := mocks.MockAutostartClientConnector{}
		clientConnector.On("ApapClient", mock.AnythingOfType("grpcserver.GrpcServerConfig")).Return(&apapprotomocks.ApapClient{}, errors.New("rekt"))

		cmd := NewListCommand(&clientConnector)
		utils.SetPersistentFlags(cmd)
		cmd.SetArgs([]string{"--json"})
		cmdBuf := &bytes.Buffer{}
		cmd.SetOut(cmdBuf)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		_, err := cmd.ExecuteC()
		clijson.HandleCLIError(cmdBuf, err)

		require.Error(t, err)
		jsonOut := cmdBuf.String()
		utils.IsValidJSON(jsonOut)
		assert.Contains(t, jsonOut, "rekt")
	})
}
