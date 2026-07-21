// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/completion"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	engine_target "github.com/Arm-Debug/apap-cli/apap-engine/target"
)

var TargetDefaultCmd = newTargetDefaultTestCmd(&fileDefaultSetter{})

type defaultSetter interface {
	SetDefaultTarget(targetName string) error
}

type fileDefaultSetter struct {
}

func (*fileDefaultSetter) SetDefaultTarget(targetName string) error {
	targetService := engine_target.NewDefaultTargetManager()
	return targetService.SetDefaultTarget(targetName)
}

const DefaultUse = "default [target_name]"

func newTargetDefaultTestCmd(ds defaultSetter) *cobra.Command {

	targetDefaultCommand := &cobra.Command{
		Use:   DefaultUse,
		Short: "Set the default target.",
		Long: `Set the default target, used by default in all commands where a target is
required and an explicit target has not been provided.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := ds.SetDefaultTarget(args[0])
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
	targetDefaultCommand.ValidArgsFunction = completion.CompleteTargetNames

	return targetDefaultCommand
}
