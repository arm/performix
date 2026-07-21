// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/serverconfig"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/client"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/mcpserver"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

var RootCmd = newMCPCommand()

func newMCPCommand() *cobra.Command {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: fmt.Sprintf("Set up the %v MCP server.", terminology.GetProductFullName()),
		Long:  fmt.Sprintf("Set up the %v Model Context Protocol (MCP) server, so that it's ready to use with an AI coding agent that supports MCP.", terminology.GetProductFullName()),
		Args:  cobra.NoArgs,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupMCP,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	mcpCmd.AddCommand(newMCPStartCmd(defaultMCPRunner()))
	return mcpCmd
}

func defaultMCPRunner() MCPRunner {
	connector := client.NewAutostartClientWithOutput(io.Discard)
	targets := target.NewDefaultTargetManager()
	return mcpserver.NewMCPServer(
		func() (apapproto.ApapClient, error) {
			return connector.ApapClient(serverconfig.FromViperForBackground())
		},
		targets,
	)
}
