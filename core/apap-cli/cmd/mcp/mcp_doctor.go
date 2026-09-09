// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file provides guided MCP client diagnosis. It translates the structured
// status returned by the engine into checks and next steps for a human user.
package mcp

import (
	"errors"
	"fmt"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Arm-Debug/apap-cli/apap-cli/cmd/grouping"
	"github.com/Arm-Debug/apap-cli/apap-cli/service/clijson"
	"github.com/Arm-Debug/apap-cli/apap-engine/mcpclientinstaller/clientids"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/clients/go/apapproto"
)

func newMCPDoctorCmd(deps mcpClientDependencies) *cobra.Command {
	shortDescription := fmt.Sprintf(
		"Check whether an AI coding agent is ready to use the %s MCP server.",
		terminology.GetProductFullName(),
	)
	longDescription := fmt.Sprintf(
		`Check whether an AI coding agent is ready to use the %s Model Context Protocol (MCP) server.

The command checks whether the agent is detected and whether the MCP server is
configured correctly. If a check fails, it explains the problem and suggests a
next step.`,
		terminology.GetProductFullName(),
	)
	command := &cobra.Command{
		Use:   "doctor",
		Short: shortDescription,
		Long:  longDescription,
		Args:  rejectUnexpectedMCPArguments,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupMCPSub,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	addMCPClientCommands(command, func(id string) string {
		return "Check MCP setup for " + clientids.DisplayName(id) + "."
	}, func(cmd *cobra.Command, id string) error {
		return runMCPDoctor(cmd, id, deps)
	})
	return command
}

func runMCPDoctor(cmd *cobra.Command, clientID string, deps mcpClientDependencies) (runErr error) {
	_, listing, shutdown, err := connectAndList(cmd.Context(), deps, clientID)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, shutdown()) }()

	status := findClient(listing.Clients, clientID)
	if status == nil {
		return message.New(message.EngineMcpRegistrationClientUnknown).WithMetadata(
			map[string]string{"client": clientID},
		)
	}
	return writeMCPDoctor(cmd, doctorOutput(status))
}

func doctorOutput(status *apapproto.MCPClientStatus) mcpClientDoctorOutput {
	detection := doctorDetectionCheck(status)
	checks := []mcpClientDoctorCheck{detection}
	if status.Detected {
		checks = append(checks, doctorRegistrationCheck(status))
	}
	return mcpClientDoctorOutput{
		ClientID:   status.ClientId,
		ClientName: status.DisplayName,
		Ready: status.Detected && status.RegistrationState ==
			apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFIGURED,
		Checks: checks,
	}
}

func writeMCPDoctor(cmd *cobra.Command, output mcpClientDoctorOutput) error {
	if viper.GetBool("json") {
		var diagnosisErr error
		if !output.Ready {
			diagnosisErr = message.New(message.CliCmdMcpDoctorFailed).
				WithMetadata(map[string]string{
					"client":   output.ClientName,
					"clientId": output.ClientID,
				})
		}
		if err := clijson.MarshalJSONCLIResponseWithError(
			cmd.OutOrStdout(),
			output,
			diagnosisErr,
		); err != nil {
			return err
		}
		if diagnosisErr != nil {
			return clijson.MarkErrorHandled(diagnosisErr)
		}
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), mcpMessageTextWithMetadata(
		message.CliCmdMcpDoctorHeading,
		map[string]string{"client": output.ClientName},
	))
	fmt.Fprintln(cmd.OutOrStdout())
	for _, check := range output.Checks {
		fmt.Fprintln(cmd.OutOrStdout(), doctorCheckLine(check))
		if check.Diagnostic != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", mcpMessageTextWithMetadata(
				message.CliCmdMcpDoctorErrorContext,
				map[string]string{"message": check.Diagnostic.Message},
			))
			if check.Diagnostic.Explanation != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", mcpMessageTextWithMetadata(
					message.CliCmdMcpDoctorExplanationContext,
					map[string]string{"explanation": check.Diagnostic.Explanation},
				))
			}
		}
		for _, detail := range check.Details {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", detail)
		}
		if len(check.Remediation) != 0 {
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"  %s\n",
				mcpMessageText(message.CliCmdMcpDoctorNextStep),
			)
			for _, remediation := range check.Remediation {
				fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", remediation)
			}
		}
	}
	fmt.Fprintln(cmd.OutOrStdout())
	if output.Ready {
		fmt.Fprintln(cmd.OutOrStdout(), text.FgGreen.Sprint(mcpMessageTextWithMetadata(
			message.CliCmdMcpDoctorReady,
			map[string]string{"client": output.ClientName},
		)))
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), text.FgRed.Sprint(mcpMessageTextWithMetadata(
		message.CliCmdMcpDoctorNotReady,
		map[string]string{"client": output.ClientName},
	)))
	return clijson.ErrorAlreadyHandled
}

