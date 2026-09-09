// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file renders current MCP client configuration. It deliberately keeps
// setup guidance in the doctor command so status stays a concise report.
package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

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

func newMCPStatusCmd(deps mcpClientDependencies) *cobra.Command {
	shortDescription := fmt.Sprintf(
		"Show %s MCP server registration status.",
		terminology.GetProductFullName(),
	)
	longDescription := fmt.Sprintf(
		`Show the %s Model Context Protocol (MCP) server registration status.

Specify an agent to show its configuration and error details.`,
		terminology.GetProductFullName(),
	)
	command := &cobra.Command{
		Use:   "status",
		Short: shortDescription,
		Long:  longDescription,
		Args:  rejectUnexpectedMCPArguments,
		Annotations: map[string]string{
			grouping.GroupAnnotation: grouping.GroupMCPSub,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPStatus(cmd, nil, deps)
		},
	}
	addMCPClientCommands(command, func(id string) string {
		return "Show MCP status for " + clientids.DisplayName(id) + "."
	}, func(cmd *cobra.Command, id string) error {
		return runMCPStatus(cmd, []string{id}, deps)
	})
	return command
}

func runMCPStatus(cmd *cobra.Command, args []string, deps mcpClientDependencies) (runErr error) {
	_, listing, shutdown, err := connectAndListWithProgress(
		cmd,
		deps,
		explicitMCPClient(args),
		message.CliCmdMcpStatusProgress,
		true,
	)
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, shutdown()) }()

	statuses := listing.Clients
	if len(args) == 1 {
		status := findClient(statuses, args[0])
		if status == nil {
			return message.New(message.EngineMcpRegistrationClientUnknown).WithMetadata(
				map[string]string{"client": args[0]},
			)
		}
		if !viper.GetBool("json") {
			return writeMCPClientStatus(cmd, status)
		}
		statuses = []*apapproto.MCPClientStatus{status}
	}
	return writeMCPStatus(cmd, listing.Server, statuses)
}

func writeMCPStatus(
	cmd *cobra.Command,
	server *apapproto.MCPServerLaunchConfiguration,
	statuses []*apapproto.MCPClientStatus,
) error {
	statuses = sortedMCPClients(statuses)
	outputs := make([]mcpClientOutput, 0, len(statuses))
	for _, status := range statuses {
		outputs = append(outputs, clientOutput(status))
	}
	if viper.GetBool("json") {
		return clijson.MarshalJSONCLIResponse(
			cmd.OutOrStdout(),
			mcpClientStatusOutput{Server: server, Clients: outputs},
		)
	}
	detected := make([]mcpClientOutput, 0, len(outputs))
	undetected := make([]mcpClientOutput, 0, len(outputs))
	for _, output := range outputs {
		if output.Detected {
			detected = append(detected, output)
		} else {
			undetected = append(undetected, output)
		}
	}
	var rendered bytes.Buffer
	w := tabwriter.NewWriter(&rendered, 1, 1, 2, ' ', 0)
	fmt.Fprintf(
		w,
		"  %s\t%s MCP\n",
		mcpMessageText(message.CliCmdMcpStatusClientColumn),
		strings.ToUpper(terminology.GetProductFullName()),
	)
	for _, output := range detected {
		fmt.Fprintf(
			w,
			"%s %s\t%s\n",
			registrationMarker(output.RegistrationState),
			output.ID,
			summaryRegistrationState(output),
		)
	}
	if len(undetected) != 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, mcpMessageText(message.CliCmdMcpStatusUndetectedHeading))
		for _, output := range undetected {
			fmt.Fprintf(w, "  %s\n", output.ID)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, mcpMessageText(message.CliCmdMcpStatusSetupHint))
	}
	return writeMCPStatusOutput(cmd.OutOrStdout(), w, &rendered)
}

// writeMCPClientStatus prints the current state for the named client. Client
// discovery and setup instructions belong to mcp doctor.
func writeMCPClientStatus(cmd *cobra.Command, status *apapproto.MCPClientStatus) error {
	diagnosticErr := mcpClientDiagnostic(status)
	output := clientOutputWithDiagnostic(status, diagnosticErr)
	var rendered bytes.Buffer
	w := tabwriter.NewWriter(&rendered, 1, 1, 2, ' ', 0)
	fmt.Fprintln(w, mcpMessageTextWithMetadata(
		message.CliCmdMcpStatusClientField,
		map[string]string{"value": output.Name},
	))
	fmt.Fprintln(w, mcpMessageTextWithMetadata(
		message.CliCmdMcpStatusClientIdField,
		map[string]string{"value": output.ID},
	))
	fmt.Fprintln(w, mcpMessageTextWithMetadata(
		message.CliCmdMcpStatusDetectedField,
		map[string]string{"value": fmt.Sprintf("%t", output.Detected)},
	))
	if output.ExecutablePath != "" {
		fmt.Fprintln(w, mcpMessageTextWithMetadata(
			message.CliCmdMcpStatusExecutableField,
			map[string]string{"path": output.ExecutablePath},
		))
	}
	if output.ConfigurationPath != "" {
		fmt.Fprintln(w, mcpMessageTextWithMetadata(
			message.CliCmdMcpStatusConfigurationField,
			map[string]string{"path": output.ConfigurationPath},
		))
	}
	registrationStatus := mcpMessageTextWithMetadata(
		message.CliCmdMcpStatusRegistrationField,
		map[string]string{
			"marker": registrationMarker(output.RegistrationState),
			"state":  displayRegistrationState(output.RegistrationState),
		},
	)
	fmt.Fprintln(w, registrationStatus)
	if err := w.Flush(); err != nil {
		return err
	}
	if diagnosticErr != nil {
		writeMCPErrorWithDetailsAndIndent(&rendered, diagnosticErr, 2)
		if detail := mcpClientTechnicalDetail(status); detail != "" {
			fmt.Fprintf(&rendered, "  %s\n", mcpMessageText(message.CliCmdCommonDetailsHeading))
			fmt.Fprintf(&rendered, "    %s\n", detail)
		}
	}
	return writeMCPRenderedOutput(cmd.OutOrStdout(), &rendered)
}

// writeMCPStatusOutput applies colour after tabular layout is complete so ANSI
// escape sequences do not affect column widths.
func writeMCPStatusOutput(out io.Writer, table *tabwriter.Writer, rendered *bytes.Buffer) error {
	if err := table.Flush(); err != nil {
		return err
	}
	return writeMCPRenderedOutput(out, rendered)
}

func writeMCPRenderedOutput(out io.Writer, rendered *bytes.Buffer) error {
	_, err := fmt.Fprint(out, colourMCPStatusMarkers(rendered.String()))
	return err
}

func colourMCPStatusMarkers(output string) string {
	output = strings.ReplaceAll(output, "✓", text.FgGreen.Sprint("✓"))
	return strings.ReplaceAll(output, "✗", text.FgRed.Sprint("✗"))
}

func mcpClientTechnicalDetail(status *apapproto.MCPClientStatus) string {
	if status.GetError() == nil || status.GetError().GetRoot() == nil {
		return ""
	}
	detail := errorDetail(status.GetError().GetRoot())
	if status.GetConfigurationPath() != "" && strings.Contains(detail, "JSON") {
		return mcpMessageTextWithMetadata(
			message.CliCmdMcpStatusJsonParseDetail,
			map[string]string{"detail": detail},
		)
	}
	return detail
}
