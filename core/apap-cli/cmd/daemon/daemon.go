// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

var RootCmd = &cobra.Command{
	Use:   "daemon",
	Short: fmt.Sprintf("Manage the %v CLI service as a daemon.", terminology.GetProductFullName()),
	Long: `This command allows you to manage the gRPC server as a daemon,
	running in the background. gRPC clients can then interact with the underlying
	engine in a way that adheres to the standards and idioms of multiple programming
	languages.`,
	Annotations: map[string]string{
		grouping.GroupAnnotation: grouping.GroupDaemon,
	},
}

func init() {
	RootCmd.AddCommand(DaemonStartCmd)
	RootCmd.AddCommand(DaemonStopCmd)
}
