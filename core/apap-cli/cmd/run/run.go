// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
)

var RootCmd = &cobra.Command{
	Use:   "run",
	Short: "Operations related to run management.",
	Long: `This command allows you to manage runs, which store data from previous recipe runs. You can list, rename, delete, export, and import runs.
You can also render runs by specifying a type of renderer.`,
	Annotations: map[string]string{
		grouping.GroupAnnotation: grouping.GroupRun,
	},
}

func init() {
	RootCmd.AddCommand(RenderCmd)
	RootCmd.AddCommand(InvokeRenderCmd)
	RootCmd.AddCommand(PrepareRenderCmd)
	RootCmd.AddCommand(ListCmd)
	RootCmd.AddCommand(InfoCmd)
	RootCmd.AddCommand(RenameCmd)
	RootCmd.AddCommand(DeleteCmd)
	RootCmd.AddCommand(ExportCmd)
	RootCmd.AddCommand(ImportCmd)
	RootCmd.AddCommand(UpdateCmd)
	RootCmd.AddCommand(LogsCmd)
}
