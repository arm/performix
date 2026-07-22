// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package support

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

var RootCmd = &cobra.Command{
	Use:   "support",
	Short: "Operations related to support packages.",
	Long: fmt.Sprintf(`Create and manage %v support packages containing diagnostic information.
Use these commands to gather logs and run data to share with Arm support.`, terminology.GetProductFullName()),
	Annotations: map[string]string{
		grouping.GroupAnnotation: grouping.GroupSupport,
	},
}

func init() {
	RootCmd.AddCommand(CollectCmd)
}
