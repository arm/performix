// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package ssh

import (
	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
)

var RootCmd = &cobra.Command{
	Use:   "ssh",
	Short: "Operations related to SSH key management.",
	Annotations: map[string]string{
		grouping.GroupAnnotation: grouping.GroupSSH,
	},
}

func init() {
	RootCmd.AddCommand(ListKeysCmd)
}
