// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
)

var RootCmd = &cobra.Command{
	Use:   "recipe",
	Short: "Operations related to recipe management.",
	Long: `This command enables you to manage recipes and workloads.
For example, you can list existing recipes, run recipes against targets, and
confirm that recipe dependencies are installed on targets.`,
	Annotations: map[string]string{
		grouping.GroupAnnotation: grouping.GroupRecipe,
	},
}

func init() {
	RootCmd.AddCommand(RunCmd)
	RootCmd.AddCommand(ReadyCmd)
	RootCmd.AddCommand(ListCmd)
	RootCmd.AddCommand(InfoCmd)
	RootCmd.AddCommand(ValidateCmd)
}
