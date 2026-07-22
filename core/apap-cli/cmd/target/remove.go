// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

const RemoveUse = "remove [target_name]"

var removeAll bool

var TargetRemoveCmd = newRemoveCommand(engine_target.NewDefaultTargetManager())

func newRemoveCommand(targetService target.TargetManagerService) *cobra.Command {
	removeCmd := &cobra.Command{
		Use:   RemoveUse,
		Short: "Remove a target configuration.",
		Long:  "Removes a named target configuration. Use --all to remove all target configurations.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if removeAll {
				return targetService.RemoveAllTargets()
			}

			if len(args) != 1 {
				return message.New(message.CliCmdTargetRemoveNoTargetSpecified)
			}

			targetName := args[0]

			err := targetService.RemoveTarget(targetName)
			if err != nil {
				return err
			}

			if viper.GetBool("json") {
				return clijson.MarshalJSONCLIResponse(cmd.OutOrStdout(), emptyStruct{})
			}
			return nil
		},
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupTargetSub,
		},
	}

	removeCmd.Flags().BoolVar(&removeAll, "all", false, "Remove all targets from local configuration")
	removeCmd.ValidArgsFunction = completion.CompleteTargetNames

	return removeCmd
}
