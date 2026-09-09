// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file defines the MCP client removal command and its selection rules.
package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-engine/mcpclientinstaller/clientids"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

// newMCPUninstallCmd removes the configured server from one named client, or
// from every detected client where it is currently configured.
func newMCPUninstallCmd(deps mcpClientDependencies) *cobra.Command {
	shortDescription := fmt.Sprintf(
		"Remove the %s MCP server from an AI coding agent.",
		terminology.GetProductFullName(),
	)
	longDescription := fmt.Sprintf(
		`Remove the %s Model Context Protocol (MCP) server from an AI coding agent.

Specify a supported agent, or use --all to remove the MCP server from every
detected agent where it is configured.`,
		terminology.GetProductFullName(),
	)
	command := &cobra.Command{
		Use:   "uninstall",
		Short: shortDescription,
		Long:  longDescription,
		Args:  rejectUnexpectedMCPArguments,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupMCPSub,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			all, _ := cmd.Flags().GetBool("all")
			if len(args) == 0 && !all {
				return cmd.Help()
			}
			return runMCPUninstall(cmd, args, deps)
		},
	}
	command.PersistentFlags().
		Bool(
			"all",
			false,
			"Remove the MCP server from all detected agents where it is configured.",
		)
	addMCPClientCommands(command, func(id string) string {
		return "Remove the MCP server from " + clientids.DisplayName(id) + "."
	}, func(cmd *cobra.Command, id string) error {
		return runMCPUninstall(cmd, []string{id}, deps)
	})
	return command
}

func runMCPUninstall(cmd *cobra.Command, args []string, deps mcpClientDependencies) (runErr error) {
	all, _ := cmd.Flags().GetBool("all")
	if err := validateMCPCommandTarget(args, all); err != nil {
		return err
	}
	operation := mcpUninstallOperation(deps)
	target := explicitMCPClient(args)
	if target != "" {
		engineClient, shutdown, err := deps.connect()
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, shutdown()) }()
		return runMCPOperations(
			cmd,
			engineClient,
			[]string{target},
			operation,
			message.CliCmdMcpUninstallProgress,
		)
	}

	engineClient, listing, shutdown, err := connectAndListWithProgress(
		cmd,
		deps,
		"",
		message.CliCmdMcpUninstallDetectionProgress,
		false,
	)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, shutdown()) }()
	ids := clientIDs(filterClients(listing.Clients, isAvailableForUninstall))
	if len(ids) == 0 {
		return writeMCPInfo(cmd, message.CliCmdMcpUninstallNoneAvailable)
	}
	return runMCPOperations(
		cmd,
		engineClient,
		ids,
		operation,
		message.CliCmdMcpUninstallProgress,
	)
}

// mcpUninstallOperation adapts the uninstall-specific protobuf result to the
// common concurrent operation runner.
func mcpUninstallOperation(deps mcpClientDependencies) operationFunc {
	return func(
		ctx context.Context,
		client apapproto.ApapClient,
		id string,
	) (operationResponse, error) {
		result, err := deps.service.Uninstall(ctx, client, id)
		if err != nil {
			return operationResponse{}, err
		}
		return operationResponse{
			status:  result.Status,
			outcome: uninstallOperationOutcome(result.Outcome),
		}, nil
	}
}
