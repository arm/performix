// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var ListCmd = NewListCommand(client.NewAutostartClient())

type RecipeNameListJSON struct {
	Recipes []RecipeNameEntryJSON `json:"recipes"`
}

type RecipeNameEntryJSON struct {
	Name           *string               `json:"name"`
	Path           *string               `json:"path"`
	LoadErrorChain *clijson.ErrorPayload `json:"load_error"`
}

func printErrorMessage(out io.Writer, err error, indentSpaces int) {
	if err == nil {
		return
	}

	if catalogMsg, lookupErr := clijson.LookupMsg(err); lookupErr == nil && catalogMsg != nil {
		fmt.Fprint(out, catalogMsg.StringWithIndent(indentSpaces))
		return
	}
}

func NewListCommand(clientConn client.ClientConnector) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "list",
		Short: "List all available recipes.",
		Long:  "List all available recipes. For detailed information about an individual recipe, use `recipe info [recipe name]`.",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listRecipes(clientConn, cmd.OutOrStdout())
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRecipeSub,
		},
	}
	return runCmd
}

func listRecipes(clientConn client.ClientConnector, out io.Writer) error {
	cl, err := clientConn.ApapClient(serverconfig.FromViperForBackground())
	if err != nil {
		return err
	}

	protoRecipes, err := cl.ListRecipes(context.Background(), &emptypb.Empty{})
	if err != nil {
		return err
	}

	recipes := protoRecipes.RecipeNames
	slices.SortFunc(recipes, compareRecipeNameEntry)
	if viper.GetBool("json") {
		var recipeJSON RecipeNameListJSON
		for _, recipe := range recipes {
			switch identifier := recipe.Identifier.(type) {
			case *apapproto.RecipeNameEntry_Name:
				recipeJSON.Recipes = append(recipeJSON.Recipes, RecipeNameEntryJSON{Name: &identifier.Name, Path: nil, LoadErrorChain: nil})
			case *apapproto.RecipeNameEntry_Path:
				loadErr := message.ReconstructFromChain(recipe.GetLoadError())
				recipeJSON.Recipes = append(recipeJSON.Recipes, RecipeNameEntryJSON{Path: &identifier.Path, LoadErrorChain: clijson.BuildErrorTree(loadErr)})
			}
		}
		return clijson.MarshalJSONCLIResponse(out, recipeJSON)
	} else {
		var recipeFilesFailed []*apapproto.RecipeNameEntry
		for _, recipe := range recipes {
			switch identifier := recipe.Identifier.(type) {
			case *apapproto.RecipeNameEntry_Name:
				fmt.Fprint(out, identifier.Name+"\n")
			case *apapproto.RecipeNameEntry_Path:
				// Failed to read/parse file or its a duplicate, collect error info
				if recipe.GetLoadError() != nil {
					recipeFilesFailed = append(recipeFilesFailed, recipe)
				}

			}
		}
		if len(recipeFilesFailed) == 0 {
			return nil
		}
		filesFailedInfo := message.New(message.CliCmdRecipeListRecipeFilesFailed)
		printErrorMessage(out, filesFailedInfo, 0)
		// Ensure first per-file error starts on a new line
		fmt.Fprintln(out)

		for i, r := range recipeFilesFailed {
			errorMessage := message.ReconstructFromChain(r.GetLoadError())
			if errorMessage == nil {
				// Shouldn't happen, but don't print non-existent errors
				continue
			}

			printErrorMessage(out, errorMessage, 2)

			// Separate multiple errors with a blank line for readability
			if i < len(recipeFilesFailed)-1 {
				fmt.Fprintln(out)
			}
		}
	}
	return nil
}

func compareRecipeNameEntry(a, b *apapproto.RecipeNameEntry) int {
	aKind, aValue := recipeNameEntrySortKey(a)
	bKind, bValue := recipeNameEntrySortKey(b)
	if aKind != bKind {
		return aKind - bKind
	}
	return strings.Compare(aValue, bValue)
}

func recipeNameEntrySortKey(recipe *apapproto.RecipeNameEntry) (int, string) {
	switch identifier := recipe.GetIdentifier().(type) {
	case *apapproto.RecipeNameEntry_Name:
		return 0, identifier.Name
	case *apapproto.RecipeNameEntry_Path:
		return 1, identifier.Path
	default:
		return 2, ""
	}
}
