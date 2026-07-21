// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
)

const RenderUse = "render"

var RootCmd = newRenderCommand()

func newRenderCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   RenderUse,
		Short: "Operations related to render data.",
		Long:  "This command enables you to manage render sessions for runs and to query data from them.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupRender,
		},
	}

	cmd.AddCommand(RenderQueryCmd)
	cmd.AddCommand(CloseRenderCmd)
	cmd.AddCommand(RenderListCmd)

	return cmd
}
