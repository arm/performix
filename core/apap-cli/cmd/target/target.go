// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
)

const targetUse = "target"

var RootCmd = NewTargetCommand()

func NewTargetCommand() *cobra.Command {
	targetCmd := &cobra.Command{
		Use:   targetUse,
		Short: "Operations related to target management.",
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupTarget,
		},
	}
	targetCmd.AddCommand(TargetListCmd)
	targetCmd.AddCommand(TargetAddCmd)
	targetCmd.AddCommand(TargetRemoveCmd)
	targetCmd.AddCommand(TargetTestCmd)
	targetCmd.AddCommand(TargetDefaultCmd)
	targetCmd.AddCommand(TargetInfoCmd)
	targetCmd.AddCommand(TargetUpdateCmd)
	targetCmd.AddCommand(TargetPrepareCmd)
	return targetCmd
}
