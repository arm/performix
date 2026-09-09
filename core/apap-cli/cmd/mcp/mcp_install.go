// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file defines the MCP client installation command and its selection
// rules. The engine remains responsible for client detection and mutation.
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

// newMCPInstallCmd installs the configured server for one named client, or
// for every client that is currently detected and not yet configured.
func newMCPInstallCmd(deps mcpClientDependencies) *cobra.Command {
	shortDescription := fmt.Sprintf(
		"Install the %s MCP server for an AI coding agent.",
		terminology.GetProductFullName(),
	)
	longDescription := fmt.Sprintf(
		`Install the %s Model Context Protocol (MCP) server for an AI coding agent.

Specify a supported agent to configure, or use --all to configure every
available agent.`,
		terminology.GetProductFullName(),
	)
	command := &cobra.Command{
		Use:   "install",
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
			return runMCPInstall(cmd, args, deps)
		},
	}
	command.PersistentFlags().
		Bool("all", false, "Install the MCP server for all available AI coding agents.")
	addMCPClientCommands(command, func(id string) string {
		return "Install the MCP server for " + clientids.DisplayName(id) + "."
	}, func(cmd *cobra.Command, id string) error {
		return runMCPInstall(cmd, []string{id}, deps)
	})
	return command
}

func runMCPInstall(cmd *cobra.Command, args []string, deps mcpClientDependencies) (runErr error) {
	all, _ := cmd.Flags().GetBool("all")
	if err := validateMCPCommandTarget(args, all); err != nil {
		return err
	}
	operation := mcpInstallOperation(deps)
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
			message.CliCmdMcpInstallProgress,
		)
	}

	engineClient, listing, shutdown, err := connectAndListWithProgress(
		cmd,
		deps,
		"",
		message.CliCmdMcpInstallDetectionProgress,
		false,
	)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, shutdown()) }()
	ids := clientIDs(filterClients(listing.Clients, isAvailableForInstall))
	if len(ids) == 0 {
		return writeMCPInfo(cmd, message.CliCmdMcpInstallNoneAvailable)
	}
	return runMCPOperations(
		cmd,
		engineClient,
		ids,
		operation,
		message.CliCmdMcpInstallProgress,
	)
}

// mcpInstallOperation adapts the install-specific protobuf result to the
// common concurrent operation runner.
func mcpInstallOperation(deps mcpClientDependencies) operationFunc {
	return func(
		ctx context.Context,
		client apapproto.ApapClient,
		id string,
	) (operationResponse, error) {
		result, err := deps.service.Install(ctx, client, id)
		if err != nil {
			return operationResponse{}, err
		}
		return operationResponse{
			status:  result.Status,
			outcome: installOperationOutcome(result.Outcome),
		}, nil
	}
}