func doctorCheckLine(check mcpClientDoctorCheck) string {
	marker := doctorCheckMarker(check.Status)
	if check.Status == "pass" || check.Status == "fail" {
		return marker + " " + text.Bold.Sprint(check.Message)
	}
	return marker + " " + check.Message
}

func doctorDetectionCheck(status *apapproto.MCPClientStatus) mcpClientDoctorCheck {
	check := mcpClientDoctorCheck{ID: "client_detection"}
	if status.Detected {
		check.Status = "pass"
		check.Message = mcpMessageTextWithMetadata(
			message.CliCmdMcpDoctorClientDetected,
			map[string]string{"client": status.DisplayName},
		)
		if path := status.GetExecutablePath(); path != "" {
			check.Details = append(check.Details, mcpMessageTextWithMetadata(
				message.CliCmdMcpDoctorExecutableDetail,
				map[string]string{"path": path},
			))
		}
		if path := status.GetConfigurationPath(); path != "" {
			check.Details = append(check.Details, mcpMessageTextWithMetadata(
				message.CliCmdMcpDoctorConfigurationDetail,
				map[string]string{"path": path},
			))
		}
		return check
	}

	check.Status = "fail"
	check.Message = mcpMessageTextWithMetadata(
		message.CliCmdMcpDoctorClientNotDetected,
		map[string]string{"client": status.DisplayName},
	)
	check.Diagnostic, _ = catalogMessagesFromChain(status.GetError())
	locations := doctorDiscoveryLocations(status)
	if len(locations) != 0 {
		check.Details = append(check.Details, mcpMessageText(message.CliCmdMcpDoctorChecked))
		for _, location := range locations {
			check.Details = append(check.Details, "  "+location)
		}
	}
	check.Remediation = []string{mcpMessageTextWithMetadata(
		message.CliCmdMcpDoctorInstallClient,
		map[string]string{"client": status.DisplayName},
	)}
	return check
}

func doctorRegistrationCheck(status *apapproto.MCPClientStatus) mcpClientDoctorCheck {
	check := mcpClientDoctorCheck{ID: "mcp_registration"}
	switch status.RegistrationState {
	case apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFIGURED:
		check.Status = "pass"
		check.Message = mcpMessageText(message.CliCmdMcpDoctorMcpConfigured)
	case apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_NOT_CONFIGURED:
		check.Status = "fail"
		check.Message = mcpMessageText(message.CliCmdMcpDoctorMcpNotConfigured)
		check.Remediation = []string{fmt.Sprintf("apx mcp install %s", status.ClientId)}
	case apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_CONFLICT:
		check.Status = "fail"
		check.Message = mcpMessageText(message.CliCmdMcpDoctorMcpConflict)
		diagnostic, details := catalogMessagesFromChain(status.GetRegistrationDetail())
		check.Diagnostic = diagnostic
		if len(details) != 0 {
			check.Details = append(
				check.Details,
				mcpMessageText(message.CliCmdCommonDetailsHeading),
			)
			for _, detail := range details {
				check.Details = append(check.Details, "  "+detail.Message)
			}
		}
		check.Remediation = catalogErrorAdvice(check.Diagnostic)
	case apapproto.MCPClientRegistrationState_MCP_CLIENT_REGISTRATION_STATE_UNREADABLE:
		check.Status = "fail"
		check.Message = mcpMessageText(message.CliCmdMcpDoctorMcpUnreadable)
		check.Diagnostic, _ = catalogMessagesFromChain(status.GetError())
		check.Details = append(check.Details, errorChainDetails(status.GetError())...)
		check.Remediation = catalogErrorAdvice(check.Diagnostic)
	default:
		check.Status = "fail"
		check.Message = mcpMessageText(message.CliCmdMcpDoctorMcpUnknown)
		check.Diagnostic, _ = catalogMessagesFromChain(status.GetError())
		check.Details = append(check.Details, errorChainDetails(status.GetError())...)
		check.Remediation = catalogErrorAdvice(check.Diagnostic)
	}
	return check
}

func doctorDiscoveryLocations(status *apapproto.MCPClientStatus) []string {
	locations := make([]string, 0, len(status.DiscoveryCommands)+len(status.DiscoveryPaths))
	for _, command := range status.DiscoveryCommands {
		locations = append(locations, mcpMessageTextWithMetadata(
			message.CliCmdMcpDoctorPathCommand,
			map[string]string{"command": command},
		))
	}
	return append(locations, status.DiscoveryPaths...)
}

func doctorCheckMarker(status string) string {
	switch status {
	case "pass":
		return text.Colors{text.Bold, text.FgGreen}.Sprint("✓")
	case "fail":
		return text.Colors{text.Bold, text.FgRed}.Sprint("✗")
	default:
		return "–"
	}
}
